package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func writeHosts(t *testing.T, home, dir, user string) {
	t.Helper()
	path := filepath.Join(home, dir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "hosts.yml"), []byte("github.com:\n    user: "+user+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExistingLogins(t *testing.T) {
	home := sandbox(t)
	writeHosts(t, home, ".config/gh", "jane-work")
	writeHosts(t, home, ".config/gh-p", "jane")
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "J", Email: "j@w.example", GHConfigDir: "~/.config/gh"}
	c.Identities["oss"] = config.Identity{Name: "J", Email: "j@o.example", GHConfigDir: "~/.config/gh"}
	c.Identities["personal"] = config.Identity{Name: "J", Email: "j@p.example", GHConfigDir: "~/.config/gh-p"}
	c.Identities["gone"] = config.Identity{Name: "J", Email: "j@g.example", GHConfigDir: "~/.config/gh-gone"}

	got := ExistingLogins(c, filepath.Join(home, ".config", "gh"), "~/.config/gh-missing")
	want := []GHLogin{
		{User: "jane-work", Dir: "~/.config/gh", UsedBy: []string{"oss", "work"}},
		{User: "jane", Dir: "~/.config/gh-p", UsedBy: []string{"personal"}},
	}
	if !slices.EqualFunc(got, want, func(a, b GHLogin) bool {
		return a.User == b.User && a.Dir == b.Dir && slices.Equal(a.UsedBy, b.UsedBy)
	}) {
		t.Fatalf("got %+v", got)
	}
	if label := got[0].label(); label != "Use jane-work (~/.config/gh, used by oss, work)" {
		t.Fatalf("label = %q", label)
	}
	if label := (GHLogin{User: "x", Dir: "~/gh"}).label(); label != "Use x (~/gh)" {
		t.Fatalf("label = %q", label)
	}
}

func TestWizardUsesExistingLogin(t *testing.T) {
	sandbox(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Author name":           {"Jane"},
		"Email":                 {"jane@x.example"},
		"Identity name":         {"x"},
		"Directory for x":       {"skip"},
		"GitHub account":        {"use:~/gh-broken", "use:~/.config/gh"},
		"SSH key":               {"generate"},
		"Key passphrase":        {"no"},
		"Prompt color":          {"<default>"},
		"Create this identity?": {"no"},
	}}
	var verified []string
	w := &Wizard{Prompt: f, Config: existing(),
		Logins: []GHLogin{{User: "jane-work", Dir: "~/.config/gh", UsedBy: []string{"work"}}, {User: "old", Dir: "~/gh-broken"}},
		Login:  func(string) (Account, string, error) { t.Fatal("must not log in"); return Account{}, "", nil },
		Verify: func(dir string) (Account, error) {
			verified = append(verified, dir)
			if dir == "~/gh-broken" {
				return Account{}, errors.New("401 Bad credentials")
			}
			return Account{Login: "jane-work", Scopes: []string{"repo"}}, nil
		},
		Logout: func(string, string) { t.Fatal("must not log out of an existing login") },
	}
	p, err := w.Run()
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(verified, ",") != "~/gh-broken,~/.config/gh" {
		t.Fatalf("verified = %v", verified)
	}
	if p.GHUser != "jane-work" || p.GHConfigDir != "~/.config/gh" || p.NewLogin || !p.NoKeyScope || p.Upload || !p.KeyNotAdded() {
		t.Fatalf("plan = %+v", p)
	}
	summary := p.Summary()
	if !strings.Contains(summary, "gh     jane-work (existing login ~/.config/gh)") || !strings.Contains(summary, "not added to GitHub: the gh login cannot add keys") {
		t.Fatalf("summary:\n%s", summary)
	}
}

func TestExecutorKeepsExistingLoginConfig(t *testing.T) {
	home := sandbox(t)
	tools, log := fakeTools(t, home)
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Out: &bytes.Buffer{}}
	defaultHosts := filepath.Join(home, ".config", "gh", "hosts.yml")
	os.MkdirAll(filepath.Dir(defaultHosts), 0o700)
	os.WriteFile(defaultHosts, []byte("github.com:\n    user: u\n"), 0o600)
	p := Plan{Name: "e", AuthorName: "J", Email: "j@e.example", GHUser: "u", GHConfigDir: "~/.config/gh", SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_e"}
	id, _, err := ex.Run(p)
	if err != nil {
		t.Fatal(err)
	}
	if id.GHConfigDir != "~/.config/git-idm/gh/e" || id.GHUser != "u" {
		t.Fatalf("an identity must never use the default gh folder: %+v", id)
	}
	copied := filepath.Join(home, ".config", "git-idm", "gh", "e", "hosts.yml")
	if info, err := os.Stat(copied); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("copied login: %v %v", err, info)
	}
	if data, _ := os.ReadFile(defaultHosts); string(data) != "github.com:\n    user: u\n" {
		t.Fatalf("the default login must stay as it is: %q", data)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Fatalf("must not change an existing gh login:\n%s", calls)
	}
}
