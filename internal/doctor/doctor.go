package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/ghauth"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/guard"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/resolve"
)

type Level int

const (
	OK Level = iota
	Info
	Warn
	Fail
)

type Check struct {
	Level Level
	Text  string
	Fix   string
}

type Env struct {
	Getenv    func(string) string
	HookHint  string
	ShellHook bool
	GHAccount func(dir string) (ghauth.Account, error)
	LookPath  func(string) (string, error)
}

type tokenResult struct {
	account ghauth.Account
	err     error
}

func Run(c *config.Config, a *gitgen.Applier, res resolve.Result, env Env) []Check {
	var checks []Check
	add := func(level Level, fix, format string, args ...any) {
		checks = append(checks, Check{Level: level, Text: fmt.Sprintf(format, args...), Fix: fix})
	}

	global, _ := os.ReadFile(a.GlobalConfig)
	switch {
	case !a.Installed():
		add(Fail, "git-idm apply", "git-idm block is missing from %s", config.ContractHome(a.GlobalConfig))
	case !strings.Contains(string(global), a.Block(c)):
		add(Warn, "git-idm apply", "git-idm block in %s is out of date", config.ContractHome(a.GlobalConfig))
	default:
		add(OK, "", "git-idm block in %s is up to date", config.ContractHome(a.GlobalConfig))
	}
	if gitgen.ContentAfterBlock(string(global)) {
		add(Warn, "git-idm apply", "settings after the git-idm block in %s can override identities", config.ContractHome(a.GlobalConfig))
	}

	tokens := checkTokens(c, env.GHAccount)
	for _, name := range c.Names() {
		checks = append(checks, identityChecks(a, name, c.Identities[name], tokens, gitgen.UsesGH(c))...)
	}

	for _, r := range c.Rules {
		if !r.Temporary {
			continue
		}
		was := ""
		if r.Was != "" {
			was = " (was " + r.Was + ")"
		}
		add(Info, "git-idm unbind "+r.Path, "%s uses %s temporarily%s", r.Path, r.Identity, was)
	}

	checks = append(checks, guardChecks(c, a, string(global))...)

	checks = append(checks, shimChecks(c, a, env.LookPath)...)

	if env.ShellHook {
		add(OK, "", "shell hook is active")
	} else {
		add(Warn, env.HookHint, "shell hook is not active in this shell (gh and the prompt do not follow the directory)")
	}
	if want := config.ExpandHome(c.Identities[res.Identity].GHConfigDir); want != "" {
		if got := env.Getenv("GH_CONFIG_DIR"); got != want {
			add(Warn, "open a new shell", "GH_CONFIG_DIR in this shell is %q, %s expects %s", got, res.Identity, config.ContractHome(want))
		}
	}
	if res.Source == resolve.SourceOverride {
		add(Info, "git-idm use --clear", "override is active in this shell: %s", res.Identity)
	}
	local := localChecks(c)
	checks = append(checks, local...)
	if res.RepoRoot != "" {
		checks = append(checks, repoChecks(c, res, local)...)
	}
	return checks
}

func localChecks(c *config.Config) []Check {
	scan := importer.ScanRules(c.Rules)
	var checks []Check
	if scan.Truncated {
		checks = append(checks, Check{Level: Info, Text: "repo scan stopped early, some repos under rules were not checked"})
	}
	for _, l := range importer.LocalLegacy(c, scan.Repos) {
		rule := c.Identities[ruleIdentity(c, l.RulePath)]
		text := fmt.Sprintf("%s: .git/config sets %s over the %s rule", l.Path, l.Summary, ruleIdentity(c, l.RulePath))
		fix := "git-idm migrate"
		if l.Author != "" {
			text = fmt.Sprintf("%s: .git/config sets %s, commits use %s, not %s", l.Path, l.Summary, l.Author, rule.Email)
			fix = "git-idm migrate, or git-idm allow in the repo (if on purpose)"
		}
		checks = append(checks, Check{Level: Warn, Text: text, Fix: fix})
	}
	return checks
}

func ruleIdentity(c *config.Config, path string) string {
	for _, r := range c.Rules {
		if r.Path == path {
			return r.Identity
		}
	}
	return ""
}

func identityChecks(a *gitgen.Applier, name string, id config.Identity, tokens map[string]tokenResult, strict bool) []Check {
	var checks []Check
	add := func(level Level, fix, format string, args ...any) {
		checks = append(checks, Check{Level: level, Text: name + ": " + fmt.Sprintf(format, args...), Fix: fix})
	}
	if data, err := os.ReadFile(a.IdentityFile(name)); err != nil || string(data) != gitgen.RenderIdentity(name, id, a.Bin, strict) {
		add(Warn, "git-idm apply", "generated git config is missing or out of date")
	}
	if id.SSHKey != "" {
		key := config.ExpandHome(id.SSHKey)
		info, err := os.Stat(key)
		switch {
		case err != nil:
			add(Fail, "fix ssh_key in the config", "ssh key %s not found", id.SSHKey)
		case info.Mode().Perm()&0o077 != 0:
			add(Fail, "chmod 600 "+id.SSHKey, "ssh key %s can be read by other users (ssh refuses it)", id.SSHKey)
		default:
			add(OK, "", "ssh key %s", id.SSHKey)
		}
	}
	if id.GHConfigDir != "" {
		user := importer.GHUserFromConfigDir(id.GHConfigDir)
		login := "git-idm login " + name
		token, checked := tokens[config.ExpandHome(id.GHConfigDir)]
		switch {
		case user == "":
			add(Fail, login, "gh is not logged in (%s)", id.GHConfigDir)
		case id.GHUser != "" && !strings.EqualFold(user, id.GHUser):
			add(Fail, login, "gh is logged in as %s, config expects %s", user, id.GHUser)
		case !checked:
			add(OK, "", "gh logged in as %s", user)
		case errors.Is(token.err, ghauth.ErrUnauthorized):
			add(Fail, login, "gh token for %s is invalid or revoked (%s)", user, id.GHConfigDir)
		case token.err != nil:
			add(Warn, "", "gh logged in as %s, but the token could not be checked: %v", user, token.err)
		case id.GHUser != "" && !strings.EqualFold(token.account.Login, id.GHUser):
			add(Fail, login, "gh token belongs to %s, config expects %s", token.account.Login, id.GHUser)
		default:
			add(OK, "", "gh logged in as %s, the token works", token.account.Login)
			if !token.account.CanAddKeys() {
				add(Info, "git-idm login "+name, "gh login cannot add SSH keys (no admin:public_key scope)")
			}
		}
	}
	return checks
}

