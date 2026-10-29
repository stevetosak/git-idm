package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

func runMigrate(t *testing.T, answers []string, args ...string) (string, error) {
	t.Helper()
	old := newPrompter
	t.Cleanup(func() { newPrompter = old })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		if answers == nil {
			return nil, errors.New("no terminal")
		}
		return setup.HuhPrompter{Accessible: true, In: &lineReader{lines: answers}, Out: &bytes.Buffer{}}, nil
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"migrate"}, args...))
	err := root.Execute()
	return out.String(), err
}

func migrateBindHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_CONFIG_GLOBAL", "GIT_IDM_OVERRIDE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	c := config.New()
	c.Default = "work"
	c.Guard = config.GuardBlock
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@x.example"}
	c.Rules = []config.Rule{{Path: "~/bound/", Identity: "work"}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"bound/a", "code/x", "code/y", "solo"} {
		path := filepath.Join(home, dir)
		os.MkdirAll(path, 0o755)
		if out, err := exec.Command("git", "init", "-q", path).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
	}
	t.Chdir(home)
	return home
}

func TestMigrateBindsUnboundRepos(t *testing.T) {
	migrateBindHome(t)

	out, err := runMigrate(t, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"~/code/  2 repos", "~/solo/  1 repo", "git-idm bind work ~/code/", "git-idm bind work ~/solo/"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := rules(t); got != "~/bound/=work" {
		t.Fatalf("without a terminal nothing changes: %s", got)
	}

	if out, err := runMigrate(t, []string{"1", "1"}, "--dry-run"); err != nil || rules(t) != "~/bound/=work" {
		t.Fatalf("--dry-run must not bind: %v %s\n%s", err, rules(t), out)
	}

	out, err = runMigrate(t, []string{"2", "3"})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := rules(t); got != "~/bound/=work ~/code/=client" {
		t.Fatalf("rules = %s\n%s", got, out)
	}
	if !strings.Contains(out, "bound ~/code/ to client") {
		t.Errorf("output:\n%s", out)
	}
}
