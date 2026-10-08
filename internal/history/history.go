// Package history keeps an in-memory undo/redo stack of changes to the files
// in a directory. Each step stores the before and after contents of the files
// it touched, so undo and redo work for any kind of edit.
package history

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrNothingToUndo = errors.New("nothing to undo")
	ErrNothingToRedo = errors.New("nothing to redo")
	// ErrStale means a file was changed by something else after the step was
	// recorded. The history is cleared rather than overwrite that change.
	ErrStale = errors.New("files changed outside parsec; undo history cleared")
)

// Files maps a slash-separated path, relative to the directory, to contents.
// A missing key means the file does not exist.
type Files map[string][]byte

type entry struct {
	label         string
	paths         []string
	before, after Files
}

// History is not safe for concurrent use; callers serialise access.
type History struct {
	dir        string
	max        int
	undo, redo []entry
}

// State describes the next undo and redo steps; empty means none.
type State struct {
	Undo string `json:"undo"`
	Redo string `json:"redo"`
}

func New(dir string, max int) *History {
	return &History{dir: dir, max: max}
}

// Capture reads every file in the directory, skipping dot files and
// directories (.git, temp files).
func (h *History) Capture() (Files, error) {
	out := Files{}
	err := filepath.WalkDir(h.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == h.dir && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if path != h.dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(h.dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	return out, err
}

// Record compares the directory with `before` and pushes the difference as
// one undo step. No step is pushed when nothing changed. Any new step clears
// the redo stack.
func (h *History) Record(label string, before Files) error {
	after, err := h.Capture()
	if err != nil {
		return err
	}
	e := entry{label: label, before: Files{}, after: Files{}}
	for p := range union(before, after) {
		if same(before, after, p) {
			continue
		}
		e.paths = append(e.paths, p)
		if b, ok := before[p]; ok {
			e.before[p] = b
		}
		if b, ok := after[p]; ok {
			e.after[p] = b
		}
	}
	if len(e.paths) == 0 {
		return nil
	}
	sort.Strings(e.paths)
	h.undo = append(h.undo, e)
	if len(h.undo) > h.max {
		h.undo = h.undo[len(h.undo)-h.max:]
	}
	h.redo = nil
	return nil
}

// Undo restores the files of the latest step and returns its label.
func (h *History) Undo() (string, error) {
	if len(h.undo) == 0 {
		return "", ErrNothingToUndo
	}
	e := h.undo[len(h.undo)-1]
	if err := h.apply(e, e.after, e.before); err != nil {
		return "", err
	}
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, e)
	return e.label, nil
}

// Redo re-applies the latest undone step and returns its label.
func (h *History) Redo() (string, error) {
	if len(h.redo) == 0 {
		return "", ErrNothingToRedo
	}
	e := h.redo[len(h.redo)-1]
	if err := h.apply(e, e.before, e.after); err != nil {
		return "", err
	}
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, e)
	return e.label, nil
}

func (h *History) Clear() {
	h.undo, h.redo = nil, nil
}

func (h *History) State() State {
	var s State
	if n := len(h.undo); n > 0 {
		s.Undo = h.undo[n-1].label
	}
	if n := len(h.redo); n > 0 {
		s.Redo = h.redo[n-1].label
	}
	return s
}

// apply checks the step's files still hold `from`, then writes `to`. On any
// failure the history is cleared, since the stack no longer matches disk.
func (h *History) apply(e entry, from, to Files) error {
	cur := Files{}
	for _, p := range e.paths {
		b, err := os.ReadFile(h.abs(p))
		switch {
		case err == nil:
			cur[p] = b
		case !errors.Is(err, fs.ErrNotExist):
			h.Clear()
			return err
		}
		if !same(cur, from, p) {
			h.Clear()
			return ErrStale
		}
	}
	for _, p := range e.paths {
		var err error
		if b, ok := to[p]; ok {
			err = writeFile(h.abs(p), b)
		} else {
			err = h.remove(p)
		}
		if err != nil {
			h.Clear()
			return err
		}
	}
	return nil
}

func (h *History) abs(p string) string { return filepath.Join(h.dir, filepath.FromSlash(p)) }

// remove deletes a file and any directories it leaves empty.
func (h *History) remove(p string) error {
	if err := os.Remove(h.abs(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for d := filepath.Dir(h.abs(p)); d != h.dir && strings.HasPrefix(d, h.dir); d = filepath.Dir(d) {
		if os.Remove(d) != nil {
			break
		}
	}
	return nil
}

// writeFile writes atomically via a temp file and rename.
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func union(a, b Files) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for p := range a {
		out[p] = true
	}
	for p := range b {
		out[p] = true
	}
	return out
}

func same(a, b Files, p string) bool {
	x, okx := a[p]
	y, oky := b[p]
	return okx == oky && bytes.Equal(x, y)
}
