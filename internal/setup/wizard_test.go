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
		"Author name":                           {"<default>"},
		"Email":                                 {"nope", "jane@work.example", "jane@client.example"},
		"Identity name":                         {"work", "Bad Name", "<default>"},
		"GitHub account":                        {"login", "login"},
		"SSH key":                               {"generate"},
		"Key passphrase":                        {"no"},
		"Add the key to jane-gh on GitHub?":     {"<default>"},
		"Directory for client":                  {"type"},
		"Directory path":                        {"relative/dir", "~/Work", "~/Clients"},
		"~/Clients/ does not exist. Create it?": {"<default>"},
		"Prompt color":                          {"<default>"},
		"Create this identity?":                 {"<default>"},
	}}
	logins := 0
	w := Wizard{Prompt: f, Config: existing(), DefaultUser: "Jane",
		Login: func(name string) (Account, string, error) {
			logins++
			if logins == 1 {
				return Account{}, "", errors.New("browser closed")
			}
			return Account{Login: "jane-gh", ID: 42}, "~/.config/git-idm/gh/" + name, nil
		},
		Logout: func(string, string) { t.Fatal("must not log out on success") },
	}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	want := Plan{
		Name: "client", AuthorName: "Jane", Email: "jane@client.example", Color: "green",
		GHUser: "jane-gh", GHConfigDir: "~/.config/git-idm/gh/client", SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_client",
		Upload: true, BindDir: "~/Clients/", CreateDir: true,
	}
	if p != want {
		t.Fatalf("plan:\n got  %+v\n want %+v", p, want)
	}
	if len(f.errors) != 6 {
		t.Fatalf("want 6 rejected answers, got %v", f.errors)
	}
	for _, e := range []string{`"work" already exists`, "already uses this email", "already bound to \"work\"", "use a full path"} {
		if !strings.Contains(strings.Join(f.errors, "\n"), e) {
			t.Errorf("missing validation error %q in %v", e, f.errors)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Clients")); !os.IsNotExist(err) {
		t.Fatal("the wizard must not create anything")
	}
	for _, line := range []string{"gh     logged in as jane-gh", "new key ~/.ssh/git-idm_client (no passphrase)", "added to GitHub", "dir    ~/Clients/ (created)"} {
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
		"Directory for oss":     {"skip"},
		"Prompt color":          {"magenta"},
		"Create this identity?": {"yes"},
	}}
	w := Wizard{Prompt: f, Config: existing(), DefaultName: "oss",
		Login: func(string) (Account, string, error) { t.Fatal("must not log in"); return Account{}, "", nil }}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "oss" || p.GHUser != "" || p.SSH != SSHNone || p.BindDir != "" || p.Color != "magenta" {
		t.Fatalf("plan = %+v", p)
	}
	for _, q := range f.asked {
		if q == "Identity name" || q == "Key passphrase" || strings.HasPrefix(q, "Add the key") {
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
		"Author name":           {"Jane"},
		"Email":                 {"jane@client.example"},
		"SSH key":               {"~/.ssh/git-idm_client"},
		"Directory for client":  {"skip"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"yes"},
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

func TestWizardDeclineLogsOut(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"GitHub account":        {"login"},
		"Author name":           {"Jane"},
		"Email":                 {"jane@x.example"},
		"SSH key":               {"none"},
		"Directory for x":       {"skip"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"no"},
	}}
	var loggedOut string
	w := &Wizard{Prompt: f, Config: existing(), DefaultName: "x",
		Login:  func(string) (Account, string, error) { return Account{Login: "jx", ID: 1}, "~/gh-x", nil },
		Logout: func(dir, user string) { loggedOut = dir + " " + user }}
	_, err := w.Run()
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
	if loggedOut != "~/gh-x jx" {
		t.Fatalf("logout = %q", loggedOut)
	}
}

