package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/resolve"
	"github.com/stevetosak/git-idm/internal/ui"
)

func uiConfig() *config.Config {
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "Jane Doe", Email: "jane@work.example", GHUser: "jane-work", GHConfigDir: "~/.config/gh"}
	c.Identities["personal"] = config.Identity{Name: "Jane Doe", Email: "jane@home.example", SSHKey: "~/.ssh/id_p", GHUser: "jane", GHConfigDir: "~/.config/gh-p", Color: "green"}
	c.Identities["spare"] = config.Identity{Name: "Jane Doe", Email: "jane@spare.example"}
	c.Rules = []config.Rule{{Path: "~/Personal/", Identity: "personal"}}
	return c
}

func plain() *ui.Theme { return ui.New(&bytes.Buffer{}) }

func uiEnv(t *testing.T) {
	t.Setenv("HOME", "/h")
	t.Setenv("GH_CONFIG_DIR", "/h/.config/gh-p")
	t.Setenv("_GIT_IDM_SEGMENT", "x")
}

func TestStatusViewRule(t *testing.T) {
	uiEnv(t)
	c := uiConfig()
	res := resolve.Result{Identity: "personal", Source: resolve.SourceRule, Rule: &c.Rules[0], RuleIdentity: "personal", RepoRoot: "/h/Personal/app"}
	want := `╭────────────────────────────────╮
│ ● personal                     │
│   Jane Doe <jane@home.example> │
│                                │
│   why   rule ~/Personal/       │
│   repo  ~/Personal/app         │
│   ssh   ~/.ssh/id_p            │
│   gh    jane                   │
╰────────────────────────────────╯
  add · edit · rm · ls · help`
	if got := statusView(plain(), c, res); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStatusViewOverrideAndNotes(t *testing.T) {
	uiEnv(t)
	c := uiConfig()
	res := resolve.Result{Identity: "work", Source: resolve.SourceOverride, Rule: &c.Rules[0], RuleIdentity: "personal",
		RepoRoot: "/h/Personal/app", LocalUser: "Martin", LocalEmail: "m@x.example"}
	got := statusView(plain(), c, res)
	for _, want := range []string{"● work  override", "override in this shell, the rule says personal", "clear: git-idm use --clear",
		"! gh uses ~/.config/gh-p here, expected ~/.config/gh; open a new shell"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Martin") {
		t.Errorf("an override wins over the repo author, so no note:\n%s", got)
	}
}

func TestStatusViewDefaultNoneAndWelcome(t *testing.T) {
	uiEnv(t)
	t.Setenv("GH_CONFIG_DIR", "/h/.config/gh")
	c := uiConfig()
	got := statusView(plain(), c, resolve.Result{Identity: "work", Source: resolve.SourceDefault, RepoRoot: "/h/x", LocalUser: "Martin", LocalEmail: "m@x.example"})
	for _, want := range []string{"why   ★ default (no rule matches here)", "! commits here use Martin <m@x.example> from .git/config, not work"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	got = statusView(plain(), c, resolve.Result{Source: resolve.SourceNone})
	if !strings.Contains(got, "○ no identity here") || !strings.Contains(got, "git-idm bind <identity> .") {
		t.Errorf("no identity:\n%s", got)
	}
	if got := welcome(plain()); !strings.Contains(got, "No identities yet.") || !strings.Contains(got, "git-idm add") {
		t.Errorf("welcome:\n%s", got)
	}
}

func TestListView(t *testing.T) {
	uiEnv(t)
	want := `   IDENTITY  AUTHOR                         GITHUB     USED IN
●  personal  Jane Doe <jane@home.example>   jane       ~/Personal/
   spare     Jane Doe <jane@spare.example>  -          nowhere (git-idm bind spare <dir>)
   work      Jane Doe <jane@work.example>   jane-work  ★ default

  ● active here  ★ default for folders without a rule
  add · edit · rm · ls · help`
	if got := listView(plain(), uiConfig(), "personal", false); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	long := listView(plain(), uiConfig(), "", true)
	if !strings.Contains(long, "SSH KEY") || !strings.Contains(long, "~/.ssh/id_p") || strings.Contains(long, "●  personal") {
		t.Fatalf("long:\n%s", long)
	}
}

func TestHelpGroupsAndUnknownCommands(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	order := []string{"Quick start:", "Essentials:", "  add ", "  edit ", "  rm ", "  ls ", "Switch identity:", "Check and protect:", "Setup:"}
	last := -1
	for _, s := range order {
		i := strings.Index(help, s)
		if i <= last {
			t.Fatalf("%q is missing or out of order in:\n%s", s, help)
		}
		last = i
	}
	for _, hidden := range []string{"check-commit", "  env "} {
		if strings.Contains(help, hidden) {
			t.Errorf("%q must be hidden", hidden)
		}
	}
	if strings.Contains(help, "Additional Commands") {
		t.Errorf("every command must be in a group:\n%s", help)
	}

	root = newRootCmd()
	root.SetArgs([]string{"lss"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "did you mean: ls") || !strings.Contains(err.Error(), "git-idm help") {
		t.Fatalf("unknown command error = %v", err)
	}
}

func TestStatusWithoutConfigShowsWelcome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("XDG_CONFIG_HOME")
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(nil)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No identities yet.") {
		t.Fatalf("got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "git-idm")); !os.IsNotExist(err) {
		t.Fatal("status must not create files")
	}
}

func TestTemporaryBindingIsVisible(t *testing.T) {
	uiEnv(t)
	c := uiConfig()
	c.Rules = []config.Rule{{Path: "~/Personal/", Identity: "spare", Temporary: true, Was: "personal"}}
	res := resolve.Result{Identity: "spare", Source: resolve.SourceRule, Rule: &c.Rules[0], RuleIdentity: "spare"}
	got := statusView(plain(), c, res)
	for _, want := range []string{"● spare  temporary", "why  temporary rule ~/Personal/", "was  personal", "end  git-idm unbind ~/Personal/"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if list := listView(plain(), c, "spare", false); !strings.Contains(list, "~/Personal/ (temporary, was personal)") {
		t.Errorf("ls:\n%s", list)
	}
	c.Rules[0].Was = ""
	if got := statusView(plain(), c, res); strings.Contains(got, "was ") || !strings.Contains(got, "end  git-idm unbind ~/Personal/") {
		t.Errorf("without an old identity:\n%s", got)
	}
}
