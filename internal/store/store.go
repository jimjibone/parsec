// Package store keeps the parsec database as YAML files in a directory
// (normally a git working tree). Layout:
//
//	people.yaml
//	projects/<projectID>/project.yaml
//	projects/<projectID>/tasks/<taskID>.yaml
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const dateLayout = "2006-01-02"

// now returns a fixed-width UTC timestamp so creation order sorts as text.
func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000Z") }

// ValidationError is returned when input is rejected.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = errors.New("not found")

type Store struct {
	dir string

	mu       sync.RWMutex
	projects map[string]*Project
	tasks    map[string]*Task
	people   []Person
}

func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload re-reads every file from disk, e.g. after a git pull.
func (s *Store) Reload() error {
	projects := map[string]*Project{}
	tasks := map[string]*Task{}
	var people []Person

	if err := readYAML(filepath.Join(s.dir, "people.yaml"), &people); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	projDir := filepath.Join(s.dir, "projects")
	entries, err := os.ReadDir(projDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid := e.Name()
		var p Project
		if err := readYAML(filepath.Join(projDir, pid, "project.yaml"), &p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		p.ID = pid
		projects[pid] = &p

		taskFiles, err := os.ReadDir(filepath.Join(projDir, pid, "tasks"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, tf := range taskFiles {
			name := tf.Name()
			if tf.IsDir() || !strings.HasSuffix(name, ".yaml") {
				continue
			}
			var t Task
			if err := readYAML(filepath.Join(projDir, pid, "tasks", name), &t); err != nil {
				return err
			}
			t.ID = strings.TrimSuffix(name, ".yaml")
			t.ProjectID = pid
			tasks[t.ID] = &t
		}
	}

	s.mu.Lock()
	s.projects, s.tasks, s.people = projects, tasks, people
	s.mu.Unlock()
	return nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		Projects: make([]Project, 0, len(s.projects)),
		Tasks:    make([]Task, 0, len(s.tasks)),
		People:   make([]Person, 0, len(s.people)),
	}
	for _, p := range s.people {
		snap.People = append(snap.People, normPerson(p))
	}
	for _, p := range s.projects {
		snap.Projects = append(snap.Projects, normProject(*p))
	}
	for _, t := range s.tasks {
		snap.Tasks = append(snap.Tasks, normTask(*t))
	}
	sort.Slice(snap.Projects, func(i, j int) bool {
		a, b := snap.Projects[i], snap.Projects[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if a.Created != b.Created {
			return a.Created < b.Created
		}
		return a.ID < b.ID
	})
	sort.Slice(snap.Tasks, func(i, j int) bool { return snap.Tasks[i].ID < snap.Tasks[j].ID })
	return snap
}

// normProject/normTask/normPerson make nil slices empty so JSON clients get
// [], and set Version.
func normProject(p Project) Project {
	if p.Milestones == nil {
		p.Milestones = []Milestone{}
	}
	p.Version = version(p)
	return p
}

func normTask(t Task) Task {
	if t.Assignees == nil {
		t.Assignees = []string{}
	}
	if t.DependsOn == nil {
		t.DependsOn = []string{}
	}
	t.Version = version(t, t.ProjectID)
	return t
}

func normPerson(p Person) Person {
	p.Version = version(p)
	return p
}

// version hashes an entity's stored fields (plus any extra identity, such as
// a task's project) into a short opaque string.
func version(v any, extra ...string) string {
	b, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	h := sha256.New()
	h.Write(b)
	for _, e := range extra {
		h.Write([]byte{0})
		h.Write([]byte(e))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// Versions of current entities, for conflict checks; ok is false if missing.

func (s *Store) TaskVersion(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return "", false
	}
	return normTask(*t).Version, true
}

func (s *Store) ProjectVersion(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.projects[id]
	if !ok {
		return "", false
	}
	return normProject(*p).Version, true
}

func (s *Store) PersonVersion(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := slices.IndexFunc(s.people, func(x Person) bool { return x.ID == id })
	if i < 0 {
		return "", false
	}
	return normPerson(s.people[i]).Version, true
}

// ---- projects ----

func (s *Store) CreateProject(p Project) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.ID = s.newID()
	p.Created = now()
	// New projects go last.
	p.Order = 0
	for _, x := range s.projects {
		p.Order = max(p.Order, x.Order+1)
	}
	if err := s.validateProject(&p); err != nil {
		return Project{}, err
	}
	if err := s.writeProject(&p); err != nil {
		return Project{}, err
	}
	s.projects[p.ID] = &p
	return normProject(p), nil
}

func (s *Store) UpdateProject(p Project) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.projects[p.ID]
	if !ok {
		return Project{}, ErrNotFound
	}
	p.Created = old.Created
	p.Order = old.Order // changed only via ReorderProjects
	if err := s.validateProject(&p); err != nil {
		return Project{}, err
	}
	if err := s.writeProject(&p); err != nil {
		return Project{}, err
	}
	s.projects[p.ID] = &p
	return normProject(p), nil
}

// ReorderProjects sets the display order. ids must list every project
// exactly once; only projects whose position changed are rewritten.
func (s *Store) ReorderProjects(ids []string) ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) != len(s.projects) {
		return nil, invalid("reorder must list all %d projects, got %d", len(s.projects), len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if _, ok := s.projects[id]; !ok {
			return nil, invalid("unknown project %q", id)
		}
		if seen[id] {
			return nil, invalid("project %q listed twice", id)
		}
		seen[id] = true
	}
	out := make([]Project, 0, len(ids))
	for i, id := range ids {
		p := s.projects[id]
		if p.Order != i {
			next := *p
			next.Order = i
			if err := s.writeProject(&next); err != nil {
				return nil, err
			}
			s.projects[id] = &next
		}
		out = append(out, normProject(*s.projects[id]))
	}
	return out, nil
}

// DeleteProject removes a project, its tasks, and any dependencies on them.
// It returns tasks in other projects that were modified as a result.
func (s *Store) DeleteProject(id string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[id]; !ok {
		return nil, ErrNotFound
	}
	if err := os.RemoveAll(s.projectDir(id)); err != nil {
		return nil, err
	}
	removed := map[string]bool{}
	for tid, t := range s.tasks {
		if t.ProjectID == id {
			removed[tid] = true
			delete(s.tasks, tid)
		}
	}
	delete(s.projects, id)
	return s.dropDependencies(removed)
}

func (s *Store) validateProject(p *Project) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return invalid("project name is required")
	}
	seen := map[string]bool{}
	for i := range p.Milestones {
		m := &p.Milestones[i]
		m.Name = strings.TrimSpace(m.Name)
		if m.ID == "" || seen[m.ID] {
			m.ID = s.newID()
		}
		seen[m.ID] = true
		if m.Name == "" {
			return invalid("milestone name is required")
		}
		if _, err := time.Parse(dateLayout, m.Date); err != nil {
			return invalid("milestone %q has invalid date %q", m.Name, m.Date)
		}
	}
	sort.SliceStable(p.Milestones, func(i, j int) bool { return p.Milestones[i].Date < p.Milestones[j].Date })
	return nil
}

