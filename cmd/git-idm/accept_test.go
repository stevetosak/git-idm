package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/guard"
)

func acceptRepo(t *testing.T) (string, string) {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(repo, "no-gitconfig"))
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=M", "-c", "user.email=m@old.example", "commit", "-q", "--allow-empty", "--no-verify", "-m", "by martin"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	if err := guard.SavePending(repo, []string{sha}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	return repo, sha
}

func runAccept(t *testing.T, answer bool, askErr error) (string, error) {
	t.Helper()
	old := confirmOnTerminal
	t.Cleanup(func() { confirmOnTerminal = old })
	asked := ""
	confirmOnTerminal = func(question string) (bool, error) {
		asked = question
		return answer, askErr
	}
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"accept"})
	err := root.Execute()
	if !strings.Contains(asked, "Accept these 1 commit(s) for push?") {
		t.Errorf("question = %q", asked)
	}
	return out.String(), err
}

func TestAcceptYes(t *testing.T) {
	repo, sha := acceptRepo(t)
	out, err := runAccept(t, true, nil)
	if err != nil || !strings.Contains(out, "M <m@old.example>  by martin") || !strings.Contains(out, "accepted 1 commit(s)") {
		t.Fatalf("accept: %v\n%s", err, out)
	}
	accepted, _ := guard.AcceptedCommits(repo)
	if !accepted[sha] {
		t.Errorf("commit not accepted: %v", accepted)
	}
	if pending, _ := guard.Pending(repo); len(pending) != 0 {
		t.Errorf("pending left: %v", pending)
	}
}

func TestAcceptNoOrNoTerminalKeepsPending(t *testing.T) {
	for _, tc := range []struct {
		name   string
		askErr error
	}{{"no", nil}, {"no terminal", errors.New("device not configured")}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := acceptRepo(t)
			out, err := runAccept(t, false, tc.askErr)
			if (tc.askErr != nil) != (err != nil) {
				t.Fatalf("accept: %v\n%s", err, out)
			}
			if accepted, _ := guard.AcceptedCommits(repo); len(accepted) != 0 {
				t.Errorf("nothing may be accepted: %v", accepted)
			}
			if pending, _ := guard.Pending(repo); len(pending) != 1 {
				t.Errorf("pending must stay: %v", pending)
			}
		})
	}
}

func TestAcceptWithoutBlockedPush(t *testing.T) {
	repo, _ := acceptRepo(t)
	os.Remove(filepath.Join(repo, ".git", "git-idm", "pending"))
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"accept"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "run git push first") {
		t.Fatalf("err = %v", err)
	}
}
