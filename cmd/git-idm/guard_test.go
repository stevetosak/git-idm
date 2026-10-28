package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/guard"
	"github.com/stevetosak/git-idm/internal/resolve"
)

func TestGuardEndToEnd(t *testing.T) {
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
[identities.personal]
  name = "P"
  email = "p@x.example"
[[rules]]
  path = "~/P/"
  identity = "personal"
`
	if err := os.MkdirAll(filepath.Join(home, ".config/git-idm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config/git-idm/config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
eval "$(` + bin + ` hook zsh)"
c() { local label=$1 err=$HOME/commit-err; shift; if git commit -q --allow-empty -m x "$@" 2>$err; then print -r -- "$label ok$(head -1 $err | sed 's/^/ /')"; else print -r -- "$label blocked: $(head -1 $err)"; fi; }
git-idm guard block >/dev/null
mkdir -p ~/P/repo && cd ~/P/repo && git init -q
printf '#!/bin/sh\necho ran >> "$(git rev-parse --git-dir)/marker"\n' > .git/hooks/pre-commit
printf '#!/bin/sh\necho msg >> "$(git rev-parse --git-dir)/marker"\n' > .git/hooks/commit-msg
chmod +x .git/hooks/pre-commit .git/hooks/commit-msg
c normal
print -r -- "repo hooks: $(tr '\n' ' ' < .git/marker)"
git config user.email other@x.example
c local-author
c no-verify --no-verify
git-idm allow >/dev/null; c allowed
git-idm allow --remove >/dev/null; git config --unset user.email
git-idm use work 2>/dev/null; c override
git-idm use --clear 2>/dev/null
GIT_AUTHOR_EMAIL=env@x.example c env-author
git config user.email other@x.example
git-idm guard warn >/dev/null; c local-warn
git-idm guard block >/dev/null
mv ` + bin + ` ` + bin + `.gone; c binary-missing; mv ` + bin + `.gone ` + bin + `
git-idm guard off >/dev/null; print -r -- "hooksPath after off: $(git config --global core.hooksPath || echo unset)"
`
	cmd := exec.Command(zsh, "-f", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}
	want := []string{
		"normal ok",
		"repo hooks: ran msg ",
		"local-author blocked: git-idm blocked: this commit uses the wrong identity",
		"no-verify ok",
		"allowed ok",
		"override ok",
		"env-author blocked: git-idm blocked: this commit uses the wrong identity",
		"local-warn ok git-idm warning: this commit uses the wrong identity",
		"binary-missing ok git-idm: identity guard skipped, " + bin + " not found (run: git-idm doctor)",
		"hooksPath after off: unset",
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

func TestMismatchHintForLocalAuthor(t *testing.T) {
	c := config.New()
	c.Guard = config.GuardBlock
	v := guard.Verdict{Actual: "martin@old.example", Expected: "w@x.example", ExpectedFrom: "W"}
	rule := &config.Rule{Path: "~/Projects/", Identity: "work"}

	var out bytes.Buffer
	printMismatch(&out, c, resolve.Result{Identity: "work", Source: resolve.SourceRule, Rule: rule, LocalEmail: "martin@old.example"}, v)
	if !strings.Contains(out.String(), "git-idm migrate") || strings.Contains(out.String(), "--unset") {
		t.Errorf("under a rule:\n%s", out.String())
	}

	out.Reset()
	printMismatch(&out, c, resolve.Result{Identity: "work", Source: resolve.SourceDefault, LocalEmail: "martin@old.example"}, v)
	if !strings.Contains(out.String(), "git config --local --unset-all user.email") || strings.Contains(out.String(), "migrate") {
		t.Errorf("without a rule:\n%s", out.String())
	}
}
