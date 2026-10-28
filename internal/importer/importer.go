package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

type LegacyKind string

const (
	LegacyInclude LegacyKind = "include"
	LegacyFile    LegacyKind = "file"
	LegacyEnvrc   LegacyKind = "envrc"
)

type Legacy struct {
	Kind      LegacyKind
	Path      string
	Detail    string
	Condition string
	RulePath  string
}

type Result struct {
	Config   *config.Config
	Findings []string
	Skipped  []string
	Legacy   []Legacy
}

type Options struct {
	DefaultName string
}

var invalidNameChars = regexp.MustCompile(`[^a-z0-9_-]+`)

func Import(opts Options) (*Result, error) {
	if opts.DefaultName == "" {
		opts.DefaultName = "work"
	}
	if err := config.ValidateIdentityName(opts.DefaultName); err != nil {
		return nil, err
	}
	global, err := readGlobal()
	if err != nil {
		return nil, err
	}
	res := &Result{Config: config.New()}
	if err := res.importDefault(global, opts.DefaultName); err != nil {
		return nil, err
	}
	for _, inc := range global.includeIfs() {
		res.importInclude(global, inc)
	}
	if err := res.Config.Validate(); err != nil {
		return nil, fmt.Errorf("imported config is not valid: %w", err)
	}
	return res, nil
}

func (r *Result) importDefault(global gitConfig, name string) error {
	email := global.value("user.email")
	if email == "" {
		r.Skipped = append(r.Skipped, "global: no user.email, no default identity created")
		return nil
	}
	id := identityFrom(global, global)
	id.GHConfigDir = defaultGHConfigDir()
	id.GHUser = ghUserFromConfigDir(id.GHConfigDir)
	r.Config.Identities[name] = id
	r.Config.Default = name
	r.Findings = append(r.Findings, describe(name, id, "~/.gitconfig", sshSource(global), "default gh config"))
	return nil
}

func (r *Result) importInclude(global gitConfig, inc include) {
	cond, pattern, ok := strings.Cut(inc.Condition, ":")
	if !ok || (cond != "gitdir" && cond != "gitdir/i") {
		r.Skipped = append(r.Skipped, fmt.Sprintf("includeIf %q: only gitdir conditions are supported", inc.Condition))
		return
	}
	rule := config.Rule{Path: config.ContractHome(config.ExpandHome(pattern))}
	if strings.ContainsAny(rule.Path, "*?[") {
		r.Skipped = append(r.Skipped, fmt.Sprintf("includeIf %q: glob patterns are not supported yet", inc.Condition))
		return
	}
	if err := rule.Validate(); err != nil {
		r.Skipped = append(r.Skipped, fmt.Sprintf("includeIf %q: %v", inc.Condition, err))
		return
	}
	file := config.ExpandHome(inc.Path)
	if managedByGitIDM(file) {
		return
	}
	included, err := readFile(file)
	if err != nil {
		r.Skipped = append(r.Skipped, fmt.Sprintf("includeIf %q: %v", inc.Condition, err))
		return
	}
	id := identityFrom(included, global)
	if !hasKey(included, "user.email") {
		r.Skipped = append(r.Skipped, fmt.Sprintf("includeIf %q: %s sets no user.email", inc.Condition, inc.Path))
		return
	}
	ghSource := ""
	envrc := filepath.Join(config.ExpandHome(rule.Path), ".envrc")
	if dir := ghConfigDirFromEnvrc(envrc); dir != "" {
		id.GHConfigDir = dir
		id.GHUser = ghUserFromConfigDir(dir)
		ghSource = config.ContractHome(envrc)
		r.Legacy = append(r.Legacy, envrcLegacy(envrc, rule.Path))
	}
	name := r.uniqueName(identityName(inc.Path, rule.Path))
	r.Config.Identities[name] = id
	r.Config.Rules = append(r.Config.Rules, config.Rule{Path: rule.Path, Identity: name})
	r.Findings = append(r.Findings, describe(name, id, inc.Path, sshSource(included), ghSource)+
		fmt.Sprintf("\n    rule:  %s  (from includeIf %q)", rule.Path, inc.Condition))
	r.Legacy = append(r.Legacy,
		Legacy{Kind: LegacyInclude, Path: "~/.gitconfig", Detail: fmt.Sprintf("remove includeIf %q", inc.Condition), Condition: inc.Condition, RulePath: rule.Path},
		Legacy{Kind: LegacyFile, Path: config.ContractHome(file), Detail: "delete (replaced by generated identity file)", RulePath: rule.Path},
	)
}

