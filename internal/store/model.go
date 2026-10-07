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
	Created     string      `yaml:"created,omitempty" json:"created"`
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
}

// Person can be assigned to tasks. Color is optional (#rrggbb); clients
// derive a stable default from the ID when it is empty.
type Person struct {
	ID    string `yaml:"id" json:"id"`
	Name  string `yaml:"name" json:"name"`
	Color string `yaml:"color,omitempty" json:"color"`
}

// Snapshot is the full database contents.
type Snapshot struct {
	Projects []Project `json:"projects"`
	Tasks    []Task    `json:"tasks"`
	People   []Person  `json:"people"`
}

var validStatuses = map[string]bool{
	"todo":    true,
	"doing":   true,
	"blocked": true,
	"done":    true,
}
