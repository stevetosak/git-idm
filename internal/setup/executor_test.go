package setup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

const fakeGH = `#!/bin/sh
stdin=tty; [ -t 0 ] || stdin=notty
echo "$* | GH_CONFIG_DIR=$GH_CONFIG_DIR | GIT_CONFIG_GLOBAL=$GIT_CONFIG_GLOBAL | stdin=$stdin" >> "$FAKE_LOG"
case "$1 $2" in
  "auth login")
    [ -n "$FAKE_LOGIN_FAIL" ] && { echo "authentication failed" >&2; exit 1; }
    echo "! One-time code (ABCD-1234) copied to clipboard" >&2
    echo "Open this URL to continue in your web browser: https://github.com/login/device" >&2
    printf 'github.com:\n    user: fake-user\n' > "$GH_CONFIG_DIR/hosts.yml"
    ;;
  "api user")
    echo '{"login":"fake-user","name":"Fake User","email":null,"id":7}'
    ;;
  "api user/keys")
    printf '[{"id":3,"key":"ssh-ed25519 OTHER"},{"id":5,"key":"%s"}]' "$(cut -d' ' -f1,2 "$HOME/.ssh/git-idm_r.pub")"
    ;;
  "api --method")
    ;;
  "auth logout")
    rm -f "$GH_CONFIG_DIR/hosts.yml"
    ;;
  "ssh-key add")
    if [ -n "$FAKE_DUPLICATE" ]; then echo "key is already in use" >&2; exit 1; fi
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
	os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\necho \"$1\" >> \"$FAKE_LOG.open\"\n"), 0o755)
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	log := filepath.Join(home, "gh.log")
	t.Setenv("FAKE_LOG", log)
	return Tools{GH: filepath.Join(bin, "gh"), SSHKeygen: keygen, SSH: filepath.Join(bin, "ssh"), Open: filepath.Join(bin, "open")}, log
}

func TestExecutorLogin(t *testing.T) {
	home := sandbox(t)
	tools, log := fakeTools(t, home)
	var out bytes.Buffer
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Out: &out}
	account, dir, err := ex.Login("client")
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if account.Login != "fake-user" || account.ID != 7 {
		t.Fatalf("account = %+v", account)
	}
	if dir != "~/.config/git-idm/gh/client" {
		t.Fatalf("dir = %s", dir)
	}
	calls, _ := os.ReadFile(log)
	login := strings.Split(string(calls), "\n")[0]
	for _, want := range []string{"auth login", "--web", "--clipboard", "--skip-ssh-key", "--scopes admin:public_key,admin:ssh_signing_key",
		"GH_CONFIG_DIR=" + filepath.Join(home, ".config", "git-idm", "gh", "client"), "stdin=notty"} {
		if !strings.Contains(login, want) {
			t.Errorf("login call misses %q: %s", want, login)
		}
	}
	if strings.Contains(login, "GIT_CONFIG_GLOBAL= ") || strings.Contains(login, "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig")) {
		t.Errorf("gh login must not see the real global gitconfig: %s", login)
	}
	opened, _ := os.ReadFile(log + ".open")
	if strings.TrimSpace(string(opened)) != "https://github.com/login/device" {
		t.Fatalf("browser opened %q", opened)
	}
	for _, want := range []string{"Your one-time code: ABCD-1234 (copied to the clipboard)", "Your browser opened", "✓ logged in as fake-user"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}

	ex.Logout(dir, account.Login)
	if _, err := os.Stat(config.ExpandHome(dir)); !os.IsNotExist(err) {
		t.Fatal("logout must remove the login folder")
	}
	calls, _ = os.ReadFile(log)
	if !strings.Contains(string(calls), "auth logout --hostname github.com --user fake-user") {
		t.Fatalf("no logout call:\n%s", calls)
	}
}

func TestExecutorLoginFailureCleansUp(t *testing.T) {
	home := sandbox(t)
	tools, _ := fakeTools(t, home)
	t.Setenv("FAKE_LOGIN_FAIL", "1")
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Out: &bytes.Buffer{}}
	_, _, err := ex.Login("c")
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(ex.LoginDir("c")); !os.IsNotExist(err) {
		t.Fatal("failed login must not leave a folder")
	}
}

func TestExecutorFullPlan(t *testing.T) {
	home := sandbox(t)
	tools, log := fakeTools(t, home)
	var out bytes.Buffer
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Hostname: "mac", Out: &out}
	p := Plan{
		Name: "client", AuthorName: "Jane", Email: "jane@client.example", Color: "green",
		GHUser: "fake-user", NewLogin: true, GHConfigDir: "~/.config/git-idm/gh/client", SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_client",
		Upload: true, BindDir: "~/Clients/", CreateDir: true,
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
		id.SigningKey != "" || id.Protocol != "ssh" || id.Email != "jane@client.example" {
		t.Fatalf("identity = %+v", id)
	}
	if len(report.Done) != 3 {
		t.Fatalf("report = %+v", report.Done)
	}

	calls, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 {
		t.Fatalf("gh calls:\n%s", calls)
	}
	if !strings.Contains(lines[0], "config set --host github.com git_protocol ssh") {
		t.Errorf("protocol call: %s", lines[0])
	}
	if !strings.Contains(lines[1], "ssh-key add") || !strings.Contains(lines[1], "--title git-idm client (mac)") || strings.Contains(lines[1], "signing") {
		t.Errorf("upload call: %s", lines[1])
	}

	login, err := ex.TestConnection(id)
	if err != nil || login != "fake-user" {
		t.Fatalf("connection test: %q %v", login, err)
	}
}

func TestExecutorStopsAndReportsOnFailure(t *testing.T) {
	home := sandbox(t)
	tools, _ := fakeTools(t, home)
	var out bytes.Buffer
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Out: &out}
	p := Plan{Name: "c", AuthorName: "J", Email: "j@c.example", GHUser: "u", NewLogin: true, GHConfigDir: "~/gh", SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_c", BindDir: "~/C/", CreateDir: true}
	ex.Tools.GH = filepath.Join(home, "missing-gh")
	_, report, err := ex.Run(p)
	if err == nil || report.Failed != "set gh git protocol" {
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
	if err := ex.Check(Plan{Upload: true}); err == nil || !strings.Contains(err.Error(), "gh is not installed") {
		t.Fatalf("err = %v", err)
	}
	if err := ex.Check(Plan{SSH: SSHGenerate}); err == nil {
		t.Fatal("want error for missing ssh-keygen")
	}
}

func TestExecutorUploadsSigningKey(t *testing.T) {
	home := sandbox(t)
	tools, log := fakeTools(t, home)
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Hostname: "mac", Out: &bytes.Buffer{}}
	p := Plan{Name: "s", AuthorName: "J", Email: "j@s.example", GHUser: "u", GHConfigDir: "~/gh", SSH: SSHExisting, SSHKey: "~/.ssh/id_s",
		SigningKey: "~/.ssh/id_s.pub", UploadSigning: true}
	id, report, err := ex.Run(p)
	if err != nil {
		t.Fatal(err)
	}
	if id.SigningKey != "~/.ssh/id_s.pub" || len(report.Done) != 1 || !strings.Contains(report.Done[0], "as a signing key") {
		t.Fatalf("identity=%+v report=%+v", id, report)
	}
	calls, _ := os.ReadFile(log)
	if want := "ssh-key add " + filepath.Join(home, ".ssh", "id_s.pub") + " --title git-idm s signing (mac) --type signing"; !strings.Contains(string(calls), want) {
		t.Fatalf("missing %q in:\n%s", want, calls)
	}
	if err := (&Executor{}).Check(Plan{UploadSigning: true}); err == nil {
		t.Fatal("a signing upload needs gh")
	}
}

func TestExecutorKeyAlreadyOnGitHubIsFine(t *testing.T) {
	home := sandbox(t)
	tools, _ := fakeTools(t, home)
	t.Setenv("FAKE_DUPLICATE", "1")
	ex := &Executor{Tools: tools, ConfigDir: filepath.Join(home, ".config", "git-idm"), Out: &bytes.Buffer{}}
	p := Plan{Name: "d", AuthorName: "J", Email: "j@d.example", GHUser: "u", NewLogin: true, GHConfigDir: "~/gh", SSH: SSHGenerate, SSHKey: "~/.ssh/git-idm_d", Upload: true}
	if _, _, err := ex.Run(p); err != nil {
		t.Fatal(err)
	}
}

func TestFindToolsSkipsGHShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "shim", "gh")
	real := filepath.Join(dir, "real", "gh")
	for path, content := range map[string]string{shim: "#!/bin/sh\n# generated by git-idm, do not edit\n", real: "#!/bin/sh\n"} {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Dir(shim)+string(os.PathListSeparator)+filepath.Dir(real))
	if got := FindTools().GH; got != real {
		t.Fatalf("GH = %q, want %q", got, real)
	}
}
