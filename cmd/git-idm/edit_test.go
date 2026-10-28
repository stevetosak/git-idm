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

func runEdit(t *testing.T, answers []string, args ...string) string {
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

	out := runEdit(t, []string{"3", "3", "4", "n", "5"}, "work")
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

	if out := runEdit(t, []string{"6"}, "work"); !strings.Contains(out, "nothing was changed") {
		t.Fatalf("cancel:\n%s", out)
	}
	if out := runEdit(t, []string{"5"}, "work"); !strings.Contains(out, "nothing was changed") {
		t.Fatalf("done without changes:\n%s", out)
	}
}
