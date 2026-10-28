package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

type RemovePlan struct {
	Name            string
	NewDefault      string
	DeleteKey       bool
	DeleteGitHubKey bool
	GitHubKeyID     int64
	Logout          bool
	Kept            []string
}

type Remover struct {
	Prompt        Prompter
	Config        *config.Config
	ManagedGHRoot string
	FindKey       func(ghDir, pubKey string) (int64, error)
}

func (r *Remover) Recommended(name string) (RemovePlan, error) {
	p, err := r.base(name)
	if err != nil {
		return p, err
	}
	id := r.Config.Identities[name]
	if r.ownKey(name) && IsGitIDMKey(id.SSHKey) {
		p.DeleteKey = true
		p.GitHubKeyID, p.DeleteGitHubKey = r.githubKey(id)
	}
	p.Logout = r.ownLogin(name)
	p.Kept = r.kept(name, p)
	return p, nil
}

func (r *Remover) ConfigOnly(name string) (RemovePlan, error) {
	p, err := r.base(name)
	if err != nil {
		return p, err
	}
	p.Kept = r.kept(name, p)
	return p, nil
}

func (r *Remover) Ask(name string) (RemovePlan, error) {
	p, err := r.base(name)
	if err != nil {
		return p, err
	}
	id := r.Config.Identities[name]
	if r.Config.Default == name && len(r.Config.Identities) > 1 {
		choices := []Choice{{"none", "No default (folders without an identity use plain git config)"}}
		for _, other := range r.Config.Names() {
			if other != name {
				choices = append(choices, Choice{other, other})
			}
		}
		if p.NewDefault, err = r.Prompt.Select("New default identity", name+" is the default for every folder without an identity.", choices, "none"); err != nil {
			return p, err
		}
		if p.NewDefault == "none" {
			p.NewDefault = ""
		}
	}
	if r.ownKey(name) {
		help := "git-idm created this key for " + name + "."
		if !IsGitIDMKey(id.SSHKey) {
			help = "git-idm did not create this key. Other tools can use it."
		}
		if p.DeleteKey, err = r.Prompt.Confirm("Delete the SSH key "+id.SSHKey+"?", help, IsGitIDMKey(id.SSHKey)); err != nil {
			return p, err
		}
		if keyID, found := r.githubKey(id); found {
			p.GitHubKeyID = keyID
			if p.DeleteGitHubKey, err = r.Prompt.Confirm("Remove the key from "+id.GHUser+" on GitHub?", "Pushes with this key stop working.", p.DeleteKey); err != nil {
				return p, err
			}
		}
	}
	if r.ownLogin(name) {
		if p.Logout, err = r.Prompt.Confirm("Log out of gh ("+id.GHUser+") and delete the login?", "The login is in "+id.GHConfigDir+".", true); err != nil {
			return p, err
		}
	}
	p.Kept = r.kept(name, p)
	if err := r.Prompt.Note("Remove "+name, p.Summary(r.Config)); err != nil {
		return p, err
	}
	ok, err := r.Prompt.Confirm("Remove "+name+"?", "", false)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrAborted
	}
	return p, nil
}

func (r *Remover) base(name string) (RemovePlan, error) {
	if _, ok := r.Config.Identities[name]; !ok {
		return RemovePlan{}, fmt.Errorf("identity %q does not exist", name)
	}
	return RemovePlan{Name: name}, nil
}

func (r *Remover) ownKey(name string) bool {
	key := r.Config.Identities[name].SSHKey
	if key == "" || !fileExists(config.ExpandHome(key)) {
		return false
	}
	for _, other := range r.Config.Names() {
		if other != name && r.Config.Identities[other].SSHKey == key {
			return false
		}
	}
	return true
}

func (r *Remover) ownLogin(name string) bool {
	id := r.Config.Identities[name]
	if id.GHConfigDir == "" || r.ManagedGHRoot == "" {
		return false
	}
	if filepath.Dir(config.ExpandHome(id.GHConfigDir)) != r.ManagedGHRoot {
		return false
	}
	for _, other := range r.Config.Names() {
		if other != name && r.Config.Identities[other].GHConfigDir == id.GHConfigDir {
			return false
		}
	}
	return true
}

