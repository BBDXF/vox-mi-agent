// Package mediaurl signs and verifies the temporary media URLs handed to the
// speaker (PROJECT_DESIGN.md §5.4) and manages the persistent server secret.
package mediaurl

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SecretPath is the file holding the server secret.
func SecretPath(dataDir string) string { return filepath.Join(dataDir, "secret") }

// LoadSecret reads <data>/secret, generating 32 random bytes (mode 0600) on
// first use.
func LoadSecret(dataDir string) ([]byte, error) {
	p := SecretPath(dataDir)
	if b, err := os.ReadFile(p); err == nil && len(b) >= 32 {
		return b, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return nil, err
	}
	return b, nil
}

// Sig returns the base64url signature (16 bytes of HMAC-SHA256).
func Sig(secret []byte, id, ext string, exp int64) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(id + "." + ext + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

// Sign builds {base}/audio/{id}.{ext}?exp=..&sig=..
func Sign(secret []byte, base, id, ext string, exp time.Time) string {
	e := exp.Unix()
	return fmt.Sprintf("%s/audio/%s.%s?exp=%d&sig=%s", strings.TrimRight(base, "/"),
		url.PathEscape(id), ext, e, Sig(secret, id, ext, e))
}

// Verify checks signature and expiry.
func Verify(secret []byte, id, ext string, exp int64, sig string, now time.Time) error {
	want := Sig(secret, id, ext, exp)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return errors.New("bad signature")
	}
	if now.Unix() > exp {
		return errors.New("expired")
	}
	return nil
}

// ValidateBase checks a public base URL: empty, or http(s)://host[:port]
// without trailing slash.
func ValidateBase(s string) error {
	if s == "" {
		return nil
	}
	if strings.HasSuffix(s, "/") {
		return errors.New("must not end with /")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("must be an http:// or https:// URL")
	}
	return nil
}

// AutoBase returns http://<first non-loopback IPv4>:port.
func AutoBase(port string) (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() {
			if ip4 := n.IP.To4(); ip4 != nil {
				return "http://" + net.JoinHostPort(ip4.String(), port), nil
			}
		}
	}
	return "", errors.New("no non-loopback IPv4 address found")
}
