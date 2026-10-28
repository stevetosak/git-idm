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

func (f *fakePrompter) PickDir(q DirQuestion) (DirAnswer, error) {
	q.Entries(q.Start)
	for {
		answer := f.next(q.Title)
		text := answer
		switch answer {
		case "skip":
			text = ""
		case "default":
			text = "~"
		}
		a, err := answerFromText(q, text)
		if err != nil {
			f.errors = append(f.errors, q.Title+": "+err.Error())
			continue
		}
		return a, nil
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
		"Author name":                       {"<default>"},
		"Email":                             {"nope", "jane@work.example", "jane@client.example"},
		"Identity name":                     {"work", "Bad Name", "<default>"},
		"GitHub account":                    {"login", "login"},
		"SSH key":                           {"generate"},
		"Key passphrase":                    {"no"},
		"Add the key to jane-gh on GitHub?": {"<default>"},
		"Directory for client":              {"relative/dir", "~/Work", "~/Clients"},
		"Prompt color":                      {"<default>"},
		"Create this identity?":             {"<default>"},
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
	want := []string{"Author name", "Email", "Identity name", "Directory for acme", "GitHub account", "SSH key", "Prompt color", "Summary", "Create this identity?"}
	if strings.Join(f.asked, ",") != strings.Join(want, ",") {
		t.Fatalf("order:\n got  %v\n want %v", f.asked, want)
	}
}

func TestCompleteDir(t *testing.T) {
	home := sandbox(t)
	for _, d := range []string{"Projects/x", "Personal", "private", ".hidden"} {
		os.MkdirAll(filepath.Join(home, d), 0o700)
	}
	os.WriteFile(filepath.Join(home, "Pfile"), []byte("x"), 0o600)
	cases := map[string]string{
		"~/Pro":       "~/Projects/",
		"~/p":         "~/Personal/,~/private/,~/Projects/",
		"~/Projects/": "~/Projects/x/",
		"~/.h":        "~/.hidden/",
		"":            "~/Personal/,~/private/,~/Projects/",
		home + "/Pe":  home + "/Personal/",
		"relative":    "",
		"~/missing/":  "",
	}
	for in, want := range cases {
		if got := strings.Join(CompleteDir(in), ","); got != want {
			t.Errorf("CompleteDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func dirFixture(t *testing.T) (string, *Wizard) {
	t.Helper()
	home := sandbox(t)
	for _, d := range []string{"Projects/a/.git", "Projects/b/.git", "Projects/plain", "Work/sub", "Library", ".hidden"} {
		os.MkdirAll(filepath.Join(home, d), 0o700)
	}
	os.WriteFile(filepath.Join(home, "file"), []byte("x"), 0o600)
	return home, &Wizard{Config: existing()}
}

func describe(entries []DirEntry) string {
	var out []string
	for _, e := range entries {
		s := string(e.Kind) + ":" + e.Path
		if e.Note != "" {
			s += "(" + e.Note + ")"
		}
		if e.Err != "" {
			s += "!"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func TestDirEntries(t *testing.T) {
	_, w := dirFixture(t)
	for input, want := range map[string]string{
		"~/":          "default: skip: dir:~/Projects/(2 repos) dir:~/Work/(bound to work)",
		"~/P":         "skip: dir:~/Projects/(2 repos)",
		"~/Projects":  "use:~/Projects/(2 repos) skip: dir:~/Projects/(2 repos)",
		"~/Projects/": "use:~/Projects/(2 repos) skip: dir:~/Projects/a/(git repo) dir:~/Projects/b/(git repo) dir:~/Projects/plain/",
		"~/Work/":     "use:~/Work/! skip: dir:~/Work/sub/",
		"~/Clients":   "create:~/Clients/ skip:",
		"~/file":      "skip:",
		"~/.h":        "skip: dir:~/.hidden/",
		"relative":    "skip:",
		"":            "skip: dir:~/Projects/(2 repos) dir:~/Work/(bound to work)",
	} {
		if got := describe(w.dirEntries(input)); got != want {
			t.Errorf("dirEntries(%q)\n got  %s\n want %s", input, got, want)
		}
	}
	if label := w.dirEntries("~/")[0].Label; !strings.Contains(label, "replaces work") {
		t.Errorf("default label: %s", label)
	}
}

func TestDirStatus(t *testing.T) {
	_, w := dirFixture(t)
	for input, want := range map[string]string{
		"~/Work/sub/": "Inside ~/Work/ (work). A rule here wins for repos below it.",
		"~/Work/":     "This folder is bound to work. Pick a folder inside it, or another folder.",
		"~/file":      "✗ this is a file",
		"relative":    "✗ use a full path, for example ~/Clients",
		"~/Projects/": "",
	} {
		if got := w.dirStatus(input); got != want {
			t.Errorf("dirStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWizardDirectoryAnswers(t *testing.T) {
	for _, tc := range []struct {
		answers []string
		want    Plan
		errors  int
	}{
		{[]string{"skip"}, Plan{}, 0},
		{[]string{"default"}, Plan{MakeDefault: true}, 0},
		{[]string{"~/Work", "~/Projects/"}, Plan{BindDir: "~/Projects/"}, 1},
		{[]string{"~/New/Deep"}, Plan{BindDir: "~/New/Deep/", CreateDir: true}, 0},
	} {
		_, w := dirFixture(t)
		f := &fakePrompter{t: t, answers: map[string][]string{"Directory for q": tc.answers}}
		w.Prompt = f
		var p Plan
		p.Name = "q"
		if err := w.askDir(&p); err != nil {
			t.Fatal(err)
		}
		if p.MakeDefault != tc.want.MakeDefault || p.BindDir != tc.want.BindDir || p.CreateDir != tc.want.CreateDir || len(f.errors) != tc.errors {
			t.Errorf("%v: plan=%+v errors=%v", tc.answers, p, f.errors)
		}
	}
}
