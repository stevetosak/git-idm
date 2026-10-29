package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/localid"
)

type UnboundGroup struct {
	Dir       string
	Repos     []string
	Suggested string
}

func (g UnboundGroup) RulePath() string {
	return strings.TrimSuffix(config.ContractHome(g.Dir), "/") + "/"
}

func ScanHome(extra ...string) localid.Result {
	roots, exclude := HomeScan()
	return localid.Scan(append(roots, extra...), localid.Options{Exclude: exclude})
}

func UnboundGroups(c *config.Config, repos []localid.Repo) []UnboundGroup {
	home, _ := os.UserHomeDir()
	suggest := map[string]string{}
	byParent := map[string][]string{}
	var parents []string
	for _, repo := range repos {
		owner := ownerDir(repo)
		if _, seen := suggest[owner]; seen || coveringRule(c.Rules, gitDataDir(repo)) != nil || insideOther(repos, owner) {
			continue
		}
		suggest[owner] = suggestIdentity(c, repo)
		parent := filepath.Dir(owner)
		if byParent[parent] == nil {
			parents = append(parents, parent)
		}
		byParent[parent] = append(byParent[parent], owner)
	}
	var groups []UnboundGroup
	for _, parent := range parents {
		roots := byParent[parent]
		if len(roots) > 1 && parent != home && filepath.Dir(parent) != parent && sameSuggestion(suggest, roots) && !hasBoundRepo(c, repos, parent) {
			groups = append(groups, UnboundGroup{Dir: parent, Repos: roots, Suggested: suggest[roots[0]]})
			continue
		}
		for _, root := range roots {
			groups = append(groups, UnboundGroup{Dir: root, Repos: []string{root}, Suggested: suggest[root]})
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Dir < groups[j].Dir })
	return groups
}

func insideOther(repos []localid.Repo, dir string) bool {
	for _, other := range repos {
		if owner := ownerDir(other); owner != dir && under(dir, owner) {
			return true
		}
	}
	return false
}

func ownerDir(repo localid.Repo) string {
	data := gitDataDir(repo)
	if filepath.Base(data) == ".git" {
		return filepath.Dir(data)
	}
	return data
}

func hasBoundRepo(c *config.Config, repos []localid.Repo, dir string) bool {
	for _, repo := range repos {
		if under(repo.Root, dir) && coveringRule(c.Rules, gitDataDir(repo)) != nil {
			return true
		}
	}
	return false
}

func sameSuggestion(suggest map[string]string, roots []string) bool {
	for _, root := range roots[1:] {
		if suggest[root] != suggest[roots[0]] {
			return false
		}
	}
	return true
}

func suggestIdentity(c *config.Config, repo localid.Repo) string {
	for _, email := range []string{repo.Value("user.email"), lastAuthor(repo.Root)} {
		if email == "" {
			continue
		}
		for _, name := range c.Names() {
			if strings.EqualFold(c.Identities[name].Email, email) {
				return name
			}
		}
	}
	return c.Default
}

func lastAuthor(root string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%ae")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
