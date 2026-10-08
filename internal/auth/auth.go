// Package auth signs users in (OpenID Connect or local passwords), keeps
// sessions in signed cookies and stores accounts and roles.
package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"parsec/internal/config"
)

// LocalUser is the identity in auth mode "none".
var LocalUser = User{Username: "local", Name: "Local user", Role: RoleAdmin}

type Auth struct {
	cfg   *config.Config
	Users *Users // nil in mode none
	sign  signer

	// oidc is set up lazily so the server starts while the provider is down.
	oidcMu   sync.Mutex
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config

	failMu sync.Mutex
	fails  map[string][]time.Time
}

func New(cfg *config.Config) (*Auth, error) {
	a := &Auth{cfg: cfg, fails: map[string][]time.Time{}}
	if !cfg.Shared() {
		return a, nil
	}
	var err error
	if a.Users, err = OpenUsers(cfg.StateDir, cfg.Auth.Admins, cfg.Auth.DefaultRole); err != nil {
		return nil, err
	}
	key, err := loadKey(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	a.sign = signer{key: key}
	if cfg.Auth.Mode == config.ModeOIDC {
		if err := a.setupOIDC(context.Background()); err != nil {
			log.Printf("oidc provider not reachable yet (will retry at sign-in): %v", err)
		}
	}
	return a, nil
}

// Identify returns the signed-in user, with their current role.
func (a *Auth) Identify(r *http.Request) (User, bool) {
	if !a.cfg.Shared() {
		return LocalUser, true
	}
	name, ok := a.sessionUser(r)
	if !ok {
		return User{}, false
	}
	return a.Users.Get(name)
}

// Info is what the sign-in screen needs.
type Info struct {
	Mode  string `json:"mode"`
	Label string `json:"label,omitempty"` // oidc provider name
}

func (a *Auth) Info() Info {
	i := Info{Mode: a.cfg.Auth.Mode}
	if a.cfg.Auth.Mode == config.ModeOIDC {
		i.Label = a.cfg.Auth.OIDC.Label
	}
	return i
}

// Routes registers the sign-in and sign-out endpoints.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		a.clearSession(w, r)
		w.WriteHeader(http.StatusNoContent)
	})
	switch a.cfg.Auth.Mode {
	case config.ModeOIDC:
		mux.HandleFunc("GET /auth/login", a.oidcLogin)
		mux.HandleFunc("GET /auth/callback", a.oidcCallback)
	case config.ModeLocal:
		mux.HandleFunc("POST /auth/login", a.localLogin)
	}
}

// ---- local passwords ----

const (
	maxFails   = 10
	failWindow = 15 * time.Minute
)

func (a *Auth) localLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}
	key := strings.ToLower(strings.TrimSpace(body.Username))
	if a.tooManyFails(key) {
		jsonError(w, http.StatusTooManyRequests, "too many failed sign-ins; try again later")
		return
	}
	u, ok := a.Users.CheckPassword(key, body.Password)
	if !ok {
		a.recordFail(key)
		jsonError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	if err := a.setSession(w, r, u.Username); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.Users.Touch(u.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) tooManyFails(key string) bool {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	recent := a.fails[key][:0]
	for _, t := range a.fails[key] {
		if time.Since(t) < failWindow {
			recent = append(recent, t)
		}
	}
	a.fails[key] = recent
	return len(recent) >= maxFails
}

func (a *Auth) recordFail(key string) {
	a.failMu.Lock()
	defer a.failMu.Unlock()
	a.fails[key] = append(a.fails[key], time.Now())
}

// ---- OpenID Connect ----

func (a *Auth) setupOIDC(ctx context.Context) error {
	a.oidcMu.Lock()
	defer a.oidcMu.Unlock()
	if a.verifier != nil {
		return nil
	}
	o := a.cfg.Auth.OIDC
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, o.Issuer)
	if err != nil {
		return err
	}
	a.verifier = p.Verifier(&oidc.Config{ClientID: o.ClientID})
	a.oauth = &oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  a.cfg.PublicURL + "/auth/callback",
		Scopes:       o.Scopes,
	}
	return nil
}

const flowCookie = "parsec_oidc"

// flow is the per-sign-in state kept in a short-lived signed cookie.
type flow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Exp      int64  `json:"e"`
}

func (a *Auth) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if err := a.setupOIDC(r.Context()); err != nil {
		http.Error(w, "sign-in provider unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	f := flow{
		State:    oauth2.GenerateVerifier(),
		Nonce:    oauth2.GenerateVerifier(),
		Verifier: oauth2.GenerateVerifier(),
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	v, err := a.sign.encode("oidc", f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookie,
		Value:    v,
		Path:     "/auth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   a.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, a.oauth.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

func (a *Auth) oidcCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		log.Printf("oidc sign-in failed: %s", msg)
		http.Redirect(w, r, "/?signin_error="+url.QueryEscape(msg), http.StatusFound)
	}
	c, err := r.Cookie(flowCookie)
	var f flow
	if err != nil || !a.sign.decode("oidc", c.Value, &f) || time.Now().Unix() > f.Exp {
		fail("sign-in expired, please try again")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: "/auth/", MaxAge: -1})
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fail(e + ": " + q.Get("error_description"))
		return
	}
	if q.Get("state") != f.State {
		fail("state mismatch, please try again")
		return
	}
	if err := a.setupOIDC(r.Context()); err != nil {
		fail("sign-in provider unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tok, err := a.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		fail("code exchange: " + err.Error())
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		fail("no id_token in response")
		return
	}
	idt, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		fail("id_token: " + err.Error())
		return
	}
	if idt.Nonce != f.Nonce {
		fail("nonce mismatch")
		return
	}
	var claims struct {
		Sub               string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
		Nickname          string `json:"nickname"`
		Name              string `json:"name"`
		Email             string `json:"email"`
	}
	if err := idt.Claims(&claims); err != nil {
		fail("claims: " + err.Error())
		return
	}
	username := claims.PreferredUsername
	if username == "" {
		username = claims.Nickname
	}
	if username == "" {
		username = claims.Sub
	}
	u, err := a.Users.SignedIn(username, claims.Name, claims.Email)
	if err != nil {
		fail(err.Error())
		return
	}
	if err := a.setSession(w, r, u.Username); err != nil {
		fail(err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
