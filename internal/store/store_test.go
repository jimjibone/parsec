package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func mustOpen(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func mkTask(t *testing.T, s *Store, pid, title string, deps ...string) Task {
	t.Helper()
	tk, err := s.CreateTask(Task{ProjectID: pid, Title: title, Start: "2026-01-05", End: "2026-01-07", DependsOn: deps})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestRoundTrip(t *testing.T) {
	s, dir := mustOpen(t)
	p, err := s.CreateProject(Project{Name: "Alpha", Milestones: []Milestone{{Name: "Beta", Date: "2026-02-01"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Milestones[0].ID == "" {
		t.Fatal("milestone id not assigned")
	}
	person, err := s.CreatePerson(Person{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	a := mkTask(t, s, p.ID, "first")
	b, err := s.CreateTask(Task{ProjectID: p.ID, Title: "second", Start: "2026-01-08", End: "2026-01-09", DependsOn: []string{a.ID}, Assignees: []string{person.ID}, Lane: 1})
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "todo" {
		t.Fatalf("default status = %q", b.Status)
	}

	if _, err := os.Stat(filepath.Join(dir, "projects", p.ID, "tasks", b.ID+".yaml")); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := s2.Snapshot()
	if len(snap.Projects) != 1 || len(snap.Tasks) != 2 || len(snap.People) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	for _, tk := range snap.Tasks {
		if tk.ID == b.ID && (tk.Lane != 1 || tk.DependsOn[0] != a.ID || tk.Assignees[0] != person.ID || tk.ProjectID != p.ID) {
			t.Fatalf("reloaded task = %+v", tk)
		}
	}
}

func TestValidation(t *testing.T) {
	s, _ := mustOpen(t)
	p, _ := s.CreateProject(Project{Name: "P"})
	cases := []Task{
		{ProjectID: p.ID, Title: "", Start: "2026-01-01", End: "2026-01-01"},
		{ProjectID: "nope", Title: "x", Start: "2026-01-01", End: "2026-01-01"},
		{ProjectID: p.ID, Title: "x", Start: "2026-01-02", End: "2026-01-01"},
		{ProjectID: p.ID, Title: "x", Start: "bad", End: "2026-01-01"},
		{ProjectID: p.ID, Title: "x", Start: "2026-01-01", End: "2026-01-01", Status: "weird"},
		{ProjectID: p.ID, Title: "x", Start: "2026-01-01", End: "2026-01-01", Assignees: []string{"ghost"}},
		{ProjectID: p.ID, Title: "x", Start: "2026-01-01", End: "2026-01-01", DependsOn: []string{"ghost"}},
	}
	for i, c := range cases {
		var ve *ValidationError
		if _, err := s.CreateTask(c); !errors.As(err, &ve) {
			t.Errorf("case %d: err = %v, want ValidationError", i, err)
		}
	}
}

func TestCycleRejected(t *testing.T) {
	s, _ := mustOpen(t)
	p, _ := s.CreateProject(Project{Name: "P"})
	a := mkTask(t, s, p.ID, "a")
	b := mkTask(t, s, p.ID, "b", a.ID)
	c := mkTask(t, s, p.ID, "c", b.ID)

	a.DependsOn = []string{c.ID}
	var ve *ValidationError
	if _, err := s.UpdateTasks([]Task{a}); !errors.As(err, &ve) {
		t.Fatalf("cycle accepted: %v", err)
	}
	a.DependsOn = []string{a.ID}
	if _, err := s.UpdateTasks([]Task{a}); !errors.As(err, &ve) {
		t.Fatalf("self dependency accepted: %v", err)
	}
}

func TestDeleteCleansReferences(t *testing.T) {
	s, _ := mustOpen(t)
	p1, _ := s.CreateProject(Project{Name: "P1"})
	p2, _ := s.CreateProject(Project{Name: "P2"})
	person, _ := s.CreatePerson(Person{Name: "bob"})
	a := mkTask(t, s, p1.ID, "a")
	b, err := s.CreateTask(Task{ProjectID: p2.ID, Title: "b", Start: "2026-01-08", End: "2026-01-09", DependsOn: []string{a.ID}, Assignees: []string{person.ID}})
	if err != nil {
		t.Fatal(err)
	}

	changed, err := s.DeleteProject(p1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].ID != b.ID || len(changed[0].DependsOn) != 0 {
		t.Fatalf("changed = %+v", changed)
	}

	changed, err = s.DeletePerson(person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || len(changed[0].Assignees) != 0 {
		t.Fatalf("changed = %+v", changed)
	}
}

func TestMoveTaskBetweenProjects(t *testing.T) {
	s, dir := mustOpen(t)
	p1, _ := s.CreateProject(Project{Name: "P1"})
	p2, _ := s.CreateProject(Project{Name: "P2"})
	a := mkTask(t, s, p1.ID, "a")
	a.ProjectID = p2.ID
	if _, err := s.UpdateTasks([]Task{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", p1.ID, "tasks", a.ID+".yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old file still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", p2.ID, "tasks", a.ID+".yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestBatchUpdateIsAllOrNothing(t *testing.T) {
	s, _ := mustOpen(t)
	p, _ := s.CreateProject(Project{Name: "P"})
	a := mkTask(t, s, p.ID, "a")
	b := mkTask(t, s, p.ID, "b")
	a.Lane = 3
	b.Title = ""
	if _, err := s.UpdateTasks([]Task{a, b}); err == nil {
		t.Fatal("expected error")
	}
	for _, tk := range s.Snapshot().Tasks {
		if tk.ID == a.ID && tk.Lane != 0 {
			t.Fatal("partial update applied")
		}
	}
}
