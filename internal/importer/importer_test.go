package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sandboxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GH_CONFIG_DIR", "/should/be/ignored")
	return home
}

func TestImportDirenvSetup(t *testing.T) {
	home := sandboxHome(t)
	write(t, filepath.Join(home, ".gitconfig"), `[user]
	name = Jane Doe
	email = jane@work.example
[includeIf "gitdir/i:~/Personal/"]
	path = ~/.gitconfig-personal
[includeIf "hasconfig:remote.*.url:git@github.com:acme/**"]
	path = ~/.gitconfig-acme
`)
	write(t, filepath.Join(home, ".gitconfig-personal"), `[user]
	email = jane@home.example
[core]
	sshCommand = ssh -F ~/.ssh/config-personal
[credential]
	helper =
[url "git@github.com:"]
	insteadOf = https://github.com/
`)
	write(t, filepath.Join(home, ".ssh", "config-personal"), "Host *\n  IdentityFile ~/.ssh/id_personal\n  IdentitiesOnly yes\n")
	write(t, filepath.Join(home, "Personal", ".envrc"), `export GH_CONFIG_DIR="$HOME/.config/gh-personal"`+"\n")
	write(t, filepath.Join(home, ".config", "gh", "hosts.yml"), "github.com:\n    user: jane-work\n")
	write(t, filepath.Join(home, ".config", "gh-personal", "hosts.yml"), "github.com:\n    user: jane\n")

	res, err := Import(Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config

	if c.Default != "work" {
		t.Fatalf("default = %q", c.Default)
	}
	work := c.Identities["work"]
	if work.Email != "jane@work.example" || work.GHUser != "jane-work" || work.GHConfigDir != "~/.config/gh" || work.SSHKey != "" {
		t.Fatalf("work = %+v", work)
	}
	personal, ok := c.Identities["personal"]
	if !ok {
		t.Fatalf("no personal identity: %v", c.Names())
	}
	want := config.Identity{
		Name: "Jane Doe", Email: "jane@home.example", SSHKey: "~/.ssh/id_personal",
		GHUser: "jane", GHConfigDir: "~/.config/gh-personal", Protocol: config.ProtocolSSH,
	}
	if personal != want {
		t.Fatalf("personal = %+v\nwant      %+v", personal, want)
	}
	if len(c.Rules) != 1 || c.Rules[0] != (config.Rule{Path: "~/Personal/", Identity: "personal"}) {
		t.Fatalf("rules = %+v", c.Rules)
	}
	if len(res.Skipped) != 1 {
		t.Fatalf("skipped = %v", res.Skipped)
	}
	kinds := map[LegacyKind]int{}
	for _, l := range res.Legacy {
		kinds[l.Kind]++
	}
	if kinds[LegacyInclude] != 1 || kinds[LegacyFile] != 1 || kinds[LegacyEnvrc] != 1 {
		t.Fatalf("legacy = %+v", res.Legacy)
	}
}

func TestImportEmptyHome(t *testing.T) {
	sandboxHome(t)
	res, err := Import(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Config.Identities) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("identities=%v skipped=%v", res.Config.Names(), res.Skipped)
	}
}

func TestImportRejectsBadDefaultName(t *testing.T) {
	sandboxHome(t)
	if _, err := Import(Options{DefaultName: "Bad Name"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestIdentityName(t *testing.T) {
	cases := map[[2]string]string{
		{"~/.gitconfig-personal", "~/Personal/"}: "personal",
		{"~/.gitconfig.Client X", "~/c/"}:        "client-x",
		{"~/git/acme.inc", "~/code/Acme Inc/"}:   "acme-inc",
	}
	for in, want := range cases {
		if got := identityName(in[0], in[1]); got != want {
			t.Errorf("identityName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
