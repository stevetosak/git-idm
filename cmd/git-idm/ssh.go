package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
)

const sshAccountTTL = 24 * time.Hour

var githubSSHHosts = map[string]bool{"github.com": true, "ssh.github.com": true}

var sshGreeting = regexp.MustCompile(`Hi ([^!\s]+)! You've successfully authenticated`)

var (
	execSSH        = syscall.Exec
	resolveSSH     = sshConfig
	sshAccountOf   = sshAccount
	sshAccountPath = func() (string, error) {
		dir, err := config.Dir()
		return filepath.Join(dir, "ssh-accounts.json"), err
	}
)

func newSSHCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "ssh <identity> [ssh arguments]",
		Short:              "Run ssh for git and check the GitHub account before a push",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("ssh needs an identity")
			}
			ssh, err := exec.LookPath("ssh")
			if err != nil {
				return errors.New("ssh is not installed or not in PATH")
			}
			argv, err := sshArgs(args[0], args[1:], ssh)
			if err != nil {
				return err
			}
			return execSSH(ssh, append([]string{"ssh"}, argv...), os.Environ())
		},
	}
}

func sshArgs(name string, args []string, ssh string) ([]string, error) {
	if err := config.ValidateIdentityName(name); err != nil {
		return nil, fmt.Errorf("identity name %q is not valid, so the push stops here", name)
	}
	c, err := config.Load()
	if err != nil {
		if isPush(args) {
			return nil, fmt.Errorf("git-idm cannot read its config, so the push stops here\n  fix: git-idm doctor")
		}
		return args, nil
	}
	id, ok := c.Identities[name]
	if !ok {
		if isPush(args) {
			return nil, fmt.Errorf("%s does not exist any more, so the push stops here\n  fix: git-idm apply", name)
		}
		return args, nil
	}
	var base []string
	if id.SSHKey != "" {
		base = []string{"-i", config.ExpandHome(id.SSHKey), "-o", "IdentitiesOnly=yes"}
	}
	argv := append(append([]string{}, base...), args...)
	if !isPush(args) || len(args) < 2 {
		return argv, nil
	}
	resolved, err := resolveSSH(ssh, append(append([]string{}, base...), args[:len(args)-1]...))
	if err != nil {
		return nil, fmt.Errorf("ssh cannot read its config (%v), so the push stops here", err)
	}
	if !githubSSHHosts[strings.ToLower(resolved["hostname"])] {
		return argv, nil
	}
	if err := checkSSHAccount(ssh, name, id, base, args[:len(args)-2], args[len(args)-2], resolved); err != nil {
		return nil, err
	}
	return argv, nil
}

func isPush(args []string) bool {
	return len(args) > 0 && strings.Contains(args[len(args)-1], "git-receive-pack")
}

func checkSSHAccount(ssh, name string, id config.Identity, base, opts []string, dest string, resolved map[string]string) error {
	stop := func(reason, fix string) error {
		return fmt.Errorf("%s, so the push stops here\n  fix: %s", reason, fix)
	}
	if id.GHUser == "" {
		return stop(name+" has no gh_user, git-idm cannot check which GitHub account the SSH key uses", "git-idm login "+name)
	}
	cacheKey := ""
	if id.SSHKey != "" {
		if key, err := os.ReadFile(config.ExpandHome(id.SSHKey)); err == nil {
			cacheKey = sshCacheKey(name, key, resolved)
		}
	}
	login := ""
	if cacheKey != "" {
		login = cachedSSHAccount(cacheKey)
	}
	if login == "" {
		var err error
		login, err = sshAccountOf(ssh, append(append(append([]string{}, base...), opts...), "-T", dest))
		if err != nil {
			return stop("git-idm cannot check which GitHub account the SSH connection uses ("+err.Error()+")", "git-idm doctor")
		}
		if cacheKey != "" && strings.EqualFold(login, id.GHUser) {
			storeSSHAccount(cacheKey, login)
		}
	}
	key := "the ssh agent"
	if id.SSHKey != "" {
		key = id.SSHKey
	}
	switch {
	case strings.Contains(login, "/"):
		return stop(fmt.Sprintf("%s is a deploy key of %s, not the account of %s", key, login, name), "use a key of "+id.GHUser+" for "+name)
	case !strings.EqualFold(login, id.GHUser):
		return stop(fmt.Sprintf("SSH logs in to GitHub as %s with %s, but %s uses %s", login, key, name, id.GHUser), "add the key of "+name+" to "+id.GHUser+", or git-idm edit "+name)
	}
	return nil
}

func sshConfig(ssh string, args []string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ssh, append([]string{"-G"}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), " ")
		if key == "identityfile" {
			values[key] += value + "\n"
		} else if _, seen := values[key]; !seen {
			values[key] = value
		}
	}
	return values, nil
}

func sshAccount(ssh string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ssh, args...)
	cmd.Stdin = nil
	out, _ := cmd.CombinedOutput()
	if m := sshGreeting.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	if ctx.Err() != nil {
		return "", errors.New("timeout")
	}
	return "", errors.New(firstLine(string(out)))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if line == "" {
		return "no answer from GitHub"
	}
	return line
}

func sshCacheKey(name string, key []byte, resolved map[string]string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%x\x00", name, sha256.Sum256(key))
	for _, k := range []string{"hostname", "port", "user", "identityfile", "identitiesonly", "identityagent", "certificatefile"} {
		fmt.Fprintf(h, "%s=%s\x00", k, resolved[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

type sshAccountEntry struct {
	Login     string    `json:"login"`
	CheckedAt time.Time `json:"checked_at"`
}

func loadSSHAccounts() map[string]sshAccountEntry {
	accounts := map[string]sshAccountEntry{}
	path, err := sshAccountPath()
	if err != nil {
		return accounts
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return accounts
	}
	json.Unmarshal(data, &accounts)
	return accounts
}

func cachedSSHAccount(key string) string {
	entry, ok := loadSSHAccounts()[key]
	if !ok || time.Since(entry.CheckedAt) > sshAccountTTL || time.Since(entry.CheckedAt) < 0 {
		return ""
	}
	return entry.Login
}

func storeSSHAccount(key, login string) {
	path, err := sshAccountPath()
	if err != nil {
		return
	}
	accounts := loadSSHAccounts()
	for k, entry := range accounts {
		if time.Since(entry.CheckedAt) > sshAccountTTL {
			delete(accounts, k)
		}
	}
	accounts[key] = sshAccountEntry{Login: login, CheckedAt: time.Now()}
	data, err := json.Marshal(accounts)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		os.Rename(tmp, path)
	}
}
