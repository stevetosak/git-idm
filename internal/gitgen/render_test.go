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
	out := RenderIdentity(config.Identity{
		Name: `Jo "JJ" \ Doe ; # x`, Email: "jo@x.example",
		SSHKey: "~/.ssh/it's key", Protocol: config.ProtocolSSH, GHConfigDir: "~/.config/gh-p",
	})
	for _, want := range []string{
		`name = "Jo \"JJ\" \\ Doe ; # x"`,
		`sshCommand = "ssh -i '/h/u/.ssh/it'\\''s key' -o IdentitiesOnly=yes"`,
		"[url \"git@github.com:\"]\n\tinsteadOf = \"https://github.com/\"",
		"[credential \"https://github.com\"]\n\thelper = \"\"\n\thelper = \"!GH_CONFIG_DIR='/h/u/.config/gh-p' ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\nin:\n%s", want, out)
		}
	}
}

func TestRenderBlockOrdersDeeperRulesLast(t *testing.T) {
	c := config.New()
	c.Default = "a"
	c.Rules = []config.Rule{{Path: "~/x/y/", Identity: "b"}, {Path: "~/x/", Identity: "a"}}
	out := RenderBlock(c, func(n string) string { return n }, "")
	if strings.Index(out, "~/x/y/") < strings.Index(out, "~/x/\"") {
		t.Fatalf("deeper rule must come last:\n%s", out)
	}
	if !strings.HasPrefix(out, BlockStart+"\n[include]\n\tpath = \"a\"\n") {
		t.Fatalf("default include must come first:\n%s", out)
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
