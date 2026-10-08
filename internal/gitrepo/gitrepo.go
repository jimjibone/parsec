// Package gitrepo drives the system git binary against the data directory,
// so existing SSH keys and credential helpers apply to push and pull.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const timeout = 2 * time.Minute

type Repo struct{ dir string }

// FileChange is one entry from git status.
type FileChange struct {
	Path string `json:"path"`
	Code string `json:"code"` // two-letter porcelain XY code
}

type Status struct {
	Branch    string       `json:"branch"`
	Upstream  string       `json:"upstream"`
	Ahead     int          `json:"ahead"`
	Behind    int          `json:"behind"`
	Remote    string       `json:"remote"` // origin URL, empty if none
	Files     []FileChange `json:"files"`
	Rebasing  bool         `json:"rebasing"`
	NoCommits bool         `json:"noCommits"`
}

type Commit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	When    string `json:"when"`
	Subject string `json:"subject"`
}

// Result carries the combined git output of an operation for display.
type Result struct {
	Output string `json:"output"`
}

// Error wraps a failed git command with its output.
type Error struct {
	Args   []string
	Output string
	Err    error
}

func (e *Error) Error() string {
	out := strings.TrimSpace(e.Output)
	if out == "" {
		return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), out)
}

// Open initialises a repository in dir if it is not one already.
func Open(dir string) (*Repo, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Repo{dir: dir}
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := r.run("init", "--quiet"); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Repo) run(args ...string) (string, error) {
	return r.runEnv(nil, args...)
}

func (r *Repo) runEnv(env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.dir
	// Never block on an interactive credential prompt.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND="+sshCommand())
	cmd.Env = append(cmd.Env, env...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		return buf.String(), &Error{Args: args, Output: buf.String(), Err: err}
	}
	return buf.String(), nil
}

func sshCommand() string {
	if c := os.Getenv("GIT_SSH_COMMAND"); c != "" {
		return c
	}
	return "ssh -o BatchMode=yes"
}

var branchLine = regexp.MustCompile(`^## (?:No commits yet on |Initial commit on )?(\S+?)(?:\.\.\.(\S+))?(?: \[(.*)\])?$`)

func (r *Repo) Status() (Status, error) {
	out, err := r.run("status", "--porcelain=v1", "--branch", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	st := Status{Files: []FileChange{}}
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(line, "## ") {
			st.NoCommits = strings.HasPrefix(line, "## No commits yet") || strings.HasPrefix(line, "## Initial commit")
			if m := branchLine.FindStringSubmatch(line); m != nil {
				st.Branch, st.Upstream = m[1], m[2]
				for _, part := range strings.Split(m[3], ", ") {
					if n, ok := strings.CutPrefix(part, "ahead "); ok {
						st.Ahead, _ = strconv.Atoi(n)
					} else if n, ok := strings.CutPrefix(part, "behind "); ok {
						st.Behind, _ = strconv.Atoi(n)
					}
				}
			}
			continue
		}
		if len(line) > 3 {
			st.Files = append(st.Files, FileChange{Code: line[:2], Path: line[3:]})
		}
	}
	if url, err := r.run("remote", "get-url", "origin"); err == nil {
		st.Remote = strings.TrimSpace(url)
	}
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(r.dir, ".git", d)); err == nil {
			st.Rebasing = true
		}
	}
	return st, nil
}

func (r *Repo) Log(n int) ([]Commit, error) {
	st, err := r.Status()
	if err != nil {
		return nil, err
	}
	commits := []Commit{}
	if st.NoCommits {
		return commits, nil
	}
	out, err := r.run("log", "-n", strconv.Itoa(n), "--pretty=format:%h%x1f%an%x1f%ar%x1f%s")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) == 4 {
			commits = append(commits, Commit{Hash: f[0], Author: f[1], When: f[2], Subject: f[3]})
		}
	}
	return commits, nil
}

// Commit stages everything and commits. A clean tree is not an error.
func (r *Repo) Commit(message string) (Result, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Update via parsec"
	}
	if _, err := r.run("add", "-A"); err != nil {
		return Result{}, err
	}
	if _, err := r.run("diff", "--cached", "--quiet"); err == nil {
		return Result{Output: "Nothing to commit."}, nil
	}
	out, err := r.run("commit", "--quiet", "-m", message)
	if err != nil {
		return Result{}, err
	}
	if out == "" {
		out = "Committed: " + message
	}
	return Result{Output: out}, nil
}

