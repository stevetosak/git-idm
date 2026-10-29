package guard

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
)

const zeroSHA = "0000000000000000000000000000000000000000"

var (
	shaPattern    = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	scpLikeRemote = regexp.MustCompile(`^[^/:]+:`)
)

type RefUpdate struct {
	LocalRef, LocalSHA, RemoteRef, RemoteSHA string
}

type Problem struct {
	Text string
	Fix  string
}

type Push struct {
	Dir      string
	URL      string
	Name     string
	Identity config.Identity
	Allowed  string
	Bin      string
	Strict   bool
	Getenv   func(string) string
}

func ParsePushInput(r io.Reader) ([]RefUpdate, error) {
	var updates []RefUpdate
	scanner := bufio.NewScanner(io.LimitReader(r, 1<<20))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 || !shaPattern.MatchString(f[1]) || !shaPattern.MatchString(f[3]) {
			return nil, fmt.Errorf("unexpected pre-push input %q", line)
		}
		updates = append(updates, RefUpdate{LocalRef: f[0], LocalSHA: f[1], RemoteRef: f[2], RemoteSHA: f[3]})
	}
	return updates, scanner.Err()
}

func CheckPush(p Push, updates []RefUpdate) ([]Problem, error) {
	var problems []Problem
	if p.Strict {
		transport, err := transportProblems(p)
		if err != nil {
			return nil, err
		}
		problems = append(problems, transport...)
	}
	commits, err := commitProblems(p, updates)
	if err != nil {
		return nil, err
	}
	return append(problems, commits...), nil
}

func isSSHRemote(remote string) bool {
	if strings.HasPrefix(remote, "ssh://") || strings.HasPrefix(remote, "git+ssh://") || strings.HasPrefix(remote, "ssh+git://") {
		return true
	}
	return !strings.Contains(remote, "://") && scpLikeRemote.MatchString(remote)
}

func transportProblems(p Push) ([]Problem, error) {
	var problems []Problem
	add := func(text, fix string) { problems = append(problems, Problem{Text: text, Fix: fix}) }
	if u, err := url.Parse(p.URL); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			add("the remote URL has a password or token in it, so git would push with that and not with "+p.Name, "git remote set-url <remote> "+redact(u))
		} else if !strings.EqualFold(u.User.Username(), p.Identity.GHUser) {
			add(fmt.Sprintf("the remote URL names the user %s, but %s uses %s", u.User.Username(), p.Name, p.Identity.GHUser), "git remote set-url <remote> "+redact(u))
		}
	}
	if isSSHRemote(p.URL) {
		for _, key := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
			if p.Getenv(key) != "" {
				add(key+" is set, so git skips the git-idm SSH account check", "unset "+key)
			}
		}
	}
	entries, err := configEntries(p.Dir, `^(core\.sshcommand|credential\..*helper|http\..*extraheader)$`)
	if err != nil {
		return nil, err
	}
	helpers := []string(nil)
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.key, "http.") && strings.Contains(strings.ToLower(e.value), "authorization"):
			add(e.key+" sends its own Authorization header, so git would not use the account of "+p.Name, "remove "+e.key+" ("+e.scope+" config)")
		case e.scope != "command":
		case e.key == "core.sshcommand":
			if e.value != gitgen.SSHWrapper(p.Bin, p.Name, p.Identity) {
				add("core.sshCommand is set for this git command only, so git skips the git-idm SSH account check", "run git without -c core.sshCommand=...")
			}
		case strings.HasPrefix(e.key, "credential.") && appliesToGitHub(e.key):
			if e.value == "" {
				helpers = nil
				continue
			}
			helpers = append(helpers, e.value)
		}
	}
	commandHelpers := false
	for _, e := range entries {
		commandHelpers = commandHelpers || (e.scope == "command" && strings.HasPrefix(e.key, "credential.") && appliesToGitHub(e.key))
	}
	if commandHelpers {
		if len(helpers) == 0 {
			add("a credential helper set for this git command only removes the git-idm helper", "run git without -c credential.helper=...")
		}
		for _, h := range helpers {
			if h != gitgen.CredentialHelper(p.Bin, p.Name) && !p.verifiedGHHelper(h) {
				add("a credential helper set for this git command only can give another GitHub account", "run git without -c credential.helper=...")
				break
			}
		}
	}
	return problems, nil
}

