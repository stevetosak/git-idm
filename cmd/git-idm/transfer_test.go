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
	"github.com/stevetosak/git-idm/internal/transfer"
)

func exportEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "Ana", Email: "ana@corp.com", SSHKey: "~/.ssh/id_work", GHUser: "ana-corp", GHConfigDir: "~/.config/gh-work"}
	c.Identities["personal"] = config.Identity{Name: "Ana", Email: "ana@mail.com"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func runExport(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"export"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestExportOneIdentity(t *testing.T) {
	dir := exportEnv(t)
	out, err := runExport(t, "work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "✓ exported work to work.git-idm.toml") {
		t.Errorf("unexpected output:\n%s", out)
	}
	f, err := transfer.ReadFile(filepath.Join(dir, "work.git-idm.toml"))
	if err != nil || len(f.Identities) != 1 || f.Identities[0].GitHubUser != "ana-corp" {
		t.Fatalf("got %+v, %v", f, err)
	}
	if info, _ := os.Stat(filepath.Join(dir, "work.git-idm.toml")); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	if _, err := runExport(t, "work"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("a second export must not overwrite, got %v", err)
	}
	if _, err := runExport(t, "work", "--force"); err != nil {
		t.Errorf("--force: %v", err)
	}
}

func TestExportAllToStdout(t *testing.T) {
	exportEnv(t)
	out, err := runExport(t, "--all", "-")
	if err != nil {
		t.Fatal(err)
	}
	f, err := transfer.Decode([]byte(out))
	if err != nil || len(f.Identities) != 2 {
		t.Fatalf("got %+v, %v\n%s", f, err, out)
	}
	if strings.Contains(out, "id_work") || strings.Contains(out, "gh-work") {
		t.Errorf("secrets in the export:\n%s", out)
	}
}

func TestExportArgs(t *testing.T) {
	exportEnv(t)
	cases := map[string][]string{
		"export needs <identity>":          {},
		"with --all, give only the file":   {"--all", "a", "b"},
		"identity \"nope\" does not exist": {"nope"},
	}
	for want, args := range cases {
		if _, err := runExport(t, args...); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: got %v, want %q", args, err, want)
		}
	}
}

func TestImportEndToEnd(t *testing.T) {
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
	c.Identities["work"] = config.Identity{Name: "Old", Email: "old@corp.example"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "ids.git-idm.toml")
	os.WriteFile(file, []byte(`format = "git-idm/identities"
version = 1

[[identity]]
id = "work"
name = "Jane Corp"
email = "jane@corp.example"
color = "red"
protocol = "https"

[[identity]]
id = "oss"
name = "Jane"
email = "jane@oss.example"
`), 0o600)

	answers := &lineReader{lines: []string{
		"",  // Import work? (1 of 2): yes
		"",  // Author name: from the file
		"",  // Email: from the file
		"",  // Identity name: work-2, because work exists
		"",  // Directory: skip
		"",  // SSH key: none, because the file says https
		"",  // Prompt color: from the file
		"",  // Create this identity: yes
		"n", // Import oss? (2 of 2): no
	}}
	var prompts bytes.Buffer
	oldPrompter, oldExecutor := newPrompter, newExecutor
	t.Cleanup(func() { newPrompter, newExecutor = oldPrompter, oldExecutor })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		return setup.HuhPrompter{Accessible: true, In: answers, Out: &prompts}, nil
	}
	newExecutor = func(cmd *cobra.Command) (*setup.Executor, error) {
		dir, _ := config.Dir()
		return &setup.Executor{ConfigDir: dir, Hostname: "test", In: strings.NewReader(""), Out: cmd.OutOrStdout()}, nil
	}

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"import", file})
	if err := root.Execute(); err != nil {
		t.Fatalf("%v\n%s\n--- prompts ---\n%s", err, out.String(), prompts.String())
	}
	c, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := c.Identities["work-2"]
	if got.Name != "Jane Corp" || got.Email != "jane@corp.example" || got.Color != "red" || got.SSHKey != "" {
		t.Fatalf("work-2 = %+v\n%s\n--- prompts ---\n%s", got, out.String(), prompts.String())
	}
	if _, ok := c.Identities["oss"]; ok {
		t.Error("oss was skipped and must not exist")
	}
	if c.Identities["work"].Email != "old@corp.example" {
		t.Error("the existing work identity must not change")
	}
	if !strings.Contains(out.String(), "skipped oss") {
		t.Errorf("missing skip message:\n%s", out.String())
	}
}

func TestImportRejectsBadFile(t *testing.T) {
	exportEnv(t)
	os.WriteFile("bad.toml", []byte("format = \"git-idm/identities\"\nversion = 1\n[[identity]]\nid = \"x\"\nname = \"A\"\nemail = \"a@b.c\"\nssh_key = \"~/.ssh/id\"\n"), 0o600)
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"import", "bad.toml"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("got %v", err)
	}
}
