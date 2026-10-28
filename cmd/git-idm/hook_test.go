package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestZshHook(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin", "git-idm")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cfg := `default = "work"
[identities.work]
  name = "W"
  email = "w@x.example"
  gh_config_dir = "~/gh-work"
[identities.personal]
  name = "P"
  email = "p@x.example"
  ssh_key = "~/.ssh/id_p"
  gh_config_dir = "~/gh-personal"
[[rules]]
  path = "~/P/"
  identity = "personal"
`
	for _, d := range []string{".config/git-idm", "P/repo", "W/repo"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".config/git-idm/config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	script := bin + ` apply >/dev/null
eval "$(` + bin + ` hook zsh)"
p() { print -r -- "$1|${GH_CONFIG_DIR:-unset}|${_GIT_IDM_SEGMENT}|$(git var GIT_AUTHOR_IDENT | cut -d'>' -f1)>|$(git config core.sshCommand)" }
cd ~/P/repo && git init -q && p rule
cd ~/W/repo && git init -q && p default
git-idm use personal 2>/dev/null; p override
cd ~/P/repo; p override-moved
git-idm use --clear 2>/dev/null; p cleared
git-idm use ghost 2>&1 | head -1
print -r -- "rprompt=$RPROMPT"
`
	cmd := exec.Command(zsh, "-f", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GH_CONFIG_DIR=/leaked"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}
	sshP := "ssh -i '" + home + "/.ssh/id_p' -o IdentitiesOnly=yes"
	want := []string{
		"rule|" + home + "/gh-personal|%F{cyan}personal%f|P <p@x.example>|" + sshP,
		"default|" + home + "/gh-work|%F{cyan}work%f|W <w@x.example>|",
		"override|" + home + "/gh-personal|%B%F{red}personal (override)%f%b|P <p@x.example>|" + sshP,
		"override-moved|" + home + "/gh-personal|%B%F{red}personal (override)%f%b|P <p@x.example>|" + sshP,
		"cleared|" + home + "/gh-personal|%F{cyan}personal%f|P <p@x.example>|" + sshP,
		`git-idm: identity "ghost" does not exist`,
		"rprompt=${_GIT_IDM_SEGMENT}",
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got  %s\n want %s", i, got[i], want[i])
		}
	}
}
