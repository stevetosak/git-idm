package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
)

func executable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindGHSkipsShimAndSelf(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "shim", "gh")
	executable(t, shim)
	self := filepath.Join(dir, "bin", "git-idm")
	executable(t, self)
	if err := os.Symlink(self, filepath.Join(dir, "bin", "gh")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "noexec"), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "noexec", "gh"), []byte("x"), 0o644)
	real := filepath.Join(dir, "real", "gh")
	executable(t, real)

	path := strings.Join([]string{"relative", filepath.Join(dir, "shim"), filepath.Join(dir, "bin"), filepath.Join(dir, "noexec"), filepath.Join(dir, "real")}, string(os.PathListSeparator))
	got, err := gitgen.FindGH(path, shim, self)
	if err != nil || got != real {
		t.Fatalf("findGH = %q, %v; want %q", got, err, real)
	}
	if _, err := gitgen.FindGH(filepath.Join(dir, "shim"), shim); err == nil {
		t.Fatal("want an error when only the shim is in PATH")
	}
}

func ghSandbox(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_IDM_OVERRIDE", "")
	t.Setenv(ghCheckEnv, "")
	for _, key := range append([]string{ghConfigEnv}, ghTokenEnvs...) {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/gh-work", GHUser: "w-gh"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@x.example", GHConfigDir: "~/gh-client", GHUser: "c-gh"}
	c.Identities["plain"] = config.Identity{Name: "P", Email: "p@x.example"}
	c.Identities["nouser"] = config.Identity{Name: "N", Email: "n@x.example", GHConfigDir: "~/gh-nouser"}
	c.Rules = []config.Rule{{Path: "~/client/", Identity: "client"}, {Path: "~/plain/", Identity: "plain"}, {Path: "~/nouser/", Identity: "nouser"}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"client/repo", "plain", "nouser", "other"} {
		os.MkdirAll(filepath.Join(home, d), 0o700)
	}
	ghLogin(t, home, "gh-work", "w-gh")
	ghLogin(t, home, "gh-client", "c-gh")
	ghLogin(t, home, "gh-nouser", "n-gh")
	return home
}

func ghLogin(t *testing.T, home, dir, user string) {
	t.Helper()
	os.MkdirAll(filepath.Join(home, dir), 0o700)
	content := ""
	if user != "" {
		content = "github.com:\n    user: " + user + "\n"
	}
	if err := os.WriteFile(filepath.Join(home, dir, "hosts.yml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ghConfigDir(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, ghConfigEnv+"="); ok {
			return v
		}
	}
	return "<unset>"
}

func TestGHEnvFollowsFolder(t *testing.T) {
	home := ghSandbox(t)
	cases := []struct {
		dir, inherited, override, want string
	}{
		{"client/repo", home + "/gh-work", "", home + "/gh-client"},
		{"client/repo", "", "", home + "/gh-client"},
		{"other", home + "/gh-client", "", home + "/gh-work"},
		{"plain", home + "/gh-work", "", "<unset>"},
		{"client/repo", "/my/own/gh", "", "/my/own/gh"},
		{"client/repo", home + "/gh-client", "work", home + "/gh-work"},
	}
	for _, tc := range cases {
		t.Chdir(filepath.Join(home, tc.dir))
		t.Setenv("GIT_IDM_OVERRIDE", tc.override)
		environ := []string{"PATH=/bin"}
		if tc.inherited != "" {
			environ = append(environ, ghConfigEnv+"="+tc.inherited)
		}
		got, _ := ghEnv(environ)
		if dir := ghConfigDir(got); dir != tc.want {
			t.Errorf("%s inherited %q override %q: GH_CONFIG_DIR = %s, want %s", tc.dir, tc.inherited, tc.override, dir, tc.want)
		}
		if got[0] != "PATH=/bin" {
			t.Errorf("env = %v", got)
		}
	}
}

type ghRun struct {
	ran  bool
	args []string
	env  []string
}

func runGH(t *testing.T, args ...string) (*ghRun, error) {
	t.Helper()
	real := filepath.Join(os.Getenv("HOME"), "tools", "gh")
	executable(t, real)
	t.Setenv("PATH", filepath.Dir(real))
	run := &ghRun{}
	old := execGH
	t.Cleanup(func() { execGH = old })
	execGH = func(path string, argv, env []string) error {
		if path != real {
			t.Errorf("exec %s, want %s", path, real)
		}
		run.ran, run.args, run.env = true, argv, env
		return nil
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"gh"}, args...))
	err := root.Execute()
	if out.Len() != 0 {
		t.Errorf("git-idm gh printed %q", out.String())
	}
	return run, err
}

