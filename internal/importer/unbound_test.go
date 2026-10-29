package importer

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func TestUnboundGroups(t *testing.T) {
	home := sandboxHome(t)
	write(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Jane\n\temail = jane@work.example\n")
	repo(t, filepath.Join(home, "bound", "a"))
	repo(t, filepath.Join(home, "code", "x"))
	repo(t, filepath.Join(home, "code", "y"))
	repo(t, filepath.Join(home, "code", "x", "sub"))
	repo(t, filepath.Join(home, "code2", "p"), "user.email", "c@client.example")
	repo(t, filepath.Join(home, "code2", "q"))
	repo(t, filepath.Join(home, "solo"))
	repo(t, filepath.Join(home, "mixed", "r1"))
	repo(t, filepath.Join(home, "mixed", "b"))
	hist := filepath.Join(home, "hist", "z")
	repo(t, hist)
	cmd := exec.Command("git", "-c", "user.name=C", "-c", "user.email=C@client.example", "commit", "-q", "--allow-empty", "--no-verify", "-m", "x")
	cmd.Dir = hist
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
	repo(t, filepath.Join(home, "Documents", "hidden"))

	c := config.New()
	c.Default = "work"
	c.Identities["work"] = config.Identity{Name: "W", Email: "jane@work.example"}
	c.Identities["client"] = config.Identity{Name: "C", Email: "c@client.example"}
	c.Rules = []config.Rule{{Path: "~/bound/", Identity: "work"}, {Path: "~/mixed/b/", Identity: "work"}}

	var got []string
	for _, g := range UnboundGroups(c, ScanHome().Repos) {
		got = append(got, g.RulePath()+"="+g.Suggested+"("+itoa(len(g.Repos))+")")
	}
	want := "~/code/=work(2) ~/code2/p/=client(1) ~/code2/q/=work(1) ~/hist/z/=client(1) ~/mixed/r1/=work(1) ~/solo/=work(1)"
	if strings.Join(got, " ") != want {
		t.Fatalf("groups:\n got  %s\n want %s", strings.Join(got, " "), want)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