func (r *Remover) githubKey(id config.Identity) (int64, bool) {
	if r.FindKey == nil || id.GHConfigDir == "" || id.SSHKey == "" {
		return 0, false
	}
	keyID, err := r.FindKey(id.GHConfigDir, config.ExpandHome(id.SSHKey)+".pub")
	return keyID, err == nil && keyID != 0
}

func (r *Remover) kept(name string, p RemovePlan) []string {
	id := r.Config.Identities[name]
	var kept []string
	if id.SSHKey != "" && !p.DeleteKey {
		kept = append(kept, "ssh key "+id.SSHKey)
	}
	if id.SSHKey != "" && id.GHUser != "" && !p.DeleteGitHubKey {
		kept = append(kept, "the key on GitHub, if you added it: https://github.com/settings/keys")
	}
	if id.GHConfigDir != "" && !p.Logout {
		kept = append(kept, "gh login in "+id.GHConfigDir)
	}
	return kept
}

func (p RemovePlan) Summary(c *config.Config) string {
	id := c.Identities[p.Name]
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s <%s>\n", p.Name, id.Name, id.Email)
	fallback := c.Default
	if c.Default == p.Name {
		fallback = p.NewDefault
		if p.NewDefault != "" {
			fmt.Fprintf(&b, "new default: %s\n", p.NewDefault)
		} else {
			fmt.Fprintf(&b, "no default identity after this\n")
		}
	}
	for _, rule := range c.RulesFor(p.Name) {
		if fallback != "" && fallback != p.Name {
			fmt.Fprintf(&b, "unbind %s (repos there then use %s)\n", rule.Path, fallback)
		} else {
			fmt.Fprintf(&b, "unbind %s\n", rule.Path)
		}
	}
	if p.DeleteGitHubKey {
		fmt.Fprintf(&b, "remove the key from %s on GitHub\n", id.GHUser)
	}
	if p.DeleteKey {
		fmt.Fprintf(&b, "delete %s and %s.pub\n", id.SSHKey, id.SSHKey)
	}
	if p.Logout {
		fmt.Fprintf(&b, "log out of gh (%s) and delete %s\n", id.GHUser, id.GHConfigDir)
	}
	for _, k := range p.Kept {
		fmt.Fprintf(&b, "keep %s\n", k)
	}
	return strings.TrimRight(b.String(), "\n")
}

func IsGitIDMKey(path string) bool {
	return strings.HasPrefix(filepath.Base(path), "git-idm_")
}

func (e *Executor) FindGitHubKey(ghDir, pubPath string) (int64, error) {
	pub, err := os.ReadFile(pubPath)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(pub))
	if len(fields) < 2 {
		return 0, fmt.Errorf("%s is not a public key", pubPath)
	}
	want := fields[0] + " " + fields[1]
	cmd := exec.Command(e.Tools.GH, "api", "user/keys")
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+config.ExpandHome(ghDir))
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var keys []struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(out, &keys); err != nil {
		return 0, err
	}
	for _, k := range keys {
		f := strings.Fields(k.Key)
		if len(f) >= 2 && f[0]+" "+f[1] == want {
			return k.ID, nil
		}
	}
	return 0, nil
}

func (e *Executor) Remove(c *config.Config, p RemovePlan) []string {
	id := c.Identities[p.Name]
	var failed []string
	try := func(what string, err error) {
		if err != nil {
			failed = append(failed, what+": "+err.Error())
			fmt.Fprintf(e.Out, "  ✗ %s: %v\n", what, err)
			return
		}
		fmt.Fprintf(e.Out, "  ✓ %s\n", what)
	}
	if p.DeleteGitHubKey {
		try("removed the key from "+id.GHUser+" on GitHub", e.gh(id.GHConfigDir, "api", "--method", "DELETE", fmt.Sprintf("user/keys/%d", p.GitHubKeyID)))
	}
	if p.DeleteKey {
		key := config.ExpandHome(id.SSHKey)
		err := os.Remove(key)
		if err == nil || os.IsNotExist(err) {
			err = os.Remove(key + ".pub")
			if os.IsNotExist(err) {
				err = nil
			}
		}
		try("deleted "+id.SSHKey, err)
	}
	if p.Logout {
		e.Logout(id.GHConfigDir, id.GHUser)
		var err error
		if fileExists(config.ExpandHome(id.GHConfigDir)) {
			err = fmt.Errorf("%s still exists", id.GHConfigDir)
		}
		try("logged out of gh ("+id.GHUser+")", err)
	}
	return failed
}