func TestWizardWithoutGHSkipsGitHubQuestion(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"Jane"},
		"Email":                 {"jane@y.example"},
		"SSH key":               {"none"},
		"Directory for y":       {"skip"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"yes"},
	}}
	if _, err := (&Wizard{Prompt: f, Config: existing(), DefaultName: "y"}).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestWizardBrowseDirectories(t *testing.T) {
	home := sandbox(t)
	for _, d := range []string{"Projects/a/.git", "Projects/b/.git", "Projects/plain", "Work", ".hidden", "Library"} {
		os.MkdirAll(filepath.Join(home, d), 0o700)
	}
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":              {"Jane"},
		"Email":                    {"jane@z.example"},
		"SSH key":                  {"none"},
		"Directory for z":          {"dir:Work", "use", "up", "dir:Projects", "dir:a", "up", "new"},
		"New folder in ~/Projects": {"a", "x/y", "Clients"},
		"Prompt color":             {"<default>"},
		"Create this identity?":    {"yes"},
	}}
	var history [][]Choice
	rec := &recordingPrompter{fakePrompter: f, onSelect: func(c []Choice) { history = append(history, c) }}
	p, err := (&Wizard{Prompt: rec, Config: existing(), DefaultName: "z"}).Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.BindDir != "~/Projects/Clients/" || !p.CreateDir {
		t.Fatalf("plan = %+v", p)
	}
	labels := func(c []Choice) string {
		var l []string
		for _, x := range c {
			l = append(l, strings.Join(strings.Fields(x.Label), " "))
		}
		return strings.Join(l, " | ")
	}
	homeView := labels(history[0])
	if !strings.Contains(homeView, "Projects/ 2 repos") || !strings.Contains(homeView, "Work/ bound to work") ||
		strings.Contains(homeView, ".hidden") || strings.Contains(homeView, "Library") {
		t.Fatalf("home view: %s", homeView)
	}
	projectsView := labels(history[4])
	if !strings.Contains(projectsView, "✓ Use ~/Projects") || !strings.Contains(projectsView, "a/ git repo") || !strings.Contains(projectsView, "plain/") {
		t.Fatalf("projects view: %s", projectsView)
	}
	joined := strings.Join(f.errors, "\n")
	for _, want := range []string{"~/Projects/a already exists", "without /"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing error %q in %v", want, f.errors)
		}
	}
	if !strings.Contains(rec.descriptions[2], "✗ ~/Work/ is already bound to \"work\"") {
		t.Errorf("binding a bound folder must show an error, got: %q", rec.descriptions[2])
	}
	if !strings.Contains(rec.descriptions[1], "This folder is bound to work") {
		t.Errorf("inside a bound folder must say so, got: %q", rec.descriptions[1])
	}
}

type recordingPrompter struct {
	*fakePrompter
	onSelect     func([]Choice)
	descriptions []string
}

func (r *recordingPrompter) Select(title, description string, choices []Choice, value string) (string, error) {
	if strings.HasPrefix(title, "Directory for") {
		r.onSelect(choices)
		r.descriptions = append(r.descriptions, description)
	}
	return r.fakePrompter.Select(title, description, choices, value)
}

func TestSuggestName(t *testing.T) {
	for email, want := range map[string]string{
		"jane@client.com":                 "client",
		"jane@mail.client.com":            "client",
		"jane@client.co.uk":               "client",
		"jane@gmail.com":                  "personal",
		"7+jane@users.noreply.github.com": "personal",
		"jane@Code_Chem.io":               "code_chem",
		"nope":                            "",
	} {
		if got := SuggestName(email); got != want {
			t.Errorf("SuggestName(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestWizardOrderAuthorEmailNameFirst(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"Jane"},
		"Email":                 {"jane@acme.io"},
		"Identity name":         {"<default>"},
		"GitHub account":        {"skip"},
		"SSH key":               {"none"},
		"Directory for acme":    {"skip"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"yes"},
	}}
	w := &Wizard{Prompt: f, Config: existing(), Login: func(string) (Account, string, error) { return Account{}, "", nil }}
	p, err := w.Run()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "acme" {
		t.Fatalf("name = %q", p.Name)
	}
	want := []string{"Author name", "Email", "Identity name", "GitHub account", "SSH key", "Directory for acme", "Prompt color", "Summary", "Create this identity?"}
	if strings.Join(f.asked, ",") != strings.Join(want, ",") {
		t.Fatalf("order:\n got  %v\n want %v", f.asked, want)
	}
}
