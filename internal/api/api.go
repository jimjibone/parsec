// Package api exposes the store and git repo over JSON HTTP.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"parsec/internal/gitrepo"
	"parsec/internal/history"
	"parsec/internal/store"
)

type Server struct {
	store   *store.Store
	repo    *gitrepo.Repo
	history *history.History
	ui      fs.FS

	// mu serialises writes and git operations so a pull never races a save.
	mu sync.Mutex
}

func New(s *store.Store, r *gitrepo.Repo, h *history.History, ui fs.FS) *Server {
	return &Server{store: s, repo: r, history: h, ui: ui}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.store.Snapshot())
	})

	mux.HandleFunc("POST /api/projects", s.mutating("Create project", func(w http.ResponseWriter, r *http.Request) {
		var p store.Project
		if !readJSON(w, r, &p) {
			return
		}
		respond(w)(s.store.CreateProject(p))
	}))
	// Body: {"ids": [...]} listing every project in the new order.
	mux.HandleFunc("PUT /api/projects/order", s.mutating("Reorder projects", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		respond(w)(s.store.ReorderProjects(body.IDs))
	}))
	mux.HandleFunc("PUT /api/projects/{id}", s.mutating("Edit project", func(w http.ResponseWriter, r *http.Request) {
		var p store.Project
		if !readJSON(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		respond(w)(s.store.UpdateProject(p))
	}))
	mux.HandleFunc("DELETE /api/projects/{id}", s.mutating("Delete project", func(w http.ResponseWriter, r *http.Request) {
		respond(w)(changed(s.store.DeleteProject(r.PathValue("id"))))
	}))

	mux.HandleFunc("POST /api/tasks", s.mutating("Create task", func(w http.ResponseWriter, r *http.Request) {
		var t store.Task
		if !readJSON(w, r, &t) {
			return
		}
		respond(w)(s.store.CreateTask(t))
	}))
	// Batch replace; body is an array of full tasks.
	mux.HandleFunc("PUT /api/tasks", s.mutating("Edit task", func(w http.ResponseWriter, r *http.Request) {
		var ts []store.Task
		if !readJSON(w, r, &ts) {
			return
		}
		respond(w)(s.store.UpdateTasks(ts))
	}))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.mutating("Delete task", func(w http.ResponseWriter, r *http.Request) {
		respond(w)(changed(s.store.DeleteTask(r.PathValue("id"))))
	}))

	mux.HandleFunc("POST /api/people", s.mutating("Add person", func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if !readJSON(w, r, &p) {
			return
		}
		respond(w)(s.store.CreatePerson(p))
	}))
	mux.HandleFunc("PUT /api/people/{id}", s.mutating("Edit person", func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if !readJSON(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		respond(w)(s.store.UpdatePerson(p))
	}))
	mux.HandleFunc("DELETE /api/people/{id}", s.mutating("Remove person", func(w http.ResponseWriter, r *http.Request) {
		respond(w)(changed(s.store.DeletePerson(r.PathValue("id"))))
	}))

	mux.HandleFunc("GET /api/git/status", s.locked(func(w http.ResponseWriter, r *http.Request) {
		respond(w)(s.repo.Status())
	}))
	mux.HandleFunc("GET /api/git/log", s.locked(func(w http.ResponseWriter, r *http.Request) {
		respond(w)(s.repo.Log(30))
	}))
	mux.HandleFunc("POST /api/git/commit", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message string `json:"message"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		res, err := s.repo.Commit(body.Message)
		if err == nil {
			s.history.Clear() // undo covers uncommitted changes only
		}
		respond(w)(res, err)
	}))
	mux.HandleFunc("POST /api/git/pull", s.locked(s.gitOp(s.repo.Pull, true)))
	mux.HandleFunc("POST /api/git/push", s.locked(s.gitOp(s.repo.Push, false)))
	mux.HandleFunc("POST /api/git/sync", s.locked(s.gitOp(s.repo.Sync, true)))
	mux.HandleFunc("POST /api/git/abort-rebase", s.locked(s.gitOp(s.repo.AbortRebase, true)))
	mux.HandleFunc("PUT /api/git/remote", s.locked(func(w http.ResponseWriter, r *http.Request) {
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
	}))

	mux.HandleFunc("GET /api/history", s.locked(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.history.State())
	}))
	mux.HandleFunc("POST /api/history/undo", s.locked(s.historyOp(s.history.Undo)))
	mux.HandleFunc("POST /api/history/redo", s.locked(s.historyOp(s.history.Redo)))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.Handle("/", s.uiHandler())
	return mux
}

// mutating runs a handler that writes data files and records what it changed
// as one undo step, whether or not the handler succeeded.
func (s *Server) mutating(label string, h http.HandlerFunc) http.HandlerFunc {
	return s.locked(func(w http.ResponseWriter, r *http.Request) {
		before, err := s.history.Capture()
		if err != nil {
			writeError(w, err)
			return
		}
		h(w, r)
		if err := s.history.Record(label, before); err != nil {
			log.Printf("record undo step: %v", err)
			s.history.Clear()
		}
	})
}

// historyOp runs an undo or redo, then reloads the store. The body has the
// label of the step and the new history state.
func (s *Server) historyOp(op func() (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		label, err := op()
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
		writeJSON(w, http.StatusOK, map[string]any{"label": label, "history": s.history.State()})
	}
}

// gitOp runs an operation that may change files on disk, then reloads the
// store so the UI sees pulled changes. The result includes reload errors.
// `clearHistory` drops the undo stack for operations that can change files.
func (s *Server) gitOp(op func() (gitrepo.Result, error), clearHistory bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := op()
		if clearHistory {
			s.history.Clear()
		}
		if rerr := s.store.Reload(); rerr != nil {
			log.Printf("reload after git: %v", rerr)
			if err == nil {
				err = rerr
			}
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
	var ge *gitrepo.Error
	switch {
	case errors.As(err, &ve):
		status = http.StatusBadRequest
	case errors.Is(err, store.ErrNotFound):
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
