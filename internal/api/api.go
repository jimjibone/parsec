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
	"parsec/internal/store"
)

type Server struct {
	store *store.Store
	repo  *gitrepo.Repo
	ui    fs.FS

	// mu serialises writes and git operations so a pull never races a save.
	mu sync.Mutex
}

func New(s *store.Store, r *gitrepo.Repo, ui fs.FS) *Server {
	return &Server{store: s, repo: r, ui: ui}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.store.Snapshot())
	})

	mux.HandleFunc("POST /api/projects", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var p store.Project
		if !readJSON(w, r, &p) {
			return
		}
		respond(w)(s.store.CreateProject(p))
	}))
	mux.HandleFunc("PUT /api/projects/{id}", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var p store.Project
		if !readJSON(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		respond(w)(s.store.UpdateProject(p))
	}))
	mux.HandleFunc("DELETE /api/projects/{id}", s.locked(func(w http.ResponseWriter, r *http.Request) {
		respond(w)(changed(s.store.DeleteProject(r.PathValue("id"))))
	}))

	mux.HandleFunc("POST /api/tasks", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var t store.Task
		if !readJSON(w, r, &t) {
			return
		}
		respond(w)(s.store.CreateTask(t))
	}))
	// Batch replace; body is an array of full tasks.
	mux.HandleFunc("PUT /api/tasks", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var ts []store.Task
		if !readJSON(w, r, &ts) {
			return
		}
		respond(w)(s.store.UpdateTasks(ts))
	}))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.locked(func(w http.ResponseWriter, r *http.Request) {
		respond(w)(changed(s.store.DeleteTask(r.PathValue("id"))))
	}))

	mux.HandleFunc("POST /api/people", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if !readJSON(w, r, &p) {
			return
		}
		respond(w)(s.store.CreatePerson(p))
	}))
	mux.HandleFunc("PUT /api/people/{id}", s.locked(func(w http.ResponseWriter, r *http.Request) {
		var p store.Person
		if !readJSON(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		respond(w)(s.store.UpdatePerson(p))
	}))
	mux.HandleFunc("DELETE /api/people/{id}", s.locked(func(w http.ResponseWriter, r *http.Request) {
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
		respond(w)(s.repo.Commit(body.Message))
	}))
	mux.HandleFunc("POST /api/git/pull", s.locked(s.gitOp(s.repo.Pull)))
	mux.HandleFunc("POST /api/git/push", s.locked(s.gitOp(s.repo.Push)))
	mux.HandleFunc("POST /api/git/sync", s.locked(s.gitOp(s.repo.Sync)))
	mux.HandleFunc("POST /api/git/abort-rebase", s.locked(s.gitOp(s.repo.AbortRebase)))
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

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.Handle("/", s.uiHandler())
	return mux
}

// gitOp runs an operation that may change files on disk, then reloads the
// store so the UI sees pulled changes. The result includes reload errors.
func (s *Server) gitOp(op func() (gitrepo.Result, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := op()
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
	case errors.As(err, &ge):
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
