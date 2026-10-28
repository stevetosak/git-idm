package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

type fakePrompter struct {
	t       *testing.T
	answers map[string][]string
	asked   []string
	errors  []string
	choices map[string][]Choice
}

func (f *fakePrompter) next(title string) string {
	f.asked = append(f.asked, title)
	list := f.answers[title]
	if len(list) == 0 {
		f.t.Fatalf("no answer for %q (asked: %v)", title, f.asked)
	}
	f.answers[title] = list[1:]
	return list[0]
}

func (f *fakePrompter) Input(title, _, value string, validate func(string) error) (string, error) {
	for {
		answer := f.next(title)
		if answer == "<default>" {
			answer = value
		}
		if err := validate(answer); err != nil {
			f.errors = append(f.errors, title+": "+err.Error())
			continue
		}
		return answer, nil
	}
}

func (f *fakePrompter) Select(title, _ string, choices []Choice, value string) (string, error) {
	if f.choices == nil {
		f.choices = map[string][]Choice{}
	}
	f.choices[title] = choices
	answer := f.next(title)
	if answer == "<default>" {
		return value, nil
	}
	for _, c := range choices {
		if c.Key == answer {
			return answer, nil
		}
	}
	f.t.Fatalf("%q is not a choice for %q: %v", answer, title, choices)
	return "", nil
}

func (f *fakePrompter) Confirm(title, _ string, value bool) (bool, error) {
	switch f.next(title) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	}
	return value, nil
}

func (f *fakePrompter) Note(title, _ string) error {
	f.asked = append(f.asked, title)
	return nil
}

func sandbox(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

func existing() *config.Config {
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "Jane", Email: "jane@work.example"}
	c.Rules = []config.Rule{{Path: "~/Work/", Identity: "work"}}
	return c
}

func TestWizardFullFlow(t *testing.T) {
	home := sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Identity name":                         {"work", "Bad Name", "client"},
		"Author name":                           {"<default>"},
		"Email":                                 {"nope", "jane@work.example", "jane@client.example"},
		"GitHub account":                        {"login"},
		"SSH key":                               {"generate"},
		"Key passphrase":                        {"no"},
		"Add the key to your GitHub account?":   {"<default>"},
		"Sign commits with this key?":           {"yes"},
		"Directory":                             {"relative/dir", "~/Work", "~/Clients"},
		"~/Clients/ does not exist. Create it?": {"<default>"},
		"Prompt color":                          {"<default>"},
		"Create this identity?":                 {"<default>"},
	}}
	w := Wizard{Prompt: f, Config: existing(), DefaultUser: "Jane"}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	want := Plan{
		Name: "client", AuthorName: "Jane", Email: "jane@client.example", Color: "green",
		GHLogin: true, SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_client", Upload: true, Sign: true,
		BindDir: "~/Clients/", CreateDir: true,
	}
	if p != want {
		t.Fatalf("plan:\n got  %+v\n want %+v", p, want)
	}
	if len(f.errors) != 6 {
		t.Fatalf("want 6 rejected answers, got %v", f.errors)
	}
	for _, e := range []string{`"work" already exists`, "already uses this email", "already used by \"work\"", "use a full path"} {
		if !strings.Contains(strings.Join(f.errors, "\n"), e) {
			t.Errorf("missing validation error %q in %v", e, f.errors)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Clients")); !os.IsNotExist(err) {
		t.Fatal("the wizard must not create anything")
	}
	for _, line := range []string{"new key ~/.ssh/git-idm_client (no passphrase)", "added to GitHub", "signs commits", "dir    ~/Clients/ (created)"} {
		if !strings.Contains(p.Summary(), line) {
			t.Errorf("summary misses %q:\n%s", line, p.Summary())
		}
	}
}

func TestWizardMinimalFlowAsksLess(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"Jane"},
		"Email":                 {"jane@oss.example"},
		"GitHub account":        {"skip"},
		"SSH key":               {"none"},
		"Directory":             {""},
		"Prompt color":          {"magenta"},
		"Create this identity?": {"yes"},
	}}
	w := Wizard{Prompt: f, Config: existing(), DefaultName: "oss"}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "oss" || p.GHLogin || p.SSH != SSHNone || p.BindDir != "" || p.Color != "magenta" {
		t.Fatalf("plan = %+v", p)
	}
	for _, q := range f.asked {
		if q == "Identity name" || q == "Key passphrase" || q == "Sign commits with this key?" || q == "Add the key to your GitHub account?" {
			t.Errorf("must not ask %q", q)
		}
	}
}

func TestWizardSSHChoices(t *testing.T) {
	home := sandbox(t)
	ssh := filepath.Join(home, ".ssh")
	os.MkdirAll(ssh, 0o700)
	for _, f := range []string{"id_ed25519", "id_ed25519.pub", "orphan.pub", "git-idm_client", "git-idm_client.pub"} {
		os.WriteFile(filepath.Join(ssh, f), []byte("k"), 0o600)
	}
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":                 {"Jane"},
		"Email":                       {"jane@client.example"},
		"GitHub account":              {"skip"},
		"SSH key":                     {"~/.ssh/git-idm_client"},
		"Sign commits with this key?": {"no"},
		"Directory":                   {""},
		"Prompt color":                {"<default>"},
		"Create this identity?":       {"yes"},
	}}
	w := Wizard{Prompt: f, Config: existing(), DefaultName: "client"}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.SSH != SSHExisting || p.SSHKey != "~/.ssh/git-idm_client" {
		t.Fatalf("plan = %+v", p)
	}
	var keys []string
	for _, c := range f.choices["SSH key"] {
		keys = append(keys, c.Key)
	}
	if got := strings.Join(keys, ","); got != "~/.ssh/git-idm_client,~/.ssh/id_ed25519,none" {
		t.Fatalf("choices = %s (generate must be hidden when the key file exists, orphan .pub must be hidden)", got)
	}
}

func TestWizardDeclineChangesNothing(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"Jane"},
		"Email":                 {"jane@x.example"},
		"GitHub account":        {"skip"},
		"SSH key":               {"none"},
		"Directory":             {""},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"no"},
	}}
	_, err := (&Wizard{Prompt: f, Config: existing(), DefaultName: "x"}).Run()
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
}
