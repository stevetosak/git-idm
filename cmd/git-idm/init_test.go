package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

func runInit(t *testing.T, answers []string) string {
	t.Helper()
	old := newPrompter
	t.Cleanup(func() { newPrompter = old })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		return setup.HuhPrompter{Accessible: true, In: &lineReader{lines: answers}, Out: &bytes.Buffer{}}, nil
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

func TestInitReviewEndToEnd(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL", "GIT_IDM_OVERRIDE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(`[user]
	name = Jane Work
	email = jane@work.example
[includeIf "gitdir:~/Personal/"]
	path = ~/.gitconfig-personal
`), 0o644)
	os.WriteFile(filepath.Join(home, ".gitconfig-personal"), []byte(`[user]
	name = Jane
	email = jane@home.example
`), 0o644)

	out := runInit(t, []string{
		"1",    // personal: keep
		"2",    // work: rename
		"acme", // new name
		"",     // default: acme (was the global identity)
		"",     // save: yes
	})
	for _, want := range []string{"✓ saved ~/.config/git-idm/config.toml", "✓ git config applied", "Remove it with: git-idm migrate"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Default != "acme" || c.Identities["acme"].Email != "jane@work.example" || len(c.Rules) != 1 || c.Rules[0].Identity != "personal" {
		t.Fatalf("config: default=%q identities=%v rules=%+v", c.Default, c.Names(), c.Rules)
	}
	email := func(dir string) string {
		os.MkdirAll(dir, 0o755)
		exec.Command("git", "init", "-q", dir).Run()
		cmd := exec.Command("git", "config", "user.email")
		cmd.Dir = dir
		got, _ := cmd.Output()
		return strings.TrimSpace(string(got))
	}
	if got := email(filepath.Join(home, "Personal", "app")); got != "jane@home.example" {
		t.Errorf("~/Personal uses %s", got)
	}
	if got := email(filepath.Join(home, "Elsewhere", "app")); got != "jane@work.example" {
		t.Errorf("elsewhere uses %s", got)
	}

	if out := runInit(t, nil); !strings.Contains(out, "git-idm is already set up") || !strings.Contains(out, "git-idm add") {
		t.Fatalf("second init:\n%s", out)
	}
}

func TestInitWithoutSetupSuggestsAdd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if out := runInit(t, nil); !strings.Contains(out, "No git identity found to import. Create one with: git-idm add") {
		t.Fatalf("out:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "git-idm")); !os.IsNotExist(err) {
		t.Fatal("init must not create files when nothing is imported")
	}
}
