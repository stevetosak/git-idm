package gitgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func credentialFill(t *testing.T, home string) (string, error) {
	t.Helper()
	repo := filepath.Join(home, "repo")
	if _, err := os.Stat(repo); err != nil {
		os.MkdirAll(repo, 0o755)
		if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		evil := "!f() { touch " + ShellQuote(filepath.Join(home, "evil-helper-ran")) + "; echo username=evil; echo password=evil; }; f"
		if out, err := exec.Command("git", "-C", repo, "config", "--local", "credential.helper", evil).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v %s", err, out)
		}
	}
	cmd := exec.Command("git", "credential", "fill")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\npath=o/r.git\n\n")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS="+filepath.Join(home, "askpass"), "SSH_ASKPASS=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCredentialHelperStopsGit(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bin := filepath.Join(home, "bin", "git-idm")
	script(t, filepath.Join(home, "askpass"), "touch "+ShellQuote(filepath.Join(home, "askpass-ran"))+"\necho evil\n")
	identity := filepath.Join(home, "work.gitconfig")
	os.WriteFile(identity, []byte(RenderIdentity("work", config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/gh"}, bin, true)), 0o644)
	global := filepath.Join(home, "global.gitconfig")
	os.WriteFile(global, []byte("[credential]\n\thelper = "+gitQuote("!echo username=keychain; echo password=keychain")+"\n[include]\n\tpath = "+identity+"\n"), 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	ran := func(name string) bool {
		_, err := os.Stat(filepath.Join(home, name))
		os.Remove(filepath.Join(home, name))
		return err == nil
	}

	script(t, bin, `[ "$1 $2 $3" = "credential work get" ] || exit 9`+"\ncat >/dev/null\necho username=w-gh\necho password=good\n")
	out, err := credentialFill(t, home)
	if err != nil || !strings.Contains(out, "username=w-gh\npassword=good") {
		t.Fatalf("good token: %v\n%s", err, out)
	}

	script(t, bin, "cat >/dev/null\necho quit=1\n")
	out, err = credentialFill(t, home)
	if err == nil || strings.Contains(out, "password=") {
		t.Fatalf("quit must stop git: %v\n%s", err, out)
	}

	os.Remove(bin)
	out, err = credentialFill(t, home)
	if err == nil || strings.Contains(out, "password=") || !strings.Contains(out, "not found, git stops") {
		t.Fatalf("missing binary must stop git: %v\n%s", err, out)
	}
	if ran("evil-helper-ran") || ran("askpass-ran") {
		t.Fatal("git asked a later helper or askpass")
	}
}
