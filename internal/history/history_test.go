package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, p, s string) {
	t.Helper()
	if err := writeFile(filepath.Join(dir, p), []byte(s)); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, p string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, p))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// step captures, runs fn and records it as one of user's steps.
func step(t *testing.T, h *History, user, label string, fn func()) {
	t.Helper()
	before, err := h.Capture()
	if err != nil {
		t.Fatal(err)
	}
	fn()
	if _, err := h.Record(user, label, before); err != nil {
		t.Fatal(err)
	}
}

func TestUndoRedo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "people.yaml", "v1")
	write(t, dir, ".git/HEAD", "ignored")
	h := New(dir, 10)

	step(t, h, "alice", "edit", func() { write(t, dir, "people.yaml", "v2") })
	step(t, h, "alice", "create", func() { write(t, dir, "projects/p1/tasks/t1.yaml", "task") })
	step(t, h, "alice", "noop", func() {})

	if got := h.State("alice"); got.Undo != "create" || got.Redo != "" {
		t.Fatalf("state = %+v", got)
	}

	if s, err := h.Undo("alice"); err != nil || s.Label != "create" || len(s.Paths) != 1 {
		t.Fatalf("undo = %+v, %v", s, err)
	}
	if _, ok := read(t, dir, "projects/p1/tasks/t1.yaml"); ok {
		t.Fatal("created file not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects")); !os.IsNotExist(err) {
		t.Fatal("empty directories left behind")
	}
	if s, err := h.Undo("alice"); err != nil || s.Label != "edit" {
		t.Fatalf("undo = %+v, %v", s, err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "v1" {
		t.Fatalf("people.yaml = %q", s)
	}
	if _, err := h.Undo("alice"); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("err = %v", err)
	}

	if s, err := h.Redo("alice"); err != nil || s.Label != "edit" {
		t.Fatalf("redo = %+v, %v", s, err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "v2" {
		t.Fatalf("people.yaml = %q", s)
	}

	// A new change drops the redo stack.
	step(t, h, "alice", "delete", func() { os.Remove(filepath.Join(dir, "people.yaml")) })
	if got := h.State("alice"); got.Undo != "delete" || got.Redo != "" {
		t.Fatalf("state = %+v", got)
	}
	if _, err := h.Undo("alice"); err != nil {
		t.Fatal(err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "v2" {
		t.Fatalf("people.yaml = %q", s)
	}
}

func TestPerUser(t *testing.T) {
	dir := t.TempDir()
	h := New(dir, 10)
	step(t, h, "alice", "a1", func() { write(t, dir, "a", "1") })
	step(t, h, "bob", "b1", func() { write(t, dir, "b", "1") })
	step(t, h, "alice", "a2", func() { write(t, dir, "a", "2") })

	if got := h.State("bob"); got.Undo != "b1" {
		t.Fatalf("bob state = %+v", got)
	}
	// Bob undoes his own step only, even though Alice changed later.
	if s, err := h.Undo("bob"); err != nil || s.Label != "b1" {
		t.Fatalf("undo = %+v, %v", s, err)
	}
	if _, ok := read(t, dir, "b"); ok {
		t.Fatal("b not removed")
	}
	if v, _ := read(t, dir, "a"); v != "2" {
		t.Fatalf("a = %q", v)
	}
	// Alice's new change does not clear Bob's redo.
	step(t, h, "alice", "a3", func() { write(t, dir, "a", "3") })
	if got := h.State("bob"); got.Redo != "b1" || got.Undo != "" {
		t.Fatalf("bob state = %+v", got)
	}

	// Bob edits a file after Alice; Alice's step for it goes stale.
	step(t, h, "bob", "b2", func() { write(t, dir, "a", "bob") })
	if _, err := h.Undo("alice"); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v", err)
	}
	if v, _ := read(t, dir, "a"); v != "bob" {
		t.Fatalf("a = %q", v)
	}
	// Only the stale step was dropped.
	if got := h.State("alice"); got.Undo != "a2" {
		t.Fatalf("alice state = %+v", got)
	}
	if got := h.State("bob"); got.Undo != "b2" {
		t.Fatalf("bob state = %+v", got)
	}
}

func TestMax(t *testing.T) {
	dir := t.TempDir()
	h := New(dir, 2)
	for _, v := range []string{"a", "b", "c"} {
		step(t, h, "alice", v, func() { write(t, dir, "f", v) })
	}
	for _, want := range []string{"c", "b"} {
		if s, err := h.Undo("alice"); err != nil || s.Label != want {
			t.Fatalf("undo = %+v, %v", s, err)
		}
	}
	if _, err := h.Undo("alice"); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("err = %v", err)
	}
}