func TestGHCommandPassesArguments(t *testing.T) {
	home := ghSandbox(t)
	t.Chdir(filepath.Join(home, "client", "repo"))
	run, err := runGH(t, "pr", "create", "--title", "x y", "--help", "-R", "o/r")
	if err != nil || !run.ran {
		t.Fatalf("ran=%v err=%v", run.ran, err)
	}
	if strings.Join(run.args, "|") != "gh|pr|create|--title|x y|--help|-R|o/r" || ghConfigDir(run.env) != home+"/gh-client" {
		t.Fatalf("args %q GH_CONFIG_DIR %s", run.args, ghConfigDir(run.env))
	}
}

func TestGHFailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		dir   string
		setup func(t *testing.T, home string)
		args  []string
		want  string
	}{
		{name: "right login", dir: "client/repo", args: []string{"pr", "list"}},
		{name: "login compare ignores case", dir: "client/repo", setup: func(t *testing.T, home string) { ghLogin(t, home, "gh-client", "C-GH") }, args: []string{"api", "user"}},
		{name: "wrong login", dir: "client/repo", setup: func(t *testing.T, home string) { ghLogin(t, home, "gh-client", "martin-gh") },
			args: []string{"pr", "list"}, want: "gh is logged in as martin-gh, but client uses c-gh, so gh stops here\n  fix:       git-idm login client"},
		{name: "wrong login on api write", dir: "client/repo", setup: func(t *testing.T, home string) { ghLogin(t, home, "gh-client", "martin-gh") },
			args: []string{"api", "-X", "POST", "repos/o/r/issues"}, want: "gh is logged in as martin-gh"},
		{name: "not logged in", dir: "client/repo", setup: func(t *testing.T, home string) { ghLogin(t, home, "gh-client", "") },
			args: []string{"repo", "fork"}, want: "gh is not logged in for client (~/gh-client)"},
		{name: "identity without gh", dir: "plain", args: []string{"pr", "create"}, want: "plain has no gh login"},
		{name: "identity without gh_user", dir: "nouser", args: []string{"pr", "view"}, want: "nouser has no gh_user"},
		{name: "token in env", dir: "client/repo", setup: func(t *testing.T, home string) { t.Setenv("GH_TOKEN", "secret") },
			args: []string{"pr", "create"}, want: "GH_TOKEN is set: gh would use that token, not the gh login of client"},
		{name: "github token in env", dir: "client/repo", setup: func(t *testing.T, home string) { t.Setenv("GITHUB_TOKEN", "secret") },
			args: []string{"issue", "list"}, want: "GITHUB_TOKEN is set"},
		{name: "own GH_CONFIG_DIR", dir: "client/repo", setup: func(t *testing.T, home string) { t.Setenv(ghConfigEnv, "/my/own/gh") }, args: []string{"pr", "create"}},
		{name: "skip once", dir: "plain", setup: func(t *testing.T, home string) { t.Setenv(ghCheckEnv, "0") }, args: []string{"pr", "create"}},
		{name: "auth is never checked", dir: "plain", args: []string{"auth", "login"}},
		{name: "version is never checked", dir: "plain", args: []string{"--version"}},
		{name: "no identity", dir: "other", setup: func(t *testing.T, home string) {
			c, _ := config.Load()
			c.Default = ""
			c.Save()
		}, args: []string{"pr", "list"}, want: "no git-idm identity uses this folder"},
		{name: "broken config", dir: "other", setup: func(t *testing.T, home string) {
			os.WriteFile(filepath.Join(home, ".config", "git-idm", "config.toml"), []byte("not toml ["), 0o600)
		}, args: []string{"pr", "list"}, want: "git-idm cannot read its config"},
		{name: "no config", dir: "other", setup: func(t *testing.T, home string) {
			os.Remove(filepath.Join(home, ".config", "git-idm", "config.toml"))
		}, args: []string{"pr", "list"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := ghSandbox(t)
			if tc.setup != nil {
				tc.setup(t, home)
			}
			t.Chdir(filepath.Join(home, tc.dir))
			run, err := runGH(t, tc.args...)
			if tc.want == "" {
				if err != nil || !run.ran {
					t.Fatalf("want gh to run: ran=%v err=%v", run.ran, err)
				}
				return
			}
			if err == nil || run.ran || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want refusal %q: ran=%v err=%v", tc.want, run.ran, err)
			}
			if !strings.Contains(err.Error(), "skip once: GIT_IDM_GH_CHECK=0 gh "+tc.args[0]) {
				t.Errorf("no skip hint: %v", err)
			}
		})
	}
}
