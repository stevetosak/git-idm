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

func bindHome(t *testing.T, defaultName string) string {
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
	c.Default = defaultName
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@x.example"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(home, "code", "repo")
	os.MkdirAll(filepath.Join(repo, "src", "deep"), 0o755)
	os.MkdirAll(filepath.Join(home, "notes"), 0o755)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return home
}

func runBind(t *testing.T, dir string, answers []string, args ...string) (string, error) {
	t.Helper()
	old := newPrompter
	t.Cleanup(func() { newPrompter = old })
	newPrompter = func(*cobra.Command) (setup.Prompter, error) {
		if answers == nil {
			return nil, errors.New("no terminal")
		}
		return setup.HuhPrompter{Accessible: true, In: &lineReader{lines: answers}, Out: &bytes.Buffer{}}, nil
	}
	t.Chdir(dir)
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"bind"}, args...))
	err := root.Execute()
	return out.String(), err
}

func rules(t *testing.T) string {
	t.Helper()
	var out []string
	for _, r := range loadConfig(t).Rules {
		out = append(out, r.Path+"="+r.Identity)
	}
	return strings.Join(out, " ")
}

func TestBindWithoutArgumentsUsesRepoAndDefault(t *testing.T) {
	home := bindHome(t, "work")
	out, err := runBind(t, filepath.Join(home, "code", "repo", "src", "deep"), nil)
	if err != nil || !strings.Contains(out, "bound ~/code/repo/ to work") {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := rules(t); got != "~/code/repo/=work" {
		t.Fatalf("rules = %s", got)
	}
}

func TestBindPicksFromListWithDefaultFirst(t *testing.T) {
	home := bindHome(t, "work")
	if out, err := runBind(t, filepath.Join(home, "code", "repo"), []string{"1"}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := rules(t); got != "~/code/repo/=work" {
		t.Fatalf("first choice must be the default: %s", got)
	}
	if out, err := runBind(t, filepath.Join(home, "notes"), []string{"2"}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := rules(t); got != "~/code/repo/=work ~/notes/=client" {
		t.Fatalf("outside a repo the current folder is bound: %s", got)
	}
}

func TestBindIdentityOnlyUsesRepoRoot(t *testing.T) {
	home := bindHome(t, "")
	if out, err := runBind(t, filepath.Join(home, "code", "repo", "src"), nil, "client"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := rules(t); got != "~/code/repo/=client" {
		t.Fatalf("rules = %s", got)
	}
}

func TestBindWithoutDefaultNeedsIdentity(t *testing.T) {
	home := bindHome(t, "")
	_, err := runBind(t, filepath.Join(home, "code", "repo"), nil)
	if err == nil || !strings.Contains(err.Error(), "no default identity: git-idm bind <identity>") {
		t.Fatalf("err = %v", err)
	}
	if out, err := runBind(t, filepath.Join(home, "code", "repo"), []string{"2"}); err != nil || rules(t) != "~/code/repo/=work" {
		t.Fatalf("in a terminal the list still works: %v %s\n%s", err, rules(t), out)
	}
}

func TestBindWorktreeUsesMainRepo(t *testing.T) {
	home := bindHome(t, "work")
	main := filepath.Join(home, "code", "repo")
	wt := filepath.Join(home, "elsewhere", "wt")
	for _, args := range [][]string{
		{"-C", main, "-c", "user.name=X", "-c", "user.email=x@x.example", "commit", "-q", "--allow-empty", "--no-verify", "-m", "x"},
		{"-C", main, "worktree", "add", "-q", "--detach", wt},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}

	_, err := runBind(t, home, nil, "client", wt)
	if err == nil || !strings.Contains(err.Error(), "~/elsewhere/wt keeps its git data in ~/code/repo/.git/worktrees/wt, so a rule for it has no effect; bind ~/code/repo instead") {
		t.Fatalf("explicit worktree folder: %v", err)
	}
	out, err := runBind(t, wt, nil)
	if err != nil || !strings.Contains(out, "so git uses the rule of ~/code/repo") || !strings.Contains(out, "bound ~/code/repo/ to work") {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := rules(t); got != "~/code/repo/=work" {
		t.Fatalf("rules = %s", got)
	}
}
