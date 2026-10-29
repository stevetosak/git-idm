package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushGuardEndToEnd(t *testing.T) {
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
[[rules]]
  path = "~/repo/"
  identity = "work"
`
	os.MkdirAll(filepath.Join(home, ".config", "git-idm"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "git-idm", "config.toml"), []byte(cfg), 0o600)
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}
	run := func(dir string, extra []string, name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(append([]string{}, env...), extra...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	must := func(dir string, extra []string, name string, args ...string) {
		t.Helper()
		if out, err := run(dir, extra, name, args...); err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	must(home, nil, bin, "apply")
	remote := filepath.Join(home, "remote.git")
	repo := filepath.Join(home, "repo")
	must(home, nil, "git", "init", "-q", "--bare", remote)
	os.MkdirAll(repo, 0o755)
	must(repo, nil, "git", "init", "-q", "-b", "main")
	must(repo, nil, "git", "remote", "add", "origin", remote)
	os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-push"), []byte("#!/bin/sh\ncat > \"$(git rev-parse --git-dir)/repo-hook-input\"\n"), 0o755)

	must(repo, nil, "git", "commit", "-q", "--allow-empty", "-m", "good")
	if out, err := run(repo, nil, "git", "push", "-q", "origin", "main"); err != nil {
		t.Fatalf("good push: %v\n%s", err, out)
	}
	input, _ := os.ReadFile(filepath.Join(repo, ".git", "repo-hook-input"))
	if !strings.HasPrefix(string(input), "refs/heads/main ") {
		t.Fatalf("the repo pre-push hook must get the ref lines, got %q", input)
	}

	out, err := run(repo, []string{"GIT_COMMITTER_EMAIL=bot@ci.example"}, "git", "commit", "-q", "--allow-empty", "-m", "bad committer")
	if err == nil || !strings.Contains(out, "committer: bot@ci.example  (from GIT_COMMITTER_EMAIL)") {
		t.Fatalf("pre-commit must block a wrong committer: %v\n%s", err, out)
	}
	must(repo, []string{"GIT_COMMITTER_EMAIL=bot@ci.example"}, "git", "commit", "-q", "--allow-empty", "--no-verify", "-m", "bad committer")
	out, err = run(repo, nil, "git", "push", "-q", "origin", "main")
	if err == nil || !strings.Contains(out, "git-idm blocked: this push does not match work") || !strings.Contains(out, "committer bot@ci.example") {
		t.Fatalf("bad committer must be blocked: %v\n%s", err, out)
	}

	must(repo, nil, "git", "commit", "-q", "--amend", "--allow-empty", "--reset-author", "--no-edit")
	if out, err := run(repo, nil, "git", "push", "-q", "origin", "main"); err != nil {
		t.Fatalf("fixed commit: %v\n%s", err, out)
	}

	must(repo, nil, "git", "commit", "-q", "--allow-empty", "--no-verify", "--author", "Martin <martin@old.example>", "-m", "bad author")
	out, err = run(repo, nil, "git", "push", "-q", "origin", "main")
	if err == nil || !strings.Contains(out, "author martin@old.example") {
		t.Fatalf("bad author must be blocked: %v\n%s", err, out)
	}
	if out, err := run(repo, nil, "git", "push", "-q", "--no-verify", "origin", "main"); err != nil {
		t.Fatalf("--no-verify: %v\n%s", err, out)
	}
	must(repo, nil, "git", "commit", "-q", "--allow-empty", "-m", "after")
	if out, err := run(repo, nil, "git", "push", "-q", "origin", "main"); err != nil {
		t.Fatalf("commits already on the remote must not block: %v\n%s", err, out)
	}

	must(home, nil, bin, "guard", "warn")
	must(repo, nil, "git", "commit", "-q", "--allow-empty", "--no-verify", "--author", "X <x@other.example>", "-m", "warn")
	out, err = run(repo, nil, "git", "push", "-q", "origin", "main")
	if err != nil || !strings.Contains(out, "git-idm warning: this push does not match work") {
		t.Fatalf("warn mode must push with a warning: %v\n%s", err, out)
	}
}
