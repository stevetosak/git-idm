package gitgen

import (
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

const block = BlockStart + "\n[include]\n" + BlockEnd + "\n"

func TestReplaceBlock(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"empty file":     {"", block},
		"append":         {"[user]\n\tname = x\n", "[user]\n\tname = x\n\n" + block},
		"replace in end": {"[a]\n\n" + BlockStart + "\nold\n" + BlockEnd + "\n", "[a]\n\n" + block},
		"move to end":    {BlockStart + "\nold\n" + BlockEnd + "\n[b]\n", "[b]\n\n" + block},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ReplaceBlock(tc.in, block)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
			again, _ := ReplaceBlock(got, block)
			if again != got {
				t.Fatalf("not idempotent:\n%q", again)
			}
		})
	}
}

func TestReplaceBlockBrokenMarkers(t *testing.T) {
	for _, in := range []string{
		BlockStart + "\n",
		BlockEnd + "\n",
		BlockEnd + "\n" + BlockStart + "\n",
		BlockStart + "\n" + BlockStart + "\n" + BlockEnd + "\n",
	} {
		if _, err := ReplaceBlock(in, block); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestRenderIdentityQuoting(t *testing.T) {
	t.Setenv("HOME", "/h/u")
	out := RenderIdentity("jo", config.Identity{
		Name: `Jo "JJ" \ Doe ; # x`, Email: "jo@x.example",
		SSHKey: "~/.ssh/it's key", Protocol: config.ProtocolSSH, GHConfigDir: "~/.config/gh-p",
	}, "/opt/my bin/git-idm", false)
	for _, want := range []string{
		`name = "Jo \"JJ\" \\ Doe ; # x"`,
		`sshCommand = "ssh -i '/h/u/.ssh/it'\\''s key' -o IdentitiesOnly=yes"`,
		"[credential \"https://github.com\"]\n\thelper = \"\"\n\thelper = " + gitQuote(CredentialHelper("/opt/my bin/git-idm", "jo")) + "\n",
		`exec '/opt/my bin/git-idm' credential 'jo' \"$@\"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\nin:\n%s", want, out)
		}
	}
	if strings.Contains(out, "insteadOf") {
		t.Errorf("the protocol has its own file:\n%s", out)
	}
	if got := RenderProtocol(config.Identity{Protocol: config.ProtocolSSH}); !strings.Contains(got, "[url \"git@github.com:\"]\n\tinsteadOf = \"https://github.com/\"") {
		t.Errorf("ssh protocol:\n%s", got)
	}
	if got := RenderProtocol(config.Identity{}); !strings.Contains(got, "[url \"https://github.com/\"]\n\tinsteadOf = \"https://github.com/\"") {
		t.Errorf("https protocol keeps https, also under an ssh parent:\n%s", got)
	}
	if strings.Contains(out, "GH_CONFIG_DIR") || strings.Count(out, "helper =") != 2 {
		t.Errorf("only the reset and the git-idm helper are allowed:\n%s", out)
	}
}

func TestRenderIdentityStrictCredential(t *testing.T) {
	plain := config.Identity{Name: "P", Email: "p@x.example"}
	if out := RenderIdentity("plain", plain, "/bin/git-idm", false); strings.Contains(out, "credential") {
		t.Errorf("no gh in the config: no helper expected:\n%s", out)
	}
	if out := RenderIdentity("plain", plain, "/bin/git-idm", true); !strings.Contains(out, "credential 'plain'") {
		t.Errorf("gh in the config: every identity needs the helper:\n%s", out)
	}
}

func TestRenderBlockOrdersDeeperRulesLast(t *testing.T) {
	c := config.New()
	c.Default = "a"
	c.Rules = []config.Rule{{Path: "~/x/y/", Identity: "b"}, {Path: "~/x/", Identity: "a"}}
	out := RenderBlock(c, func(n string) string { return n }, func(n string) string { return "proto-" + n }, "", "")
	order := func(a, b string) bool { return strings.Index(out, a) < strings.Index(out, b) }
	if !order("path = \"proto-b\"", "path = \"proto-a\"") {
		t.Fatalf("protocol: the deeper rule must come first, because git keeps the first equal insteadOf:\n%s", out)
	}
	if !order("path = \"proto-a\"", "path = \"a\"") || !order("path = \"a\"", "path = \"b\"") {
		t.Fatalf("identities: protocol files first, then the deeper rule last:\n%s", out)
	}
	if strings.Contains(out, "[include]") {
		t.Fatalf("the default identity must never be included for every folder:\n%s", out)
	}
}

func TestRemoveSectionOutsideBlock(t *testing.T) {
	in := "[user]\n\tname = x\n[includeIf \"gitdir/i:~/P/\"]\n\tpath = ~/.old\n[core]\n\teditor = vim\n" +
		BlockStart + "\n[includeIf \"gitdir/i:~/P/\"]\n\tpath = new\n" + BlockEnd + "\n"
	want := "[user]\n\tname = x\n[core]\n\teditor = vim\n" +
		BlockStart + "\n[includeIf \"gitdir/i:~/P/\"]\n\tpath = new\n" + BlockEnd + "\n"
	got, removed := RemoveSectionOutsideBlock(in, "includeif", "gitdir/i:~/P/")
	if got != want || removed != 1 {
		t.Fatalf("removed=%d got:\n%s", removed, got)
	}
	if _, removed := RemoveSectionOutsideBlock(want, "includeIf", "gitdir/i:~/P/"); removed != 0 {
		t.Fatal("must not touch the git-idm block")
	}
}
