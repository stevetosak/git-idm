package setup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func editConfig() *config.Config {
	c := existing()
	c.Identities["work"] = config.Identity{Name: "Jane", Email: "jane@work.example", SSHKey: "~/.ssh/id_w", Color: "green",
		GHUser: "jane-work", GHConfigDir: "~/.config/gh"}
	c.Identities["personal"] = config.Identity{Name: "Jane", Email: "jane@home.example"}
	return c
}

func TestEditorChangesSimpleFields(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":                {"email", "name", "color", "default", "email", "done"},
		"Email":                    {"jane@home.example", "Jane@Work.example", "<default>"},
		"Author name":              {"", "Jane Doe"},
		"Prompt color":             {"magenta"},
		"Use work as the default?": {"no"},
	}}
	edit, err := (&Editor{Prompt: f, Config: editConfig()}).Run("work")
	if err != nil {
		t.Fatal(err)
	}
	p := edit.Plan
	if p.AuthorName != "Jane Doe" || p.Email != "Jane@Work.example" || p.Color != "magenta" || p.MakeDefault ||
		p.SSH != SSHExisting || p.SSHKey != "~/.ssh/id_w" || p.GHUser != "jane-work" {
		t.Fatalf("plan = %+v", p)
	}
	if got := strings.Join(edit.Changed, ","); got != "author name,email,color,default" {
		t.Fatalf("changed = %s", got)
	}
	wantErrors := []string{`Email: identity "personal" already uses this email`, "Author name: enter the author name"}
	if !reflect.DeepEqual(f.errors, wantErrors) {
		t.Fatalf("errors = %v", f.errors)
	}
	var labels []string
	for _, c := range f.choices["Edit work"] {
		labels = append(labels, c.Label)
	}
	want := []string{"Author name  Jane Doe", "Email        Jane@Work.example", "SSH key      ~/.ssh/id_w", "Signing      off", "Color        magenta", "Default      no", "Done", "Cancel"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("menu without gh = %q", labels)
	}
}

func TestEditorCancelAndNoChanges(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":    {"color", "cancel"},
		"Prompt color": {"red"},
	}}
	if _, err := (&Editor{Prompt: f, Config: editConfig()}).Run("work"); !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
	f = &fakePrompter{t: t, answers: map[string][]string{
		"Edit personal": {"color", "ssh", "done"},
		"Prompt color":  {"<default>"},
		"SSH key":       {"<default>"},
	}}
	edit, err := (&Editor{Prompt: f, Config: editConfig()}).Run("personal")
	if err != nil || len(edit.Changed) != 0 {
		t.Fatalf("the current values are preselected, no change: %+v %v", edit, err)
	}
	if _, err := (&Editor{Prompt: f, Config: editConfig()}).Run("nope"); err == nil {
		t.Fatal("want error for an unknown identity")
	}
}

func TestEditorNewKeyWithoutKeyScope(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":      {"ssh", "ssh", "done"},
		"SSH key":        {"generate", "generate"},
		"Key passphrase": {"no", "no"},
	}}
	verified := 0
	e := &Editor{Prompt: f, Config: editConfig(),
		Login: func(string) (Account, string, error) { t.Fatal("must not log in"); return Account{}, "", nil },
		Verify: func(string) (Account, error) {
			verified++
			return Account{Login: "jane-work", Scopes: []string{"repo"}}, nil
		},
	}
	edit, err := e.Run("work")
	if err != nil {
		t.Fatal(err)
	}
	p := edit.Plan
	if p.SSH != SSHGenerate || p.SSHKey != "~/.ssh/git-idm_work" || p.Upload || !p.NoKeyScope || verified != 1 {
		t.Fatalf("plan = %+v verified=%d", p, verified)
	}
	if got := strings.Join(edit.Changed, ","); got != "ssh key" {
		t.Fatalf("changed = %s", got)
	}
}

func TestEditorSwitchesGitHubLogin(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":                      {"github", "github", "ssh", "done"},
		"GitHub account":                 {"login", "use:~/.config/gh-p"},
		"SSH key":                        {"<default>"},
		"Add the key to jane on GitHub?": {"<default>"},
	}}
	var loggedOut []string
	e := &Editor{Prompt: f, Config: editConfig(),
		Logins: []GHLogin{{User: "jane-work", Dir: "~/.config/gh", UsedBy: []string{"work"}}, {User: "jane", Dir: "~/.config/gh-p"}},
		Login: func(string) (Account, string, error) {
			return Account{Login: "jane-new"}, "~/.config/git-idm/gh/work", nil
		},
		Verify: func(string) (Account, error) { return Account{Login: "jane"}, nil },
		Logout: func(dir, user string) { loggedOut = append(loggedOut, user+" "+dir) },
	}
	edit, err := e.Run("work")
	if err != nil {
		t.Fatal(err)
	}
	p := edit.Plan
	if p.GHUser != "jane" || p.GHConfigDir != "~/.config/gh-p" || p.NewLogin || !p.Upload {
		t.Fatalf("plan = %+v", p)
	}
	if !reflect.DeepEqual(loggedOut, []string{"jane-new ~/.config/git-idm/gh/work"}) {
		t.Fatalf("the replaced new login must be logged out: %v", loggedOut)
	}
	if !hasChoice(f.choices["GitHub account"], "use:~/.config/gh") {
		t.Fatalf("after a change, the old login must be listed again: %+v", f.choices["GitHub account"])
	}
}

