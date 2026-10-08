package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// isolate keeps the user's git config out of tests and sets a fake identity.
func isolate(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "alice")
	t.Setenv("GIT_AUTHOR_EMAIL", "alice@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "alice")
	t.Setenv("GIT_COMMITTER_EMAIL", "alice@example.com")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitPushPull(t *testing.T) {
	isolate(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitT(t, t.TempDir(), "init", "--quiet", "--bare", "--initial-branch=main", remote)

	a, err := Open(filepath.Join(t.TempDir(), "a"))
	if err != nil {
		t.Fatal(err)
	}
	gitT(t, a.dir, "checkout", "--quiet", "-b", "main")
	if err := a.SetRemote(remote); err != nil {
		t.Fatal(err)
	}

	st, err := a.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.NoCommits || st.Branch != "main" || st.Remote != remote {
		t.Fatalf("status = %+v", st)
	}

	if r, err := a.Commit(""); err != nil || r.Output != "Nothing to commit." {
		t.Fatalf("empty commit: %+v %v", r, err)
	}
	write(t, filepath.Join(a.dir, "people.yaml"), "- id: x\n  name: bob\n")
	if st, _ := a.Status(); len(st.Files) != 1 {
		t.Fatalf("files = %+v", st.Files)
	}
	if _, err := a.Commit("add people"); err != nil {
		t.Fatal(err)
	}
	// Pull before the remote branch exists is a no-op with a hint.
	if _, err := a.Pull(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Push(); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.Status(); st.Upstream != "origin/main" || st.Ahead != 0 || len(st.Files) != 0 {
		t.Fatalf("after push: %+v", st)
	}

	// Second clone starts empty and adopts the remote branch on pull.
	b, err := Open(filepath.Join(t.TempDir(), "b"))
	if err != nil {
		t.Fatal(err)
	}
	gitT(t, b.dir, "checkout", "--quiet", "-b", "main")
	if err := b.SetRemote(remote); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Pull(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.dir, "people.yaml")); err != nil {
		t.Fatal("pull did not bring file:", err)
	}

	// Concurrent edits to different files sync cleanly.
	write(t, filepath.Join(b.dir, "b.yaml"), "b: 1\n")
	if _, err := b.Commit("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(a.dir, "a.yaml"), "a: 1\n")
	if _, err := a.Commit("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "b.yaml")); err != nil {
		t.Fatal("sync did not bring b.yaml:", err)
	}
	log, err := a.Log(10)
	if err != nil || len(log) != 3 {
		t.Fatalf("log = %+v %v", log, err)
	}
}

func TestCommitPaths(t *testing.T) {
	isolate(t)
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// First commit on an empty repo.
	write(t, filepath.Join(r.dir, "a.yaml"), "a: 1\n")
	write(t, filepath.Join(r.dir, "b.yaml"), "b: 1\n")
	if ok, err := r.CommitPaths([]string{"a.yaml", "gone.yaml"}, "Bob Sample <bob@example.com>", "bob edits"); err != nil || !ok {
		t.Fatalf("commit = %v %v", ok, err)
	}
	st, _ := r.Status()
	if len(st.Files) != 1 || st.Files[0].Path != "b.yaml" || st.Files[0].Code != "??" {
		t.Fatalf("files = %+v", st.Files)
	}

	// Deletion plus a new file in a subdirectory; b.yaml stays uncommitted.
	os.Remove(filepath.Join(r.dir, "a.yaml"))
	os.MkdirAll(filepath.Join(r.dir, "p", "tasks"), 0o755)
	write(t, filepath.Join(r.dir, "p", "tasks", "t.yaml"), "t: 1\n")
	if ok, err := r.CommitPaths([]string{"a.yaml", "p/tasks/t.yaml"}, "Bob Sample <bob@example.com>", "second"); err != nil || !ok {
		t.Fatalf("commit = %v %v", ok, err)
	}
	st, _ = r.Status()
	if len(st.Files) != 1 || st.Files[0].Path != "b.yaml" {
		t.Fatalf("files = %+v", st.Files)
	}
	log, _ := r.Log(5)
	if len(log) != 2 || log[0].Author != "Bob Sample" || log[0].Subject != "second" {
		t.Fatalf("log = %+v", log)
	}

	// Unchanged paths make no commit.
	if ok, err := r.CommitPaths([]string{"p/tasks/t.yaml"}, "Bob Sample <bob@example.com>", "none"); err != nil || ok {
		t.Fatalf("commit = %v %v", ok, err)
	}
}

func TestPushWithoutRemote(t *testing.T) {
	isolate(t)
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Push(); err == nil {
		t.Fatal("expected error")
	}
}
