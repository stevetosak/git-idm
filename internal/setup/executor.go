package setup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/importer"
)

type Tools struct {
	GH        string
	SSHKeygen string
	SSH       string
}

func FindTools() Tools {
	look := func(name string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		return ""
	}
	return Tools{GH: look("gh"), SSHKeygen: look("ssh-keygen"), SSH: look("ssh")}
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
	if p.GHLogin && e.Tools.GH == "" {
		return fmt.Errorf("gh is not installed (https://cli.github.com), or choose Skip for GitHub")
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
		if p.Sign {
			id.SigningKey = p.SSHKey + ".pub"
		}
	}
	if p.GHLogin {
		dir := filepath.Join(e.ConfigDir, "gh", p.Name)
		if err := e.ghLogin(p, dir); err != nil {
			return fail("gh login", err)
		}
		id.GHConfigDir = config.ContractHome(dir)
		id.GHUser = importer.GHUserFromConfigDir(dir)
		if id.GHUser == "" {
			return fail("gh login", fmt.Errorf("no account found in %s after login", id.GHConfigDir))
		}
		e.step(r, "gh logged in as %s", id.GHUser)
	}
	if p.Upload {
		if err := e.uploadKey(p, id.GHConfigDir, "authentication"); err != nil {
			return fail("add ssh key to GitHub", err)
		}
		e.step(r, "added %s.pub to GitHub as an authentication key", p.SSHKey)
		if p.Sign {
			if err := e.uploadKey(p, id.GHConfigDir, "signing"); err != nil {
				return fail("add signing key to GitHub", err)
			}
			e.step(r, "added %s.pub to GitHub as a signing key", p.SSHKey)
		}
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

func (e *Executor) ghLogin(p Plan, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "git-idm-login-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	protocol := "https"
	if p.SSH != SSHNone {
		protocol = "ssh"
	}
	args := []string{"auth", "login", "--hostname", "github.com", "--git-protocol", protocol, "--web", "--skip-ssh-key"}
	var scopes []string
	if p.Upload {
		scopes = append(scopes, "admin:public_key")
		if p.Sign {
			scopes = append(scopes, "admin:ssh_signing_key")
		}
	}
	if len(scopes) > 0 {
		args = append(args, "--scopes", strings.Join(scopes, ","))
	}
	cmd := exec.Command(e.Tools.GH, args...)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+dir, "GIT_CONFIG_GLOBAL="+filepath.Join(scratch, "gitconfig"))
	fmt.Fprintf(e.Out, "\n  gh opens your browser. Log in with the GitHub account for %s.\n\n", p.Name)
	return e.interactive(cmd)
}

func (e *Executor) uploadKey(p Plan, ghDir, keyType string) error {
	title := "git-idm " + p.Name
	if e.Hostname != "" {
		title += " (" + e.Hostname + ")"
	}
	cmd := exec.Command(e.Tools.GH, "ssh-key", "add", config.ExpandHome(p.SSHKey)+".pub", "--title", title, "--type", keyType)
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
