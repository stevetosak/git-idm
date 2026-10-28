package setup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeGH = `#!/bin/sh
echo "$* | GH_CONFIG_DIR=$GH_CONFIG_DIR | GIT_CONFIG_GLOBAL=$GIT_CONFIG_GLOBAL" >> "$FAKE_LOG"
case "$1 $2" in
  "auth login")
    [ -n "$FAKE_LOGIN_FAIL" ] && exit 1
    printf 'github.com:\n    user: fake-user\n' > "$GH_CONFIG_DIR/hosts.yml"
    ;;
  "ssh-key add")
    case "$*" in *signing*) [ -n "$FAKE_DUPLICATE" ] && { echo "key is already in use" >&2; exit 1; } ;; esac
    ;;
esac
`

const fakeSSH = `#!/bin/sh
echo "Hi fake-user! You've successfully authenticated, but GitHub does not provide shell access." >&2
exit 1
`

func fakeTools(t *testing.T, home string) (Tools, string) {
	t.Helper()
	bin := filepath.Join(home, "fakebin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(fakeSSH), 0o755)
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	log := filepath.Join(home, "gh.log")
	t.Setenv("FAKE_LOG", log)
	return Tools{GH: filepath.Join(bin, "gh"), SSHKeygen: keygen, SSH: filepath.Join(bin, "ssh")}, log
}

func TestExecutorFullPlan(t *testing.T) {
	home := sandbox(t)
	tools, log := fakeTools(t, home)
	t.Setenv("FAKE_DUPLICATE", "1")
	var out bytes.Buffer
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Hostname: "mac", Out: &out}
	p := Plan{
		Name: "client", AuthorName: "Jane", Email: "jane@client.example", Color: "green",
		GHLogin: true, SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_client", Upload: true, Sign: true,
		BindDir: "~/Clients/", CreateDir: true,
	}
	id, report, err := ex.Run(p)
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	key := filepath.Join(home, ".ssh", "git-idm_client")
	info, err := os.Stat(key)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private key: %v %v", info, err)
	}
	pub, _ := os.ReadFile(key + ".pub")
	if !strings.HasPrefix(string(pub), "ssh-ed25519 ") || !strings.Contains(string(pub), "jane@client.example") {
		t.Fatalf("public key = %q", pub)
	}
	if _, err := os.Stat(filepath.Join(home, "Clients")); err != nil {
		t.Fatal("directory not created")
	}
	if id.GHUser != "fake-user" || id.GHConfigDir != "~/.config/git-idm/gh/client" || id.SSHKey != "~/.ssh/git-idm_client" ||
		id.SigningKey != "~/.ssh/git-idm_client.pub" || id.Protocol != "ssh" || id.Email != "jane@client.example" {
		t.Fatalf("identity = %+v", id)
	}
	if len(report.Done) != 5 {
		t.Fatalf("report = %+v", report.Done)
	}

	calls, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 3 {
		t.Fatalf("gh calls:\n%s", calls)
	}
	ghDir := filepath.Join(home, ".config", "git-idm", "gh", "client")
	login := lines[0]
	for _, want := range []string{"auth login", "--git-protocol ssh", "--web", "--skip-ssh-key", "--scopes admin:public_key,admin:ssh_signing_key", "GH_CONFIG_DIR=" + ghDir} {
		if !strings.Contains(login, want) {
			t.Errorf("login call misses %q: %s", want, login)
		}
	}
	if strings.Contains(login, "GIT_CONFIG_GLOBAL= ") || strings.Contains(login, "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig")) {
		t.Errorf("gh login must not see the real global gitconfig: %s", login)
	}
	if !strings.Contains(lines[1], "--type authentication") || !strings.Contains(lines[1], "--title git-idm client (mac)") {
		t.Errorf("upload call: %s", lines[1])
	}
	if !strings.Contains(lines[2], "--type signing") {
		t.Errorf("signing upload call: %s", lines[2])
	}

	login2, err := ex.TestConnection(id)
	if err != nil || login2 != "fake-user" {
		t.Fatalf("connection test: %q %v", login2, err)
	}
}

func TestExecutorStopsAndReportsOnFailure(t *testing.T) {
	home := sandbox(t)
	tools, _ := fakeTools(t, home)
	t.Setenv("FAKE_LOGIN_FAIL", "1")
	var out bytes.Buffer
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Out: &out}
	p := Plan{Name: "c", AuthorName: "J", Email: "j@c.example", GHLogin: true, SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_c", BindDir: "~/C/", CreateDir: true}
	_, report, err := ex.Run(p)
	if err == nil || report.Failed != "gh login" {
		t.Fatalf("err=%v report=%+v", err, report)
	}
	if len(report.Done) != 1 || !strings.Contains(report.Done[0], "git-idm_c") {
		t.Fatalf("done = %v", report.Done)
	}
	if _, err := os.Stat(filepath.Join(home, "C")); !os.IsNotExist(err) {
		t.Fatal("must stop before later steps")
	}
}

func TestExecutorCheckMissingTools(t *testing.T) {
	ex := &Executor{}
	if err := ex.Check(Plan{GHLogin: true}); err == nil || !strings.Contains(err.Error(), "gh is not installed") {
		t.Fatalf("err = %v", err)
	}
	if err := ex.Check(Plan{SSH: SSHGenerate}); err == nil {
		t.Fatal("want error for missing ssh-keygen")
	}
}
