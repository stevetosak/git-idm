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

func initHome(t *testing.T) string {
	t.Helper()
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
	return home
}

func gitEmail(dir string) string {
	os.MkdirAll(dir, 0o755)
	exec.Command("git", "init", "-q", dir).Run()
	cmd := exec.Command("git", "config", "user.email")
	cmd.Dir = dir
	got, _ := cmd.Output()
	return strings.TrimSpace(string(got))
}

func TestInitReviewEndToEnd(t *testing.T) {
	home := initHome(t)

	out := runInit(t, []string{
		"1",    // personal: keep
		"2",    // work: rename
		"acme", // new name
		"",     // default: acme (was the global identity)
		"",     // save: yes
		"",     // remove the old setup: yes
	})
	for _, want := range []string{"✓ saved ~/.config/git-idm/config.toml", "✓ git config applied", "Removing the old setup",
		`done  include ~/.gitconfig: removed includeIf "gitdir:~/Personal/"`, "done  file    ~/.gitconfig-personal"} {
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
	global, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if strings.Contains(string(global), "gitconfig-personal") {
		t.Errorf("the old includeIf must be gone:\n%s", global)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig-personal")); !os.IsNotExist(err) {
		t.Error("the old identity file must be gone")
	}
	if backups, _ := filepath.Glob(filepath.Join(home, ".config", "git-idm", "backups", "*", "*")); len(backups) == 0 {
		t.Error("the old files must be backed up")
	}
	if got := gitEmail(filepath.Join(home, "Personal", "app")); got != "jane@home.example" {
		t.Errorf("~/Personal uses %s", got)
	}
	if got := gitEmail(filepath.Join(home, "Elsewhere", "app")); got != "jane@work.example" {
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

func TestInitKeepsOldSetupWhenAsked(t *testing.T) {
	home := initHome(t)
	out := runInit(t, []string{"1", "1", "", "", "n"})
	if !strings.Contains(out, "Remove it later with: git-idm migrate") || strings.Contains(out, "Removing") {
		t.Fatalf("out:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig-personal")); err != nil {
		t.Fatal("the old setup must stay")
	}
	if got := gitEmail(filepath.Join(home, "Personal", "app")); got != "jane@home.example" {
		t.Errorf("~/Personal uses %s", got)
	}
}

func TestInitAsksBeforeChangingEnvrc(t *testing.T) {
	home := initHome(t)
	t.Setenv("_GIT_IDM_SEGMENT", "")
	os.Unsetenv("_GIT_IDM_SEGMENT")
	os.MkdirAll(filepath.Join(home, "Personal"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "gh-p"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "gh-p", "hosts.yml"), []byte("github.com:\n    user: jane\n"), 0o600)
	envrc := filepath.Join(home, "Personal", ".envrc")
	os.WriteFile(envrc, []byte("export GH_CONFIG_DIR=~/.config/gh-p\nexport FOO=1\n"), 0o644)

	out := runInit(t, []string{"1", "1", "", "", "", ""})
	if !strings.Contains(out, "kept  envrc   ~/Personal/.envrc") {
		t.Fatalf("without the hook, the default must keep .envrc:\n%s", out)
	}
	if data, _ := os.ReadFile(envrc); !strings.Contains(string(data), "GH_CONFIG_DIR") {
		t.Fatalf(".envrc = %q", data)
	}
	c, _ := config.Load()
	if c.Identities["personal"].GHUser != "jane" {
		t.Fatalf("the gh login from .envrc must be imported: %+v", c.Identities["personal"])
	}
}

func TestWelcomeSuggestsInitForAnOldSetup(t *testing.T) {
	initHome(t)
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(nil)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Your git setup already has jane@home.example, jane@work.example.", "git-idm init  import them (recommended)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}
