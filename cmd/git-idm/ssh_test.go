package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
)

type sshStub struct {
	hostname string
	login    string
	err      error
	calls    [][]string
}

func sshSandbox(t *testing.T) (string, *sshStub) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", SSHKey: "~/.ssh/id_work", GHUser: "w-gh", GHConfigDir: "~/gh"}
	c.Identities["agent"] = config.Identity{Name: "A", Email: "a@x.example", GHUser: "a-gh", GHConfigDir: "~/gh-a"}
	c.Identities["nouser"] = config.Identity{Name: "N", Email: "n@x.example", SSHKey: "~/.ssh/id_work"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "id_work"), []byte("key-1"), 0o600)

	stub := &sshStub{hostname: "github.com", login: "w-gh"}
	oldResolve, oldAccount := resolveSSH, sshAccountOf
	t.Cleanup(func() { resolveSSH, sshAccountOf = oldResolve, oldAccount })
	resolveSSH = func(ssh string, args []string) (map[string]string, error) {
		return map[string]string{"hostname": stub.hostname, "port": "22", "user": "git"}, nil
	}
	sshAccountOf = func(ssh string, args []string) (string, error) {
		stub.calls = append(stub.calls, args)
		return stub.login, stub.err
	}
	return home, stub
}

const pushCmd = "git-receive-pack 'o/r.git'"

func TestSSHChecksAccountOnPush(t *testing.T) {
	home, stub := sshSandbox(t)
	key := filepath.Join(home, ".ssh", "id_work")

	argv, err := sshArgs("work", []string{"-o", "SendEnv=GIT_PROTOCOL", "git@github.com", "git-upload-pack 'o/r.git'"}, "/usr/bin/ssh")
	if err != nil || len(stub.calls) != 0 {
		t.Fatalf("fetch must not check: %v %v", err, stub.calls)
	}
	if strings.Join(argv, " ") != "-i "+key+" -o IdentitiesOnly=yes -o SendEnv=GIT_PROTOCOL git@github.com git-upload-pack 'o/r.git'" {
		t.Fatalf("argv = %q", argv)
	}

	push := []string{"-p", "22", "git@github.com", pushCmd}
	if _, err := sshArgs("work", push, "/usr/bin/ssh"); err != nil {
		t.Fatal(err)
	}
	if len(stub.calls) != 1 || strings.Join(stub.calls[0], " ") != "-i "+key+" -o IdentitiesOnly=yes -p 22 -T git@github.com" {
		t.Fatalf("check call = %q", stub.calls)
	}
	if _, err := sshArgs("work", push, "/usr/bin/ssh"); err != nil || len(stub.calls) != 1 {
		t.Fatalf("second push must use the cache: %v %d", err, len(stub.calls))
	}

	os.WriteFile(key, []byte("key-2"), 0o600)
	stub.login = "martin"
	_, err = sshArgs("work", push, "/usr/bin/ssh")
	if err == nil || !strings.Contains(err.Error(), "SSH logs in to GitHub as martin with ~/.ssh/id_work, but work uses w-gh, so the push stops here") {
		t.Fatalf("changed key must be checked again: %v", err)
	}
	if _, err := sshArgs("work", push, "/usr/bin/ssh"); err == nil || len(stub.calls) != 3 {
		t.Fatalf("a mismatch must never be cached: %v %d", err, len(stub.calls))
	}
}

func TestSSHCacheExpires(t *testing.T) {
	_, stub := sshSandbox(t)
	push := []string{"git@github.com", pushCmd}
	if _, err := sshArgs("work", push, "/usr/bin/ssh"); err != nil {
		t.Fatal(err)
	}
	path, _ := sshAccountPath()
	data, _ := os.ReadFile(path)
	var accounts map[string]sshAccountEntry
	json.Unmarshal(data, &accounts)
	for k, e := range accounts {
		e.CheckedAt = time.Now().Add(-25 * time.Hour)
		accounts[k] = e
	}
	data, _ = json.Marshal(accounts)
	os.WriteFile(path, data, 0o600)
	if _, err := sshArgs("work", push, "/usr/bin/ssh"); err != nil || len(stub.calls) != 2 {
		t.Fatalf("expired cache must check again: %v %d", err, len(stub.calls))
	}
}

func TestSSHPushRefusals(t *testing.T) {
	cases := []struct {
		name, identity, dest, hostname, login string
		checkErr                              error
		args                                  []string
		want                                  string
	}{
		{name: "deploy key", identity: "work", login: "org/repo", want: "~/.ssh/id_work is a deploy key of org/repo, not the account of work"},
		{name: "no answer", identity: "work", checkErr: errors.New("Permission denied (publickey)."), want: "cannot check which GitHub account the SSH connection uses (Permission denied (publickey).)"},
		{name: "agent other account", identity: "agent", login: "w-gh", want: "SSH logs in to GitHub as w-gh with the ssh agent, but agent uses a-gh"},
		{name: "no gh_user", identity: "nouser", want: "nouser has no gh_user"},
		{name: "removed identity", identity: "gone", want: "gone does not exist any more"},
		{name: "bad name", identity: "../x", want: "is not valid"},
		{name: "host alias", identity: "work", dest: "git@github-work", login: "martin", want: "as martin"},
		{name: "port 443 host", identity: "work", hostname: "ssh.github.com", login: "martin", want: "as martin"},
		{name: "other host", identity: "work", hostname: "gitlab.com", login: "martin"},
		{name: "right account", identity: "agent", login: "A-GH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stub := sshSandbox(t)
			stub.login, stub.err = tc.login, tc.checkErr
			if tc.hostname != "" {
				stub.hostname = tc.hostname
			}
			dest := tc.dest
			if dest == "" {
				dest = "git@github.com"
			}
			_, err := sshArgs(tc.identity, []string{dest, pushCmd}, "/usr/bin/ssh")
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want push to run: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "so the push stops here") {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSSHAgentResultIsNeverCached(t *testing.T) {
	_, stub := sshSandbox(t)
	stub.login = "a-gh"
	for i := 0; i < 2; i++ {
		if _, err := sshArgs("agent", []string{"git@github.com", pushCmd}, "/usr/bin/ssh"); err != nil {
			t.Fatal(err)
		}
	}
	if len(stub.calls) != 2 {
		t.Fatalf("agent keys can change, want a check per push, got %d", len(stub.calls))
	}
}
