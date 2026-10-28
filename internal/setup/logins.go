package setup

import (
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/importer"
)

type GHLogin struct {
	User   string
	Dir    string
	UsedBy []string
}

func (l GHLogin) label() string {
	where := l.Dir
	if len(l.UsedBy) > 0 {
		where += ", used by " + strings.Join(l.UsedBy, ", ")
	}
	return "Use " + l.User + " (" + where + ")"
}

func ExistingLogins(c *config.Config, extraDirs ...string) []GHLogin {
	var logins []GHLogin
	index := map[string]int{}
	add := func(dir, usedBy string) {
		dir = config.ContractHome(config.ExpandHome(dir))
		i, ok := index[dir]
		if !ok {
			user := importer.GHUserFromConfigDir(dir)
			if user == "" {
				return
			}
			i = len(logins)
			index[dir] = i
			logins = append(logins, GHLogin{User: user, Dir: dir})
		}
		if usedBy != "" {
			logins[i].UsedBy = append(logins[i].UsedBy, usedBy)
		}
	}
	for _, name := range c.Names() {
		if dir := c.Identities[name].GHConfigDir; dir != "" {
			add(dir, name)
		}
	}
	for _, dir := range extraDirs {
		add(dir, "")
	}
	return logins
}
