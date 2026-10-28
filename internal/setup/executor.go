package setup

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/ghauth"
	"github.com/stevetosak/git-idm/internal/importer"
)

type Tools struct {
	GH        string
	SSHKeygen string
	SSH       string
	Open      string
}

func FindTools() Tools {
	look := func(name string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		return ""
	}
	open := look("open")
	if runtime.GOOS != "darwin" {
		open = look("xdg-open")
	}
	return Tools{GH: look("gh"), SSHKeygen: look("ssh-keygen"), SSH: look("ssh"), Open: open}
}

type Executor struct {
	Tools     Tools
	ConfigDir string
	Hostname  string
	In        io.Reader
	Out       io.Writer
}

type Report struct {
	Done   []string
	Failed string
}

func (e *Executor) step(r *Report, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	r.Done = append(r.Done, line)
	fmt.Fprintf(e.Out, "  ✓ %s\n", line)
}

func (e *Executor) Check(p Plan) error {
	if p.SSH == SSHGenerate && e.Tools.SSHKeygen == "" {
		return fmt.Errorf("ssh-keygen is not installed")
	}
	if p.Upload && e.Tools.GH == "" {
		return fmt.Errorf("gh is not installed (https://cli.github.com)")
	}
	return nil
}

func (e *Executor) Run(p Plan) (config.Identity, *Report, error) {
	r := &Report{}
	id := config.Identity{Name: p.AuthorName, Email: p.Email, Color: p.Color}
	fail := func(what string, err error) (config.Identity, *Report, error) {
		r.Failed = what
		return id, r, fmt.Errorf("%s: %w", what, err)
	}

	if p.SSH == SSHGenerate {
		if err := e.generateKey(p); err != nil {
			return fail("generate ssh key", err)
		}
		e.step(r, "generated %s", p.SSHKey)
	}
	if p.SSH != SSHNone {
		id.SSHKey = p.SSHKey
		id.Protocol = config.ProtocolSSH
	}
	if p.GHUser != "" {
		id.GHConfigDir = config.ContractHome(p.GHConfigDir)
		id.GHUser = p.GHUser
		if p.SSH != SSHNone && p.NewLogin {
			if err := e.UseSSHProtocol(p.GHConfigDir); err != nil {
				return fail("set gh git protocol", err)
			}
		}
	}
	if p.Upload {
		if err := e.uploadKey(p, id.GHConfigDir); err != nil {
			return fail("add ssh key to GitHub", err)
		}
		e.step(r, "added %s.pub to GitHub", p.SSHKey)
	}
	if p.CreateDir {
		if err := os.MkdirAll(config.ExpandHome(p.BindDir), 0o755); err != nil {
			return fail("create directory", err)
		}
		e.step(r, "created %s", p.BindDir)
	}
	return id, r, nil
}

func (e *Executor) generateKey(p Plan) error {
	path := config.ExpandHome(p.SSHKey)
	if fileExists(path) {
		return fmt.Errorf("%s already exists", p.SSHKey)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	args := []string{"-t", "ed25519", "-C", p.Email, "-f", path}
	if !p.Passphrase {
		args = append(args, "-N", "", "-q")
	}
	return e.interactive(exec.Command(e.Tools.SSHKeygen, args...))
}

var (
	deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)
	deviceURL  = regexp.MustCompile(`https://\S+`)
)

func (e *Executor) LoginDir(name string) string {
	return filepath.Join(e.ConfigDir, "gh", name)
}

func (e *Executor) Login(name string) (Account, string, error) {
	if e.Tools.GH == "" {
		return Account{}, "", fmt.Errorf("gh is not installed (https://cli.github.com)")
	}
	dir := e.LoginDir(name)
	e.Logout(dir, importer.GHUserFromConfigDir(dir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Account{}, "", err
	}
	account, err := e.loginInto(dir, "https")
	if err != nil {
		e.Logout(dir, importer.GHUserFromConfigDir(dir))
		return Account{}, "", err
	}
	return account, config.ContractHome(dir), nil
}

func (e *Executor) Relogin(dir string) (Account, error) {
	if e.Tools.GH == "" {
		return Account{}, fmt.Errorf("gh is not installed (https://cli.github.com)")
	}
	dir = config.ExpandHome(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Account{}, err
	}
	return e.loginInto(dir, e.gitProtocol(dir))
}

func (e *Executor) UseSSHProtocol(dir string) error {
	return e.gh(dir, "config", "set", "--host", "github.com", "git_protocol", "ssh")
}

func (e *Executor) gitProtocol(dir string) string {
	cmd := exec.Command(e.Tools.GH, "config", "get", "--host", "github.com", "git_protocol")
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+dir)
	out, _ := cmd.Output()
	if protocol := strings.TrimSpace(string(out)); protocol == "ssh" {
		return protocol
	}
	return "https"
}

