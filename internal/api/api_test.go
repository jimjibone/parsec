package api

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-jose/go-jose/v4"

	"parsec/internal/auth"
	"parsec/internal/config"
	"parsec/internal/gitrepo"
	"parsec/internal/history"
	"parsec/internal/store"
)

// ---- fake OpenID provider ----

type fakeIdP struct {
	srv  *httptest.Server
	key  *rsa.PrivateKey
	mu   sync.Mutex
	code map[string]codeInfo
}

type codeInfo struct {
	nonce, challenge string
	claims           map[string]any
}

func newIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIdP{key: key, code: map[string]codeInfo{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.srv.URL,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p.mu.Lock()
		info, ok := p.code[r.Form.Get("code")]
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != info.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		claims := map[string]any{
			"iss": p.srv.URL, "aud": "parsec", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "nonce": info.nonce,
		}
		maps.Copy(claims, info.claims)
		payload, _ := json.Marshal(claims)
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		if err != nil {
			t.Error(err)
			return
		}
		jws, _ := signer.Sign(payload)
		idToken, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
		})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

// ---- parsec under test ----

type env struct {
	t    *testing.T
	srv  *Server
	url  string
	idp  *fakeIdP
	data string
}

func isolateGit(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_COMMITTER_NAME", "parsec")
	t.Setenv("GIT_COMMITTER_EMAIL", "parsec@example.com")
}

func newEnv(t *testing.T) *env {
	isolateGit(t)
	idp := newIdP(t)
	var h http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(ts.Close)

	yes, no := true, false
	cfg := &config.Config{
		Data:      t.TempDir(),
		StateDir:  t.TempDir(),
		PublicURL: ts.URL,
		Auth: config.Auth{
			Mode:   config.ModeOIDC,
			Admins: []string{"alice"},
			OIDC:   config.OIDC{Issuer: idp.srv.URL, ClientID: "parsec", ClientSecret: "s3cret"},
		},
		Git: config.Git{AutoCommit: &yes, Push: &no, Idle: time.Minute},
	}
	if err := cfg.Finish(); err != nil {
		t.Fatal(err)
	}
	repo, err := gitrepo.Open(cfg.Data)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Data)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, a, st, repo, history.New(cfg.Data, 100), fstest.MapFS{"index.html": {Data: []byte("ui")}})
	h = srv.Handler()
	return &env{t: t, srv: srv, url: ts.URL, idp: idp, data: cfg.Data}
}

type client struct {
	e    *env
	http *http.Client
	tab  string
}

func (e *env) anon(tab string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, tab: tab, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// signIn runs the whole authorization code flow for a user.
func (e *env) signIn(username, name, tab string) *client {
	c := e.anon(tab)
	res, err := c.http.Get(e.url + "/auth/login")
	if err != nil {
		e.t.Fatal(err)
	}
	res.Body.Close()
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(loc.String(), e.idp.srv.URL+"/authorize") {
		e.t.Fatalf("login redirect = %q", res.Header.Get("Location"))
	}
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != e.url+"/auth/callback" {
		e.t.Fatalf("authorize query = %v", q)
	}
	code := "code-" + username
	e.idp.mu.Lock()
	e.idp.code[code] = codeInfo{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), claims: map[string]any{
		"sub": "id-" + username, "preferred_username": username, "name": name, "email": username + "@example.com",
	}}
	e.idp.mu.Unlock()
	res, err = c.http.Get(e.url + "/auth/callback?code=" + code + "&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		e.t.Fatal(err)
	}
	res.Body.Close()
	if loc := res.Header.Get("Location"); loc != "/" {
		e.t.Fatalf("callback redirect = %q", loc)
	}
	return c
}

