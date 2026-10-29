package resolve

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func setup(t *testing.T) (string, *config.Config) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, d := range []string{"P/a/.git", "P/deep/x/.git", "W/r/.git", "W/r/sub", "P/plain"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example"}
	c.Identities["personal"] = config.Identity{Name: "P", Email: "p@x.example"}
	c.Identities["deep"] = config.Identity{Name: "D", Email: "d@x.example"}
	c.Rules = []config.Rule{{Path: "~/P/deep/", Identity: "deep"}, {Path: "~/P/", Identity: "personal"}}
	return home, c
}

func TestResolve(t *testing.T) {
	home, c := setup(t)
	cases := []struct {
		dir, override string
		identity      string
		source        Source
		repo          string
	}{
		{"P/a", "", "personal", SourceRule, "P/a"},
		{"P/plain", "", "personal", SourceRule, ""},
		{"P/deep/x", "", "deep", SourceRule, "P/deep/x"},
		{"W/r/sub", "", "", SourceNone, "W/r"},
		{"P/a", "work", "work", SourceOverride, "P/a"},
	}
	for _, tc := range cases {
		res := Resolve(c, filepath.Join(home, tc.dir), tc.override)
		if res.Identity != tc.identity || res.Source != tc.source {
			t.Errorf("%s: got %s/%s, want %s/%s", tc.dir, res.Identity, res.Source, tc.identity, tc.source)
		}
		wantRepo := ""
		if tc.repo != "" {
			wantRepo = filepath.Join(home, tc.repo)
		}
		if res.RepoRoot != wantRepo {
			t.Errorf("%s: repo %q, want %q", tc.dir, res.RepoRoot, wantRepo)
		}
	}
	if res := Resolve(c, filepath.Join(home, "W/r"), ""); res.Suggested != c.Default || c.Default == "" {
		t.Errorf("unbound folder must suggest the default %q, got %q", c.Default, res.Suggested)
	}
	if res := Resolve(c, filepath.Join(home, "P/a"), ""); res.Suggested != "" {
		t.Errorf("a bound folder suggests nothing, got %q", res.Suggested)
	}
	if res := Resolve(c, filepath.Join(home, "P/a"), "work"); res.RuleIdentity != "personal" {
		t.Errorf("override must still report the rule identity, got %q", res.RuleIdentity)
	}
}

func TestResolveCaseInsensitiveOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	home, c := setup(t)
	c.Rules = []config.Rule{{Path: "~/p/", Identity: "personal"}}
	if res := Resolve(c, filepath.Join(home, "P/a"), ""); res.Identity != "personal" {
		t.Fatalf("got %s", res.Identity)
	}
}

func TestResolveWorktreeAndSymlink(t *testing.T) {
	home, c := setup(t)
	wt := filepath.Join(home, "W", "wt")
	if err := os.MkdirAll(wt, 0o700); err != nil {
		t.Fatal(err)
	}
	gitdir := filepath.Join(home, "P", "a", ".git", "worktrees", "wt")
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := Resolve(c, wt, ""); res.Identity != "personal" || res.RepoRoot != wt {
		t.Fatalf("worktree of a personal repo: got %s repo=%s", res.Identity, res.RepoRoot)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(filepath.Join(home, "P", "plain"), link); err != nil {
		t.Fatal(err)
	}
	if res := Resolve(c, link, ""); res.Identity != "personal" {
		t.Fatalf("symlink into ~/P: got %s", res.Identity)
	}
}

func TestLocalUser(t *testing.T) {
	home, c := setup(t)
	cfg := "[core]\n\tbare = false\n[user]\n\tname = Martin\n\temail = \"m@x.example\"\n[remote \"origin\"]\n\turl = x\n"
	if err := os.WriteFile(filepath.Join(home, "W/r/.git/config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	res := Resolve(c, filepath.Join(home, "W/r"), "")
	if res.LocalUser != "Martin" || res.LocalEmail != "m@x.example" {
		t.Fatalf("got %q <%q>", res.LocalUser, res.LocalEmail)
	}
}
