package setup

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

type SSHMode string

const (
	SSHNone     SSHMode = "none"
	SSHGenerate SSHMode = "generate"
	SSHExisting SSHMode = "existing"
)

type Plan struct {
	Name       string
	AuthorName string
	Email      string
	Color      string
	GHLogin    bool
	SSH        SSHMode
	SSHKey     string
	Passphrase bool
	Upload     bool
	Sign       bool
	BindDir    string
	CreateDir  bool
}

type Wizard struct {
	Prompt      Prompter
	Config      *config.Config
	DefaultName string
	DefaultUser string
}

var colorChoices = []string{"cyan", "green", "magenta", "yellow", "blue", "red", "white"}

func (w *Wizard) Run() (Plan, error) {
	var p Plan
	var err error
	if p.Name, err = w.askName(); err != nil {
		return p, err
	}
	if p.AuthorName, err = w.Prompt.Input("Author name", "The name on your commits.", w.DefaultUser, required("author name")); err != nil {
		return p, err
	}
	if p.Email, err = w.Prompt.Input("Email", "The email on your commits. GitHub uses it to link commits to your account.", "", w.validateEmail); err != nil {
		return p, err
	}
	gh, err := w.Prompt.Select("GitHub account",
		"git-idm keeps this login separate from your other gh accounts.",
		[]Choice{{"login", "Log in now (opens the browser)"}, {"skip", "Skip"}}, "login")
	if err != nil {
		return p, err
	}
	p.GHLogin = gh == "login"
	if err := w.askSSH(&p); err != nil {
		return p, err
	}
	if err := w.askDir(&p); err != nil {
		return p, err
	}
	if p.Color, err = w.Prompt.Select("Prompt color", "The color of this identity in your shell prompt.", w.colorOptions(), w.unusedColor()); err != nil {
		return p, err
	}
	if err := w.Prompt.Note("Summary", p.Summary()); err != nil {
		return p, err
	}
	ok, err := w.Prompt.Confirm("Create this identity?", "Nothing is changed before you confirm.", true)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrAborted
	}
	return p, nil
}

func (w *Wizard) askName() (string, error) {
	validate := func(s string) error {
		if err := config.ValidateIdentityName(s); err != nil {
			return fmt.Errorf("use 1-32 characters: a-z, 0-9, - or _")
		}
		if _, taken := w.Config.Identities[s]; taken {
			return fmt.Errorf("%q already exists", s)
		}
		return nil
	}
	if w.DefaultName != "" && validate(w.DefaultName) == nil {
		return w.DefaultName, nil
	}
	return w.Prompt.Input("Identity name", "A short name, used in the prompt and in commands. Example: client", w.DefaultName, validate)
}

func (w *Wizard) validateEmail(s string) error {
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return fmt.Errorf("enter an email address, for example you@example.com")
	}
	for _, name := range w.Config.Names() {
		if strings.EqualFold(w.Config.Identities[name].Email, s) {
			return fmt.Errorf("identity %q already uses this email", name)
		}
	}
	return nil
}

func (w *Wizard) askSSH(p *Plan) error {
	generated := filepath.Join("~", ".ssh", "git-idm_"+p.Name)
	var choices []Choice
	if !fileExists(config.ExpandHome(generated)) {
		choices = append(choices, Choice{"generate", "Generate a new key (recommended)"})
	}
	for _, key := range existingKeys() {
		choices = append(choices, Choice{key, "Use " + key})
	}
	choices = append(choices, Choice{"none", "None (use your default SSH key)"})
	pick, err := w.Prompt.Select("SSH key", "A separate key makes sure pushes use this account.", choices, "")
	if err != nil {
		return err
	}
	switch pick {
	case "none":
		p.SSH = SSHNone
		return nil
	case "generate":
		p.SSH, p.SSHKey = SSHGenerate, generated
		pass, err := w.Prompt.Select("Key passphrase", "A passphrase protects the key file. ssh asks for it when the key is used.",
			[]Choice{{"no", "No passphrase"}, {"yes", "Set a passphrase (ssh-keygen asks for it)"}}, "no")
		if err != nil {
			return err
		}
		p.Passphrase = pass == "yes"
	default:
		p.SSH, p.SSHKey = SSHExisting, pick
	}
	if p.GHLogin {
		if p.Upload, err = w.Prompt.Confirm("Add the key to your GitHub account?", "", true); err != nil {
			return err
		}
	}
	p.Sign, err = w.Prompt.Confirm("Sign commits with this key?", "GitHub then shows your commits as Verified.", false)
	return err
}

