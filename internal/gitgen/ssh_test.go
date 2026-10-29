package gitgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func TestSSHWrapperWithGit(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_SSH_COMMAND", "")
	os.Unsetenv("GIT_SSH_COMMAND")
	os.Unsetenv("GIT_SSH")
	log := filepath.Join(home, "calls.log")
	bin := filepath.Join(home, "bin", "git-idm")
	script(t, bin, `echo "git-idm $*" >> `+ShellQuote(log)+"\nexit 1\n")
	script(t, filepath.Join(home, "fake", "ssh"), `echo "ssh $*" >> `+ShellQuote(log)+"\nexit 1\n")
	t.Setenv("PATH", filepath.Join(home, "fake")+":/usr/bin:/bin")

	identity := filepath.Join(home, "work.gitconfig")
	id := config.Identity{Name: "W", Email: "w@x.example", SSHKey: "~/.ssh/id_work", GHUser: "w-gh", GHConfigDir: "~/gh"}
	os.WriteFile(identity, []byte(RenderIdentity("work", id, bin, true)), 0o644)
	global := filepath.Join(home, "global.gitconfig")
	os.WriteFile(global, []byte("[include]\n\tpath = "+identity+"\n"), 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	repo := filepath.Join(home, "repo")
	gitRun := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	calls := func() string {
		data, _ := os.ReadFile(log)
		os.Remove(log)
		return string(data)
	}
	os.MkdirAll(repo, 0o755)
	if out, err := gitRun("init", "-q"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := gitRun("-c", "user.email=w@x.example", "commit", "-q", "--allow-empty", "--no-verify", "-m", "x"); err != nil {
		t.Fatalf("%v %s", err, out)
	}

	gitRun("push", "git@github.com:o/r.git", "HEAD:refs/heads/main")
	if got := calls(); !strings.Contains(got, "git-idm ssh work ") || !strings.Contains(got, "git@github.com git-receive-pack 'o/r.git'") || strings.Contains(got, "\nssh ") || strings.HasPrefix(got, "ssh ") {
		t.Fatalf("push must go through git-idm ssh:\n%s", got)
	}

	os.Remove(bin)
	out, err := gitRun("push", "git@github.com:o/r.git", "HEAD:refs/heads/main")
	if err == nil || !strings.Contains(out, "not found, the push stops") {
		t.Fatalf("push without git-idm must stop: %v\n%s", err, out)
	}
	if got := calls(); got != "" {
		t.Fatalf("ssh must not run for a push without git-idm:\n%s", got)
	}
	gitRun("ls-remote", "git@github.com:o/r.git")
	if got := calls(); !strings.Contains(got, "ssh -i "+home+"/.ssh/id_work -o IdentitiesOnly=yes") || !strings.Contains(got, "git-upload-pack") {
		t.Fatalf("fetch without git-idm uses plain ssh with the key:\n%s", got)
	}
}