func (c *client) do(method, path string, body any, out any) int {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.url+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Parsec-Client", c.tab)
	res, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func gitLog(t *testing.T, dir string) string {
	cmd := exec.Command("git", "log", "--format=%an <%ae>|%s")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not have any commits") {
			return ""
		}
		t.Fatalf("git log: %v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSharedServer(t *testing.T) {
	e := newEnv(t)

	// Not signed in.
	anon := e.anon("anon")
	var me struct {
		User *auth.User `json:"user"`
		Auth auth.Info  `json:"auth"`
	}
	if c := anon.do("GET", "/api/me", nil, &me); c != 200 || me.User != nil || me.Auth.Mode != "oidc" {
		t.Fatalf("anon me = %d %+v", c, me)
	}
	if c := anon.do("GET", "/api/state", nil, nil); c != 401 {
		t.Fatalf("anon state = %d", c)
	}

	// Alice is an admin from the config; Bob signs in as a viewer.
	alice := e.signIn("alice", "Alice Example", "tab-a")
	bob := e.signIn("bob", "Bob Sample", "tab-b")
	if alice.do("GET", "/api/me", nil, &me); me.User == nil || me.User.Role != "admin" || me.User.Name != "Alice Example" {
		t.Fatalf("alice me = %+v", me.User)
	}
	if bob.do("GET", "/api/me", nil, &me); me.User.Role != "viewer" {
		t.Fatalf("bob me = %+v", me.User)
	}
	if c := bob.do("GET", "/api/state", nil, nil); c != 200 {
		t.Fatalf("viewer state = %d", c)
	}
	if c := bob.do("POST", "/api/projects", store.Project{Name: "Nope"}, nil); c != 403 {
		t.Fatalf("viewer create = %d", c)
	}

	// Admin promotes Bob; cross-site writes are refused.
	if c := bob.do("PUT", "/api/users/bob", auth.Update{Role: "editor"}, nil); c != 403 {
		t.Fatalf("bob self-promote = %d", c)
	}
	if c := alice.do("PUT", "/api/users/bob", auth.Update{Role: "editor"}, nil); c != 200 {
		t.Fatalf("promote = %d", c)
	}
	if c := alice.do("PUT", "/api/users/alice", auth.Update{Role: "viewer"}, nil); c != 400 {
		t.Fatalf("self-demote = %d", c)
	}
	req, _ := http.NewRequest("POST", e.url+"/api/projects", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Origin", "https://evil.example.com")
	if res, _ := bob.http.Do(req); res.StatusCode != 403 {
		t.Fatalf("cross-origin = %d", res.StatusCode)
	}

	var proj store.Project
	if c := alice.do("POST", "/api/projects", store.Project{Name: "Falcon"}, &proj); c != 200 {
		t.Fatalf("create project = %d", c)
	}
	var task store.Task
	if c := alice.do("POST", "/api/tasks", store.Task{ProjectID: proj.ID, Title: "Motivator", Status: "todo", Start: "2026-10-05", End: "2026-10-07"}, &task); c != 200 {
		t.Fatalf("create task = %d", c)
	}

	// Alice saves twice quickly from one tab, both from the original version.
	stale := task
	a1 := task
	a1.Status = "doing"
	var saved []store.Task
	if c := alice.do("PUT", "/api/tasks", []store.Task{a1}, &saved); c != 200 || saved[0].Version == task.Version {
		t.Fatalf("save 1 = %d %+v", c, saved)
	}
	a2 := a1 // still carries the original version, like an optimistic UI
	a2.Title = "Motivator v2"
	if c := alice.do("PUT", "/api/tasks", []store.Task{a2}, nil); c != 200 {
		t.Fatalf("same-tab quick save = %d", c)
	}
	// Bob edits from the version he loaded before Alice's saves: conflict.
	b1 := stale
	b1.Status = "blocked"
	var cerr struct {
		Error    string `json:"error"`
		Conflict bool   `json:"conflict"`
	}
	if c := bob.do("PUT", "/api/tasks", []store.Task{b1}, &cerr); c != 409 || !cerr.Conflict || !strings.Contains(cerr.Error, "Alice Example") {
		t.Fatalf("conflict = %d %+v", c, cerr)
	}
	// After reloading, Bob's save goes through.
	var snap store.Snapshot
	bob.do("GET", "/api/state", nil, &snap)
	b1 = snap.Tasks[0]
	b1.Status = "blocked"
	if c := bob.do("PUT", "/api/tasks", []store.Task{b1}, nil); c != 200 {
		t.Fatalf("bob save = %d", c)
	}

	// Undo is per user: Alice's undo skips Bob's change to the same task,
	// so her step is stale and dropped; Bob undoes his own.
	var hist history.State
	alice.do("GET", "/api/history", nil, &hist)
	if hist.Undo != "Edit task" {
		t.Fatalf("alice history = %+v", hist)
	}
	if c := alice.do("POST", "/api/history/undo", nil, &cerr); c != 409 {
		t.Fatalf("alice stale undo = %d %+v", c, cerr)
	}
	if c := bob.do("POST", "/api/history/undo", nil, nil); c != 200 {
		t.Fatalf("bob undo = %d", c)
	}
	bob.do("GET", "/api/state", nil, &snap)
	if snap.Tasks[0].Status != "doing" || snap.Tasks[0].Title != "Motivator v2" {
		t.Fatalf("after bob undo = %+v", snap.Tasks[0])
	}

	if c := bob.do("POST", "/api/people", store.Person{Name: "Carol Test"}, nil); c != 200 {
		t.Fatalf("add person = %d", c)
	}

	// Automatic commits: nothing before the idle time, then one per user.
	// Each commit takes the current content of that user's files.
	e.srv.mu.Lock()
	e.srv.flush(false)
	e.srv.mu.Unlock()
	if out := gitLog(t, e.data); out != "" {
		t.Fatalf("committed too early: %s", out)
	}
	e.srv.Shutdown() // flushes everything
	log := gitLog(t, e.data)
	if !strings.Contains(log, "Alice Example <alice@example.com>|Create project, Create task, Edit task x2") ||
		!strings.Contains(log, "Bob Sample <bob@example.com>|Edit task, Undo: Edit task, Add person") {
		t.Fatalf("git log:\n%s", log)
	}

	// Sign out.
	if c := bob.do("POST", "/auth/logout", nil, nil); c != 204 {
		t.Fatalf("logout = %d", c)
	}
	if c := bob.do("GET", "/api/state", nil, nil); c != 401 {
		t.Fatalf("after logout = %d", c)
	}
}

func TestPlanning(t *testing.T) {
	e := newEnv(t)
	alice := e.signIn("alice", "Alice Example", "tab-a")
	bob := e.signIn("bob", "Bob Sample", "tab-b")

	var snap store.Snapshot
	bob.do("GET", "/api/state", nil, &snap)
	if snap.Planning.HoursPerDay != 6 || snap.Planning.AssigneeFactor != 1 || snap.Planning.Version == "" {
		t.Fatalf("default planning = %+v", snap.Planning)
	}
	stale := snap.Planning

	// Viewers cannot change it.
	if c := bob.do("PUT", "/api/planning", store.Planning{HoursPerDay: 5, AssigneeFactor: 1, Version: stale.Version}, nil); c != 403 {
		t.Fatalf("viewer edit = %d", c)
	}

	var saved store.Planning
	if c := alice.do("PUT", "/api/planning", store.Planning{HoursPerDay: 7, AssigneeFactor: 0.5, Version: stale.Version}, &saved); c != 200 ||
		saved.HoursPerDay != 7 || saved.AssigneeFactor != 0.5 || saved.Version == stale.Version {
		t.Fatalf("edit = %d %+v", c, saved)
	}
	if c := alice.do("PUT", "/api/planning", store.Planning{HoursPerDay: 0, AssigneeFactor: 1, Version: saved.Version}, nil); c != 400 {
		t.Fatalf("invalid edit = %d", c)
	}

	// Bob, now an editor, saves over the version he loaded before Alice's edit.
	if c := alice.do("PUT", "/api/users/bob", auth.Update{Role: "editor"}, nil); c != 200 {
		t.Fatalf("promote = %d", c)
	}
	var cerr struct {
		Error    string `json:"error"`
		Conflict bool   `json:"conflict"`
	}
	if c := bob.do("PUT", "/api/planning", store.Planning{HoursPerDay: 5, AssigneeFactor: 1, Version: stale.Version}, &cerr); c != 409 ||
		!cerr.Conflict || !strings.Contains(cerr.Error, "Alice Example") {
		t.Fatalf("conflict = %d %+v", c, cerr)
	}

	// planning.yaml is committed automatically under the editor's name.
	e.srv.Shutdown()
	if log := gitLog(t, e.data); !strings.Contains(log, "Alice Example <alice@example.com>|Edit planning") {
		t.Fatalf("git log:\n%s", log)
	}
	cmd := exec.Command("git", "show", "--name-only", "--format=", "HEAD")
	cmd.Dir = e.data
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "planning.yaml" {
		t.Fatalf("committed files: %v %s", err, out)
	}
}

func TestEventsStream(t *testing.T) {
	e := newEnv(t)
	alice := e.signIn("alice", "Alice Example", "tab-a")
	req, _ := http.NewRequest("GET", e.url+"/api/events", nil)
	res, err := alice.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var acc string
		for {
			n, err := res.Body.Read(buf)
			acc += string(buf[:n])
			if strings.Contains(acc, "data: ") {
				got <- acc
				return
			}
			if err != nil {
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond) // let the subscription register
	alice.do("POST", "/api/projects", store.Project{Name: "Shields"}, nil)
	select {
	case s := <-got:
		if !strings.Contains(s, `"kind":"data"`) || !strings.Contains(s, `"client":"tab-a"`) {
			t.Fatalf("event = %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
}
