// Package xiaomi implements the Xiaomi cloud protocol (account login, MiNA,
// conversation polling, MIoT) used by the xiaomi speaker driver. All base URLs
// and the HTTP client are injectable so tests run against httptest servers.
package xiaomi

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Account login states.
const (
	StateLoggedOut  = "logged_out"
	StateLoggingIn  = "logging_in"
	StateNeedVerify = "need_verify"
	StateLoggedIn   = "logged_in"
	StateCooldown   = "cooldown"
	StateError      = "error"
)

const (
	rateLimitCode = 70022
	maxFailures   = 5
	cooldownFor   = 2 * time.Hour
	jsonPrefix    = "&&&START&&&"

	uaLogin = "APP/com.xiaomi.mihome APPV/11.3.203 iosPassportSDK/4.2.50 iOS/26.3.1 MK/aVBob25lMTcsMg== DEVT/aVBob25l DEVS/aU9T BRA/QXBwbGU= L/zh_CN"
	uaOTP   = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 iOS/26.3.1 MiHome/11.3.203 iPhone/iPhone17,2 APP/com.xiaomi.mihome APPV/11.3.203 Platform/iPhone Region/CN"
	uaMiIO  = "iOS-14.4-6.0.103-iPhone12,3--D7744744F7AF32F0544445285880DD63E47D9BE9-8816080-84A3F44E137B71AE-iPhone"
	uaMina  = "MiHome/6.0.103 (com.xiaomi.mihome; build:6.0.103.1; iOS 14.4.0) Alamofire/6.0.103 MICO/iOSApp/appStore/6.0.103"

	SIDMina = "micoapi"
	SIDMiIO = "xiaomiio"
)

// URLs are the service base URLs (override in tests).
type URLs struct {
	Account string // https://account.xiaomi.com
	Mina    string // https://api2.mina.mi.com
	Profile string // https://userprofile.mina.mi.com
	MIIO    string // https://api.io.mi.com/app
}

func (u URLs) withDefaults() URLs {
	d := func(s *string, v string) {
		if *s == "" {
			*s = v
		}
		*s = strings.TrimRight(*s, "/")
	}
	d(&u.Account, "https://account.xiaomi.com")
	d(&u.Mina, "https://api2.mina.mi.com")
	d(&u.Profile, "https://userprofile.mina.mi.com")
	d(&u.MIIO, "https://api.io.mi.com/app")
	return u
}

// Options configure an Account.
type Options struct {
	DataDir string // token file: <DataDir>/mi_token.json
	HTTP    *http.Client
	URLs    URLs
	Log     *slog.Logger
	Now     func() time.Time
}

// Status is the account state served to the UI.
type Status struct {
	State           string     `json:"state"`
	UserID          string     `json:"user_id,omitempty"`
	Message         string     `json:"message,omitempty"`
	CooldownUntil   *time.Time `json:"cooldown_until,omitempty"`
	VerifyMethods   []string   `json:"verify_methods,omitempty"`
	NotificationURL string     `json:"notification_url,omitempty"`
	Method          string     `json:"method,omitempty"` // password | token (when logged in)
	Hint            string     `json:"hint,omitempty"`   // suggestion shown with errors
}

const tokenHint = "小米账号密码登录常因验证码/风控失败；可改用浏览器 passToken 登录（account.xiaomi.com 登录后从 Cookie 复制 passToken 与 userId）"

// ErrNotLoggedIn is returned when no valid Xiaomi session exists.
var ErrNotLoggedIn = errors.New("xiaomi: not logged in")

