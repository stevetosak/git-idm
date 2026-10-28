package setup

import (
	"fmt"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

type Editor struct {
	Prompt Prompter
	Config *config.Config
	Login  func(name string) (Account, string, error)
	Logout func(dir, user string)
	Logins []GHLogin
	Verify func(dir string) (Account, error)

	scopeChecked bool
}

type Edit struct {
	Plan    Plan
	Changed []string
}

func PlanFor(c *config.Config, name string) Plan {
	id := c.Identities[name]
	p := Plan{Name: name, AuthorName: id.Name, Email: id.Email, Color: colorOf(id),
		GHUser: id.GHUser, GHConfigDir: id.GHConfigDir, SSH: SSHNone, MakeDefault: c.Default == name, SigningKey: id.SigningKey}
	if id.SSHKey != "" {
		p.SSH, p.SSHKey = SSHExisting, id.SSHKey
	}
	return p
}

func (e *Editor) Run(name string) (Edit, error) {
	if _, ok := e.Config.Identities[name]; !ok {
		return Edit{}, fmt.Errorf("identity %q does not exist", name)
	}
	edit := Edit{Plan: PlanFor(e.Config, name)}
	err := e.run(&edit)
	if err != nil {
		if edit.Plan.NewLogin && e.Logout != nil {
			e.Logout(edit.Plan.GHConfigDir, edit.Plan.GHUser)
		}
		return Edit{}, err
	}
	return edit, nil
}

func (e *Editor) run(edit *Edit) error {
	original := edit.Plan
	last := "name"
	for {
		edit.Changed = changedFields(original, edit.Plan)
		pick, err := e.Prompt.Select("Edit "+original.Name, editDescription(edit.Changed), e.menu(edit.Plan), last)
		if err != nil {
			return err
		}
		last = pick
		switch pick {
		case "done":
			return nil
		case "cancel":
			return ErrAborted
		}
		if err := e.ask(pick, &edit.Plan); err != nil {
			return err
		}
	}
}

func (e *Editor) wizard() *Wizard {
	return &Wizard{Prompt: e.Prompt, Config: e.Config, Login: e.Login, Logout: e.Logout, Logins: e.Logins, Verify: e.Verify}
}

func (e *Editor) ask(field string, p *Plan) error {
	var err error
	switch field {
	case "name":
		p.AuthorName, err = e.Prompt.Input("Author name", "The name on your commits.", p.AuthorName, required("author name"))
	case "email":
		p.Email, err = e.Prompt.Input("Email", "The email on your commits. GitHub uses it to link commits to your account.", p.Email,
			func(s string) error { return validateEmail(e.Config, p.Name, s) })
	case "ssh":
		e.checkScope(p)
		before := p.SSHKey
		if err = e.wizard().askSSH(p); err == nil && before != "" && p.SigningKey == before+".pub" {
			if p.SSHKey != "" {
				p.SigningKey = p.SSHKey + ".pub"
			} else {
				p.SigningKey, p.UploadSigning = "", false
			}
		}
	case "signing":
		e.checkScope(p)
		err = e.askSigning(p)
	case "github":
		err = e.askGitHub(p)
	case "color":
		p.Color, err = e.Prompt.Select("Prompt color", "The color of this identity in your shell prompt.", colorOptions(), p.Color)
	case "default":
		p.MakeDefault, err = e.Prompt.Confirm("Use "+p.Name+" as the default?", "The default applies in every folder without a rule.", p.MakeDefault)
	}
	return err
}

func (e *Editor) checkScope(p *Plan) {
	if e.scopeChecked || p.NewLogin || p.GHUser == "" || e.Verify == nil {
		return
	}
	e.scopeChecked = true
	account, err := e.Verify(p.GHConfigDir)
	p.NoKeyScope = err != nil || !account.CanAddKeys()
	p.NoSigningScope = err != nil || !account.CanAddSigningKeys()
}

func (e *Editor) askSigning(p *Plan) error {
	var choices []Choice
	own := ""
	if p.SSHKey != "" {
		own = p.SSHKey + ".pub"
		choices = append(choices, Choice{own, "Sign with this identity's SSH key (" + own + ")"})
	}
	for _, key := range existingKeys() {
		if pub := key + ".pub"; pub != own {
			choices = append(choices, Choice{pub, "Sign with " + pub})
		}
	}
	if p.SigningKey != "" && !hasChoice(choices, p.SigningKey) {
		choices = append(choices, Choice{p.SigningKey, "Keep " + p.SigningKey})
	}
	choices = append(choices, Choice{"off", "Do not sign commits"})
	current := p.SigningKey
	if current == "" {
		current = "off"
	}
	pick, err := e.Prompt.Select("Commit signing", "Signed commits show as Verified on GitHub when the key and the email are on your account.", choices, current)
	if err != nil {
		return err
	}
	p.UploadSigning = false
	if pick == "off" {
		p.SigningKey = ""
		return nil
	}
	p.SigningKey = pick
	if p.GHUser == "" || p.NoSigningScope || !strings.HasSuffix(pick, ".pub") {
		return nil
	}
	p.UploadSigning, err = e.Prompt.Confirm("Add the key to "+p.GHUser+" on GitHub as a signing key?", "", true)
	return err
}

func hasChoice(choices []Choice, key string) bool {
	for _, c := range choices {
		if c.Key == key {
			return true
		}
	}
	return false
}

func (e *Editor) askGitHub(p *Plan) error {
	before := *p
	var head, tail []Choice
	if p.GHUser != "" {
		head = []Choice{{"keep", "Keep " + ghLabel(*p)}}
		tail = []Choice{{"remove", "No GitHub login"}}
	} else {
		tail = []Choice{{"keep", "Skip"}}
	}
	pick, err := e.wizard().chooseGitHub(p, head, tail, "keep", p.GHConfigDir)
	if err != nil {
		return err
	}
	if pick == "keep" {
		return nil
	}
	if pick == "remove" {
		p.GHUser, p.GHConfigDir, p.NoKeyScope, p.NewLogin = "", "", false, false
	}
	if before.NewLogin && before.GHConfigDir != p.GHConfigDir && e.Logout != nil {
		e.Logout(before.GHConfigDir, before.GHUser)
	}
	e.scopeChecked = true
	p.Upload, p.UploadSigning = false, false
	return nil
}

func (e *Editor) menu(p Plan) []Choice {
	row := func(key, label, value string) Choice {
		return Choice{key, fmt.Sprintf("%-12s %s", label, value)}
	}
	choices := []Choice{
		row("name", "Author name", p.AuthorName),
		row("email", "Email", p.Email),
		row("ssh", "SSH key", sshLabel(p)),
	}
	if e.Login != nil {
		choices = append(choices, row("github", "GitHub", ghLabel(p)))
	}
	choices = append(choices, row("signing", "Signing", signingLabel(p)))
	return append(choices,
		row("color", "Color", p.Color),
		row("default", "Default", yesNo(p.MakeDefault)),
		Choice{"done", "Done"},
		Choice{"cancel", "Cancel"},
	)
}

func sshLabel(p Plan) string {
	label := p.SSHKey
	switch p.SSH {
	case SSHNone:
		return "none, " + p.httpsText()
	case SSHGenerate:
		label = "new key " + p.SSHKey
	}
	if p.Upload {
		label += ", add to GitHub"
	}
	return label
}

func signingLabel(p Plan) string {
	switch {
	case p.SigningKey == "":
		return "off"
	case p.UploadSigning:
		return p.SigningKey + ", add to GitHub"
	}
	return p.SigningKey
}

func ghLabel(p Plan) string {
	if p.GHUser == "" {
		return "none"
	}
	return p.GHUser + " (" + p.GHConfigDir + ")"
}

func changedFields(original, p Plan) []string {
	var changed []string
	add := func(differs bool, name string) {
		if differs {
			changed = append(changed, name)
		}
	}
	add(p.AuthorName != original.AuthorName, "author name")
	add(p.Email != original.Email, "email")
	add(p.SSH != original.SSH || p.SSHKey != original.SSHKey || p.Upload, "ssh key")
	add(p.GHConfigDir != original.GHConfigDir || p.GHUser != original.GHUser, "GitHub")
	add(p.SigningKey != original.SigningKey || p.UploadSigning, "signing")
	add(p.Color != original.Color, "color")
	add(p.MakeDefault != original.MakeDefault, "default")
	return changed
}

func editDescription(changed []string) string {
	if len(changed) == 0 {
		return "Pick a field to change. Nothing is saved before you pick Done."
	}
	return "Changed: " + strings.Join(changed, ", ") + ". Pick Done to save."
}

func colorOf(id config.Identity) string {
	if id.Color == "" {
		return "cyan"
	}
	return id.Color
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
