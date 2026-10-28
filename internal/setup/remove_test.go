package setup

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func removeFixture(t *testing.T) (string, *config.Config, string) {
	t.Helper()
	home := sandbox(t)
	managed := filepath.Join(home, ".config", "git-idm", "gh")
	for _, f := range []string{".ssh/git-idm_r", ".ssh/git-idm_r.pub", ".ssh/id_shared", ".ssh/id_own"} {
		os.MkdirAll(filepath.Dir(filepath.Join(home, f)), 0o700)
		os.WriteFile(filepath.Join(home, f), []byte("ssh-ed25519 AAAAr r@x"), 0o600)
	}
	os.MkdirAll(filepath.Join(managed, "r"), 0o700)
	c := existing()
	c.Identities["r"] = config.Identity{Name: "R", Email: "r@x.example", SSHKey: "~/.ssh/git-idm_r", GHUser: "r-gh", GHConfigDir: "~/.config/git-idm/gh/r"}
	c.Identities["ext"] = config.Identity{Name: "E", Email: "e@x.example", SSHKey: "~/.ssh/id_own", GHUser: "e-gh", GHConfigDir: "~/.config/gh-ext"}
	c.Identities["shared"] = config.Identity{Name: "S", Email: "s@x.example", SSHKey: "~/.ssh/id_shared"}
	c.Identities["shared2"] = config.Identity{Name: "S2", Email: "s2@x.example", SSHKey: "~/.ssh/id_shared"}
	c.Rules = append(c.Rules, config.Rule{Path: "~/R/", Identity: "r"})
	return home, c, managed
}

func TestRemoveAskOwnedResources(t *testing.T) {
	_, c, managed := removeFixture(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Delete the SSH key ~/.ssh/git-idm_r?":       {"<default>"},
		"Remove the key from r-gh on GitHub?":        {"<default>"},
		"Log out of gh (r-gh) and delete the login?": {"<default>"},
		"Remove r?": {"yes"},
	}}
	r := &Remover{Prompt: f, Config: c, ManagedGHRoot: managed, FindKey: func(string, string) (int64, error) { return 5, nil }}
	p, err := r.Ask("r")
	if err != nil {
		t.Fatal(err)
	}
	if !p.DeleteKey || !p.DeleteGitHubKey || p.GitHubKeyID != 5 || !p.Logout || len(p.Kept) != 0 {
		t.Fatalf("plan = %+v", p)
	}
	if s := p.Summary(c); !strings.Contains(s, "unbind ~/R/ (repos there then use work)") || !strings.Contains(s, "delete ~/.ssh/git-idm_r") {
		t.Fatalf("summary:\n%s", s)
	}
}

func TestRemoveAskForeignResources(t *testing.T) {
	_, c, managed := removeFixture(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Delete the SSH key ~/.ssh/id_own?": {"<default>"},
		"Remove ext?":                       {"yes"},
	}}
	r := &Remover{Prompt: f, Config: c, ManagedGHRoot: managed}
	p, err := r.Ask("ext")
	if err != nil {
		t.Fatal(err)
	}
	if p.DeleteKey || p.Logout {
		t.Fatalf("foreign key must default to keep, foreign login must not be offered: %+v", p)
	}
	if kept := strings.Join(p.Kept, "\n"); !strings.Contains(kept, "ssh key ~/.ssh/id_own") || !strings.Contains(kept, "gh login in ~/.config/gh-ext") {
		t.Fatalf("kept = %v", p.Kept)
	}

	f = &fakePrompter{t: t, answers: map[string][]string{"Remove shared?": {"yes"}}}
	if _, err := (&Remover{Prompt: f, Config: c, ManagedGHRoot: managed}).Ask("shared"); err != nil {
		t.Fatalf("a key used by another identity must not be offered: %v (asked %v)", err, f.asked)
	}
}

func TestRemoveDefaultAsksReplacement(t *testing.T) {
	_, c, managed := removeFixture(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"New default identity": {"<default>"},
		"Remove work?":         {"yes"},
	}}
	p, err := (&Remover{Prompt: f, Config: c, ManagedGHRoot: managed}).Ask("work")
	if err != nil {
		t.Fatal(err)
	}
	if p.NewDefault != "" || !strings.Contains(p.Summary(c), "no default identity after this") {
		t.Fatalf("plan = %+v\n%s", p, p.Summary(c))
	}
	if choices := f.choices["New default identity"]; len(choices) == 0 || choices[0].Key != "none" {
		t.Fatalf("choices = %v", choices)
	}
}

func TestRemoveDeclineAndRecommended(t *testing.T) {
	_, c, managed := removeFixture(t)
	f := &fakePrompter{t: t, answers: map[string][]string{
		"Delete the SSH key ~/.ssh/git-idm_r?":       {"yes"},
		"Log out of gh (r-gh) and delete the login?": {"yes"},
		"Remove r?": {"<default>"},
	}}
	if _, err := (&Remover{Prompt: f, Config: c, ManagedGHRoot: managed}).Ask("r"); !errors.Is(err, ErrAborted) {
		t.Fatalf("the final question must default to No: %v", err)
	}
	r := &Remover{Config: c, ManagedGHRoot: managed}
	if p, _ := r.Recommended("r"); !p.DeleteKey || !p.Logout {
		t.Fatalf("recommended for git-idm resources: %+v", p)
	}
	if p, _ := r.Recommended("ext"); p.DeleteKey || p.Logout {
		t.Fatalf("recommended must keep foreign resources: %+v", p)
	}
}

func TestExecutorRemove(t *testing.T) {
	home := sandbox(t)
	tools, log := fakeTools(t, home)
	keygen, _ := exec.LookPath("ssh-keygen")
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	if out, err := exec.Command(keygen, "-t", "ed25519", "-N", "", "-q", "-f", filepath.Join(home, ".ssh", "git-idm_r")).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	ghDir := filepath.Join(home, ".config", "git-idm", "gh", "r")
	os.MkdirAll(ghDir, 0o700)
	ex := &Executor{Tools: tools, Out: &bytes.Buffer{}}
	keyID, err := ex.FindGitHubKey("~/.config/git-idm/gh/r", filepath.Join(home, ".ssh", "git-idm_r.pub"))
	if err != nil || keyID != 5 {
		t.Fatalf("FindGitHubKey = %d, %v", keyID, err)
	}
	c := config.New()
	c.Identities["r"] = config.Identity{Name: "R", Email: "r@x.example", SSHKey: "~/.ssh/git-idm_r", GHUser: "r-gh", GHConfigDir: "~/.config/git-idm/gh/r"}
	failed := ex.Remove(c, RemovePlan{Name: "r", DeleteKey: true, DeleteGitHubKey: true, GitHubKeyID: keyID, Logout: true})
	if len(failed) != 0 {
		t.Fatalf("failed: %v", failed)
	}
	for _, gone := range []string{".ssh/git-idm_r", ".ssh/git-idm_r.pub", ".config/git-idm/gh/r"} {
		if _, err := os.Stat(filepath.Join(home, gone)); !os.IsNotExist(err) {
			t.Errorf("%s must be removed", gone)
		}
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"api --method DELETE user/keys/5", "auth logout --hostname github.com --user r-gh"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("missing gh call %q:\n%s", want, calls)
		}
	}
}
