// Package api exposes the store and git repo over JSON HTTP.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"parsec/internal/auth"
	"parsec/internal/config"
	"parsec/internal/gitrepo"
	"parsec/internal/history"
	"parsec/internal/store"
)

type Server struct {
	cfg     *config.Config
	auth    *auth.Auth
	store   *store.Store
	repo    *gitrepo.Repo
	history *history.History
	events  *hub
	ui      fs.FS

	// mu serialises writes and git operations so a pull never races a save.
	// It also guards the fields below.
	mu sync.Mutex
	// lastClient and lastEditor record, per entity ("task:<id>" etc.), the
	// browser tab and person that last changed it, for conflict checks.
	lastClient map[string]string
	lastEditor map[string]string
	pending    map[string]*pending // by username
	gitError   string              // last automatic commit/sync failure
}

func New(cfg *config.Config, a *auth.Auth, s *store.Store, r *gitrepo.Repo, h *history.History, ui fs.FS) *Server {
	return &Server{
		cfg: cfg, auth: a, store: s, repo: r, history: h, events: newHub(), ui: ui,
		lastClient: map[string]string{},
		lastEditor: map[string]string{},
		pending:    map[string]*pending{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.auth.Routes(mux)

	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"auth": s.auth.Info(), "autoCommit": s.autoCommit(), "user": nil}
		if u, ok := s.auth.Identify(r); ok {
			body["user"] = u
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("GET /api/state", s.require(auth.RoleViewer, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.store.Snapshot())
	}))
	mux.HandleFunc("GET /api/events", s.require(auth.RoleViewer, s.events.serveEvents))

	mux.HandleFunc("POST /api/projects", s.mutating("Create project", func(w http.ResponseWriter, r *http.Request) {
		var p store.Project
		if !readJSON(w, r, &p) {
			return
		}
		p, err := s.store.CreateProject(p)
		s.touched(r, err, "project:"+p.ID)
		respond(w)(p, err)
	}))
	// Body: {"ids": [...]} listing every project in the new order.
	mux.HandleFunc("PUT /api/projects/order", s.mutating("Reorder projects", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		ps, err := s.store.ReorderProjects(body.IDs)
		for _, p := range ps {
			s.touched(r, err, "project:"+p.ID)
		}
		respond(w)(ps, err)
	}))
	mux.HandleFunc("PUT /api/projects/{id}", s.mutating("Edit project", func(w http.ResponseWriter, r *http.Request) {
		var p store.Project
		if !readJSON(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		if cur, ok := s.store.ProjectVersion(p.ID); ok && s.conflict(w, r, "project:"+p.ID, p.Version, cur, "Project "+quote(p.Name)) {
			return
		}
		p, err := s.store.UpdateProject(p)
		s.touched(r, err, "project:"+p.ID)
		respond(w)(p, err)
	}))
	mux.HandleFunc("DELETE /api/projects/{id}", s.mutating("Delete project", func(w http.ResponseWriter, r *http.Request) {
		ts, err := s.store.DeleteProject(r.PathValue("id"))
		s.touchedTasks(r, err, ts)
		respond(w)(changed(ts, err))
	}))

	mux.HandleFunc("POST /api/tasks", s.mutating("Create task", func(w http.ResponseWriter, r *http.Request) {
		var t store.Task
		if !readJSON(w, r, &t) {
			return
		}
		t, err := s.store.CreateTask(t)
		s.touched(r, err, "task:"+t.ID)
		respond(w)(t, err)
	}))
	// Batch replace; body is an array of full tasks.
	mux.HandleFunc("PUT /api/tasks", s.mutating("Edit task", func(w http.ResponseWriter, r *http.Request) {
		var ts []store.Task
		if !readJSON(w, r, &ts) {
			return
		}
		for _, t := range ts {
			if cur, ok := s.store.TaskVersion(t.ID); ok && s.conflict(w, r, "task:"+t.ID, t.Version, cur, "Task "+quote(t.Title)) {
				return
			}
		}
		ts, err := s.store.UpdateTasks(ts)
		s.touchedTasks(r, err, ts)
		respond(w)(ts, err)
	}))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.mutating("Delete task", func(w http.ResponseWriter, r *http.Request) {
		ts, err := s.store.DeleteTask(r.PathValue("id"))
		s.touchedTasks(r, err, ts)
		respond(w)(changed(ts, err))
	}))

	mux.HandleFunc("POST /api/people", s.mutating("Add person", func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if !readJSON(w, r, &p) {
			return
		}
		p, err := s.store.CreatePerson(p)
		s.touched(r, err, "person:"+p.ID)
		respond(w)(p, err)
	}))
	mux.HandleFunc("PUT /api/people/{id}", s.mutating("Edit person", func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if !readJSON(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		if cur, ok := s.store.PersonVersion(p.ID); ok && s.conflict(w, r, "person:"+p.ID, p.Version, cur, quote(p.Name)) {
			return
		}
		p, err := s.store.UpdatePerson(p)
		s.touched(r, err, "person:"+p.ID)
		respond(w)(p, err)
	}))
	mux.HandleFunc("DELETE /api/people/{id}", s.mutating("Remove person", func(w http.ResponseWriter, r *http.Request) {
		ts, err := s.store.DeletePerson(r.PathValue("id"))
		s.touchedTasks(r, err, ts)
		respond(w)(changed(ts, err))
	}))

	// Team capacity settings for task pressure; read via /api/state.
	mux.HandleFunc("PUT /api/planning", s.mutating("Edit planning", func(w http.ResponseWriter, r *http.Request) {
		var p store.Planning
		if !readJSON(w, r, &p) {
			return
		}
		if s.conflict(w, r, "planning", p.Version, s.store.PlanningVersion(), "Planning settings") {
			return
		}
		p, err := s.store.UpdatePlanning(p)
		s.touched(r, err, "planning")
		respond(w)(p, err)
	}))

	// Git: anyone can look; only admins act. With automatic commits the
	// status also reports the last automatic failure.
	mux.HandleFunc("GET /api/git/status", s.require(auth.RoleViewer, s.locked(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.repo.Status()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			gitrepo.Status
			AutoError string `json:"autoError"`
		}{st, s.gitError})
	})))
	mux.HandleFunc("GET /api/git/log", s.require(auth.RoleViewer, s.locked(func(w http.ResponseWriter, r *http.Request) {
		respond(w)(s.repo.Log(30))
	})))
	mux.HandleFunc("POST /api/git/commit", s.require(auth.RoleAdmin, s.locked(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message string `json:"message"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		res, err := s.repo.Commit(body.Message)
		if err == nil {
			s.afterManualGit()
			s.pending = map[string]*pending{}
			s.events.publish(Event{Kind: "git"})
		}
		respond(w)(res, err)
	})))
	mux.HandleFunc("POST /api/git/pull", s.require(auth.RoleAdmin, s.locked(s.gitOp(s.repo.Pull, true))))
	mux.HandleFunc("POST /api/git/push", s.require(auth.RoleAdmin, s.locked(s.gitOp(s.repo.Push, false))))
	mux.HandleFunc("POST /api/git/sync", s.require(auth.RoleAdmin, s.locked(s.gitOp(s.repo.Sync, true))))
	mux.HandleFunc("POST /api/git/abort-rebase", s.require(auth.RoleAdmin, s.locked(s.gitOp(s.repo.AbortRebase, true))))
	mux.HandleFunc("PUT /api/git/remote", s.require(auth.RoleAdmin, s.locked(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		if err := s.repo.SetRemote(body.URL); err != nil {
			writeError(w, err)
			return
		}
		respond(w)(s.repo.Status())
	})))

	mux.HandleFunc("GET /api/history", s.require(auth.RoleViewer, s.locked(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.history.State(userOf(r).Username))
	})))
	mux.HandleFunc("POST /api/history/undo", s.require(auth.RoleEditor, s.locked(s.historyOp("Undo", s.history.Undo))))
	mux.HandleFunc("POST /api/history/redo", s.require(auth.RoleEditor, s.locked(s.historyOp("Redo", s.history.Redo))))

	s.userRoutes(mux)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.Handle("/", s.uiHandler())
	return mux
}

// userRoutes is the admin settings API (shared mode only).
func (s *Server) userRoutes(mux *http.ServeMux) {
	users := s.auth.Users
	if users == nil {
		return
	}
	changedUsers := func(r *http.Request) {
		u := userOf(r)
		s.events.publish(Event{Kind: "users", By: u.Username, ByName: u.DisplayName()})
	}
	mux.HandleFunc("GET /api/users", s.require(auth.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"users": users.List(), "settings": users.Settings()})
	}))
	mux.HandleFunc("POST /api/users", s.require(auth.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var in auth.Update
		if !readJSON(w, r, &in) {
			return
		}
		u, err := users.Create(in)
		if err == nil {
			changedUsers(r)
		}
		respond(w)(u, err)
	}))
	mux.HandleFunc("PUT /api/users/{username}", s.require(auth.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var in auth.Update
		if !readJSON(w, r, &in) {
			return
		}
		in.Username = r.PathValue("username")
		if strings.EqualFold(in.Username, userOf(r).Username) && in.Role != "" && in.Role != auth.RoleAdmin {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "admins cannot remove their own admin role"})
			return
		}
		u, err := users.Update(in)
		if err == nil {
			changedUsers(r)
		}
		respond(w)(u, err)
	}))
	mux.HandleFunc("DELETE /api/users/{username}", s.require(auth.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("username")
		if strings.EqualFold(name, userOf(r).Username) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "admins cannot remove themselves"})
			return
		}
		err := users.Delete(name)
		if err == nil {
			changedUsers(r)
		}
		respond(w)(map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/settings", s.require(auth.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
		var in auth.Settings
		if !readJSON(w, r, &in) {
			return
		}
		respond(w)(users.SetSettings(in))
	}))
}

// ---- identity and roles ----

type ctxKey struct{}

func userOf(r *http.Request) auth.User {
	u, _ := r.Context().Value(ctxKey{}).(auth.User)
	return u
}

// clientOf is the browser tab id sent by the frontend.
func clientOf(r *http.Request) string { return r.Header.Get("X-Parsec-Client") }

// require checks the session, the role and, for writes, the Origin header.
func (s *Server) require(role string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.auth.Identify(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
			return
		}
		if !auth.AtLeast(u.Role, role) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "this needs the " + role + " role; ask an admin"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.sameOrigin(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request refused"})
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

// sameOrigin rejects writes sent from other sites. Browsers always send
// Origin on cross-site POST/PUT/DELETE.
func (s *Server) sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	if s.cfg.PublicURL != "" {
		p, _ := url.Parse(s.cfg.PublicURL)
		if strings.EqualFold(u.Host, p.Host) {
			return true
		}
	}
	return strings.EqualFold(u.Host, r.Host)
}

// mutating runs a data-changing handler for an editor and records what it
// changed as one undo step, whether or not the handler succeeded.
func (s *Server) mutating(label string, h http.HandlerFunc) http.HandlerFunc {
	return s.require(auth.RoleEditor, s.locked(func(w http.ResponseWriter, r *http.Request) {
		before, err := s.history.Capture()
		if err != nil {
			writeError(w, err)
			return
		}
		h(w, r)
		u := userOf(r)
		paths, err := s.history.Record(u.Username, label, before)
		if err != nil {
			log.Printf("record undo step: %v", err)
			s.history.Clear()
		}
		s.changed(r, label, paths)
	}))
}

// changed queues an automatic commit and tells other browsers.
func (s *Server) changed(r *http.Request, label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	u := userOf(r)
	s.noteChange(u, label, paths)
	s.events.publish(Event{Kind: "data", By: u.Username, ByName: u.DisplayName(), Client: clientOf(r), Label: label})
}

// conflict writes a 409 when the client edited an old version of an entity
// that someone else has changed since. A tab's own quick successive saves
// (sent before the previous response arrived) do not conflict.
func (s *Server) conflict(w http.ResponseWriter, r *http.Request, key, sent, current, what string) bool {
	if sent == "" || sent == current {
		return false
	}
	if c := clientOf(r); c != "" && s.lastClient[key] == c {
		return false
	}
	by := s.lastEditor[key]
	if by == "" {
		by = "someone else"
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":    fmt.Sprintf("%s was changed by %s in the meantime. Showing the latest version.", what, by),
		"conflict": true,
	})
	return true
}

func (s *Server) touched(r *http.Request, err error, keys ...string) {
	if err != nil {
		return
	}
	for _, k := range keys {
		s.lastClient[k] = clientOf(r)
		s.lastEditor[k] = userOf(r).DisplayName()
	}
}

func (s *Server) touchedTasks(r *http.Request, err error, ts []store.Task) {
	for _, t := range ts {
		s.touched(r, err, "task:"+t.ID)
	}
}

func quote(s string) string { return `"` + s + `"` }

// ---- undo, git ----

// historyOp runs an undo or redo for the signed-in user, then reloads the
// store. The body has the step's label and the user's new history state.
func (s *Server) historyOp(verb string, op func(string) (history.Step, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userOf(r)
		step, err := op(u.Username)
		if rerr := s.store.Reload(); rerr != nil {
			log.Printf("reload after undo/redo: %v", rerr)
			if err == nil {
				err = rerr
			}
		}
		if err != nil {
			writeError(w, err)
			return
		}
		s.changed(r, verb+": "+step.Label, step.Paths)
		writeJSON(w, http.StatusOK, map[string]any{"label": step.Label, "history": s.history.State(u.Username)})
	}
}

// afterManualGit drops the undo stack after a manual commit, pull or sync
// when undo is meant to cover uncommitted changes only (no automatic
// commits). With automatic commits undo spans commits, and stale steps are
// caught when undone.
func (s *Server) afterManualGit() {
	if !s.autoCommit() {
		s.history.Clear()
	}
}

// gitOp runs an operation that may change files on disk, then reloads the
// store so the UI sees pulled changes. The result includes reload errors.
// `changesFiles` marks operations that can rewrite the working tree.
func (s *Server) gitOp(op func() (gitrepo.Result, error), changesFiles bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := op()
		if changesFiles {
			s.afterManualGit()
		}
		if rerr := s.store.Reload(); rerr != nil {
			log.Printf("reload after git: %v", rerr)
			if err == nil {
				err = rerr
			}
		}
		s.events.publish(Event{Kind: "git"})
		if changesFiles {
			s.events.publish(Event{Kind: "data"})
		}
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "output": res.Output})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func (s *Server) locked(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		h(w, r)
	}
}

// Shutdown commits anything still pending.
func (s *Server) Shutdown() {
	if !s.autoCommit() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flush(true)
}

// uiHandler serves the built SPA, falling back to index.html.
func (s *Server) uiHandler() http.Handler {
	files := http.FileServerFS(s.ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(s.ui, p); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(s.ui, "index.html")
		if err != nil {
			http.Error(w, "frontend not built: run `make build`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}

// changed wraps a list of side-effect-modified tasks into a response body.
func changed(ts []store.Task, err error) (map[string]any, error) {
	if ts == nil {
		ts = []store.Task{}
	}
	return map[string]any{"changedTasks": ts}, err
}

func respond(w http.ResponseWriter) func(any, error) {
	return func(v any, err error) {
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var ve *store.ValidationError
	var ie *auth.InputError
	var ge *gitrepo.Error
	switch {
	case errors.As(err, &ve), errors.As(err, &ie):
		status = http.StatusBadRequest
	case errors.Is(err, store.ErrNotFound), errors.Is(err, auth.ErrNotFound):
		status = http.StatusNotFound
	case errors.As(err, &ge),
		errors.Is(err, history.ErrNothingToUndo),
		errors.Is(err, history.ErrNothingToRedo),
		errors.Is(err, history.ErrStale):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