// tokenFile is the persisted form of mi_token.json (no password).
type tokenFile struct {
	DeviceID      string    `json:"deviceId"`
	UserID        string    `json:"userId,omitempty"`
	PassToken     string    `json:"passToken,omitempty"`
	Micoapi       []string  `json:"micoapi,omitempty"`  // [ssecurity, serviceToken]
	Xiaomiio      []string  `json:"xiaomiio,omitempty"` // [ssecurity, serviceToken]
	Method        string    `json:"method,omitempty"`
	Failures      int       `json:"failures,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
}

type otpSession struct {
	sid     string
	ntfURL  string
	context string
	flags   []int
	method  string // last method a code was sent by
	cookies map[string]string
}

// Account manages login, OTP verification, silent relogin and the token file.
type Account struct {
	opt  Options
	urls URLs
	hc   *http.Client
	log  *slog.Logger

	mu  sync.Mutex
	tok tokenFile
	st  Status
	otp *otpSession
}

// NewAccount creates an account and restores <DataDir>/mi_token.json.
func NewAccount(o Options) *Account {
	a := &Account{opt: o, urls: o.URLs.withDefaults(), log: o.Log}
	if a.log == nil {
		a.log = slog.Default()
	}
	if o.Now == nil {
		a.opt.Now = time.Now
	}
	base := o.HTTP
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	c := *base
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	a.hc = &c
	a.st = Status{State: StateLoggedOut}
	a.load()
	return a
}

func (a *Account) path() string { return filepath.Join(a.opt.DataDir, "mi_token.json") }

func (a *Account) load() {
	b, err := os.ReadFile(a.path())
	if err != nil {
		return
	}
	var t tokenFile
	if json.Unmarshal(b, &t) != nil {
		return
	}
	a.tok = t
	switch {
	case len(t.Micoapi) == 2 && t.Micoapi[1] != "":
		a.st = Status{State: StateLoggedIn, UserID: t.UserID}
	case t.PassToken != "":
		a.st = Status{State: StateLoggedOut, UserID: t.UserID, Message: "session expired, log in again"}
	}
	if a.opt.Now().Before(t.CooldownUntil) {
		a.setCooldown(t.CooldownUntil)
	}
}

func (a *Account) save() {
	if a.tok.DeviceID == "" {
		return
	}
	b, _ := json.MarshalIndent(a.tok, "", "  ")
	if err := os.MkdirAll(a.opt.DataDir, 0o700); err != nil {
		a.log.Warn("xiaomi: token dir", "err", err)
		return
	}
	tmp := a.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		a.log.Warn("xiaomi: save token", "err", err)
		return
	}
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, a.path()); err != nil {
		a.log.Warn("xiaomi: save token", "err", err)
	}
}

func (a *Account) setCooldown(until time.Time) {
	a.st = Status{State: StateCooldown, UserID: a.st.UserID, CooldownUntil: &until,
		Message: "too many login attempts (rate limited); automatic retries are paused"}
}

// Status returns the current account state.
func (a *Account) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.State == StateCooldown && a.st.CooldownUntil != nil && !a.opt.Now().Before(*a.st.CooldownUntil) {
		a.st = Status{State: StateLoggedOut, Message: "cooldown finished, log in again"}
		a.tok.Failures, a.tok.CooldownUntil = 0, time.Time{}
		a.save()
	}
	s := a.st
	switch s.State {
	case StateLoggedIn:
		s.Method = a.tok.Method
	case StateError, StateNeedVerify, StateCooldown:
		s.Hint = tokenHint
	}
	s.VerifyMethods = append([]string(nil), a.st.VerifyMethods...)
	return s
}

// LoggedIn reports whether a micoapi session exists.
func (a *Account) LoggedIn() bool { return a.Status().State == StateLoggedIn }

// Creds is the data needed to call a service.
type Creds struct{ UserID, SSecurity, ServiceToken, DeviceID string }

// Creds returns the credentials for sid (micoapi | xiaomiio). For xiaomiio a
// silent login with the passToken is performed when needed.
func (a *Account) Creds(ctx context.Context, sid string) (Creds, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.st.State != StateLoggedIn {
		return Creds{}, ErrNotLoggedIn
	}
	if c, ok := a.credsLocked(sid); ok {
		return c, nil
	}
	if err := a.silentLocked(ctx, sid); err != nil {
		return Creds{}, err
	}
	c, _ := a.credsLocked(sid)
	return c, nil
}

func (a *Account) credsLocked(sid string) (Creds, bool) {
	pair := a.tok.Micoapi
	if sid == SIDMiIO {
		pair = a.tok.Xiaomiio
	}
	if len(pair) != 2 || pair[1] == "" {
		return Creds{}, false
	}
	return Creds{UserID: a.tok.UserID, SSecurity: pair[0], ServiceToken: pair[1], DeviceID: a.tok.DeviceID}, true
}

// Refresh re-logs in silently with the passToken (called on 401). On failure
// the account becomes logged_out.
func (a *Account) Refresh(ctx context.Context, sid string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.silentLocked(ctx, sid)
}

func (a *Account) silentLocked(ctx context.Context, sid string) error {
	if a.tok.PassToken == "" {
		a.expireLocked("no passToken stored, log in again")
		return ErrNotLoggedIn
	}
	if until := a.cooldownLocked(); until != nil {
		return errors.New("xiaomi: login rate limited")
	}
	resp, err := a.serviceLogin(ctx, sid, nil)
	if err == nil {
		if err = a.checkRate(resp); err == nil && intOf(resp["code"]) != 0 {
			err = errors.New("passToken rejected by Xiaomi")
		}
	}
	if err == nil {
		err = a.finishLocked(ctx, sid, resp)
	}
	if err != nil {
		a.log.Warn("xiaomi: silent relogin failed", "sid", sid, "err", err)
		if a.st.State != StateCooldown && sid == SIDMina {
			a.expireLocked("session expired and silent relogin failed: " + err.Error())
		}
		return fmt.Errorf("%w: %v", ErrNotLoggedIn, err)
	}
	return nil
}

func (a *Account) expireLocked(msg string) {
	a.tok.Micoapi, a.tok.Xiaomiio, a.tok.PassToken = nil, nil, ""
	a.save()
	a.st = Status{State: StateLoggedOut, UserID: a.st.UserID, Message: msg}
}

func (a *Account) cooldownLocked() *time.Time {
	if a.tok.Failures >= maxFailures && a.opt.Now().Before(a.tok.CooldownUntil) {
		t := a.tok.CooldownUntil
		a.setCooldown(t)
		return &t
	}
	return nil
}

// Logout forgets the session and deletes the token file.
func (a *Account) Logout() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tok, a.otp = tokenFile{}, nil
	_ = os.Remove(a.path())
	a.st = Status{State: StateLoggedOut}
}

// ---- login ----

// Login performs the password login. The password is never stored. The
// resulting state is available via Status (logged_in / need_verify / error /
// cooldown); the returned error is the failure message, if any.
func (a *Account) Login(ctx context.Context, username, password string) (err error) {
	defer func() { a.logOutcome(ctx, "password", "login", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	if username == "" || password == "" {
		return errors.New("username and password are required")
	}
	if until := a.cooldownLocked(); until != nil {
		return fmt.Errorf("login is rate limited until %s", until.Format(time.RFC3339))
	}
	if a.tok.DeviceID == "" {
		a.tok.DeviceID = strings.ToUpper(randString(16))
	}
	a.otp = nil
	a.tok.Method = "password"
	a.st = Status{State: StateLoggingIn}
	err = a.passwordLogin(ctx, SIDMina, username, password)
	if err != nil && a.st.State == StateLoggingIn {
		a.st = Status{State: StateError, Message: err.Error()}
	}
	return err
}

// parseCookieFields extracts name=value pairs from a Cookie-header style string.
func parseCookieFields(s string) map[string]string {
	m := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return m
}

// LoginWithToken logs in with a passToken copied from a browser session at
// account.xiaomi.com (workaround for captcha/risk-control failures). passToken
// may be the bare value, "passToken=...", or a whole Cookie header string
// (which may also carry userId and deviceId).
func (a *Account) LoginWithToken(ctx context.Context, userID, passToken, deviceID string) (err error) {
	defer func() { a.logOutcome(ctx, "token", "login", err) }()
	userID, passToken, deviceID = strings.TrimSpace(userID), strings.TrimSpace(passToken), strings.TrimSpace(deviceID)
	if strings.Contains(passToken, ";") || strings.HasPrefix(passToken, "passToken=") || strings.Contains(passToken, "userId=") {
		f := parseCookieFields(passToken)
		passToken = f["passToken"]
		if userID == "" {
			userID = f["userId"]
		}
		if deviceID == "" {
			deviceID = f["deviceId"]
		}
	}
	userID = strings.TrimPrefix(userID, "userId=")
	if passToken == "" {
		return errors.New("passToken is required (请填写 passToken)")
	}
	if userID == "" {
		return errors.New("userId is required (请填写 userId)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if until := a.cooldownLocked(); until != nil {
		return fmt.Errorf("login is rate limited until %s", until.Format(time.RFC3339))
	}
	if deviceID != "" {
		a.tok.DeviceID = deviceID
	} else if a.tok.DeviceID == "" {
		a.tok.DeviceID = strings.ToUpper(randString(16))
	}
	a.otp = nil
	a.tok.UserID, a.tok.PassToken, a.tok.Method = userID, passToken, "token"
	a.tok.Micoapi, a.tok.Xiaomiio = nil, nil
	a.st = Status{State: StateLoggingIn, UserID: userID}
	err = a.silentLocked(ctx, SIDMina)
	if err == nil {
		err = a.silentLocked(ctx, SIDMiIO)
	}
	if err != nil {
		if a.st.State == StateCooldown {
			return err
		}
		a.expireLocked("passToken 无效或已过期")
		a.st.State = StateError
		a.st.UserID = ""
		return fmt.Errorf("passToken 无效或已过期 (passToken invalid or expired): %v", err)
	}
	a.save()
	return nil
}

func (a *Account) passwordLogin(ctx context.Context, sid, user, pass string) error {
	resp, err := a.serviceLogin(ctx, sid, nil)
	if err != nil {
		return err
	}
	if err := a.checkRate(resp); err != nil {
		return err
	}
	if intOf(resp["code"]) != 0 {
		sum := md5.Sum([]byte(pass))
		form := url.Values{
			"_json": {"true"}, "qs": {str(resp["qs"])}, "sid": {str(resp["sid"])},
			"_sign": {str(resp["_sign"])}, "callback": {str(resp["callback"])},
			"user": {user}, "hash": {strings.ToUpper(hex.EncodeToString(sum[:]))},
		}
		resp, err = a.serviceLogin(ctx, "", form)
		if err != nil {
			return err
		}
		if err := a.checkRate(resp); err != nil {
			return err
		}
		if intOf(resp["code"]) != 0 {
			return fmt.Errorf("login failed: %s", firstNonEmpty(str(resp["desc"]), str(resp["description"]), str(resp["tips"]), "code "+strconv.Itoa(intOf(resp["code"]))))
		}
		if ntf := str(resp["notificationUrl"]); ntf != "" {
			return a.startOTP(ctx, sid, ntf)
		}
	}
	a.tok.UserID = userIDOf(resp)
	return a.finishLocked(ctx, sid, resp)
}

func (a *Account) checkRate(resp map[string]any) error {
	if intOf(resp["code"]) != rateLimitCode {
		return nil
	}
	a.tok.Failures++
	if a.tok.Failures >= maxFailures {
		a.tok.CooldownUntil = a.opt.Now().Add(cooldownFor)
		a.setCooldown(a.tok.CooldownUntil)
	}
	a.save()
	return fmt.Errorf("rate limited by Xiaomi (70022): %s", str(resp["tips"]))
}

// finishLocked turns a successful serviceLogin response into a serviceToken.
func (a *Account) finishLocked(ctx context.Context, sid string, resp map[string]any) error {
	for _, k := range []string{"userId", "passToken", "location", "nonce", "ssecurity"} {
		if _, ok := resp[k]; !ok {
			return fmt.Errorf("login response missing %q", k)
		}
	}
	a.tok.UserID, a.tok.PassToken = userIDOf(resp), str(resp["passToken"])
	ssec := str(resp["ssecurity"])
	sum := sha1.Sum([]byte("nonce=" + str(resp["nonce"]) + "&" + ssec))
	sign := base64.StdEncoding.EncodeToString(sum[:])
	r, err := a.do(ctx, http.MethodGet, str(resp["location"])+"&_userIdNeedEncrypt=true&clientSign="+url.QueryEscape(sign), nil, nil, nil)
	if err != nil {
		return err
	}
	svc := r.cookies["serviceToken"]
	if svc == "" {
		return fmt.Errorf("no serviceToken in response (HTTP %d): %s", r.status, trunc(string(r.body), 200))
	}
	pair := []string{ssec, svc}
	if sid == SIDMiIO {
		a.tok.Xiaomiio = pair
	} else {
		a.tok.Micoapi = pair
		a.st = Status{State: StateLoggedIn, UserID: a.tok.UserID}
		a.tok.Failures, a.tok.CooldownUntil = 0, time.Time{}
		a.otp = nil
	}
	a.save()
	return nil
}

func (a *Account) serviceLogin(ctx context.Context, sid string, form url.Values) (map[string]any, error) {
	cookies := map[string]string{"sdkVersion": "3.9", "deviceId": a.tok.DeviceID}
	if a.tok.PassToken != "" {
		cookies["userId"], cookies["passToken"] = a.tok.UserID, a.tok.PassToken
	}
	if a.otp != nil {
		for k, v := range a.otp.cookies {
			cookies[k] = v
		}
	}
	hdr := map[string]string{"User-Agent": uaLogin}
	var r *result
	var err error
	if form == nil {
		r, err = a.do(ctx, http.MethodGet, a.urls.Account+"/pass/serviceLogin?sid="+url.QueryEscape(sid)+"&_json=true&_locale=zh_CN", nil, cookies, hdr)
	} else {
		r, err = a.do(ctx, http.MethodPost, a.urls.Account+"/pass/serviceLoginAuth2", form, cookies, hdr)
	}
	if err != nil {
		return nil, err
	}
	return parseResp(r.body)
}

// ---- OTP ----

func (a *Account) startOTP(ctx context.Context, sid, ntf string) error {
	ntf = a.abs(ntf)
	s := &otpSession{sid: sid, ntfURL: ntf, cookies: map[string]string{"deviceId": a.tok.DeviceID}}
	hdr := map[string]string{"User-Agent": uaOTP}
	r, err := a.do(ctx, http.MethodGet, ntf, nil, s.cookies, hdr)
	if err != nil {
		return err
	}
	s.merge(r)
	if u, err := url.Parse(ntf); err == nil {
		s.context = u.Query().Get("context")
	}
	listURL := a.urls.Account + "/identity/list?sid=" + url.QueryEscape(sid) + "&supportedMask=0&_locale=zh_CN&context=" + url.QueryEscape(s.context)
	r, err = a.do(ctx, http.MethodGet, listURL, nil, s.cookies, hdr)
	if err != nil {
		return err
	}
	s.merge(r)
	if lr, err := parseResp(r.body); err == nil {
		if opts, ok := lr["options"].([]any); ok {
			for _, o := range opts {
				s.flags = append(s.flags, intOf(o))
			}
		}
		if len(s.flags) == 0 {
			if f := intOf(lr["flag"]); f != 0 {
				s.flags = []int{f}
			}
		}
	}
	if len(s.flags) == 0 {
		s.flags = []int{4}
	}
	a.otp = s
	a.st = Status{State: StateNeedVerify, Message: "Xiaomi requires a security verification", NotificationURL: ntf, VerifyMethods: s.methods()}
	return nil
}

func (s *otpSession) merge(r *result) {
	for k, v := range r.cookies {
		s.cookies[k] = v
	}
}

func (s *otpSession) methods() []string {
	var m []string
	for _, f := range s.flags {
		switch f {
		case 4:
			m = append(m, "phone")
		case 8:
			m = append(m, "email")
		}
	}
	return m
}

func (s *otpSession) flagFor(method string) (int, string) {
	if method == "email" {
		return 8, "Email"
	}
	return 4, "Phone"
}

// SendCode asks Xiaomi to send the verification code ("phone" | "email").
func (a *Account) SendCode(ctx context.Context, method string) (err error) {
	defer func() { a.logOutcome(ctx, a.methodName(), "send_code", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.otp
	if s == nil || a.st.State != StateNeedVerify {
		return errors.New("no verification in progress")
	}
	if method != "phone" && method != "email" {
		return errors.New("method must be phone or email")
	}
	ok := false
	for _, m := range s.methods() {
		ok = ok || m == method
	}
	if !ok {
		return fmt.Errorf("verification by %s is not available for this account", method)
	}
	flag, name := s.flagFor(method)
	s.method = method
	hdr := map[string]string{"User-Agent": uaOTP}
	r, err := a.do(ctx, http.MethodGet, fmt.Sprintf("%s/identity/auth/verify%s?_flag=%d&_json=true", a.urls.Account, name, flag), nil, s.cookies, hdr)
	if err != nil {
		return err
	}
	s.merge(r)
	if resp, err := parseResp(r.body); err == nil && code0(resp) != 0 {
		return fmt.Errorf("trigger %s verification failed: %s", method, trunc(string(r.body), 200))
	}
	r, err = a.do(ctx, http.MethodPost, fmt.Sprintf("%s/identity/auth/send%sTicket", a.urls.Account, name),
		url.Values{"retry": {"0"}, "icode": {""}, "_json": {"true"}}, s.cookies, hdr)
	if err != nil {
		return err
	}
	s.merge(r)
	if resp, err := parseResp(r.body); err == nil && code0(resp) != 0 {
		return fmt.Errorf("sending the %s code failed: %s", method, firstNonEmpty(str(resp["desc"]), str(resp["tips"]), trunc(string(r.body), 200)))
	}
	return nil
}

// Verify submits the verification code and completes the login.
func (a *Account) Verify(ctx context.Context, code string) (err error) {
	defer func() { a.logOutcome(ctx, a.methodName(), "verify", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.otp
	if s == nil || a.st.State != StateNeedVerify {
		return errors.New("no verification in progress")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("code is required")
	}
	method := s.method
	if method == "" {
		method = s.methods()[0]
	}
	flag, name := s.flagFor(method)
	hdr := map[string]string{"User-Agent": uaOTP}
	r, err := a.do(ctx, http.MethodPost, fmt.Sprintf("%s/identity/auth/verify%s?_dc=%d", a.urls.Account, name, a.opt.Now().UnixMilli()),
		url.Values{"_flag": {strconv.Itoa(flag)}, "ticket": {code}, "trust": {"true"}, "_json": {"true"}}, s.cookies, hdr)
	if err != nil {
		return err
	}
	s.merge(r)
	resp, err := parseResp(r.body)
	if err != nil {
		return err
	}
	loc := str(resp["location"])
	if loc == "" {
		return fmt.Errorf("verification failed: %s", firstNonEmpty(str(resp["desc"]), str(resp["tips"]), trunc(string(r.body), 200)))
	}
	if err := a.followResume(ctx, s, a.abs(loc)); err != nil {
		return err
	}
	return a.resumeLocked(ctx, s)
}

// Retry redoes serviceLogin after the user verified in a browser.
func (a *Account) Retry(ctx context.Context) (err error) {
	defer func() { a.logOutcome(ctx, a.methodName(), "retry", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.otp
	if s == nil || a.st.State != StateNeedVerify {
		return errors.New("no verification in progress")
	}
	return a.resumeLocked(ctx, s)
}

// followResume follows the post-verification location (and confirmUrl).
func (a *Account) followResume(ctx context.Context, s *otpSession, loc string) error {
	hdr := map[string]string{"User-Agent": uaOTP}
	for i := 0; i < 5 && loc != ""; i++ {
		r, err := a.do(ctx, http.MethodGet, loc, nil, s.cookies, hdr)
		if err != nil {
			return err
		}
		s.merge(r)
		if u, err := url.Parse(loc); err == nil {
			if c := u.Query().Get("confirmUrl"); c != "" {
				r, err := a.do(ctx, http.MethodGet, a.abs(c), nil, s.cookies, hdr)
				if err != nil {
					return err
				}
				s.merge(r)
				return nil
			}
		}
		next := r.header.Get("Location")
		if next == "" {
			return nil
		}
		nu, err := url.Parse(loc)
		if err != nil {
			return nil
		}
		ref, err := url.Parse(next)
		if err != nil {
			return nil
		}
		loc = nu.ResolveReference(ref).String()
	}
	return nil
}

func (a *Account) resumeLocked(ctx context.Context, s *otpSession) error {
	resp, err := a.serviceLogin(ctx, s.sid, nil)
	if err != nil {
		return err
	}
	if intOf(resp["code"]) != 0 {
		return errors.New("verification not completed yet: Xiaomi still asks for verification")
	}
	if err := a.finishLocked(ctx, s.sid, resp); err != nil {
		a.st = Status{State: StateError, Message: err.Error()}
		return err
	}
	return nil
}

// ---- http helpers ----

type result struct {
	status  int
	body    []byte
	header  http.Header
	cookies map[string]string
}

func (a *Account) abs(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	return a.urls.Account + u
}

func (a *Account) do(ctx context.Context, method, u string, form url.Values, cookies, headers map[string]string) (*result, error) {
	return doReq(ctx, a.hc, method, u, form, cookies, headers)
}

func doReq(ctx context.Context, hc *http.Client, method, u string, form url.Values, cookies, headers map[string]string) (*result, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if len(cookies) > 0 {
		var parts []string
		for k, v := range cookies {
			parts = append(parts, k+"="+v)
		}
		req.Header.Set("Cookie", strings.Join(parts, "; "))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	r := &result{status: resp.StatusCode, body: b, header: resp.Header, cookies: map[string]string{}}
	for _, c := range resp.Cookies() {
		r.cookies[c.Name] = c.Value
	}
	return r, nil
}

func parseResp(b []byte) (map[string]any, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), jsonPrefix))
	var m map[string]any
	if s == "" {
		return map[string]any{}, nil
	}
	// UseNumber: nonce/userId are 64-bit integers; float64 would corrupt them
	// (a wrong nonce breaks clientSign and STS answers 401).
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", trunc(s, 200))
	}
	return m, nil
}

func code0(m map[string]any) int {
	if _, ok := m["code"]; !ok {
		return 0
	}
	return intOf(m["code"])
}

func intOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func userIDOf(m map[string]any) string { return str(m["userId"]) }

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alnum))))
		b[i] = alnum[x.Int64()]
	}
	return string(b)
}

func maskID(id string) string {
	if len(id) <= 4 {
		return "****"
	}
	return "****" + id[len(id)-4:]
}

func (a *Account) methodName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tok.Method == "" {
		return "password"
	}
	return a.tok.Method
}

// logOutcome records a login/verify outcome. It never logs secrets.
func (a *Account) logOutcome(ctx context.Context, method, stage string, err error) {
	if a.log == nil {
		return
	}
	a.mu.Lock()
	st, uid, until := a.st, a.tok.UserID, a.tok.CooldownUntil
	fails := a.tok.Failures
	a.mu.Unlock()
	if err != nil {
		attrs := []any{"method", method, "stage", stage, "state", string(st.State), "err", err.Error(), "failures", fails}
		if !until.IsZero() && a.opt.Now().Before(until) {
			attrs = append(attrs, "cooldown_until", until.Format(time.RFC3339))
		}
		a.log.WarnContext(ctx, "xiaomi: "+stage+" failed", attrs...)
		return
	}
	if st.State == StateLoggedIn {
		a.log.InfoContext(ctx, "xiaomi: login succeeded", "method", method, "stage", stage, "user_id", maskID(uid))
	} else {
		a.log.InfoContext(ctx, "xiaomi: "+stage+" ok", "method", method, "stage", stage, "state", string(st.State))
	}
}