func (w *Wizard) askDir(p *Plan) error {
	validate := func(s string) error {
		if s == "" {
			return nil
		}
		if !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "~") {
			return fmt.Errorf("use a full path, for example ~/Clients")
		}
		rule := config.Rule{Path: dirRulePath(s)}
		if err := rule.Validate(); err != nil {
			return err
		}
		for _, r := range w.Config.Rules {
			if r.Path == rule.Path {
				return fmt.Errorf("%s is already used by %q", r.Path, r.Identity)
			}
		}
		if info, err := os.Stat(config.ExpandHome(s)); err == nil && !info.IsDir() {
			return fmt.Errorf("%s is a file, not a directory", s)
		}
		return nil
	}
	dir, err := w.Prompt.Input("Directory", "Every repo under this directory uses "+p.Name+". Leave empty to skip.", "", validate)
	if err != nil || dir == "" {
		return err
	}
	p.BindDir = dirRulePath(dir)
	if !fileExists(config.ExpandHome(p.BindDir)) {
		create, err := w.Prompt.Confirm(p.BindDir+" does not exist. Create it?", "", true)
		if err != nil {
			return err
		}
		if !create {
			p.BindDir = ""
			return nil
		}
		p.CreateDir = true
	}
	return nil
}

func (w *Wizard) colorOptions() []Choice {
	out := make([]Choice, len(colorChoices))
	for i, c := range colorChoices {
		out[i] = Choice{c, c}
	}
	return out
}

func (w *Wizard) unusedColor() string {
	used := map[string]bool{}
	for _, id := range w.Config.Identities {
		color := id.Color
		if color == "" {
			color = "cyan"
		}
		used[color] = true
	}
	for _, c := range colorChoices {
		if !used[c] {
			return c
		}
	}
	return colorChoices[0]
}

func (p Plan) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s <%s>\n", p.Name, p.AuthorName, p.Email)
	if p.GHLogin {
		fmt.Fprintf(&b, "gh     log in, kept in its own folder\n")
	} else {
		fmt.Fprintf(&b, "gh     not set up\n")
	}
	switch p.SSH {
	case SSHGenerate:
		pass := "no passphrase"
		if p.Passphrase {
			pass = "with passphrase"
		}
		fmt.Fprintf(&b, "ssh    new key %s (%s)\n", p.SSHKey, pass)
	case SSHExisting:
		fmt.Fprintf(&b, "ssh    %s\n", p.SSHKey)
	default:
		fmt.Fprintf(&b, "ssh    default key\n")
	}
	if p.Upload {
		fmt.Fprintf(&b, "       added to GitHub\n")
	}
	if p.Sign {
		fmt.Fprintf(&b, "       signs commits\n")
	}
	if p.BindDir != "" {
		created := ""
		if p.CreateDir {
			created = " (created)"
		}
		fmt.Fprintf(&b, "dir    %s%s\n", p.BindDir, created)
	} else {
		fmt.Fprintf(&b, "dir    none (bind later: git-idm bind %s <dir>)\n", p.Name)
	}
	fmt.Fprintf(&b, "color  %s", p.Color)
	return b.String()
}

func dirRulePath(s string) string {
	abs := filepath.Clean(config.ExpandHome(s))
	if abs == "/" {
		return "/"
	}
	return config.ContractHome(abs) + "/"
}

func existingKeys() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	pubs, _ := filepath.Glob(filepath.Join(home, ".ssh", "*.pub"))
	var keys []string
	for _, pub := range pubs {
		private := strings.TrimSuffix(pub, ".pub")
		if fileExists(private) {
			keys = append(keys, config.ContractHome(private))
		}
	}
	sort.Strings(keys)
	return keys
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("enter the %s", what)
		}
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("the %s contains a control character", what)
			}
		}
		return nil
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
