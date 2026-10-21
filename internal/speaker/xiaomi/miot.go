package xiaomi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// signNonce = b64(sha256(b64dec(ssecurity) + b64dec(nonce))).
func signNonce(ssecurity, nonce string) (string, error) {
	sec, err := base64.StdEncoding.DecodeString(ssecurity)
	if err != nil {
		return "", err
	}
	n, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(sec)
	h.Write(n)
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// SignData signs a miio request (Yonsm MiIOService.sign_data). now supplies
// the clock (minutes are embedded in the nonce); rnd the 8 random bytes.
func SignData(uri, data, ssecurity string, now time.Time, rnd []byte) (url.Values, error) {
	nb := make([]byte, 12)
	copy(nb, rnd)
	binary.BigEndian.PutUint32(nb[8:], uint32(now.Unix()/60))
	nonce := base64.StdEncoding.EncodeToString(nb)
	snonce, err := signNonce(ssecurity, nonce)
	if err != nil {
		return nil, err
	}
	key, _ := base64.StdEncoding.DecodeString(snonce)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(strings.Join([]string{uri, snonce, nonce, "data=" + data}, "&")))
	return url.Values{"_nonce": {nonce}, "data": {data}, "signature": {base64.StdEncoding.EncodeToString(mac.Sum(nil))}}, nil
}

// MiotAction calls miotspec/action with the text as the only input.
func (m *Mina) MiotAction(ctx context.Context, miotDID string, siid, aiid int, in []any) error {
	uri := "/miotspec/action"
	payload, _ := json.Marshal(map[string]any{"params": map[string]any{"did": miotDID, "siid": siid, "aiid": aiid, "in": in}})
	r, err := m.call(ctx, SIDMiIO, http.MethodPost, m.acct.urls.MIIO+uri, nil, func(c Creds) (map[string]string, map[string]string, url.Values) {
		rnd := make([]byte, 8)
		_, _ = rand.Read(rnd)
		form, err := SignData(uri, string(payload), c.SSecurity, time.Now(), rnd)
		if err != nil {
			return nil, nil, url.Values{}
		}
		return map[string]string{"PassportDeviceId": c.DeviceID},
			map[string]string{"User-Agent": uaMiIO, "x-xiaomi-protocal-flag-cli": "PROTOCAL-HTTP2"}, form
	})
	if err != nil {
		return err
	}
	var resp struct {
		Code    int `json:"code"`
		Message string
		Result  *struct {
			Code int `json:"code"`
		} `json:"result"`
	}
	if err := json.Unmarshal(r.body, &resp); err != nil {
		return fmt.Errorf("xiaomi: miot action: %w", err)
	}
	if resp.Code != 0 || resp.Result == nil {
		return fmt.Errorf("xiaomi: miot action failed: %s", trunc(string(r.body), 200))
	}
	if resp.Result.Code != 0 {
		return fmt.Errorf("xiaomi: miot action result code %d", resp.Result.Code)
	}
	return nil
}

// ParseIID splits "5-1" into (5, 1).
func ParseIID(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("invalid siid-aiid %q", s)
	}
	x, e1 := strconv.Atoi(a)
	y, e2 := strconv.Atoi(b)
	if e1 != nil || e2 != nil {
		return 0, 0, fmt.Errorf("invalid siid-aiid %q", s)
	}
	return x, y, nil
}
