package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"parsec/internal/config"
)

func TestLocalLogin(t *testing.T) {
	cfg := &config.Config{Data: t.TempDir(), StateDir: t.TempDir(), Auth: config.Auth{Mode: config.ModeLocal, Admins: []string{"alice"}}}
	if err := cfg.Finish(); err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Users.Create(Update{Username: "alice", Password: "short"}); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := a.Users.Create(Update{Username: "alice", Name: "Alice Example", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.Routes(mux)

	login := func(user, pw string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"`+user+`","password":"`+pw+`"}`)))
		return w
	}
	w := login("alice", "correct horse")
	if w.Code != 204 {
		t.Fatalf("login = %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	u, ok := a.Identify(r)
	if !ok || u.Username != "alice" || u.Role != RoleAdmin || !u.Fixed {
		t.Fatalf("identify = %+v %v", u, ok)
	}

	// A tampered cookie is rejected.
	r = httptest.NewRequest("GET", "/", nil)
	c := w.Result().Cookies()[0]
	c.Value = "x" + c.Value
	r.AddCookie(c)
	if _, ok := a.Identify(r); ok {
		t.Fatal("tampered cookie accepted")
	}

	for range maxFails {
		if w := login("alice", "wrong"); w.Code != 401 {
			t.Fatalf("wrong password = %d", w.Code)
		}
	}
	if w := login("alice", "correct horse"); w.Code != 429 {
		t.Fatalf("after too many fails = %d", w.Code)
	}
	if w := login("nobody", "whatever1"); w.Code != 401 {
		t.Fatalf("unknown user = %d", w.Code)
	}
}

func TestConfigRejectsStateInsideData(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Data: dir, StateDir: dir + "/state", Auth: config.Auth{Mode: config.ModeLocal}}
	if err := cfg.Finish(); err == nil {
		t.Fatal("state dir inside data dir accepted")
	}
}
