package setup

import (
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/transfer"
)

func TestWizardImportFillsValues(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"<default>"},
		"Email":                 {"<default>"},
		"GitHub account":        {"<default>"},
		"SSH key":               {"<default>"},
		"Directory for oss":     {"skip"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"yes"},
	}}
	e := transfer.Entry{ID: "oss", Name: "Jane OSS", Email: "jane@oss.example", Color: "red", Protocol: config.ProtocolHTTPS, GitHubUser: "jane-oss"}
	w := Wizard{Prompt: f, Config: existing(), DefaultName: e.ID, Import: &e,
		Logins: []GHLogin{{User: "other", Dir: "~/.config/gh-other"}, {User: "Jane-OSS", Dir: "~/.config/gh-oss"}},
		Login:  func(string) (Account, string, error) { t.Fatal("must not log in"); return Account{}, "", nil },
		Verify: func(dir string) (Account, error) { return Account{Login: "jane-oss"}, nil },
	}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "oss" || p.AuthorName != "Jane OSS" || p.Email != "jane@oss.example" || p.Color != "red" ||
		p.SSH != SSHNone || p.GHUser != "jane-oss" || p.GHConfigDir != "~/.config/gh-oss" {
		t.Fatalf("plan = %+v", p)
	}
	for _, q := range f.asked {
		if q == "Identity name" {
			t.Error("a free imported name must not be asked")
		}
	}
}

func TestWizardImportRenamesTakenName(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"<default>"},
		"Email":                 {"<default>"},
		"Identity name":         {"<default>"},
		"SSH key":               {"none"},
		"Directory for work-2":  {"skip"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"yes"},
	}}
	e := transfer.Entry{ID: "work", Name: "Jane", Email: "jane@new.example"}
	w := Wizard{Prompt: f, Config: existing(), DefaultName: e.ID, Import: &e}
	p, err := w.Run()
	if err != nil || p.Name != "work-2" {
		t.Fatalf("plan = %+v, %v", p, err)
	}
}

func TestWizardImportOtherGitHubAccount(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":                       {"<default>"},
		"Email":                             {"<default>"},
		"GitHub account":                    {"login", "login"},
		"Use wrong-gh instead of jane-oss?": {"no"},
		"SSH key":                           {"none"},
		"Directory for oss":                 {"skip"},
		"Prompt color":                      {"<default>"},
		"Create this identity?":             {"yes"},
	}}
	e := transfer.Entry{ID: "oss", Name: "Jane", Email: "jane@oss.example", GitHubUser: "jane-oss"}
	logins, loggedOut := 0, ""
	w := Wizard{Prompt: f, Config: existing(), DefaultName: e.ID, Import: &e,
		Login: func(name string) (Account, string, error) {
			logins++
			if logins == 1 {
				return Account{Login: "wrong-gh"}, "~/gh/oss", nil
			}
			return Account{Login: "jane-oss"}, "~/gh/oss-2", nil
		},
		Logout: func(dir, user string) { loggedOut = user + " " + dir },
	}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.GHUser != "jane-oss" || p.GHConfigDir != "~/gh/oss-2" || loggedOut != "wrong-gh ~/gh/oss" {
		t.Fatalf("plan = %+v, logged out %q", p, loggedOut)
	}
	if !strings.Contains(strings.Join(f.asked, "\n"), "Use wrong-gh instead of jane-oss?") {
		t.Errorf("must ask about the other account: %v", f.asked)
	}
}
