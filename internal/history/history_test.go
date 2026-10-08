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

// step captures, runs fn and records it as one step.
func step(t *testing.T, h *History, label string, fn func()) {
	t.Helper()
	before, err := h.Capture()
	if err != nil {
		t.Fatal(err)
	}
	fn()
	if err := h.Record(label, before); err != nil {
		t.Fatal(err)
	}
}

func TestUndoRedo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "people.yaml", "v1")
	write(t, dir, ".git/HEAD", "ignored")
	h := New(dir, 10)

	step(t, h, "edit", func() { write(t, dir, "people.yaml", "v2") })
	step(t, h, "create", func() { write(t, dir, "projects/p1/tasks/t1.yaml", "task") })
	step(t, h, "noop", func() {})

	if got := h.State(); got.Undo != "create" || got.Redo != "" {
		t.Fatalf("state = %+v", got)
	}

	if l, err := h.Undo(); err != nil || l != "create" {
		t.Fatalf("undo = %q, %v", l, err)
	}
	if _, ok := read(t, dir, "projects/p1/tasks/t1.yaml"); ok {
		t.Fatal("created file not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects")); !os.IsNotExist(err) {
		t.Fatal("empty directories left behind")
	}
	if l, err := h.Undo(); err != nil || l != "edit" {
		t.Fatalf("undo = %q, %v", l, err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "v1" {
		t.Fatalf("people.yaml = %q", s)
	}
	if _, err := h.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("err = %v", err)
	}

	if l, err := h.Redo(); err != nil || l != "edit" {
		t.Fatalf("redo = %q, %v", l, err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "v2" {
		t.Fatalf("people.yaml = %q", s)
	}

	// A new change drops the redo stack.
	step(t, h, "delete", func() { os.Remove(filepath.Join(dir, "people.yaml")) })
	if got := h.State(); got.Undo != "delete" || got.Redo != "" {
		t.Fatalf("state = %+v", got)
	}
	if _, err := h.Undo(); err != nil {
		t.Fatal(err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "v2" {
		t.Fatalf("people.yaml = %q", s)
	}
}

func TestStaleClearsHistory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "people.yaml", "v1")
	h := New(dir, 10)
	step(t, h, "edit", func() { write(t, dir, "people.yaml", "v2") })
	write(t, dir, "people.yaml", "outside")

	if _, err := h.Undo(); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v", err)
	}
	if s, _ := read(t, dir, "people.yaml"); s != "outside" {
		t.Fatalf("people.yaml = %q", s)
	}
	if got := h.State(); got != (State{}) {
		t.Fatalf("state = %+v", got)
	}
}

func TestMax(t *testing.T) {
	dir := t.TempDir()
	h := New(dir, 2)
	for _, v := range []string{"a", "b", "c"} {
		step(t, h, v, func() { write(t, dir, "f", v) })
	}
	for _, want := range []string{"c", "b"} {
		if l, err := h.Undo(); err != nil || l != want {
			t.Fatalf("undo = %q, %v", l, err)
		}
	}
	if _, err := h.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("err = %v", err)
	}
}
