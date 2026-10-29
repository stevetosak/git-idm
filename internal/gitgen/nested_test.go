package gitgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
)

type nestedWant struct {
	email, gpgsign, signingKey, url, ssh string
}

func nestedIdentities(gh bool) map[string]config.Identity {
	ids := map[string]config.Identity{
		"signer": {Name: "S", Email: "s@x.example", SSHKey: "~/.ssh/id_s", SigningKey: "~/.ssh/id_s.pub", Protocol: config.ProtocolSSH},
		"plain":  {Name: "P", Email: "p@x.example"},
		"agent":  {Name: "A", Email: "a@x.example", Protocol: config.ProtocolSSH},
	}
	if gh {
		for name, id := range ids {
			id.GHConfigDir, id.GHUser = "~/gh-"+name, name+"-gh"
			ids[name] = id
		}
	}
	return ids
}

func TestNestedRulesNeverLeak(t *testing.T) {
	for _, gh := range []bool{true, false} {
		name := "without gh"
		if gh {
			name = "with gh"
		}
		t.Run(name, func(t *testing.T) { checkNested(t, gh) })
	}
}

func checkNested(t *testing.T, gh bool) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	bin := filepath.Join(home, "bin", "git-idm")
	script(t, bin, "exit 0\n")
	c := config.New()
	c.Guard = config.GuardBlock
	c.Identities = nestedIdentities(gh)
	c.Rules = []config.Rule{
		{Path: "~/p/", Identity: "signer"}, {Path: "~/p/c/", Identity: "plain"},
		{Path: "~/q/", Identity: "plain"}, {Path: "~/q/c/", Identity: "signer"},
		{Path: "~/r/", Identity: "plain"}, {Path: "~/r/c/", Identity: "agent"},
		{Path: "~/s/", Identity: "agent"}, {Path: "~/s/c/", Identity: "plain"},
	}
	a := &Applier{ConfigDir: filepath.Join(home, ".config", "git-idm"), GlobalConfig: filepath.Join(home, ".gitconfig"), Bin: bin, Now: time.Now}
	if _, err := a.Apply(c); err != nil {
		t.Fatal(err)
	}
	signer := nestedWant{"s@x.example", "true", "~/.ssh/id_s.pub", "git@github.com:o/r.git", "signer"}
	plain := nestedWant{"p@x.example", "false", "", "https://github.com/o/r.git", "plain"}
	agent := nestedWant{"a@x.example", "false", "", "git@github.com:o/r.git", "agent"}
	cases := map[string]nestedWant{
		"p/repo": signer, "p/c/repo": plain,
		"q/repo": plain, "q/c/repo": signer,
		"r/repo": plain, "r/c/repo": agent,
		"s/repo": agent, "s/c/repo": plain,
	}
	for dir, want := range cases {
		repo := filepath.Join(home, dir)
		os.MkdirAll(repo, 0o755)
		gitOut := func(args ...string) string {
			cmd := exec.Command("git", args...)
			cmd.Dir = repo
			out, _ := cmd.Output()
			return strings.TrimSpace(string(out))
		}
		if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
			gitOut("init", "-q")
			gitOut("remote", "add", "o", "https://github.com/o/r.git")
		}
		ssh := gitOut("config", "core.sshCommand")
		got := nestedWant{
			email:      gitOut("config", "user.email"),
			gpgsign:    gitOut("config", "commit.gpgsign"),
			signingKey: gitOut("config", "user.signingkey"),
			url:        gitOut("remote", "get-url", "--push", "o"),
		}
		switch {
		case gh && strings.Contains(ssh, " ssh '"+want.ssh+"' "):
			got.ssh = want.ssh
		case !gh && want.ssh == "signer" && strings.Contains(ssh, "id_s"):
			got.ssh = want.ssh
		case !gh && want.ssh != "signer" && ssh == "ssh":
			got.ssh = want.ssh
		default:
			got.ssh = ssh
		}
		if got != want {
			t.Errorf("%s:\n got  %+v\n want %+v", dir, got, want)
		}
	}
}
