package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/localid"
)

var protectedHomeDirs = []string{"Applications", "Desktop", "Documents", "Downloads", "Library", "Movies", "Music", "Pictures", "Public"}

var publicMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true,
	"yahoo.com": true, "icloud.com": true, "me.com": true, "proton.me": true, "protonmail.com": true,
	"users.noreply.github.com": true,
}

func HomeScan() (roots, exclude []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	for _, dir := range protectedHomeDirs {
		exclude = append(exclude, filepath.Join(home, dir))
	}
	return []string{home}, exclude
}

func (r *Result) importRepos(global gitConfig, opts Options) {
	roots := append([]string{}, opts.Scan...)
	for _, rule := range r.Config.Rules {
		roots = append(roots, config.ExpandHome(rule.Path))
	}
	scan := localid.Scan(roots, localid.Options{Exclude: opts.Exclude})
	if scan.Truncated {
		r.Skipped = append(r.Skipped, "repo scan stopped early, some repos were not read (add folders with --scan)")
	}
	effective := map[string]string{}
	groups := map[string][]localid.Repo{}
	var order []string
	for _, repo := range scan.Repos {
		email := repo.Value("user.email")
		if email == "" {
			effective[repo.Root] = enclosingEmail(scan.Repos, repo.Root)
			if effective[repo.Root] == "" {
				effective[repo.Root] = r.inheritedEmail(repo.Root)
			}
			continue
		}
		key := strings.ToLower(email)
		effective[repo.Root] = key
		if groups[key] == nil {
			order = append(order, key)
		}
		groups[key] = append(groups[key], repo)
	}
	home, _ := os.UserHomeDir()
	for _, key := range order {
		repos := groups[key]
		name, ok := r.repoIdentity(global, repos)
		if !ok {
			continue
		}
		rules, mixed := suggestRules(repos, effective, key, home)
		for _, dir := range rules {
			r.addRepoRule(dir, name, key)
		}
		for _, root := range mixed {
			r.Skipped = append(r.Skipped, fmt.Sprintf("%s sets %s, but other authors work in its folder (bind it later with `git-idm bind`)", config.ContractHome(root), key))
		}
	}
}

func (r *Result) repoIdentity(global gitConfig, repos []localid.Repo) (string, bool) {
	email := repos[0].Value("user.email")
	for _, name := range r.Config.Names() {
		if strings.EqualFold(r.Config.Identities[name].Email, email) {
			return name, true
		}
	}
	id := config.Identity{
		Name:       mostCommon(repos, "user.name"),
		Email:      email,
		SigningKey: mostCommon(repos, "user.signingkey"),
	}
	if id.Name == "" {
		id.Name = global.value("user.name")
	}
	key, sshFrom := sshKeyFromCommand(mostCommon(repos, "core.sshcommand"))
	if key != "" {
		id.SSHKey = config.ContractHome(config.ExpandHome(key))
	}
	if err := id.Validate(); err != nil {
		r.Skipped = append(r.Skipped, fmt.Sprintf("repos with author %q: %v", email, err))
		return "", false
	}
	name := r.uniqueName(nameFromEmail(email))
	r.Config.Identities[name] = id
	r.Findings = append(r.Findings, describe(name, id, fmt.Sprintf("%s in %s", plural(len(repos), "repo config"), repoList(repos)), sshFrom, ""))
	return name, true
}

func (r *Result) addRepoRule(dir, name, email string) {
	if strings.EqualFold(r.inheritedEmail(dir), email) {
		return
	}
	rule := config.Rule{Path: strings.TrimSuffix(config.ContractHome(dir), "/") + "/", Identity: name}
	if err := rule.Validate(); err != nil {
		r.Skipped = append(r.Skipped, fmt.Sprintf("%s: %v", config.ContractHome(dir), err))
		return
	}
	for _, existing := range r.Config.Rules {
		if existing.Path == rule.Path {
			r.Skipped = append(r.Skipped, fmt.Sprintf("%s: already uses %s, repo configs there set %s", rule.Path, existing.Identity, email))
			return
		}
	}
	r.Config.Rules = append(r.Config.Rules, rule)
	r.Findings = append(r.Findings, fmt.Sprintf("%s: rule %s  (from repo configs)", name, rule.Path))
}

func (r *Result) inheritedEmail(dir string) string {
	best := ""
	email := ""
	for _, rule := range r.Config.Rules {
		prefix := config.ExpandHome(rule.Path)
		if under(dir, prefix) && len(prefix) > len(best) {
			best, email = prefix, r.Config.Identities[rule.Identity].Email
		}
	}
	if best == "" && r.Config.Default != "" {
		email = r.Config.Identities[r.Config.Default].Email
	}
	return strings.ToLower(email)
}

func enclosingEmail(repos []localid.Repo, root string) string {
	best, email := "", ""
	for _, repo := range repos {
		if repo.Root != root && len(repo.Root) > len(best) && under(root, repo.Root) && repo.Value("user.email") != "" {
			best, email = repo.Root, strings.ToLower(repo.Value("user.email"))
		}
	}
	return email
}

func suggestRules(repos []localid.Repo, effective map[string]string, email, home string) (rules, mixed []string) {
	pure := func(dir string) bool {
		for root, e := range effective {
			if e != email && under(root, dir) {
				return false
			}
		}
		return true
	}
	byTop := map[string][]string{}
	var tops []string
	for _, repo := range repos {
		top := ""
		for d := repo.Root; d != home && filepath.Dir(d) != d && pure(d); d = filepath.Dir(d) {
			top = d
		}
		if top == "" {
			mixed = append(mixed, repo.Root)
			continue
		}
		if byTop[top] == nil {
			tops = append(tops, top)
		}
		byTop[top] = append(byTop[top], repo.Root)
	}
	for _, top := range tops {
		rules = append(rules, commonDir(byTop[top]))
	}
	return rules, mixed
}

func commonDir(paths []string) string {
	common := paths[0]
	for _, p := range paths[1:] {
		for !under(p, common) {
			common = filepath.Dir(common)
		}
	}
	return common
}

func under(path, dir string) bool {
	path = filepath.Clean(path) + string(filepath.Separator)
	dir = strings.TrimSuffix(filepath.Clean(dir), string(filepath.Separator)) + string(filepath.Separator)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path, dir = strings.ToLower(path), strings.ToLower(dir)
	}
	return strings.HasPrefix(path, dir)
}

func mostCommon(repos []localid.Repo, key string) string {
	counts := map[string]int{}
	var order []string
	for _, repo := range repos {
		if v := repo.Value(key); v != "" {
			if counts[v] == 0 {
				order = append(order, v)
			}
			counts[v]++
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	if len(order) == 0 {
		return ""
	}
	return order[0]
}

func nameFromEmail(email string) string {
	local, domain, _ := strings.Cut(strings.ToLower(email), "@")
	if strings.HasSuffix(domain, "users.noreply.github.com") {
		if _, login, ok := strings.Cut(local, "+"); ok {
			local = login
		}
		return sanitizeName(local)
	}
	if publicMailDomains[domain] {
		return sanitizeName(local)
	}
	label, _, _ := strings.Cut(domain, ".")
	return sanitizeName(label)
}

func repoList(repos []localid.Repo) string {
	const max = 3
	var names []string
	for i, repo := range repos {
		if i == max {
			names = append(names, fmt.Sprintf("+%d more", len(repos)-max))
			break
		}
		names = append(names, config.ContractHome(repo.Root))
	}
	return strings.Join(names, ", ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