// ---- tasks ----

func (s *Store) CreateTask(t Task) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = s.newID()
	t.Created = now()
	if err := s.validateTasks([]*Task{&t}); err != nil {
		return Task{}, err
	}
	if err := s.writeTask(&t); err != nil {
		return Task{}, err
	}
	s.tasks[t.ID] = &t
	return normTask(t), nil
}

// UpdateTasks replaces several tasks at once. All are validated before any is
// written, so lane re-packing on the timeline lands as a single change.
func (s *Store) UpdateTasks(in []Task) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ptrs := make([]*Task, len(in))
	seen := map[string]bool{}
	for i := range in {
		t := &in[i]
		old, ok := s.tasks[t.ID]
		if !ok {
			return nil, fmt.Errorf("task %s: %w", t.ID, ErrNotFound)
		}
		if seen[t.ID] {
			return nil, invalid("task %s listed twice", t.ID)
		}
		seen[t.ID] = true
		t.Created = old.Created
		ptrs[i] = t
	}
	if err := s.validateTasks(ptrs); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(in))
	for _, t := range ptrs {
		old := s.tasks[t.ID]
		if old.ProjectID != t.ProjectID {
			if err := os.Remove(s.taskPath(old.ProjectID, t.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		if err := s.writeTask(t); err != nil {
			return nil, err
		}
		s.tasks[t.ID] = t
		out = append(out, normTask(*t))
	}
	return out, nil
}

// DeleteTask removes a task and returns other tasks whose dependencies changed.
func (s *Store) DeleteTask(id string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	if err := os.Remove(s.taskPath(t.ProjectID, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	delete(s.tasks, id)
	return s.dropDependencies(map[string]bool{id: true})
}

func (s *Store) dropDependencies(removed map[string]bool) ([]Task, error) {
	var changed []Task
	for _, t := range s.tasks {
		n := slices.DeleteFunc(slices.Clone(t.DependsOn), func(d string) bool { return removed[d] })
		if len(n) == len(t.DependsOn) {
			continue
		}
		t.DependsOn = n
		if err := s.writeTask(t); err != nil {
			return changed, err
		}
		changed = append(changed, normTask(*t))
	}
	return changed, nil
}

// validateTasks checks the given (new or replacement) tasks against the
// current store state as if they had already been applied.
func (s *Store) validateTasks(ts []*Task) error {
	pending := map[string]*Task{}
	for _, t := range ts {
		pending[t.ID] = t
	}
	lookup := func(id string) *Task {
		if t, ok := pending[id]; ok {
			return t
		}
		return s.tasks[id]
	}
	people := map[string]bool{}
	for _, p := range s.people {
		people[p.ID] = true
	}

	for _, t := range ts {
		t.Title = strings.TrimSpace(t.Title)
		if t.Title == "" {
			return invalid("task title is required")
		}
		if _, ok := s.projects[t.ProjectID]; !ok {
			return invalid("task %q: unknown project %q", t.Title, t.ProjectID)
		}
		if t.Status == "" {
			t.Status = "todo"
		}
		if !validStatuses[t.Status] {
			return invalid("task %q: invalid status %q", t.Title, t.Status)
		}
		start, err := time.Parse(dateLayout, t.Start)
		if err != nil {
			return invalid("task %q: invalid start date %q", t.Title, t.Start)
		}
		end, err := time.Parse(dateLayout, t.End)
		if err != nil {
			return invalid("task %q: invalid end date %q", t.Title, t.End)
		}
		if end.Before(start) {
			return invalid("task %q: end is before start", t.Title)
		}
		if t.EstimateHours < 0 {
			return invalid("task %q: estimate cannot be negative", t.Title)
		}
		if t.Lane < 0 {
			t.Lane = 0
		}
		t.Assignees = uniq(t.Assignees)
		for _, a := range t.Assignees {
			if !people[a] {
				return invalid("task %q: unknown assignee %q", t.Title, a)
			}
		}
		t.DependsOn = uniq(t.DependsOn)
		for _, d := range t.DependsOn {
			if d == t.ID {
				return invalid("task %q cannot depend on itself", t.Title)
			}
			if lookup(d) == nil {
				return invalid("task %q: unknown dependency %q", t.Title, d)
			}
		}
	}

	// Cycle detection over the merged graph.
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var visit func(id string) error
	visit = func(id string) error {
		color[id] = grey
		for _, d := range lookup(id).DependsOn {
			switch color[d] {
			case grey:
				return invalid("dependency cycle involving %q", lookup(d).Title)
			case white:
				if err := visit(d); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	ids := make([]string, 0, len(s.tasks)+len(pending))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	for id := range pending {
		if _, ok := s.tasks[id]; !ok {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if color[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- people ----

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validatePerson(p *Person) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return invalid("person name is required")
	}
	p.Color = strings.ToLower(strings.TrimSpace(p.Color))
	if p.Color != "" && !hexColor.MatchString(p.Color) {
		return invalid("person colour must be #rrggbb, got %q", p.Color)
	}
	return nil
}

func (s *Store) CreatePerson(p Person) (Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validatePerson(&p); err != nil {
		return Person{}, err
	}
	p.ID = s.newID()
	people := append(slices.Clone(s.people), p)
	if err := s.writePeople(people); err != nil {
		return Person{}, err
	}
	s.people = people
	return normPerson(p), nil
}

func (s *Store) UpdatePerson(p Person) (Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validatePerson(&p); err != nil {
		return Person{}, err
	}
	i := slices.IndexFunc(s.people, func(x Person) bool { return x.ID == p.ID })
	if i < 0 {
		return Person{}, ErrNotFound
	}
	people := slices.Clone(s.people)
	people[i] = p
	if err := s.writePeople(people); err != nil {
		return Person{}, err
	}
	s.people = people
	return normPerson(p), nil
}

// DeletePerson removes a person and unassigns them; returns changed tasks.
func (s *Store) DeletePerson(id string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.people, func(x Person) bool { return x.ID == id })
	if i < 0 {
		return nil, ErrNotFound
	}
	people := slices.Delete(slices.Clone(s.people), i, i+1)
	if err := s.writePeople(people); err != nil {
		return nil, err
	}
	s.people = people
	var changed []Task
	for _, t := range s.tasks {
		n := slices.DeleteFunc(slices.Clone(t.Assignees), func(a string) bool { return a == id })
		if len(n) == len(t.Assignees) {
			continue
		}
		t.Assignees = n
		if err := s.writeTask(t); err != nil {
			return changed, err
		}
		changed = append(changed, normTask(*t))
	}
	return changed, nil
}

// ---- files ----

func (s *Store) projectDir(id string) string { return filepath.Join(s.dir, "projects", id) }

func (s *Store) taskPath(pid, tid string) string {
	return filepath.Join(s.projectDir(pid), "tasks", tid+".yaml")
}

func (s *Store) writeProject(p *Project) error {
	return writeYAML(filepath.Join(s.projectDir(p.ID), "project.yaml"), p)
}

func (s *Store) writeTask(t *Task) error { return writeYAML(s.taskPath(t.ProjectID, t.ID), t) }

func (s *Store) writePeople(p []Person) error {
	return writeYAML(filepath.Join(s.dir, "people.yaml"), p)
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeYAML writes atomically via a temp file and rename.
func writeYAML(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
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

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// newID returns a random 8 character id not used by any entity.
func (s *Store) newID() string {
	for {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		for i := range b {
			b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
		}
		id := string(b)
		_, p := s.projects[id]
		_, t := s.tasks[id]
		if !p && !t {
			return id
		}
	}
}

func uniq(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
