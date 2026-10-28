package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
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
