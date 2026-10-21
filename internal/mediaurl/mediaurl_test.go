package mediaurl

import (
	"bytes"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	sec := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Now().Add(time.Hour)
	u, err := url.Parse(Sign(sec, "http://h:1", "abc", "mp3", exp))
	if err != nil || u.Path != "/audio/abc.mp3" || u.Host != "h:1" {
		t.Fatalf("%v %v", u, err)
	}
	e, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	sig := u.Query().Get("sig")
	if err := Verify(sec, "abc", "mp3", e, sig, time.Now()); err != nil {
		t.Fatal(err)
	}
	if Verify(sec, "abd", "mp3", e, sig, time.Now()) == nil ||
		Verify(sec, "abc", "flac", e, sig, time.Now()) == nil ||
		Verify(sec, "abc", "mp3", e+1, sig, time.Now()) == nil ||
		Verify([]byte("other"), "abc", "mp3", e, sig, time.Now()) == nil {
		t.Fatal("tamper accepted")
	}
	if Verify(sec, "abc", "mp3", e, sig, exp.Add(time.Second)) == nil {
		t.Fatal("expired accepted")
	}
}

func TestSecretPersistence(t *testing.T) {
	d := t.TempDir()
	a, err := LoadSecret(d)
	if err != nil || len(a) != 32 {
		t.Fatal(err, len(a))
	}
	st, _ := os.Stat(SecretPath(d))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	b, _ := LoadSecret(d)
	if !bytes.Equal(a, b) {
		t.Fatal("secret changed")
	}
}

func TestValidateBase(t *testing.T) {
	for s, ok := range map[string]bool{"": true, "http://1.2.3.4:80": true, "https://x": true,
		"http://x/": false, "ftp://x": false, "x": false, "http://": false} {
		if (ValidateBase(s) == nil) != ok {
			t.Errorf("%q", s)
		}
	}
}
