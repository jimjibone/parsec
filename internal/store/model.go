package store

// Milestone is a named date shown on a project's swimlane.
type Milestone struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
	Date string `yaml:"date" json:"date"`
}

// Project groups tasks and owns a swimlane on the timeline.
type Project struct {
	ID          string      `yaml:"-" json:"id"`
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description,omitempty" json:"description"`
	Color       string      `yaml:"color,omitempty" json:"color"`
	Milestones  []Milestone `yaml:"milestones,omitempty" json:"milestones"`
	// Order is the display position (ascending); ties fall back to Created.
	Order   int    `yaml:"order,omitempty" json:"order"`
	Created string `yaml:"created,omitempty" json:"created"`
	// Version is a hash of the stored content, for conflict detection.
	Version string `yaml:"-" json:"version"`
}

// Task is a unit of work with a date range on the timeline.
// Start and End are inclusive calendar dates (YYYY-MM-DD). Unless
// WeekendWork is set, weekends inside the range are not working days.
type Task struct {
	ID            string   `yaml:"-" json:"id"`
	ProjectID     string   `yaml:"-" json:"projectId"`
	Title         string   `yaml:"title" json:"title"`
	Description   string   `yaml:"description,omitempty" json:"description"`
	Status        string   `yaml:"status" json:"status"`
	Start         string   `yaml:"start" json:"start"`
	End           string   `yaml:"end" json:"end"`
	WeekendWork   bool     `yaml:"weekendWork,omitempty" json:"weekendWork"`
	EstimateHours float64  `yaml:"estimateHours,omitempty" json:"estimateHours"`
	Assignees     []string `yaml:"assignees,omitempty" json:"assignees"`
	DependsOn     []string `yaml:"dependsOn,omitempty" json:"dependsOn"`
	Lane          int      `yaml:"lane" json:"lane"`
	Created       string   `yaml:"created,omitempty" json:"created"`
	Version       string   `yaml:"-" json:"version"`
}

// Person can be assigned to tasks. Color is optional (#rrggbb); clients
// derive a stable default from the ID when it is empty.
type Person struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Color string `yaml:"color,omitempty" json:"color"`
	// HoursPerDay overrides Planning.HoursPerDay for this person; 0 means
	// use the team default.
	HoursPerDay float64 `yaml:"hoursPerDay,omitempty" json:"hoursPerDay"`
	Version     string  `yaml:"-" json:"version"`
}

// Planning holds team-wide capacity settings used for task pressure.
type Planning struct {
	// HoursPerDay is the focused hours one person gives a task per working day.
	HoursPerDay float64 `yaml:"hoursPerDay" json:"hoursPerDay"`
	// AssigneeFactor is how much each assignee after the first adds (0..1);
	// 1 means work splits evenly, lower values allow for coordination overhead.
	AssigneeFactor float64 `yaml:"assigneeFactor" json:"assigneeFactor"`
	Version        string  `yaml:"-" json:"version"`
}

// DefaultPlanning applies when planning.yaml is absent or leaves a field out.
func DefaultPlanning() Planning {
	return Planning{HoursPerDay: 6, AssigneeFactor: 1}
}

// Snapshot is the full database contents.
type Snapshot struct {
	Projects []Project `json:"projects"`
	Tasks    []Task    `json:"tasks"`
	People   []Person  `json:"people"`
	Planning Planning  `json:"planning"`
}

var validStatuses = map[string]bool{
	"todo":    true,
	"doing":   true,
	"blocked": true,
	"done":    true,
}
