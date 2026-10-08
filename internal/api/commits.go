package api

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"parsec/internal/auth"
)

// pending is one user's uncommitted changes, waiting for them to go idle.
type pending struct {
	user   auth.User
	paths  map[string]bool
	labels map[string]int
	order  []string // labels in first-seen order
	last   time.Time
}

// noteChange queues changed paths for the user's next automatic commit.
// Callers hold s.mu.
func (s *Server) noteChange(u auth.User, label string, paths []string) {
	if !s.autoCommit() || len(paths) == 0 {
		return
	}
	p := s.pending[u.Username]
	if p == nil {
		p = &pending{user: u, paths: map[string]bool{}, labels: map[string]int{}}
		s.pending[u.Username] = p
	}
	for _, x := range paths {
		p.paths[x] = true
	}
	if p.labels[label] == 0 {
		p.order = append(p.order, label)
	}
	p.labels[label]++
	p.last = time.Now()
}

func (s *Server) autoCommit() bool { return *s.cfg.Git.AutoCommit }

// RunCommitter commits idle users' changes until ctx ends, then commits
// everything still pending.
func (s *Server) RunCommitter(ctx context.Context) {
	if !s.autoCommit() {
		return
	}
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.flush(true)
			s.mu.Unlock()
			return
		case <-tick.C:
			s.mu.Lock()
			s.flush(false)
			s.mu.Unlock()
		}
	}
}

// flush commits each user's pending paths once they have been idle long
// enough (or all of them), then syncs with the remote. Callers hold s.mu.
func (s *Server) flush(all bool) {
	committed := false
	for _, name := range slices.Sorted(maps.Keys(s.pending)) {
		p := s.pending[name]
		if !all && time.Since(p.last) < s.cfg.Git.Idle {
			continue
		}
		ok, err := s.repo.CommitPaths(slices.Sorted(maps.Keys(p.paths)), author(p.user), commitMessage(p))
		if err != nil {
			s.setGitError(fmt.Errorf("automatic commit for %s: %w", name, err))
			continue
		}
		delete(s.pending, name)
		committed = committed || ok
	}
	if !committed {
		return
	}
	s.setGitError(nil)
	if *s.cfg.Git.Push {
		s.autoSync()
	}
	s.events.publish(Event{Kind: "git"})
}

// autoSync pulls (rebase) and pushes. A conflicting pull is aborted so the
// working tree never holds conflict markers; an admin has to resolve it.
func (s *Server) autoSync() {
	st, err := s.repo.Status()
	if err != nil || st.Remote == "" {
		return
	}
	if _, err := s.repo.Sync(); err != nil {
		if st, _ := s.repo.Status(); st.Rebasing {
			s.repo.AbortRebase()
			err = fmt.Errorf("remote has conflicting changes; resolve them in a clone and push, then pull here: %w", err)
		}
		s.setGitError(fmt.Errorf("automatic sync: %w", err))
	}
	if err := s.store.Reload(); err != nil {
		log.Printf("reload after sync: %v", err)
	}
	s.events.publish(Event{Kind: "data"})
}

func (s *Server) setGitError(err error) {
	if err == nil {
		s.gitError = ""
		return
	}
	log.Print(err)
	s.gitError = err.Error()
}

func author(u auth.User) string {
	email := u.Email
	if email == "" {
		email = u.Username + "@users.parsec.invalid"
	}
	return fmt.Sprintf("%s <%s>", u.DisplayName(), email)
}

func commitMessage(p *pending) string {
	parts := make([]string, 0, len(p.order))
	for _, l := range p.order {
		if n := p.labels[l]; n > 1 {
			parts = append(parts, fmt.Sprintf("%s x%d", l, n))
		} else {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, ", ")
}
