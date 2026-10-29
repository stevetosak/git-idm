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
	t.Setenv(ghConfigEnv, "")
	os.Unsetenv(ghConfigEnv)
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/gh-work"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@x.example", GHConfigDir: "~/gh-client"}
	c.Identities["plain"] = config.Identity{Name: "P", Email: "p@x.example"}
	c.Rules = []config.Rule{{Path: "~/client/", Identity: "client"}, {Path: "~/plain/", Identity: "plain"}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"client/repo", "plain", "other"} {
		os.MkdirAll(filepath.Join(home, d), 0o700)
	}
	return home
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
		t.Setenv(ghConfigEnv, tc.inherited)
		if tc.inherited == "" {
			os.Unsetenv(ghConfigEnv)
		}
		t.Setenv("GIT_IDM_OVERRIDE", tc.override)
		environ := []string{"PATH=/bin"}
		if tc.inherited != "" {
			environ = append(environ, ghConfigEnv+"="+tc.inherited)
		}
		var stderr bytes.Buffer
		got := ghEnv(environ, &stderr)
		if dir := ghConfigDir(got); dir != tc.want {
			t.Errorf("%s inherited %q override %q: GH_CONFIG_DIR = %s, want %s", tc.dir, tc.inherited, tc.override, dir, tc.want)
		}
		if got[0] != "PATH=/bin" || stderr.Len() != 0 {
			t.Errorf("env = %v, stderr = %q", got, stderr.String())
		}
	}
}

func TestGHCommandPassesArguments(t *testing.T) {
	home := ghSandbox(t)
	real := filepath.Join(home, "tools", "gh")
	executable(t, real)
	t.Setenv("PATH", filepath.Dir(real))
	t.Chdir(filepath.Join(home, "client", "repo"))

	var gotPath string
	var gotArgs, gotEnv []string
	old := execGH
	t.Cleanup(func() { execGH = old })
	execGH = func(path string, args, env []string) error {
		gotPath, gotArgs, gotEnv = path, args, env
		return nil
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"gh", "pr", "create", "--title", "x y", "--help", "-R", "o/r"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotPath != real || strings.Join(gotArgs, "|") != "gh|pr|create|--title|x y|--help|-R|o/r" {
		t.Fatalf("exec %s %q", gotPath, gotArgs)
	}
	if ghConfigDir(gotEnv) != home+"/gh-client" || out.Len() != 0 {
		t.Fatalf("GH_CONFIG_DIR = %s, output = %q", ghConfigDir(gotEnv), out.String())
	}
}

func TestGHWithoutConfigStillRuns(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(home)
	var stderr bytes.Buffer
	env := ghEnv([]string{"GH_CONFIG_DIR=/keep"}, &stderr)
	if ghConfigDir(env) != "/keep" || !strings.Contains(stderr.String(), "gh runs without a git-idm identity") {
		t.Fatalf("env = %v, stderr = %q", env, stderr.String())
	}
}