func (p Push) verifiedGHHelper(helper string) bool {
	if !strings.Contains(helper, "auth git-credential") || p.Identity.GHConfigDir == "" || p.Identity.GHUser == "" {
		return false
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		if p.Getenv(key) != "" {
			return false
		}
	}
	if p.Getenv("GH_CONFIG_DIR") != config.ExpandHome(p.Identity.GHConfigDir) {
		return false
	}
	return strings.EqualFold(importer.GHUserFromConfigDir(p.Identity.GHConfigDir), p.Identity.GHUser)
}

func appliesToGitHub(key string) bool {
	if key == "credential.helper" {
		return true
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(key, "credential."), ".helper")
	return strings.Contains(strings.ToLower(middle), "github.com")
}

func redact(u *url.URL) string {
	clean := *u
	clean.User = nil
	return clean.String()
}

type configEntry struct {
	scope, key, value string
}

func configEntries(dir, pattern string) ([]configEntry, error) {
	cmd := exec.Command("git", "config", "--show-scope", "--null", "--get-regexp", pattern)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("git config: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var entries []configEntry
	fields := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git config: unexpected output")
	}
	for i := 0; i < len(fields); i += 2 {
		key, value, _ := strings.Cut(string(fields[i+1]), "\n")
		entries = append(entries, configEntry{scope: string(fields[i]), key: strings.ToLower(key), value: value})
	}
	return entries, nil
}

func commitProblems(p Push, updates []RefUpdate) ([]Problem, error) {
	v := Verdict{Expected: p.Identity.Email, AllowedEmail: p.Allowed}
	var bad []string
	count := 0
	for _, u := range updates {
		if u.LocalSHA == zeroSHA || strings.Trim(u.LocalSHA, "0") == "" {
			continue
		}
		args := []string{"log", "--format=%h%x1f%ae%x1f%ce", u.LocalSHA, "--not", "--remotes"}
		if strings.Trim(u.RemoteSHA, "0") != "" {
			if _, err := git(p.Dir, "cat-file", "-e", u.RemoteSHA+"^{commit}"); err == nil {
				args = append(args, u.RemoteSHA)
			}
		}
		out, err := git(p.Dir, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			f := strings.Split(line, "\x1f")
			if len(f) != 3 {
				continue
			}
			var wrong []string
			authorOK, committerOK := v.Accepts(f[1]), v.Accepts(f[2])
			switch {
			case !authorOK && !committerOK && strings.EqualFold(f[1], f[2]):
				wrong = append(wrong, "author and committer "+f[1])
			default:
				if !authorOK {
					wrong = append(wrong, "author "+f[1])
				}
				if !committerOK {
					wrong = append(wrong, "committer "+f[2])
				}
			}
			if len(wrong) > 0 {
				count++
				if len(bad) < 5 {
					bad = append(bad, f[0]+" "+strings.Join(wrong, ", "))
				}
			}
		}
	}
	if count == 0 {
		return nil, nil
	}
	text := fmt.Sprintf("%d commit(s) to push do not use %s <%s>:\n    %s", count, p.Name, p.Identity.Email, strings.Join(bad, "\n    "))
	if count > len(bad) {
		text += fmt.Sprintf("\n    and %d more", count-len(bad))
	}
	return []Problem{{Text: text, Fix: "git commit --amend --reset-author --no-edit (last commit), git rebase -i to fix older ones, or git-idm allow (if on purpose)"}}, nil
}
