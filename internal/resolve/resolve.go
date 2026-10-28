package resolve

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

const OverrideEnv = "GIT_IDM_OVERRIDE"

type Source string

const (
	SourceOverride Source = "override"
	SourceRule     Source = "rule"
	SourceDefault  Source = "default"
	SourceNone     Source = "none"
)

type Result struct {
	Identity     string
	Source       Source
	Rule         *config.Rule
	RuleIdentity string
	RepoRoot     string
	LocalUser    string
	LocalEmail   string
}

func Resolve(c *config.Config, cwd, override string) Result {
	var res Result
	matchPath := cwd
	if root, gitdir := findRepo(cwd); root != "" {
		res.RepoRoot = root
		matchPath = gitdir
		res.LocalUser, res.LocalEmail = localUser(filepath.Join(gitdir, "config"))
	}
	if rule := matchRule(c.Rules, matchPath); rule != nil {
		res.Rule = rule
		res.RuleIdentity = rule.Identity
	}
	switch {
	case override != "":
		res.Identity, res.Source = override, SourceOverride
	case res.Rule != nil:
		res.Identity, res.Source = res.Rule.Identity, SourceRule
	case c.Default != "":
		res.Identity, res.Source = c.Default, SourceDefault
	default:
		res.Source = SourceNone
	}
	return res
}

func matchRule(rules []config.Rule, path string) *config.Rule {
	candidates := []string{path}
	if real, err := filepath.EvalSymlinks(path); err == nil && real != path {
		candidates = append(candidates, real)
	}
	var best *config.Rule
	for i := range rules {
		prefix := config.ExpandHome(rules[i].Path)
		for _, p := range candidates {
			if hasDirPrefix(p, prefix) && (best == nil || len(rules[i].Path) >= len(best.Path)) {
				best = &rules[i]
			}
		}
	}
	return best
}

func hasDirPrefix(path, prefix string) bool {
	path = filepath.Clean(path) + "/"
	if caseInsensitive() {
		path, prefix = strings.ToLower(path), strings.ToLower(prefix)
	}
	return strings.HasPrefix(path, prefix)
}

func caseInsensitive() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func findRepo(dir string) (root, gitdir string) {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		dotgit := filepath.Join(d, ".git")
		info, err := os.Stat(dotgit)
		if err == nil {
			if info.IsDir() {
				return d, dotgit
			}
			if target := gitdirFromFile(dotgit); target != "" {
				return d, target
			}
		}
		if parent := filepath.Dir(d); parent == d {
			return "", ""
		}
	}
}

func gitdirFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	target = strings.TrimSpace(target)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

func localUser(configPath string) (name, email string) {
	f, err := os.Open(configPath)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	inUser := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inUser = strings.EqualFold(strings.Trim(line, "[] \t"), "user")
			continue
		}
		if !inUser {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			name = value
		case "email":
			email = value
		}
	}
	return name, email
}
