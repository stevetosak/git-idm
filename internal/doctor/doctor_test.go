package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/ghauth"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/resolve"
)

func setup(t *testing.T) (string, *config.Config, *gitgen.Applier) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	os.Unsetenv("XDG_CONFIG_HOME")
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", SSHKey: "~/.ssh/id_w", GHConfigDir: "~/gh", GHUser: "w-gh"}
	a := &gitgen.Applier{ConfigDir: filepath.Join(home, ".config", "git-idm"), GlobalConfig: filepath.Join(home, ".gitconfig"), Bin: "/bin/git-idm", Now: time.Now}
	return home, c, a
}

func find(checks []Check, substr string) *Check {
	for i := range checks {
		if strings.Contains(checks[i].Text, substr) {
			return &checks[i]
		}
	}
	return nil
}

func run(c *config.Config, a *gitgen.Applier, env map[string]string) []Check {
	_, hook := env["_GIT_IDM_SEGMENT"]
	return Run(c, a, resolve.Result{Identity: c.Default, Source: resolve.SourceDefault},
		Env{Getenv: func(k string) string { return env[k] }, HookHint: "hint", ShellHook: hook})
}

func TestDoctorFindsProblems(t *testing.T) {
	home, c, a := setup(t)
	checks := run(c, a, nil)
	for substr, level := range map[string]Level{
		"block is missing":     Fail,
		"ssh key ~/.ssh/id_w":  Fail,
		"gh is not logged in":  Fail,
		"shell hook is not":    Warn,
		"guard is off":         Info,
		"generated git config": Warn,
	} {
		ch := find(checks, substr)
		if ch == nil || ch.Level != level {
			t.Errorf("%q: got %+v, want level %d", substr, ch, level)
		}
	}

	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_w"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ch := find(run(c, a, nil), "can be read by other users"); ch == nil || ch.Level != Fail {
		t.Errorf("open key permissions: got %+v", ch)
	}

	if err := os.MkdirAll(filepath.Join(home, "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "gh", "hosts.yml"), []byte("github.com:\n    user: someone-else\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ch := find(run(c, a, nil), "config expects w-gh"); ch == nil || ch.Level != Fail {
		t.Errorf("wrong gh account: got %+v", ch)
	}
}

func TestDoctorHealthy(t *testing.T) {
	home, c, a := setup(t)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_w"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "gh", "hosts.yml"), []byte("github.com:\n    user: w-gh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Guard = config.GuardBlock
	if _, err := a.Apply(c); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"_GIT_IDM_SEGMENT": "x", "GH_CONFIG_DIR": filepath.Join(home, "gh")}
	for _, ch := range run(c, a, env) {
		if ch.Level != OK {
			t.Errorf("unexpected: %+v", ch)
		}
	}

	f, err := os.OpenFile(a.GlobalConfig, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[core]\n\thooksPath = /elsewhere\n")
	f.Close()
	checks := run(c, a, env)
	if ch := find(checks, "settings after the git-idm block"); ch == nil || ch.Level != Warn {
		t.Errorf("content after block: got %+v", ch)
	}
	if ch := find(checks, "core.hooksPath = /elsewhere"); ch == nil || ch.Level != Fail {
		t.Errorf("foreign hooksPath: got %+v", ch)
	}
}

func TestDoctorChecksTokens(t *testing.T) {
	home, c, a := setup(t)
	for dir, user := range map[string]string{"gh": "w-gh", "gh-p": "p-gh", "gh-x": "x-gh", "gh-o": "o-gh", "gh-n": "n-gh"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, dir, "hosts.yml"), []byte("github.com:\n    user: "+user+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/gh", GHUser: "w-gh"}
	c.Identities["work2"] = config.Identity{Name: "W", Email: "w2@x.example", GHConfigDir: "~/gh", GHUser: "w-gh"}
	c.Identities["personal"] = config.Identity{Name: "P", Email: "p@x.example", GHConfigDir: "~/gh-p", GHUser: "p-gh"}
	c.Identities["offline"] = config.Identity{Name: "X", Email: "x@x.example", GHConfigDir: "~/gh-x", GHUser: "x-gh"}
	c.Identities["other"] = config.Identity{Name: "O", Email: "o@x.example", GHConfigDir: "~/gh-o", GHUser: "o-gh"}
	c.Identities["noscope"] = config.Identity{Name: "N", Email: "n@x.example", GHConfigDir: "~/gh-n", GHUser: "n-gh"}

	var mu sync.Mutex
	calls := map[string]int{}
	fetch := func(dir string) (ghauth.Account, error) {
		mu.Lock()
		calls[filepath.Base(dir)]++
		mu.Unlock()
		switch filepath.Base(dir) {
		case "gh-p":
			return ghauth.Account{}, ghauth.ErrUnauthorized
		case "gh-x":
			return ghauth.Account{}, ghauth.ErrUnreachable
		case "gh-o":
			return ghauth.Account{Login: "someone-else"}, nil
		case "gh-n":
			return ghauth.Account{Login: "n-gh", Scopes: []string{"repo"}}, nil
		}
		return ghauth.Account{Login: "w-gh", Scopes: []string{"admin:public_key"}}, nil
	}
	checks := Run(c, a, resolve.Result{Identity: "work", Source: resolve.SourceDefault},
		Env{Getenv: func(string) string { return "" }, GHAccount: fetch})

	want := []struct {
		text  string
		level Level
	}{
		{"work: gh logged in as w-gh, the token works", OK},
		{"work2: gh logged in as w-gh, the token works", OK},
		{"personal: gh token for p-gh is invalid or revoked (~/gh-p)", Fail},
		{"offline: gh logged in as x-gh, but the token could not be checked: GitHub could not be reached", Warn},
		{"other: gh token belongs to someone-else, config expects o-gh", Fail},
		{"noscope: gh login cannot add SSH keys (no admin:public_key scope)", Info},
	}
	for _, w := range want {
		if ch := find(checks, w.text); ch == nil || ch.Level != w.level {
			t.Errorf("%q: got %+v", w.text, ch)
		}
	}
	if ch := find(checks, "invalid or revoked"); ch == nil || ch.Fix != "git-idm login personal" {
		t.Errorf("fix: %+v", ch)
	}
	if calls["gh"] != 1 || len(calls) != 5 {
		t.Errorf("each gh folder must be checked once: %v", calls)
	}
}

func gitRepo(t *testing.T, dir string, keys ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	args := [][]string{{"init", "-q"}}
	for i := 0; i+1 < len(keys); i += 2 {
		args = append(args, []string{"config", "--local", "--add", keys[i], keys[i+1]})
	}
	for _, a := range args {
		cmd := exec.Command("git", a...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
}

func TestDoctorFindsLocalKeysUnderRules(t *testing.T) {
	home, c, a := setup(t)
	c.Rules = []config.Rule{{Path: "~/Projects/", Identity: "work"}}
	other := filepath.Join(home, "Projects", "other")
	gitRepo(t, other, "user.name", "Martin", "user.name", "Martin", "user.email", "martin@old.example")
	gitRepo(t, filepath.Join(home, "Projects", "named"), "user.name", "W2")
	gitRepo(t, filepath.Join(home, "Projects", "allowed"), "user.email", "bot@ci.example", "git-idm.allowAuthor", "bot@ci.example")
	gitRepo(t, filepath.Join(home, "outside"), "user.email", "martin@old.example")

	res := resolve.Result{Identity: "work", Source: resolve.SourceRule, RepoRoot: other, LocalEmail: "martin@old.example"}
	checks := Run(c, a, res, Env{Getenv: func(string) string { return "" }})

	ch := find(checks, "~/Projects/other: .git/config sets user.name (2×), user.email, commits use martin@old.example, not w@x.example")
	if ch == nil || ch.Level != Warn || !strings.Contains(ch.Fix, "git-idm migrate") {
		t.Errorf("other: got %+v", ch)
	}
	if ch := find(checks, "~/Projects/named: .git/config sets user.name over the work rule"); ch == nil || ch.Fix != "git-idm migrate" {
		t.Errorf("named: got %+v", ch)
	}
	for _, substr := range []string{"~/Projects/allowed", "~/outside", "sets author"} {
		if ch := find(checks, substr); ch != nil {
			t.Errorf("%q must not be reported: %+v", substr, ch)
		}
	}
}
