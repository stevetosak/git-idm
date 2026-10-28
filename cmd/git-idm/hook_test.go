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
	home, bin := hookHome(t)
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
	checkLines(t, string(out), []string{
		"rule|" + home + "/gh-personal|%F{cyan}personal%f|P <p@x.example>|" + sshP,
		"default|" + home + "/gh-work|%F{cyan}work%f|W <w@x.example>|",
		"override|" + home + "/gh-personal|%B%F{red}personal (override)%f%b|P <p@x.example>|" + sshP,
		"override-moved|" + home + "/gh-personal|%B%F{red}personal (override)%f%b|P <p@x.example>|" + sshP,
		"cleared|" + home + "/gh-personal|%F{cyan}personal%f|P <p@x.example>|" + sshP,
		`git-idm: identity "ghost" does not exist`,
		"rprompt=${_GIT_IDM_SEGMENT}",
	})
}

func TestBashHook(t *testing.T) {
	shells := map[string]bool{}
	for _, candidate := range []string{"/bin/bash", "bash"} {
		if path, err := exec.LookPath(candidate); err == nil {
			if real, err := filepath.EvalSymlinks(path); err == nil {
				shells[real] = true
			}
		}
	}
	if len(shells) == 0 {
		t.Skip("bash not installed")
	}
	home, bin := hookHome(t)
	script := bin + ` apply >/dev/null
PS1='\$ '
PROMPT_COMMAND='echo -n'
eval "$(` + bin + ` hook bash)"
eval "$(` + bin + ` hook bash)"
prompt() { eval "$PROMPT_COMMAND"; }
p() { printf '%s|%s|%s|%s>|%s\n' "$1" "${GH_CONFIG_DIR:-unset}" "${_GIT_IDM_SEGMENT}" "$(git var GIT_AUTHOR_IDENT | cut -d'>' -f1)" "$(git config core.sshCommand)"; }
cd ~/P/repo && git init -q && prompt && p rule
cd ~/W/repo && git init -q && prompt && p default
git-idm use personal 2>/dev/null; p override
cd ~/P/repo; prompt; p override-moved
git-idm use --clear 2>/dev/null; p cleared
git-idm use ghost 2>&1 | head -1
printf 'ps1=%s\n' "$PS1"
printf 'prompt_command=%s\n' "$PROMPT_COMMAND"
`
	sshP := "ssh -i '" + home + "/.ssh/id_p' -o IdentitiesOnly=yes"
	seg := func(code, label string) string { return "\x01\x1b[" + code + "m\x02" + label + "\x01\x1b[0m\x02" }
	want := []string{
		"rule|" + home + "/gh-personal|" + seg("36", "personal") + "|P <p@x.example>|" + sshP,
		"default|" + home + "/gh-work|" + seg("36", "work") + "|W <w@x.example>|",
		"override|" + home + "/gh-personal|" + seg("1;31", "personal (override)") + "|P <p@x.example>|" + sshP,
		"override-moved|" + home + "/gh-personal|" + seg("1;31", "personal (override)") + "|P <p@x.example>|" + sshP,
		"cleared|" + home + "/gh-personal|" + seg("36", "personal") + "|P <p@x.example>|" + sshP,
		`git-idm: identity "ghost" does not exist`,
		"ps1=${_GIT_IDM_SEGMENT:+$_GIT_IDM_SEGMENT }\\$ ",
		"prompt_command=_git_idm_prompt; echo -n",
	}
	for sh := range shells {
		t.Run(sh, func(t *testing.T) {
			cmd := exec.Command(sh, "--norc", "--noprofile", "-c", script)
			cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GH_CONFIG_DIR=/leaked"}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, out)
			}
			checkLines(t, string(out), want)
		})
	}
}

func TestFishHook(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	home, bin := hookHome(t)
	script := bin + ` apply >/dev/null
function fish_right_prompt
    echo -n USER
end
` + bin + ` hook fish | source
` + bin + ` hook fish | source
function p
    set -l gh unset
    set -q GH_CONFIG_DIR; and set gh $GH_CONFIG_DIR
    set -l author (git var GIT_AUTHOR_IDENT | cut -d'>' -f1)
    set -l ssh (git config core.sshCommand)
    printf '%s|%s|%s|%s>|%s\n' $argv[1] "$gh" "$_GIT_IDM_SEGMENT" "$author" "$ssh"
end
cd ~/P/repo; and git init -q; and p rule
cd ~/W/repo; and git init -q; and p default
git-idm use personal 2>/dev/null; p override
cd ~/P/repo; p override-moved
git-idm use --clear 2>/dev/null; p cleared
git-idm use ghost 2>&1 | head -1
printf 'rprompt=%s\n' (fish_right_prompt)
`
	cmd := exec.Command(fish, "--no-config", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GH_CONFIG_DIR=/leaked"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish: %v\n%s", err, out)
	}
	sshP := "ssh -i '" + home + "/.ssh/id_p' -o IdentitiesOnly=yes"
	seg := func(code, label string) string { return "\x1b[" + code + "m" + label + "\x1b[0m" }
	checkLines(t, string(out), []string{
		"rule|" + home + "/gh-personal|" + seg("36", "personal") + "|P <p@x.example>|" + sshP,
		"default|" + home + "/gh-work|" + seg("36", "work") + "|W <w@x.example>|",
		"personal is active in this shell only. Clear with: git-idm use --clear",
		"override|" + home + "/gh-personal|" + seg("1;31", "personal (override)") + "|P <p@x.example>|" + sshP,
		"override-moved|" + home + "/gh-personal|" + seg("1;31", "personal (override)") + "|P <p@x.example>|" + sshP,
		"override cleared, personal is active: rule ~/P/",
		"cleared|" + home + "/gh-personal|" + seg("36", "personal") + "|P <p@x.example>|" + sshP,
		`git-idm: identity "ghost" does not exist`,
		"rprompt=" + seg("36", "personal") + " USER",
	})
}

func checkLines(t *testing.T, out string, want []string) {
	t.Helper()
	got := strings.Split(strings.TrimSpace(out), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got  %q\n want %q", i, got[i], want[i])
		}
	}
}

func hookHome(t *testing.T) (home, bin string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin = filepath.Join(home, "bin", "git-idm")
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
	return home, bin
}