// CommitPaths commits the working-tree state of only the given paths (relative
// to the repo, slash-separated), with `author` as "Name <email>". Other
// uncommitted changes stay uncommitted. It uses a temporary index, so it
// works for new and deleted files alike. Returns false when nothing changed.
func (r *Repo) CommitPaths(paths []string, author, message string) (bool, error) {
	if _, err := os.Stat(filepath.Join(r.dir, ".git", "rebase-merge")); err == nil {
		return false, errors.New("rebase in progress")
	}
	if _, err := os.Stat(filepath.Join(r.dir, ".git", "rebase-apply")); err == nil {
		return false, errors.New("rebase in progress")
	}
	_, headErr := r.run("rev-parse", "--verify", "--quiet", "HEAD")
	hasHead := headErr == nil

	// Drop paths that exist neither on disk nor in HEAD (created then
	// deleted before the commit); git add rejects them.
	var keep []string
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(r.dir, filepath.FromSlash(p))); err == nil {
			keep = append(keep, p)
		} else if hasHead {
			if _, err := r.run("cat-file", "-e", "HEAD:"+p); err == nil {
				keep = append(keep, p)
			}
		}
	}
	if len(keep) == 0 {
		return false, nil
	}

	idx, err := os.CreateTemp(filepath.Join(r.dir, ".git"), "parsec-index-*")
	if err != nil {
		return false, err
	}
	idx.Close()
	os.Remove(idx.Name()) // git wants to create it; an empty file is invalid
	defer os.Remove(idx.Name())
	env := []string{"GIT_INDEX_FILE=" + idx.Name()}

	if hasHead {
		if _, err := r.runEnv(env, "read-tree", "HEAD"); err != nil {
			return false, err
		}
	}
	if _, err := r.runEnv(env, append([]string{"add", "-A", "--"}, keep...)...); err != nil {
		return false, err
	}
	if hasHead {
		if _, err := r.runEnv(env, "diff", "--cached", "--quiet", "HEAD"); err == nil {
			return false, nil
		}
	}
	if _, err := r.runEnv(env, "commit", "--quiet", "--author="+author, "-m", message); err != nil {
		return false, err
	}
	// Bring the real index up to the new HEAD so the paths do not show as
	// staged reversions. parsec never keeps anything staged between calls.
	if _, err := r.run("reset", "--quiet"); err != nil {
		return true, err
	}
	return true, nil
}

// Pull rebases local commits onto the upstream branch. With no upstream it
// adopts origin/<branch> if that exists.
func (r *Repo) Pull() (Result, error) {
	st, err := r.Status()
	if err != nil {
		return Result{}, err
	}
	if st.Remote == "" {
		return Result{}, errors.New("no remote configured")
	}
	if st.Upstream == "" {
		if out, err := r.run("fetch", "origin"); err != nil {
			return Result{Output: out}, err
		}
		if _, err := r.run("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+st.Branch); err != nil {
			return Result{Output: "Remote branch origin/" + st.Branch + " does not exist yet. Push first."}, nil
		}
		if st.NoCommits {
			out, err := r.run("reset", "--hard", "origin/"+st.Branch)
			if err != nil {
				return Result{Output: out}, err
			}
		}
		if out, err := r.run("branch", "--set-upstream-to=origin/"+st.Branch); err != nil {
			return Result{Output: out}, err
		}
	}
	out, err := r.run("pull", "--rebase", "--autostash")
	return Result{Output: out}, err
}

// Push pushes the current branch, setting upstream on first push.
func (r *Repo) Push() (Result, error) {
	st, err := r.Status()
	if err != nil {
		return Result{}, err
	}
	if st.Remote == "" {
		return Result{}, errors.New("no remote configured")
	}
	if st.NoCommits {
		return Result{}, errors.New("nothing to push: no commits yet")
	}
	var out string
	if st.Upstream == "" {
		out, err = r.run("push", "--set-upstream", "origin", "HEAD")
	} else {
		out, err = r.run("push")
	}
	if strings.TrimSpace(out) == "" {
		out = "Pushed."
	}
	return Result{Output: out}, err
}

// Sync is pull followed by push.
func (r *Repo) Sync() (Result, error) {
	pull, err := r.Pull()
	if err != nil {
		return pull, err
	}
	push, err := r.Push()
	return Result{Output: strings.TrimSpace(pull.Output + "\n" + push.Output)}, err
}

func (r *Repo) AbortRebase() (Result, error) {
	out, err := r.run("rebase", "--abort")
	return Result{Output: out}, err
}

// SetRemote sets (or replaces) the origin URL.
func (r *Repo) SetRemote(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		_, err := r.run("remote", "remove", "origin")
		return err
	}
	if _, err := r.run("remote", "get-url", "origin"); err == nil {
		_, err := r.run("remote", "set-url", "origin", url)
		return err
	}
	_, err := r.run("remote", "add", "origin", url)
	return err
}