func (e *Executor) loginInto(dir, protocol string) (Account, error) {
	scratch, err := os.MkdirTemp("", "git-idm-login-")
	if err != nil {
		return Account{}, err
	}
	defer os.RemoveAll(scratch)
	cmd := exec.Command(e.Tools.GH, "auth", "login", "--hostname", "github.com", "--web", "--clipboard",
		"--skip-ssh-key", "--git-protocol", protocol, "--scopes", "admin:public_key")
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+dir, "GIT_CONFIG_GLOBAL="+filepath.Join(scratch, "gitconfig"))
	cmd.Stdin = strings.NewReader("")
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return Account{}, err
	}
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		return Account{}, err
	}
	var transcript strings.Builder
	code, opened := "", false
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		transcript.WriteString(line + "\n")
		if code == "" {
			if m := deviceCode.FindString(line); m != "" {
				code = m
				copied := ""
				if strings.Contains(line, "clipboard") {
					copied = " (copied to the clipboard)"
				}
				fmt.Fprintf(e.Out, "\n  Your one-time code: %s%s\n", code, copied)
			}
		}
		if url := deviceURL.FindString(line); url != "" && !opened {
			opened = true
			e.openBrowser(url)
		}
	}
	if err := cmd.Wait(); err != nil {
		return Account{}, fmt.Errorf("%v: %s", err, strings.TrimSpace(transcript.String()))
	}
	account, err := e.Account(dir)
	if err != nil {
		return Account{}, err
	}
	fmt.Fprintf(e.Out, "  ✓ logged in as %s\n\n", account.Login)
	return account, nil
}

func (e *Executor) openBrowser(url string) {
	if e.Tools.Open == "" || exec.Command(e.Tools.Open, url).Run() != nil {
		fmt.Fprintf(e.Out, "  Open %s in your browser and enter the code.\n", url)
		return
	}
	fmt.Fprintf(e.Out, "  Your browser opened %s. Paste the code there and log in with the account for this identity.\n", url)
}

func (e *Executor) Account(dir string) (Account, error) {
	if e.Tools.GH == "" {
		return Account{}, fmt.Errorf("gh is not installed (https://cli.github.com)")
	}
	return ghauth.Fetch(e.Tools.GH, dir)
}

func (e *Executor) Logout(dir, user string) {
	e.LogoutUser(dir, user)
	os.RemoveAll(config.ExpandHome(dir))
}

func (e *Executor) LogoutUser(dir, user string) {
	if user == "" || e.Tools.GH == "" {
		return
	}
	cmd := exec.Command(e.Tools.GH, "auth", "logout", "--hostname", "github.com", "--user", user)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+config.ExpandHome(dir))
	_ = cmd.Run()
}

func (e *Executor) gh(dir string, args ...string) error {
	cmd := exec.Command(e.Tools.GH, args...)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+config.ExpandHome(dir))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (e *Executor) uploadKey(p Plan, ghDir string) error {
	title := "git-idm " + p.Name
	if e.Hostname != "" {
		title += " (" + e.Hostname + ")"
	}
	cmd := exec.Command(e.Tools.GH, "ssh-key", "add", config.ExpandHome(p.SSHKey)+".pub", "--title", title)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+config.ExpandHome(ghDir))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(msg), "already") {
			return nil
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}

var sshGreeting = regexp.MustCompile(`Hi ([A-Za-z0-9-]+)!`)

func (e *Executor) TestConnection(id config.Identity) (string, error) {
	if e.Tools.SSH == "" {
		return "", fmt.Errorf("ssh is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Tools.SSH, "-i", config.ExpandHome(id.SSHKey), "-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=10", "-T", "git@github.com")
	cmd.Stdin = e.In
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	m := sshGreeting.FindStringSubmatch(out.String())
	if m == nil {
		return "", fmt.Errorf("GitHub did not accept the key: %s", strings.TrimSpace(out.String()))
	}
	return m[1], nil
}

func (e *Executor) interactive(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.In, e.Out, e.Out
	return cmd.Run()
}
