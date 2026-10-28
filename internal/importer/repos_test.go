package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func repo(t *testing.T, dir string, keys ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	args := [][]string{{"init", "-q"}}
	for i := 0; i+1 < len(keys); i += 2 {
		args = append(args, []string{"config", "--local", "--add", keys[i], keys[i+1]})
	}
	for _, a := range args {
		cmd := exec.Command("git", a...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
}

func TestImportRepos(t *testing.T) {
	home := sandboxHome(t)
	write(t, filepath.Join(home, ".gitconfig"), `[user]
	name = Jane Doe
	email = jane@work.example
[includeIf "gitdir/i:~/Personal/"]
	path = ~/.gitconfig-personal
`)
	write(t, filepath.Join(home, ".gitconfig-personal"), "[user]\n\temail = jane@home.example\n")
	martin := []string{"user.email", "martin@codechem.com", "core.sshCommand", "ssh -i ~/.ssh/id_martin"}
	repo(t, filepath.Join(home, "Projects", "codechem", "api"), append([]string{"user.name", "Martin", "user.name", "Martin"}, martin...)...)
	repo(t, filepath.Join(home, "Projects", "codechem", "web"), append([]string{"user.name", "Martin"}, martin...)...)
	repo(t, filepath.Join(home, "Projects", "codechem", "api", "Submodules", "common"))
	repo(t, filepath.Join(home, "Projects", "clean"))
	repo(t, filepath.Join(home, "Projects", "mix"), append([]string{"user.name", "Martin"}, martin...)...)
	repo(t, filepath.Join(home, "Projects", "mix", "sub"), "user.name", "Other", "user.email", "Other@gmail.com")
	repo(t, filepath.Join(home, "Personal", "site"), "user.email", "jane@home.example")
	repo(t, filepath.Join(home, "oss", "tool"), "user.email", "jane@work.example")
	repo(t, filepath.Join(home, "Documents", "secret"), "user.email", "hidden@nowhere.example")

	roots, exclude := HomeScan()
	res, err := Import(Options{Scan: roots, Exclude: exclude})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config

	if got := strings.Join(c.Names(), ","); got != "codechem,other,personal,work" {
		t.Fatalf("identities = %s", got)
	}
	want := config.Identity{Name: "Martin", Email: "martin@codechem.com", SSHKey: "~/.ssh/id_martin"}
	if c.Identities["codechem"] != want {
		t.Errorf("codechem = %+v", c.Identities["codechem"])
	}
	if other := c.Identities["other"]; other.Name != "Other" || other.Email != "Other@gmail.com" {
		t.Errorf("other = %+v", other)
	}
	var rules []string
	for _, r := range c.Rules {
		rules = append(rules, r.Path+"="+r.Identity)
	}
	if got := strings.Join(rules, " "); got != "~/Personal/=personal ~/Projects/codechem/=codechem ~/Projects/mix/sub/=other" {
		t.Errorf("rules = %s", got)
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "~/Projects/mix sets martin@codechem.com, but other authors") {
		t.Errorf("skipped = %v", res.Skipped)
	}
}

func TestImportWithoutScanIgnoresRepos(t *testing.T) {
	home := sandboxHome(t)
	write(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Jane\n\temail = jane@work.example\n")
	repo(t, filepath.Join(home, "Projects", "api"), "user.email", "martin@codechem.com")

	res, err := Import(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Config.Names(), ","); got != "work" {
		t.Fatalf("identities = %s", got)
	}
}

func TestNameFromEmail(t *testing.T) {
	cases := map[string]string{
		"martin@codechem.com":                      "codechem",
		"Jane.Doe@Gmail.com":                       "jane-doe",
		"123456+octo-cat@users.noreply.github.com": "octo-cat",
		"dev@mail.example.co.uk":                   "mail",
		"x@proton.me":                              "x",
	}
	for email, want := range cases {
		if got := nameFromEmail(email); got != want {
			t.Errorf("nameFromEmail(%q) = %q, want %q", email, got, want)
		}
	}
}
