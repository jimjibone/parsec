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
	b, err := s.CreateTask(Task{ProjectID: p.ID, Title: "second", Start: "2026-01-08", End: "2026-01-09", DependsOn: []string{a.ID}, Assignees: []string{person.ID}, Lane: 1, WeekendWork: true})
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
		if tk.ID == b.ID && (tk.Lane != 1 || !tk.WeekendWork || tk.DependsOn[0] != a.ID || tk.Assignees[0] != person.ID || tk.ProjectID != p.ID) {
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

func TestReorderProjects(t *testing.T) {
	s, dir := mustOpen(t)
	a, _ := s.CreateProject(Project{Name: "A"})
	b, _ := s.CreateProject(Project{Name: "B"})
	c, _ := s.CreateProject(Project{Name: "C"})
	names := func(st *Store) string {
		out := ""
		for _, p := range st.Snapshot().Projects {
			out += p.Name
		}
		return out
	}
	if got := names(s); got != "ABC" {
		t.Fatalf("initial order %q", got)
	}
	if _, err := s.ReorderProjects([]string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	// Order survives a reload and a full-object update.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.Name = "A2"
	a.Order = 99
	if _, err := s2.UpdateProject(a); err != nil {
		t.Fatal(err)
	}
	if got := names(s2); got != "CA2B" {
		t.Fatalf("after reorder %q", got)
	}
	var ve *ValidationError
	for _, ids := range [][]string{{a.ID, b.ID}, {a.ID, a.ID, b.ID}, {a.ID, b.ID, "nope"}} {
		if _, err := s2.ReorderProjects(ids); !errors.As(err, &ve) {
			t.Errorf("ids %v: err = %v", ids, err)
		}
	}
}

func TestPersonColor(t *testing.T) {
	s, _ := mustOpen(t)
	p, err := s.CreatePerson(Person{Name: "carol", Color: "#AABBCC"})
	if err != nil || p.Color != "#aabbcc" {
		t.Fatalf("create: %+v %v", p, err)
	}
	var ve *ValidationError
	p.Color = "red"
	if _, err := s.UpdatePerson(p); !errors.As(err, &ve) {
		t.Fatalf("bad colour accepted: %v", err)
	}
	p.Color = ""
	if got, err := s.UpdatePerson(p); err != nil || got.Color != "" {
		t.Fatalf("clear colour: %+v %v", got, err)
	}
}

func TestPersonHoursPerDay(t *testing.T) {
	s, dir := mustOpen(t)
	p, err := s.CreatePerson(Person{Name: "dave", HoursPerDay: 4.5})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Snapshot().People[0].HoursPerDay; got != 4.5 {
		t.Fatalf("reloaded hoursPerDay = %v", got)
	}
	var ve *ValidationError
	for _, h := range []float64{-1, 25} {
		p.HoursPerDay = h
		if _, err := s.UpdatePerson(p); !errors.As(err, &ve) {
			t.Fatalf("hoursPerDay %v accepted: %v", h, err)
		}
	}
}

func TestPlanningDefaults(t *testing.T) {
	s, dir := mustOpen(t)
	if got := s.Snapshot().Planning; got.HoursPerDay != 6 || got.AssigneeFactor != 1 || got.Version == "" {
		t.Fatalf("defaults = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "planning.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("planning.yaml written before any edit: %v", err)
	}
	// A partly written file keeps defaults for the missing keys.
	if err := os.WriteFile(filepath.Join(dir, "planning.yaml"), []byte("hoursPerDay: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().Planning; got.HoursPerDay != 5 || got.AssigneeFactor != 1 {
		t.Fatalf("partial file = %+v", got)
	}
}

func TestPlanningUpdate(t *testing.T) {
	s, dir := mustOpen(t)
	before := s.PlanningVersion()
	got, err := s.UpdatePlanning(Planning{HoursPerDay: 7.5, AssigneeFactor: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version == before || got.Version != s.PlanningVersion() {
		t.Fatalf("version %q, before %q, now %q", got.Version, before, s.PlanningVersion())
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// An explicit factor of 0 is a real value, not "unset".
	if p := s2.Snapshot().Planning; p.HoursPerDay != 7.5 || p.AssigneeFactor != 0 {
		t.Fatalf("reloaded = %+v", p)
	}

	var ve *ValidationError
	for _, bad := range []Planning{
		{HoursPerDay: 0, AssigneeFactor: 1},
		{HoursPerDay: -1, AssigneeFactor: 1},
		{HoursPerDay: 25, AssigneeFactor: 1},
		{HoursPerDay: 6, AssigneeFactor: -0.1},
		{HoursPerDay: 6, AssigneeFactor: 1.5},
	} {
		if _, err := s.UpdatePlanning(bad); !errors.As(err, &ve) {
			t.Fatalf("%+v accepted: %v", bad, err)
		}
	}
	if p := s.Snapshot().Planning; p.HoursPerDay != 7.5 {
		t.Fatalf("rejected update changed state: %+v", p)
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
