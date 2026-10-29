package guard

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/resolve"
)

const AllowKey = "git-idm.allowAuthor"

type Verdict struct {
	Expected     string
	ExpectedFrom string
	Actual       string
	Committer    string
	AllowedEmail string
}

func (v Verdict) Accepts(email string) bool {
	return v.Expected == "" || strings.EqualFold(v.Expected, email) || (v.AllowedEmail != "" && strings.EqualFold(v.AllowedEmail, email))
}

func (v Verdict) OK() bool {
	return v.Accepts(v.Actual) && (v.Committer == "" || v.Accepts(v.Committer))
}

func Check(c *config.Config, res resolve.Result, dir string) (Verdict, error) {
	var v Verdict
	id, ok := c.Identities[res.Identity]
	if !ok {
		return v, nil
	}
	v.Expected = id.Email
	v.ExpectedFrom = res.Identity
	ident, err := git(dir, "var", "GIT_AUTHOR_IDENT")
	if err != nil {
		return v, err
	}
	v.Actual = EmailFromIdent(ident)
	committer, err := git(dir, "var", "GIT_COMMITTER_IDENT")
	if err != nil {
		return v, err
	}
	v.Committer = EmailFromIdent(committer)
	v.AllowedEmail, _ = git(dir, "config", "--local", "--get", AllowKey)
	return v, nil
}

func Allow(dir, email string) error {
	_, err := git(dir, "config", "--local", AllowKey, email)
	return err
}

func Disallow(dir string) error {
	_, err := git(dir, "config", "--local", "--unset", AllowKey)
	return err
}

func Allowed(dir string) string {
	v, _ := git(dir, "config", "--local", "--get", AllowKey)
	return v
}

func EmailFromIdent(ident string) string {
	start := strings.LastIndex(ident, "<")
	end := strings.LastIndex(ident, ">")
	if start == -1 || end <= start {
		return ""
	}
	return ident[start+1 : end]
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return "", nil
		}
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