func EnvrcLegacy(rules []config.Rule) []Legacy {
	var out []Legacy
	for _, r := range rules {
		envrc := filepath.Join(config.ExpandHome(r.Path), ".envrc")
		if ghConfigDirFromEnvrc(envrc) != "" {
			out = append(out, envrcLegacy(envrc, r.Path))
		}
	}
	return out
}

func envrcLegacy(envrc, rulePath string) Legacy {
	detail := "remove GH_CONFIG_DIR line"
	if envrcOnlySetsGHConfigDir(envrc) {
		detail = "delete (it only sets GH_CONFIG_DIR)"
	}
	return Legacy{Kind: LegacyEnvrc, Path: config.ContractHome(envrc), Detail: detail, RulePath: rulePath}
}

func managedByGitIDM(file string) bool {
	dir, err := config.Dir()
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dir, file)
	return err == nil && !strings.HasPrefix(rel, "..")
}

func identityFrom(cfg, global gitConfig) config.Identity {
	id := config.Identity{
		Name:       cfg.value("user.name"),
		Email:      cfg.value("user.email"),
		SigningKey: cfg.value("user.signingkey"),
	}
	if id.Name == "" {
		id.Name = global.value("user.name")
	}
	if key, _ := sshKeyFromCommand(cfg.value("core.sshcommand")); key != "" {
		id.SSHKey = config.ContractHome(config.ExpandHome(key))
	}
	if usesSSHForGitHub(cfg) {
		id.Protocol = config.ProtocolSSH
	}
	return id
}

func usesSSHForGitHub(cfg gitConfig) bool {
	for _, e := range cfg {
		lower := strings.ToLower(e.Key)
		if strings.HasPrefix(lower, "url.git@github.com:") && strings.HasSuffix(lower, ".insteadof") &&
			strings.HasPrefix(e.Value, "https://github.com/") {
			return true
		}
	}
	return false
}

func sshSource(cfg gitConfig) string {
	_, source := sshKeyFromCommand(cfg.value("core.sshcommand"))
	return source
}

func hasKey(cfg gitConfig, key string) bool {
	_, ok := cfg.get(key)
	return ok
}

func identityName(includePath, rulePath string) string {
	base := filepath.Base(includePath)
	for _, prefix := range []string{".gitconfig-", ".gitconfig_", "gitconfig-", ".gitconfig."} {
		if rest, ok := strings.CutPrefix(base, prefix); ok && rest != "" {
			return sanitizeName(rest)
		}
	}
	return sanitizeName(filepath.Base(strings.TrimSuffix(rulePath, "/")))
}

func sanitizeName(s string) string {
	s = invalidNameChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-_")
	if len(s) > 32 {
		s = s[:32]
	}
	if s == "" {
		return "identity"
	}
	return s
}

func (r *Result) uniqueName(base string) string {
	name := base
	for i := 2; ; i++ {
		if _, taken := r.Config.Identities[name]; !taken {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

func describe(name string, id config.Identity, from, sshFrom, ghFrom string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s <%s>  (from %s)", name, id.Name, id.Email, from)
	if id.SSHKey != "" {
		fmt.Fprintf(&b, "\n    ssh:   %s  (from %s)", id.SSHKey, sshFrom)
	}
	if id.GHConfigDir != "" {
		user := id.GHUser
		if user == "" {
			user = "not logged in"
		}
		fmt.Fprintf(&b, "\n    gh:    %s in %s  (from %s)", user, id.GHConfigDir, ghFrom)
	}
	if id.Protocol == config.ProtocolSSH {
		b.WriteString("\n    proto: ssh  (from url insteadOf)")
	}
	return b.String()
}

func Exists() bool {
	path, err := config.Path()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
