package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

const loginFakeGH = `#!/bin/sh
case "$1 $2" in
  "auth login")
    echo "$* in $GH_CONFIG_DIR" >> "$HOME/calls"
    [ -n "$LOGIN_FAIL" ] && { echo "authentication cancelled" >&2; exit 1; }
    echo "! First copy your one-time code: WXYZ-9876" >&2
    printf 'github.com:\n    user: %s\n' "$LOGIN_AS" > "$GH_CONFIG_DIR/hosts.yml" ;;
  "api user") printf '{"login":"%s","id":1}' "$LOGIN_AS" ;;
  "config get") if [ -f "$GH_CONFIG_DIR/protocol" ]; then cat "$GH_CONFIG_DIR/protocol"; fi ;;
  "config set") echo "$* in $GH_CONFIG_DIR" >> "$HOME/calls" ;;
  "auth logout") echo "$* in $GH_CONFIG_DIR" >> "$HOME/calls" ;;
esac
`

type loginEnv struct {
	home    string
	answers *lineReader
}

func newLoginEnv(t *testing.T, terminal bool) *loginEnv {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL", "GIT_IDM_OVERRIDE", "LOGIN_FAIL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	bin := filepath.Join(home, "fakebin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "gh"), []byte(loginFakeGH), 0o755)
	os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	for dir, user := range map[string]string{"gh": "jane-work", "gh-p": "jane"} {
		os.MkdirAll(filepath.Join(home, dir), 0o700)
		os.WriteFile(filepath.Join(home, dir, "hosts.yml"), []byte("github.com:\n    user: "+user+"\n"), 0o600)
		os.WriteFile(filepath.Join(home, dir, "config.yml"), []byte("editor: vim\n"), 0o600)
	}
	os.WriteFile(filepath.Join(home, "gh", "protocol"), []byte("ssh\n"), 0o600)

	c := config.New()
	c.Identities["work"] = config.Identity{Name: "J", Email: "j@work.example", GHConfigDir: "~/gh", GHUser: "jane-work", Protocol: config.ProtocolSSH}
	c.Identities["martin"] = config.Identity{Name: "M", Email: "m@work.example", GHConfigDir: "~/gh", GHUser: "jane-work"}
	c.Identities["personal"] = config.Identity{Name: "J", Email: "j@home.example", GHConfigDir: "~/gh-p", GHUser: "jane"}
	c.Identities["solo"] = config.Identity{Name: "J", Email: "j@solo.example"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	env := &loginEnv{home: home, answers: &lineReader{}}
	oldPrompter, oldExecutor := newPrompter, newExecutor
	t.Cleanup(func() { newPrompter, newExecutor = oldPrompter, oldExecutor })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		if !terminal {
			return nil, os.ErrInvalid
		}
		return setup.HuhPrompter{Accessible: true, In: env.answers, Out: &bytes.Buffer{}}, nil
	}
	newExecutor = func(cmd *cobra.Command) (*setup.Executor, error) {
		dir, _ := config.Dir()
		return &setup.Executor{
			Tools:     setup.Tools{GH: filepath.Join(bin, "gh"), Open: filepath.Join(bin, "open")},
			ConfigDir: dir, Out: cmd.OutOrStdout(),
		}, nil
	}
	return env
}

func (e *loginEnv) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"login"}, args...))
	err := root.Execute()
	return out.String(), err
}

func (e *loginEnv) calls() string {
	data, _ := os.ReadFile(filepath.Join(e.home, "calls"))
	return string(data)
}

func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoginAgainKeepsFolderAndSettings(t *testing.T) {
	env := newLoginEnv(t, false)
	t.Setenv("LOGIN_AS", "jane-work")
	out, err := env.run(t, "work")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Use the account jane-work", "also used by martin", "✓ martin, work uses the gh login jane-work again"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	calls := env.calls()
	if !strings.Contains(calls, "--git-protocol ssh") || !strings.Contains(calls, "--scopes admin:public_key") ||
		!strings.Contains(calls, "in "+filepath.Join(env.home, "gh")+"\n") || strings.Contains(calls, "logout") {
		t.Fatalf("calls:\n%s", calls)
	}
	if data, _ := os.ReadFile(filepath.Join(env.home, "gh", "config.yml")); string(data) != "editor: vim\n" {
		t.Fatalf("gh settings changed: %q", data)
	}
}

func TestLoginFailureChangesNothing(t *testing.T) {
	env := newLoginEnv(t, false)
	t.Setenv("LOGIN_FAIL", "1")
	_, err := env.run(t, "personal")
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(env.home, "gh-p", "hosts.yml")); !strings.Contains(string(data), "user: jane") {
		t.Fatalf("hosts.yml = %q", data)
	}
	if !strings.Contains(env.calls(), "--git-protocol https") {
		t.Fatalf("calls:\n%s", env.calls())
	}
}

func TestLoginOtherAccountWithoutConfirmLogsOutAgain(t *testing.T) {
	env := newLoginEnv(t, false)
	t.Setenv("LOGIN_AS", "someone-else")
	out, err := env.run(t, "personal")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "logged out of someone-else again, personal still uses jane") {
		t.Fatalf("out:\n%s", out)
	}
	if !strings.Contains(env.calls(), "auth logout --hostname github.com --user someone-else in "+filepath.Join(env.home, "gh-p")) {
		t.Fatalf("calls:\n%s", env.calls())
	}
	if got := loadConfig(t).Identities["personal"].GHUser; got != "jane" {
		t.Fatalf("gh user = %s", got)
	}
}

func TestLoginOtherAccountConfirmedUpdatesSharedIdentities(t *testing.T) {
	env := newLoginEnv(t, true)
	t.Setenv("LOGIN_AS", "jane-new")
	env.answers.lines = []string{"y"}
	out, err := env.run(t, "martin")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	c := loadConfig(t)
	if c.Identities["work"].GHUser != "jane-new" || c.Identities["martin"].GHUser != "jane-new" || c.Identities["personal"].GHUser != "jane" {
		t.Fatalf("identities = %+v", c.Identities)
	}
	if !strings.Contains(env.calls(), "auth logout --hostname github.com --user jane-work") {
		t.Fatalf("the old account must be logged out:\n%s", env.calls())
	}
}

func TestFirstLoginUsesExistingLogin(t *testing.T) {
	env := newLoginEnv(t, true)
	t.Setenv("LOGIN_AS", "jane")
	env.answers.lines = []string{"3"}
	out, err := env.run(t, "solo")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	id := loadConfig(t).Identities["solo"]
	if id.GHUser != "jane" || id.GHConfigDir != "~/gh-p" {
		t.Fatalf("identity = %+v\n%s", id, out)
	}
	generated, _ := os.ReadFile(filepath.Join(env.home, ".config", "git-idm", "generated", "solo.gitconfig"))
	if !strings.Contains(string(generated), "GH_CONFIG_DIR='"+filepath.Join(env.home, "gh-p")+"'") {
		t.Fatalf("generated config:\n%s", generated)
	}
	if strings.Contains(env.calls(), "auth login") {
		t.Fatalf("must not log in:\n%s", env.calls())
	}
}

func TestLoginNeedsAnIdentity(t *testing.T) {
	env := newLoginEnv(t, false)
	if _, err := env.run(t); err == nil || !strings.Contains(err.Error(), "git-idm login <identity>") {
		t.Fatalf("err = %v", err)
	}
	if _, err := env.run(t, "nope"); err == nil || !strings.Contains(err.Error(), `"nope" does not exist`) {
		t.Fatalf("err = %v", err)
	}
}
