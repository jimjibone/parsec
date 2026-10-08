package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// signer makes and checks tamper-proof cookie values: base64(json).base64(mac).
type signer struct{ key []byte }

// loadKey reads <stateDir>/session.key, creating it on first run.
func loadKey(stateDir string) ([]byte, error) {
	path := filepath.Join(stateDir, "session.key")
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		return b, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b = make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return b, nil
}

func (s signer) mac(purpose string, payload []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write(payload)
	return m.Sum(nil)
}

func (s signer) encode(purpose string, v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(s.mac(purpose, payload)), nil
}

func (s signer) decode(purpose, value string, v any) bool {
	p, m, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	enc := base64.RawURLEncoding
	payload, err1 := enc.DecodeString(p)
	sum, err2 := enc.DecodeString(m)
	if err1 != nil || err2 != nil || !hmac.Equal(sum, s.mac(purpose, payload)) {
		return false
	}
	return json.Unmarshal(payload, v) == nil
}

const sessionCookie = "parsec_session"

type session struct {
	User string `json:"u"`
	Exp  int64  `json:"e"`
}

func (a *Auth) setSession(w http.ResponseWriter, r *http.Request, username string) error {
	exp := time.Now().Add(time.Duration(a.cfg.Auth.SessionDays) * 24 * time.Hour)
	v, err := a.sign.encode("session", session{User: username, Exp: exp.Unix()})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    v,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   a.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *Auth) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) sessionUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	var s session
	if !a.sign.decode("session", c.Value, &s) || time.Now().Unix() > s.Exp {
		return "", false
	}
	return s.User, true
}

// secure marks cookies Secure when the public URL (or the request) is https.
func (a *Auth) secure(r *http.Request) bool {
	if a.cfg.PublicURL != "" {
		return strings.HasPrefix(a.cfg.PublicURL, "https://")
	}
	return r.TLS != nil
}
