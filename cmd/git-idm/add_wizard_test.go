package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

type lineReader struct{ lines []string }

func (r *lineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.lines[0]+"\n")
	r.lines = r.lines[1:]
	return n, nil
}

func TestAddWizardEndToEnd(t *testing.T) {
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
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	bin := filepath.Join(home, "fakebin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "gh"), []byte(`#!/bin/sh
case "$1 $2" in
  "auth login")
    echo "! First copy your one-time code: WXYZ-9876" >&2
    echo "Open this URL to continue in your web browser: https://github.com/login/device" >&2
    printf 'github.com:\n    user: jane-client\n' > "$GH_CONFIG_DIR/hosts.yml" ;;
  "api user") echo '{"login":"jane-client","id":9}' ;;
  "api user/keys") printf '[{"id":11,"key":"%s"}]' "$(cut -d' ' -f1,2 "$HOME/.ssh/git-idm_client.pub")" ;;
  "api --method") echo "$*" >> "$HOME/deleted" ;;
  "auth logout") echo "$*" >> "$HOME/loggedout" ;;
esac
`), 0o755)
	os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\necho 'Hi jane-client! You have authenticated.' >&2\nexit 1\n"), 0o755)

	answers := &lineReader{lines: []string{
		"Jane Doe",            // Author name
		"jane@client.example", // Email
		"",                    // Identity name: suggested "client"
		"3",                   // Directory: new folder here (in ~)
		"Clients",             // New folder name
		"1",                   // GitHub account: log in
		"1",                   // SSH key: generate
		"1",                   // Key passphrase: none
		"",                    // Add the key to GitHub: default yes
		"",                    // Prompt color: default
		"",                    // Create this identity: default yes
	}}
	var prompts bytes.Buffer
	oldPrompter, oldExecutor := newPrompter, newExecutor
	t.Cleanup(func() { newPrompter, newExecutor = oldPrompter, oldExecutor })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		return setup.HuhPrompter{Accessible: true, In: answers, Out: &prompts}, nil
	}
	newExecutor = func(cmd *cobra.Command) (*setup.Executor, error) {
		dir, _ := config.Dir()
		return &setup.Executor{
			Tools:     setup.Tools{GH: filepath.Join(bin, "gh"), SSHKeygen: keygen, SSH: filepath.Join(bin, "ssh"), Open: filepath.Join(bin, "open")},
			ConfigDir: dir, Hostname: "test", In: strings.NewReader(""), Out: cmd.OutOrStdout(),
		}, nil
	}

	add := newRootCmd()
	var out bytes.Buffer
	add.SetOut(&out)
	add.SetErr(&out)
	add.SetArgs([]string{"add"})
	if err := add.Execute(); err != nil {
		t.Fatalf("%v\n%s\n--- prompts ---\n%s", err, out.String(), prompts.String())
	}

	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	id := c.Identities["client"]
	if c.Default != "" || id.Name != "Jane Doe" || id.Email != "jane@client.example" || id.GHUser != "jane-client" || id.SigningKey != "" {
		t.Fatalf("config: default=%s identity=%+v", c.Default, id)
	}
	if len(c.Rules) != 1 || c.Rules[0] != (config.Rule{Path: "~/Clients/", Identity: "client"}) {
		t.Fatalf("rules = %+v", c.Rules)
	}
	repo := filepath.Join(home, "Clients", "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	cmd := exec.Command("git", "config", "user.email")
	cmd.Dir = repo
	if got, _ := cmd.Output(); strings.TrimSpace(string(got)) != "jane@client.example" {
		t.Fatalf("email in bound repo = %q", got)
	}

	root := newRootCmd()
	var editOut bytes.Buffer
	root.SetOut(&editOut)
	root.SetArgs([]string{"edit", "client", "--signing-key", "~/.ssh/git-idm_client.pub"})
	if err := root.Execute(); err != nil {
		t.Fatalf("edit: %v", err)
	}
	cmd = exec.Command("git", "config", "user.signingkey")
	cmd.Dir = repo
	if got, _ := cmd.Output(); strings.TrimSpace(string(got)) != "~/.ssh/git-idm_client.pub" {
		t.Fatalf("signingkey after edit = %q", got)
	}
	if !strings.Contains(editOut.String(), "gh ssh-key add ~/.ssh/git-idm_client.pub --type signing") {
		t.Errorf("edit must explain how to get Verified:\n%s", editOut.String())
	}
	root.SetArgs([]string{"edit", "client", "--signing-key", "~/.ssh/missing.pub"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("edit with a missing key file: %v", err)
	}
	for _, want := range []string{"Your one-time code: WXYZ-9876\n", "✓ logged in as jane-client", "✓ generated ~/.ssh/git-idm_client", "✓ created ~/Clients/", "✓ GitHub accepts the key as jane-client", "Repos under ~/Clients/ now use client"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}
	answers.lines = []string{
		"",  // Delete the SSH key: default yes
		"",  // Remove the key from GitHub: default yes
		"",  // Log out of gh: default yes
		"y", // Remove client: default no
	}
	root = newRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetArgs([]string{"rm", "client"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(); len(c.Identities) != 0 || len(c.Rules) != 0 || c.Default != "" {
		t.Fatalf("after rm: %+v", c)
	}
	for _, want := range []string{"✓ removed the key from jane-client on GitHub", "✓ deleted ~/.ssh/git-idm_client", "✓ logged out of gh (jane-client)", "✓ removed client from the config"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("rm output misses %q:\n%s", want, out.String())
		}
	}
	for _, gone := range []string{".ssh/git-idm_client", ".ssh/git-idm_client.pub", ".config/git-idm/gh/client"} {
		if _, err := os.Stat(filepath.Join(home, gone)); !os.IsNotExist(err) {
			t.Errorf("%s must be removed", gone)
		}
	}
	if deleted, _ := os.ReadFile(filepath.Join(home, "deleted")); !strings.Contains(string(deleted), "DELETE user/keys/11") {
		t.Errorf("GitHub key not deleted: %q", deleted)
	}
	if len(answers.lines) != 0 {
		t.Errorf("unused answers %v; prompts:\n%s", answers.lines, prompts.String())
	}
}
