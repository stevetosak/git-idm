package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
)

func pushRepo(t *testing.T) (string, string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	for _, k := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	repo := filepath.Join(home, "repo")
	os.MkdirAll(repo, 0o755)
	gitT(t, repo, nil, "init", "-q")
	return home, repo
}

func gitT(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, repo, author, committer string) string {
	t.Helper()
	env := []string{"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=" + author, "GIT_COMMITTER_NAME=C", "GIT_COMMITTER_EMAIL=" + committer}
	gitT(t, repo, env, "commit", "-q", "--allow-empty", "--no-verify", "-m", "x")
	return gitT(t, repo, nil, "rev-parse", "HEAD")
}

func work() config.Identity {
	return config.Identity{Name: "W", Email: "w@x.example", GHUser: "w-gh", GHConfigDir: "~/gh"}
}

func update(sha string) []RefUpdate {
	return []RefUpdate{{LocalRef: "refs/heads/main", LocalSHA: sha, RemoteRef: "refs/heads/main", RemoteSHA: zeroSHA}}
}

func texts(problems []Problem) string {
	var out []string
	for _, p := range problems {
		out = append(out, p.Text)
	}
	return strings.Join(out, "\n")
}

func TestPushChecksEveryNewCommit(t *testing.T) {
	_, repo := pushRepo(t)
	p := Push{Dir: repo, URL: "/tmp/remote.git", Name: "work", Identity: work(), Getenv: func(string) string { return "" }}

	good := commit(t, repo, "w@x.example", "W@X.example")
	if problems, err := CheckPush(p, update(good)); err != nil || len(problems) != 0 {
		t.Fatalf("good commit: %v %v", err, problems)
	}
	commit(t, repo, "martin@old.example", "w@x.example")
	commit(t, repo, "w@x.example", "ci@bot.example")
	last := commit(t, repo, "evil@x.example", "evil@x.example")
	problems, err := CheckPush(p, update(last))
	if err != nil {
		t.Fatal(err)
	}
	got := texts(problems)
	for _, want := range []string{"3 commit(s) to push do not use work <w@x.example>", "author and committer evil@x.example", "committer ci@bot.example", "author martin@old.example"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	p.Allowed = "martin@old.example"
	problems, _ = CheckPush(p, update(last))
	if strings.Contains(texts(problems), "martin@old.example") || !strings.Contains(texts(problems), "2 commit(s)") {
		t.Errorf("allowed author still reported:\n%s", texts(problems))
	}

	gitT(t, repo, nil, "update-ref", "refs/remotes/origin/main", last)
	if problems, err := CheckPush(p, update(last)); err != nil || len(problems) != 0 {
		t.Errorf("commits already on a remote must not be checked again: %v %v", err, problems)
	}
	deletion := []RefUpdate{{LocalRef: "(delete)", LocalSHA: zeroSHA, RemoteRef: "refs/heads/old", RemoteSHA: last}}
	if problems, err := CheckPush(p, deletion); err != nil || len(problems) != 0 {
		t.Errorf("a deletion has no commits: %v %v", err, problems)
	}
}

func TestPushTransport(t *testing.T) {
	home, repo := pushRepo(t)
	bin := "/opt/git-idm"
	id := work()
	os.MkdirAll(filepath.Join(home, "gh"), 0o700)
	os.WriteFile(filepath.Join(home, "gh", "hosts.yml"), []byte("github.com:\n    user: w-gh\n"), 0o600)
	cases := []struct {
		name   string
		url    string
		env    map[string]string
		config [][2]string
		want   string
	}{
		{name: "clean https", url: "https://github.com/o/r.git"},
		{name: "token in url", url: "https://w-gh:ghp_secret@github.com/o/r.git", want: "the remote URL has a password or token in it"},
		{name: "other user in url", url: "https://martin@github.com/o/r.git", want: "the remote URL names the user martin, but work uses w-gh"},
		{name: "own user in url", url: "https://W-GH@github.com/o/r.git"},
		{name: "GIT_SSH_COMMAND on ssh", url: "git@github.com:o/r.git", env: map[string]string{"GIT_SSH_COMMAND": "ssh -i other"}, want: "GIT_SSH_COMMAND is set"},
		{name: "GIT_SSH on ssh url", url: "ssh://git@github.com/o/r.git", env: map[string]string{"GIT_SSH": "/usr/bin/ssh"}, want: "GIT_SSH is set"},
		{name: "GIT_SSH_COMMAND on https", url: "https://github.com/o/r.git", env: map[string]string{"GIT_SSH_COMMAND": "ssh"}},
		{name: "command line sshCommand", url: "git@github.com:o/r.git", config: [][2]string{{"core.sshCommand", "ssh -i other"}}, want: "core.sshCommand is set for this git command only"},
		{name: "use override sshCommand", url: "git@github.com:o/r.git", config: [][2]string{{"core.sshCommand", gitgen.SSHWrapper(bin, "work", id)}}},
		{name: "command line helper", url: "https://github.com/o/r.git", config: [][2]string{{"credential.helper", ""}, {"credential.helper", "store"}}, want: "can give another GitHub account"},
		{name: "command line reset only", url: "https://github.com/o/r.git", config: [][2]string{{"credential.https://github.com.helper", ""}}, want: "removes the git-idm helper"},
		{name: "use override helper", url: "https://github.com/o/r.git", config: [][2]string{{"credential.https://github.com.helper", ""}, {"credential.https://github.com.helper", gitgen.CredentialHelper(bin, "work")}}},
		{name: "gh pr create helper", url: "https://github.com/o/r.git", env: map[string]string{"GH_CONFIG_DIR": filepath.Join(home, "gh")},
			config: [][2]string{{"credential.https://github.com.helper", ""}, {"credential.https://github.com.helper", "!/opt/homebrew/bin/gh auth git-credential"}}},
		{name: "gh helper with other login", url: "https://github.com/o/r.git", env: map[string]string{"GH_CONFIG_DIR": "/elsewhere"},
			config: [][2]string{{"credential.https://github.com.helper", ""}, {"credential.https://github.com.helper", "!/opt/homebrew/bin/gh auth git-credential"}}, want: "can give another GitHub account"},
		{name: "gh helper with token env", url: "https://github.com/o/r.git", env: map[string]string{"GH_CONFIG_DIR": filepath.Join(home, "gh"), "GH_TOKEN": "x"},
			config: [][2]string{{"credential.https://github.com.helper", "!/opt/homebrew/bin/gh auth git-credential"}}, want: "can give another GitHub account"},
		{name: "helper for another host", url: "https://github.com/o/r.git", config: [][2]string{{"credential.https://gitlab.com.helper", "store"}}},
		{name: "extra header", url: "https://github.com/o/r.git", config: [][2]string{{"http.https://github.com/.extraHeader", "AUTHORIZATION: bearer x"}}, want: "sends its own Authorization header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"GIT_CONFIG_COUNT", "GH_CONFIG_DIR", "GH_TOKEN"} {
				t.Setenv(k, "")
				os.Unsetenv(k)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if len(tc.config) > 0 {
				t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(len(tc.config)))
				for i, kv := range tc.config {
					t.Setenv("GIT_CONFIG_KEY_"+strconv.Itoa(i), kv[0])
					t.Setenv("GIT_CONFIG_VALUE_"+strconv.Itoa(i), kv[1])
				}
			}
			p := Push{Dir: repo, URL: tc.url, Name: "work", Identity: id, Bin: bin, Strict: true, Getenv: os.Getenv}
			problems, err := CheckPush(p, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := texts(problems)
			if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
			if strings.Contains(got, "ghp_secret") {
				t.Fatalf("the token must not be printed: %q", got)
			}
			for _, pr := range problems {
				if strings.Contains(pr.Fix, "ghp_secret") {
					t.Fatalf("the token must not be printed in the fix: %q", pr.Fix)
				}
			}
		})
	}
}

func TestParsePushInput(t *testing.T) {
	sha := strings.Repeat("a", 40)
	got, err := ParsePushInput(strings.NewReader("refs/heads/x " + sha + " refs/heads/x " + zeroSHA + "\n\n"))
	if err != nil || len(got) != 1 || got[0].LocalSHA != sha {
		t.Fatalf("%v %+v", err, got)
	}
	for _, bad := range []string{"refs/heads/x abc refs/heads/x " + zeroSHA, "one two three", "refs/heads/x " + sha + " refs/heads/x " + zeroSHA + " extra"} {
		if _, err := ParsePushInput(strings.NewReader(bad)); err == nil {
			t.Errorf("want error for %q", bad)
		}
	}
}
