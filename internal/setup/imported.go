package setup

import (
	"fmt"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/transfer"
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

func (w *Wizard) askImportedSigning(p *Plan) error {
	if w.Import == nil || w.Import.Signing == "" {
		return nil
	}
	description := "The export signs commits with SSH. Signed commits show as Verified on GitHub."
	p.SigningKey = ""
	switch w.Import.Signing {
	case transfer.SigningSSH:
		if p.SSHKey != "" {
			p.SigningKey = p.SSHKey + ".pub"
		}
	case transfer.SigningGPG:
		key := w.Import.GPGKey
		switch {
		case key == "":
			description = "The export signs commits with gpg, but the file has no key ID. Pick a key or turn signing off."
		case w.HasGPGKey != nil && w.HasGPGKey(key):
			p.SigningKey = key
			description = "The export signs commits with the gpg key " + key + ". The key is in your gpg keyring."
		default:
			description = "The export signs commits with the gpg key " + key + ", but your gpg keyring does not have it.\n" +
				"Import the key with gpg first, or pick another key."
		}
	}
	return askSigning(w.Prompt, p, description)
}
