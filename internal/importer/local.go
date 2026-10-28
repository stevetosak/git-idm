package importer

import (
	"fmt"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/localid"
)

func ScanRules(rules []config.Rule) localid.Result {
	var roots []string
	for _, r := range rules {
		roots = append(roots, config.ExpandHome(r.Path))
	}
	return localid.Scan(roots, localid.Options{})
}

func LocalLegacy(c *config.Config, repos []localid.Repo) []Legacy {
	var out []Legacy
	for _, repo := range repos {
		if !repo.Overrides() {
			continue
		}
		email := repo.Value("user.email")
		if email != "" && strings.EqualFold(repo.Allowed, email) {
			continue
		}
		rule := coveringRule(c.Rules, repo.Root)
		if rule == nil {
			continue
		}
		id := c.Identities[rule.Identity]
		var keys, parts []string
		for _, k := range repo.Keys {
			if (k.Name == "user.signingkey" && id.SigningKey == "") || (k.Name == "core.sshcommand" && id.SSHKey == "") {
				continue
			}
			keys = append(keys, k.Name)
			part := k.Name
			if len(k.Values) > 1 {
				part = fmt.Sprintf("%s (%d×)", k.Name, len(k.Values))
			}
			parts = append(parts, part)
		}
		if len(keys) == 0 {
			continue
		}
		summary := strings.Join(parts, ", ")
		effect := " (" + rule.Identity + " sets them)"
		author := ""
		if email != "" && !strings.EqualFold(email, id.Email) {
			author = email
			effect = fmt.Sprintf(", author %s becomes %s (%s)", email, id.Email, rule.Identity)
		}
		out = append(out, Legacy{
			Kind:     LegacyLocal,
			Path:     config.ContractHome(repo.Root),
			Detail:   "remove " + summary + effect,
			RulePath: rule.Path,
			Config:   repo.Config,
			Keys:     keys,
			Summary:  summary,
			Author:   author,
		})
	}
	return out
}

func coveringRule(rules []config.Rule, dir string) *config.Rule {
	var best *config.Rule
	for i := range rules {
		if under(dir, config.ExpandHome(rules[i].Path)) && (best == nil || len(rules[i].Path) > len(best.Path)) {
			best = &rules[i]
		}
	}
	return best
}