func checkTokens(c *config.Config, fetch func(string) (ghauth.Account, error)) map[string]tokenResult {
	results := map[string]tokenResult{}
	if fetch == nil {
		return results
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[string]bool{}
	for _, name := range c.Names() {
		dir := config.ExpandHome(c.Identities[name].GHConfigDir)
		if dir == "" || seen[dir] || importer.GHUserFromConfigDir(dir) == "" {
			continue
		}
		seen[dir] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			account, err := fetch(dir)
			mu.Lock()
			results[dir] = tokenResult{account, err}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func shimChecks(c *config.Config, a *gitgen.Applier, lookPath func(string) (string, error)) []Check {
	shim := a.ShimPath()
	if shim == "" || lookPath == nil || !gitgen.UsesGH(c) {
		return nil
	}
	path := config.ContractHome(shim)
	found, err := lookPath("gh")
	if err != nil {
		return nil
	}
	_, statErr := os.Lstat(shim)
	switch {
	case statErr != nil:
		return []Check{{Level: Warn, Text: "gh shim is missing: gh outside the shell hook (scripts, editors, AI agents) uses the wrong login", Fix: "git-idm apply"}}
	case !gitgen.IsShim(shim):
		return []Check{{Level: Info, Text: path + " is not made by git-idm: gh outside the shell hook does not follow the folder"}}
	}
	if data, _ := os.ReadFile(shim); string(data) != gitgen.RenderShim(a.Bin) {
		return []Check{{Level: Warn, Text: "gh shim is out of date", Fix: "git-idm apply"}}
	}
	if real, err := filepath.EvalSymlinks(found); err != nil || !gitgen.IsShim(real) {
		return []Check{{Level: Warn, Text: fmt.Sprintf("gh in PATH is %s, not the git-idm shim", config.ContractHome(found)),
			Fix: "put " + config.ContractHome(a.BinDir) + " first in PATH"}}
	}
	return []Check{{Level: OK, Text: "gh follows the folder in all programs (" + path + ")"}}
}

func guardChecks(c *config.Config, a *gitgen.Applier, global string) []Check {
	if !c.GuardEnabled() {
		return []Check{{Level: Info, Text: "guard is off", Fix: "git-idm guard block"}}
	}
	var checks []Check
	if hp := gitgen.HooksPathOutsideBlock(global); hp != "" {
		checks = append(checks, Check{Level: Fail, Text: "core.hooksPath = " + hp + " outside the git-idm block disables the guard", Fix: "remove it from " + config.ContractHome(a.GlobalConfig)})
	}
	script, err := os.ReadFile(filepath.Join(a.HooksDir(), "pre-commit"))
	switch {
	case err != nil:
		checks = append(checks, Check{Level: Fail, Text: "guard hooks are missing", Fix: "git-idm apply"})
	case string(script) != gitgen.RenderDispatcher(a.Bin):
		checks = append(checks, Check{Level: Warn, Text: "guard hooks point to another git-idm binary", Fix: "git-idm apply"})
	default:
		checks = append(checks, Check{Level: OK, Text: fmt.Sprintf("guard is on (%s)", c.Guard)})
	}
	return checks
}

func repoChecks(c *config.Config, res resolve.Result, local []Check) []Check {
	var checks []Check
	repo := config.ContractHome(res.RepoRoot)
	expected := c.Identities[res.Identity].Email
	reported := false
	for _, ch := range local {
		reported = reported || strings.HasPrefix(ch.Text, repo+": ")
	}
	if res.LocalEmail != "" && !strings.EqualFold(res.LocalEmail, expected) && !reported {
		if strings.EqualFold(guard.Allowed(res.RepoRoot), res.LocalEmail) {
			checks = append(checks, Check{Level: OK, Text: fmt.Sprintf("%s: author %s is accepted on purpose", repo, res.LocalEmail)})
		} else {
			checks = append(checks, Check{Level: Warn, Text: fmt.Sprintf("%s: .git/config sets author %s, %s expects %s", repo, res.LocalEmail, res.Identity, expected),
				Fix: "git-idm allow (if on purpose) or git config --local --unset user.email"})
		}
	}
	if c.GuardEnabled() {
		cmd := exec.Command("git", "config", "--local", "--get", "core.hooksPath")
		cmd.Dir = res.RepoRoot
		if out, _ := cmd.Output(); len(strings.TrimSpace(string(out))) > 0 {
			checks = append(checks, Check{Level: Warn, Text: fmt.Sprintf("%s: local core.hooksPath = %s, the guard does not run here", repo, strings.TrimSpace(string(out)))})
		}
	}
	return checks
}
