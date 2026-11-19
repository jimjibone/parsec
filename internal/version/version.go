// Package version reports the commit parsec was built from and checks
// GitHub for newer commits on main, which is what the :latest image tracks.
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"sync"
	"time"
)

// Commit is the full git commit hash, set at build time when the source has
// no .git directory (as in the container build):
//
//	go build -ldflags "-X parsec/internal/version.Commit=$(git rev-parse HEAD)"
//
// Otherwise the Go toolchain's VCS stamp is used, when there is one.
var Commit string

// BuildDate is when the binary was built, RFC 3339 (e.g.
// 2026-11-19T21:30:00Z), set at build time like Commit. Empty when unknown.
var BuildDate string

// Repo is the GitHub repository published images are built from.
const Repo = "jimjibone/parsec"

// Branch is the branch the :latest image is built from.
const Branch = "main"

// Build describes the running binary.
type Build struct {
	Commit   string // full hash; empty when unknown
	Modified bool   // built from a working tree with uncommitted changes
	Built    string // build date, RFC 3339; empty when unknown
}

// Short is the abbreviated commit, or "dev" when it is unknown.
func (b Build) Short() string {
	if b.Commit == "" {
		return "dev"
	}
	s := b.Commit
	if len(s) > 7 {
		s = s[:7]
	}
	if b.Modified {
		s += "-dirty"
	}
	return s
}

// Current reads the build stamp.
func Current() Build {
	b := Build{Built: BuildDate}
	if Commit != "" {
		b.Commit = Commit
		return b
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Commit = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			}
		}
	}
	return b
}

// Update describes newer commits on Branch than the running build.
type Update struct {
	Behind    int       `json:"behind"`    // commits on Branch not in this build
	URL       string    `json:"url"`       // GitHub page comparing the two
	CheckedAt time.Time `json:"checkedAt"` // when GitHub was last asked
}

// Checker asks GitHub now and then whether Branch has moved past the build.
type Checker struct {
	build  Build
	base   string // GitHub API root; tests point it elsewhere
	client *http.Client

	mu     sync.Mutex
	update *Update // nil until a check finds newer commits
}

// NewChecker returns a checker for b. It does nothing until Run.
func NewChecker(b Build) *Checker {
	return &Checker{build: b, base: "https://api.github.com", client: &http.Client{Timeout: 30 * time.Second}}
}

// Build is the build being checked.
func (c *Checker) Build() Build { return c.build }

// Update returns the last check's result, or nil when the build is current
// or nothing is known yet.
func (c *Checker) Update() *Update {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.update == nil {
		return nil
	}
	u := *c.update
	return &u
}

// Run checks now and then every interval until ctx ends. Builds without a
// known, clean commit are never checked: there is nothing to compare.
func (c *Checker) Run(ctx context.Context, interval time.Duration) {
	if c.build.Commit == "" || c.build.Modified {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := c.check(ctx); err != nil && ctx.Err() == nil {
			log.Printf("parsec: update check: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Checker) check(ctx context.Context) error {
	u := fmt.Sprintf("%s/repos/%s/compare/%s...%s?per_page=1", c.base, Repo, url.PathEscape(c.build.Commit), Branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "parsec-update-check")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// 404: the commit is not on GitHub (a local or forked build).
		return fmt.Errorf("GitHub answered %s", res.Status)
	}
	var body struct {
		AheadBy int    `json:"ahead_by"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if body.AheadBy > 0 {
		c.update = &Update{Behind: body.AheadBy, URL: body.HTMLURL, CheckedAt: time.Now()}
	} else {
		c.update = nil
	}
	return nil
}
