package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnboundFolderStopsEveryAction(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin", "git-idm")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cfg := `default = "work"
guard = "block"
[identities.work]
  name = "W"
  email = "w@x.example"
  gh_user = "w-gh"
  gh_config_dir = "~/gh-work"
[[rules]]
  path = "~/bound/"
  identity = "work"
`
	os.MkdirAll(filepath.Join(home, ".config", "git-idm"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "git-idm", "config.toml"), []byte(cfg), 0o600)
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Old\n\temail = old@x.example\n"), 0o644)
	fake := filepath.Join(home, "fake")
	os.MkdirAll(fake, 0o755)
	sshLog := filepath.Join(home, "ssh.log")
	os.WriteFile(filepath.Join(fake, "ssh"), []byte("#!/bin/sh\necho \"ssh $*\" >> '"+sshLog+"'\nexit 1\n"), 0o755)
	env := []string{"HOME=" + home, "PATH=" + fake + ":" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
	run := func(dir, stdin string, name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run(home, "", bin, "apply"); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	unbound := filepath.Join(home, "loose")
	bound := filepath.Join(home, "bound", "repo")
	for _, dir := range []string{unbound, bound} {
		os.MkdirAll(dir, 0o755)
		if out, err := run(dir, "", "git", "init", "-q"); err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
	}

	for _, args := range [][]string{{"commit", "-q", "--allow-empty", "-m", "x"}, {"commit", "-q", "--allow-empty", "--no-verify", "-m", "x"}} {
		out, err := run(unbound, "", "git", args...)
		if err == nil || !strings.Contains(out, "empty ident name (for <no identity here, run: git-idm bind>) not allowed") {
			t.Fatalf("git itself must refuse %v: %v\n%s", args, err, out)
		}
	}
	out, err := run(unbound, "", bin, "check-commit")
	if err == nil || !strings.Contains(out, "git-idm blocked: no identity is bound to this folder") || !strings.Contains(out, "git-idm bind  (binds this folder to work)") {
		t.Fatalf("guard: %v\n%s", err, out)
	}
	out, err = run(unbound, "protocol=https\nhost=github.com\n\n", "git", "credential", "fill")
	if err == nil || strings.Contains(out, "password=") || !strings.Contains(out, "no identity is bound to this folder, so git stops here") {
		t.Fatalf("credential: %v\n%s", err, out)
	}
	if out, err := run(unbound, "", "git", "-c", "user.name=X", "-c", "user.email=x@x.example", "commit", "-q", "--allow-empty", "--no-verify", "-m", "x"); err != nil {
		t.Fatalf("commit with explicit values: %v\n%s", err, out)
	}
	out, err = run(unbound, "", "git", "push", "git@github.com:o/r.git", "HEAD")
	if err == nil || !strings.Contains(out, "no identity is bound to this folder, so the push stops here") {
		t.Fatalf("ssh push: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(sshLog); len(data) != 0 {
		t.Fatalf("ssh must not run for an unbound push:\n%s", data)
	}
	run(unbound, "", "git", "ls-remote", "git@github.com:o/r.git")
	if data, _ := os.ReadFile(sshLog); !strings.Contains(string(data), "git-upload-pack") {
		t.Fatalf("fetch must still run ssh:\n%s", data)
	}

	if out, err := run(bound, "", "git", "commit", "-q", "--allow-empty", "-m", "x"); err != nil {
		t.Fatalf("bound commit: %v\n%s", err, out)
	}
	if out, _ := run(bound, "", "git", "log", "-1", "--format=%an <%ae> %cn <%ce>"); strings.TrimSpace(out) != "W <w@x.example> W <w@x.example>" {
		t.Fatalf("bound author = %q", out)
	}

	if out, err := run(home, "", bin, "guard", "off"); err != nil {
		t.Fatalf("guard off: %v\n%s", err, out)
	}
	if out, err := run(unbound, "", "git", "commit", "-q", "--allow-empty", "-m", "x"); err != nil {
		t.Fatalf("guard off is an opt-out: %v\n%s", err, out)
	}
	if out, _ := run(unbound, "", "git", "log", "-1", "--format=%ae"); strings.TrimSpace(out) != "old@x.example" {
		t.Fatalf("guard off must not add the default either, got %q", out)
	}
}
