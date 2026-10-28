package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
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
