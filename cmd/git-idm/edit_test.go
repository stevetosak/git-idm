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

func runEdit(t *testing.T, answers []string, args ...string) string {
	t.Helper()
	old, oldExecutor := newPrompter, newExecutor
	t.Cleanup(func() { newPrompter, newExecutor = old, oldExecutor })
	newExecutor = func(cmd *cobra.Command) (*setup.Executor, error) {
		dir, _ := config.Dir()
		return &setup.Executor{ConfigDir: dir, Out: cmd.OutOrStdout()}, nil
	}
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		return setup.HuhPrompter{Accessible: true, In: &lineReader{lines: answers}, Out: &bytes.Buffer{}}, nil
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"edit"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

func TestEditMenuEndToEnd(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	out := runEdit(t, []string{"5", "3", "6", "n", "7"}, "work")
	if !strings.Contains(out, "updated work: color, default") {
		t.Fatalf("out:\n%s", out)
	}
	c, _ = config.Load()
	if c.Identities["work"].Color != "magenta" || c.Default != "" {
		t.Fatalf("config: default=%q identity=%+v", c.Default, c.Identities["work"])
	}
	generated, _ := os.ReadFile(filepath.Join(home, ".config", "git-idm", "generated", "work.gitconfig"))
	if !strings.Contains(string(generated), "w@x.example") {
		t.Fatalf("the edit must apply the git config:\n%s", generated)
	}

	if out := runEdit(t, []string{"8"}, "work"); !strings.Contains(out, "nothing was changed") {
		t.Fatalf("cancel:\n%s", out)
	}
	if out := runEdit(t, []string{"7"}, "work"); !strings.Contains(out, "nothing was changed") {
		t.Fatalf("done without changes:\n%s", out)
	}
}

func TestEditMenuSwitchesLoginAndAddsKey(t *testing.T) {
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	env := newLoginEnv(t, true)
	t.Setenv("LOGIN_AS", "jane")
	managed := filepath.Join(env.home, ".config", "git-idm", "gh", "solo")
	os.MkdirAll(managed, 0o700)
	os.WriteFile(filepath.Join(managed, "hosts.yml"), []byte("github.com:\n    user: solo-gh\n"), 0o600)
	c := loadConfig(t)
	c.Identities["solo"] = config.Identity{Name: "S", Email: "s@x.example", GHUser: "solo-gh", GHConfigDir: "~/.config/git-idm/gh/solo"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	withKeygen := newExecutor
	newExecutor = func(cmd *cobra.Command) (*setup.Executor, error) {
		ex, err := withKeygen(cmd)
		ex.Tools.SSHKeygen = keygen
		return ex, err
	}
	env.answers.lines = []string{
		"4", // GitHub
		"4", // use jane in ~/gh-p (keep, login, ~/gh, ~/gh-p, remove)
		"3", // SSH key
		"1", // generate
		"1", // no passphrase
		"",  // add the key to jane: yes
		"8", // Done
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"edit", "solo"})
	if err := root.Execute(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	id := loadConfig(t).Identities["solo"]
	if id.GHUser != "jane" || id.GHConfigDir != "~/gh-p" || id.SSHKey != "~/.ssh/git-idm_solo" || id.Protocol != config.ProtocolSSH {
		t.Fatalf("identity = %+v\n%s", id, out.String())
	}
	for _, want := range []string{"generated ~/.ssh/git-idm_solo", "updated solo: ssh key, GitHub", "logged out of solo-gh and deleted ~/.config/git-idm/gh/solo"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Error("the unused git-idm login must be deleted")
	}
	if _, err := os.Stat(filepath.Join(env.home, "gh-p", "hosts.yml")); err != nil {
		t.Error("the reused login must stay")
	}
	calls := env.calls()
	wantCalls := []string{
		"ssh-key add " + filepath.Join(env.home, ".ssh", "git-idm_solo.pub") + " --title git-idm solo in " + filepath.Join(env.home, "gh-p"),
		"auth logout --hostname github.com --user solo-gh in " + managed,
	}
	for _, want := range wantCalls {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %q in:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "auth login") || strings.Count(calls, "auth logout") != 1 {
		t.Errorf("only the unused login may be logged out:\n%s", calls)
	}
}
