package setup

import (
	"fmt"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

func (w *Wizard) askImportedGitHub(p *Plan, want string) error {
	value := "login"
	for _, l := range w.Logins {
		if strings.EqualFold(l.User, want) {
			value = "use:" + l.Dir
			break
		}
	}
	for {
		if _, err := w.chooseGitHub(p, nil, []Choice{{"skip", "Skip"}}, value, ""); err != nil {
			return err
		}
		if p.GHUser == "" || strings.EqualFold(p.GHUser, want) {
			return nil
		}
		ok, err := w.Prompt.Confirm("Use "+p.GHUser+" instead of "+want+"?",
			"The export used "+want+". No picks the GitHub account again.", false)
		if err != nil || ok {
			return err
		}
		if p.NewLogin && w.Logout != nil {
			w.Logout(p.GHConfigDir, p.GHUser)
		}
		p.GHUser, p.GHConfigDir, p.NewLogin, p.NoKeyScope, p.NoSigningScope = "", "", false, false, false
	}
}

func (w *Wizard) defaultColor() string {
	if w.Import != nil && w.Import.Color != "" {
		return w.Import.Color
	}
	return w.unusedColor()
}

func freeName(c *config.Config, base string) string {
	for i := 2; i < 100; i++ {
		name := fmt.Sprintf("%s-%d", base, i)
		if _, taken := c.Identities[name]; !taken && config.ValidateIdentityName(name) == nil {
			return name
		}
	}
	return ""
}
