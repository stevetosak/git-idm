package localid

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sandbox(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	return home
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func repo(t *testing.T, dir string, keys ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	for i := 0; i+1 < len(keys); i += 2 {
		git(t, dir, "config", "--local", "--add", keys[i], keys[i+1])
	}
}

func roots(res Result, base string) []string {
	var out []string
	for _, r := range res.Repos {
		rel, _ := filepath.Rel(base, r.Root)
		out = append(out, rel)
	}
	return out
}

func TestScanFindsLocalKeys(t *testing.T) {
	home := sandbox(t)
	root := filepath.Join(home, "Projects")
	repo(t, filepath.Join(root, "api"),
		"user.name", "Martin", "user.name", "Martin", "user.name", "Todor",
		"user.email", "martin@work.example", "core.sshCommand", "ssh -i ~/.ssh/id_martin")
	repo(t, filepath.Join(root, "clean"))
	repo(t, filepath.Join(root, "allowed"), "user.email", "bot@ci.example", "git-idm.allowAuthor", "bot@ci.example")
	repo(t, filepath.Join(root, "api", "lib", "nested"), "user.signingkey", "ABC123")
	repo(t, filepath.Join(root, "web", "node_modules", "pkg"), "user.email", "x@y.example")
	repo(t, filepath.Join(root, ".cache", "repo"), "user.email", "x@y.example")
	repo(t, filepath.Join(root, "a", "b", "c", "d", "too-deep"), "user.email", "x@y.example")
	repo(t, filepath.Join(root, "a", "b", "c", "at-limit"), "user.email", "x@y.example")

	res := Scan([]string{root, root, "relative/path"}, Options{})
	if res.Truncated {
		t.Fatal("scan was truncated")
	}
	got := strings.Join(roots(res, root), ",")
	want := strings.Join([]string{"a/b/c/at-limit", "allowed", "api", "api/lib/nested", "clean"}, ",")
	if got != want {
		t.Fatalf("repos = %s, want %s", got, want)
	}

	api := res.Repos[2]
	if api.Count("user.name") != 3 || api.Value("user.name") != "Todor" {
		t.Errorf("user.name count %d value %q", api.Count("user.name"), api.Value("user.name"))
	}
	if api.Value("user.email") != "martin@work.example" || api.Value("core.sshcommand") != "ssh -i ~/.ssh/id_martin" {
		t.Errorf("keys = %+v", api.Keys)
	}
	if api.Config != filepath.Join(root, "api", ".git", "config") {
		t.Errorf("config = %s", api.Config)
	}
	if res.Repos[1].Allowed != "bot@ci.example" {
		t.Errorf("allowed = %q", res.Repos[1].Allowed)
	}
	if res.Repos[3].Value("user.signingkey") != "ABC123" || res.Repos[3].Count("user.email") != 0 {
		t.Errorf("nested keys = %+v", res.Repos[3].Keys)
	}
	if res.Repos[4].Overrides() {
		t.Errorf("clean repo keys = %+v", res.Repos[4].Keys)
	}
}

func TestScanReadsWorktreeOnce(t *testing.T) {
	home := sandbox(t)
	root := filepath.Join(home, "code")
	main := filepath.Join(root, "main")
	repo(t, main, "user.email", "martin@work.example")
	git(t, main, "-c", "user.name=t", "-c", "user.email=t@t.example", "commit", "-q", "--no-verify", "--allow-empty", "-m", "x")
	git(t, main, "worktree", "add", "-q", "--detach", filepath.Join(root, "wt"))

	res := Scan([]string{filepath.Join(root, "wt"), root}, Options{})
	if len(res.Repos) != 1 {
		t.Fatalf("repos = %v", roots(res, root))
	}
	if res.Repos[0].Config != filepath.Join(main, ".git", "config") {
		t.Errorf("config = %s", res.Repos[0].Config)
	}
}

func TestScanExclude(t *testing.T) {
	home := sandbox(t)
	repo(t, filepath.Join(home, "Documents", "repo"), "user.email", "x@y.example")
	repo(t, filepath.Join(home, "src", "repo"), "user.email", "x@y.example")

	res := Scan([]string{home}, Options{Exclude: []string{filepath.Join(home, "Documents"), "Documents"}})
	if got := strings.Join(roots(res, home), ","); got != "src/repo" {
		t.Fatalf("repos = %s", got)
	}
}

func TestScanDeadline(t *testing.T) {
	home := sandbox(t)
	repo(t, filepath.Join(home, "src", "repo"), "user.email", "x@y.example")

	res := Scan([]string{home}, Options{Deadline: time.Nanosecond})
	if !res.Truncated || len(res.Repos) != 0 {
		t.Fatalf("truncated = %v, repos = %d", res.Truncated, len(res.Repos))
	}
}
