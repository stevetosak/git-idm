package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

func ghDefaultHome(t *testing.T, defaultUser string) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("XDG_CONFIG_HOME")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	os.Unsetenv("GIT_CONFIG_GLOBAL")
	if defaultUser != "" {
		os.MkdirAll(filepath.Join(home, ".config", "gh"), 0o700)
		os.WriteFile(filepath.Join(home, ".config", "gh", "hosts.yml"), []byte("github.com:\n    user: "+defaultUser+"\n"), 0o600)
	}
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/.config/gh", GHUser: "mdr"}
	c.Identities["martin"] = config.Identity{Name: "M", Email: "m@x.example", GHConfigDir: "~/.config/gh", GHUser: "mdr"}
	c.Identities["personal"] = config.Identity{Name: "P", Email: "p@x.example", GHConfigDir: "~/gh-p", GHUser: "me"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return home
}

func runIsolate(t *testing.T, answers []string, verify func(string) (string, error), dryRun bool) (string, error) {
	t.Helper()
	oldPrompter, oldVerify := newPrompter, verifyGHLogin
	t.Cleanup(func() { newPrompter, verifyGHLogin = oldPrompter, oldVerify })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		if answers == nil {
			return nil, errors.New("no terminal")
		}
		return setup.HuhPrompter{Accessible: true, In: &lineReader{lines: answers}, Out: &bytes.Buffer{}}, nil
	}
	verifyGHLogin = verify
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = isolateDefaultGH(&cobra.Command{}, &out, c, dryRun)
	return out.String(), err
}

func loggedInAs(user string) func(string) (string, error) {
	return func(string) (string, error) { return user, nil }
}

func TestIsolateDefaultGHMovesLoginsThenLogsOut(t *testing.T) {
	home := ghDefaultHome(t, "mdr")
	defaultHosts := filepath.Join(home, ".config", "gh", "hosts.yml")

	out, err := runIsolate(t, nil, loggedInAs("mdr"), false)
	if err != nil || !strings.Contains(out, "is logged in as mdr") || !strings.Contains(out, "Run git-idm migrate in a terminal") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(defaultHosts); err != nil || loadConfig(t).Identities["work"].GHConfigDir != "~/.config/gh" {
		t.Fatal("without a terminal nothing may change")
	}

	out, err = runIsolate(t, []string{"y"}, loggedInAs("MDR"), false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	c := loadConfig(t)
	for _, name := range []string{"work", "martin"} {
		want := "~/.config/git-idm/gh/" + name
		if c.Identities[name].GHConfigDir != want {
			t.Errorf("%s gh folder = %s, want %s", name, c.Identities[name].GHConfigDir, want)
		}
		if _, err := os.Stat(filepath.Join(config.ExpandHome(want), "hosts.yml")); err != nil {
			t.Errorf("%s: no copied login: %v", name, err)
		}
	}
	if c.Identities["personal"].GHConfigDir != "~/gh-p" {
		t.Errorf("personal must not change: %+v", c.Identities["personal"])
	}
	if _, err := os.Stat(defaultHosts); !os.IsNotExist(err) {
		t.Fatal("the default gh folder must be logged out")
	}
	backups, _ := filepath.Glob(filepath.Join(home, ".config", "git-idm", "backups", "*", "hosts.yml"))
	if len(backups) != 1 || !strings.Contains(out, "logged out ~/.config/gh (backup: ") {
		t.Fatalf("backup = %v\n%s", backups, out)
	}
}

func TestIsolateDefaultGHChangesNothingWhenACopyFails(t *testing.T) {
	home := ghDefaultHome(t, "mdr")
	calls := 0
	verify := func(dir string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("token not found")
		}
		return "mdr", nil
	}
	_, err := runIsolate(t, []string{"y"}, verify, false)
	if err == nil || !strings.Contains(err.Error(), "does not work (token not found), nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if loadConfig(t).Identities["work"].GHConfigDir != "~/.config/gh" {
		t.Fatal("config changed")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "gh", "hosts.yml")); err != nil {
		t.Fatal("the default login must stay")
	}
	if left, _ := filepath.Glob(filepath.Join(home, ".config", "git-idm", "gh", "*")); len(left) != 0 {
		t.Fatalf("copies must be removed: %v", left)
	}
}

func TestIsolateDefaultGHWithoutLogin(t *testing.T) {
	ghDefaultHome(t, "")
	out, err := runIsolate(t, nil, loggedInAs("x"), false)
	if err != nil || !strings.Contains(out, "log in: git-idm login work") {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := loadConfig(t).Identities["work"]; got.GHConfigDir != "~/.config/git-idm/gh/work" || got.GHUser != "" {
		t.Fatalf("work = %+v", got)
	}
}