func TestEditorCancelLogsOutNewLoginAndRemove(t *testing.T) {
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":      {"github", "cancel"},
		"GitHub account": {"login"},
	}}
	var loggedOut string
	e := &Editor{Prompt: f, Config: editConfig(),
		Login:  func(string) (Account, string, error) { return Account{Login: "jane-new"}, "~/new", nil },
		Logout: func(dir, user string) { loggedOut = user + " " + dir },
	}
	if _, err := e.Run("work"); !errors.Is(err, ErrAborted) || loggedOut != "jane-new ~/new" {
		t.Fatalf("err=%v loggedOut=%q", err, loggedOut)
	}
	f = &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":      {"github", "done"},
		"GitHub account": {"remove"},
	}}
	e = &Editor{Prompt: f, Config: editConfig(), Login: e.Login,
		Logins: []GHLogin{{User: "jane-work", Dir: "~/.config/gh", UsedBy: []string{"work"}}, {User: "jane", Dir: "~/.config/gh-p"}},
		Verify: func(string) (Account, error) { return Account{}, nil }}
	edit, err := e.Run("work")
	if err != nil || edit.Plan.GHUser != "" || edit.Plan.GHConfigDir != "" || strings.Join(edit.Changed, ",") != "GitHub" {
		t.Fatalf("%+v %v", edit, err)
	}
	choices := f.choices["GitHub account"]
	if hasChoice(choices, "use:~/.config/gh") || !hasChoice(choices, "use:~/.config/gh-p") || choices[0].Label != "Keep jane-work (~/.config/gh)" {
		t.Fatalf("the current login is the Keep choice, not a list entry: %+v", choices)
	}
}

func TestEditorSigningWithOwnKey(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":      {"signing", "ssh", "done"},
		"Commit signing": {"~/.ssh/id_w.pub"},
		"Add the key to jane-work on GitHub as a signing key?": {"<default>"},
		"SSH key":                             {"generate"},
		"Key passphrase":                      {"no"},
		"Add the key to jane-work on GitHub?": {"<default>"},
	}}
	e := &Editor{Prompt: f, Config: editConfig(),
		Login:  func(string) (Account, string, error) { return Account{}, "", nil },
		Verify: func(string) (Account, error) { return Account{Login: "jane-work"}, nil }}
	edit, err := e.Run("work")
	if err != nil {
		t.Fatal(err)
	}
	p := edit.Plan
	if p.SigningKey != "~/.ssh/git-idm_work.pub" || !p.UploadSigning || !p.Upload {
		t.Fatalf("signing must follow the new SSH key: %+v", p)
	}
	if got := strings.Join(edit.Changed, ","); got != "ssh key,signing" {
		t.Fatalf("changed = %s", got)
	}
	if label := f.choices["Commit signing"][0].Label; label != "Sign with this identity's SSH key (~/.ssh/id_w.pub)" {
		t.Fatalf("first choice = %q", label)
	}
}

func TestEditorSigningWithoutScopeAndOff(t *testing.T) {
	sandbox(t)
	c := editConfig()
	id := c.Identities["work"]
	id.SigningKey = "ABCD1234"
	c.Identities["work"] = id
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":      {"signing", "signing", "done"},
		"Commit signing": {"<default>", "~/.ssh/id_w.pub"},
	}}
	e := &Editor{Prompt: f, Config: c,
		Login: func(string) (Account, string, error) { return Account{}, "", nil },
		Verify: func(string) (Account, error) {
			return Account{Login: "jane-work", Scopes: []string{"admin:public_key"}}, nil
		}}
	edit, err := e.Run("work")
	if err != nil {
		t.Fatal(err)
	}
	if !hasChoice(f.choices["Commit signing"], "ABCD1234") {
		t.Fatalf("a GPG key must stay a choice: %+v", f.choices["Commit signing"])
	}
	if p := edit.Plan; p.SigningKey != "~/.ssh/id_w.pub" || p.UploadSigning || !p.NoSigningScope {
		t.Fatalf("plan = %+v", p)
	}

	f = &fakePrompter{t: t, answers: map[string][]string{
		"Edit work":      {"signing", "done"},
		"Commit signing": {"off"},
	}}
	edit, err = (&Editor{Prompt: f, Config: c}).Run("work")
	if err != nil || edit.Plan.SigningKey != "" || strings.Join(edit.Changed, ",") != "signing" {
		t.Fatalf("%+v %v", edit, err)
	}
}
