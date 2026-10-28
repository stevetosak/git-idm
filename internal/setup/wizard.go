package setup

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
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

type Account struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type Plan struct {
	Name        string
	AuthorName  string
	Email       string
	Color       string
	GHUser      string
	GHConfigDir string
	SSH         SSHMode
	SSHKey      string
	Passphrase  bool
	Upload      bool
	BindDir     string
	CreateDir   bool
	MakeDefault bool
}

type Wizard struct {
	Prompt      Prompter
	Config      *config.Config
	DefaultName string
	DefaultUser string
	Login       func(name string) (Account, string, error)
	Logout      func(dir, user string)
}

var nonNameChars = regexp.MustCompile(`[^a-z0-9_-]+`)

var colorChoices = []string{"cyan", "green", "magenta", "yellow", "blue", "red", "white"}

func (w *Wizard) Run() (Plan, error) {
	p, err := w.run()
	if err != nil && p.GHUser != "" && w.Logout != nil {
		w.Logout(p.GHConfigDir, p.GHUser)
	}
	return p, err
}

func (w *Wizard) run() (Plan, error) {
	var p Plan
	var err error
	if p.AuthorName, err = w.Prompt.Input("Author name", "The name on your commits.", w.DefaultUser, required("author name")); err != nil {
		return p, err
	}
	if p.Email, err = w.Prompt.Input("Email", "The email on your commits. GitHub uses it to link commits to your account.", "", w.validateEmail); err != nil {
		return p, err
	}
	if p.Name, err = w.askName(p.Email); err != nil {
		return p, err
	}
	if err := w.askDir(&p); err != nil {
		return p, err
	}
	if err := w.askGitHub(&p); err != nil {
		return p, err
	}
	if err := w.askSSH(&p); err != nil {
		return p, err
	}
	if p.Color, err = w.Prompt.Select("Prompt color", "The color of this identity in your shell prompt.", w.colorOptions(), w.unusedColor()); err != nil {
		return p, err
	}
	if err := w.Prompt.Note("Summary", p.Summary()); err != nil {
		return p, err
	}
	ok, err := w.Prompt.Confirm("Create this identity?", "Nothing else is changed before you confirm.", true)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrAborted
	}
	return p, nil
}

func (w *Wizard) askGitHub(p *Plan) error {
	if w.Login == nil {
		return nil
	}
	description := "Your browser opens now. git-idm keeps this login separate from your other gh accounts."
	for {
		pick, err := w.Prompt.Select("GitHub account", description,
			[]Choice{{"login", "Log in now (opens the browser)"}, {"skip", "Skip"}}, "login")
		if err != nil || pick == "skip" {
			return err
		}
		account, dir, err := w.Login(p.Name)
		if err == nil {
			p.GHUser, p.GHConfigDir = account.Login, dir
			return nil
		}
		description = "Login failed: " + err.Error() + "\nTry again, or skip and log in later."
	}
}

func (w *Wizard) askName(email string) (string, error) {
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
	suggestion := w.DefaultName
	if suggestion == "" {
		suggestion = SuggestName(email)
	}
	if validate(suggestion) != nil {
		suggestion = ""
	}
	return w.Prompt.Input("Identity name", "A short name, used in the prompt and in commands. Example: client", suggestion, validate)
}

var personalMailDomains = map[string]bool{
	"gmail": true, "googlemail": true, "outlook": true, "hotmail": true, "live": true, "yahoo": true,
	"icloud": true, "me": true, "mac": true, "proton": true, "protonmail": true, "pm": true,
	"gmx": true, "aol": true, "fastmail": true, "hey": true, "zoho": true, "yandex": true, "noreply": true,
}

func SuggestName(email string) string {
	_, domain, ok := strings.Cut(strings.ToLower(email), "@")
	if !ok {
		return ""
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return ""
	}
	name := labels[len(labels)-2]
	if len(labels) >= 3 && len(name) <= 3 && !personalMailDomains[name] {
		name = labels[len(labels)-3]
	}
	if strings.HasSuffix(domain, "users.noreply.github.com") || personalMailDomains[name] {
		return "personal"
	}
	name = strings.Trim(nonNameChars.ReplaceAllString(name, "-"), "-")
	if config.ValidateIdentityName(name) != nil {
		return ""
	}
	return name
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
	if p.GHUser != "" {
		if p.Upload, err = w.Prompt.Confirm("Add the key to "+p.GHUser+" on GitHub?", "", true); err != nil {
			return err
		}
	}
	return nil
}

func (w *Wizard) askDir(p *Plan) error {
	home, err := os.UserHomeDir()
	if err != nil {
		_, err := w.typeDir(p)
		return err
	}
	cur := home
	title := "Directory for " + p.Name
	problem := ""
	for {
		description := "Every repo under the folder uses " + p.Name + ". Enter opens a folder, / searches.\nNow in: " + config.ContractHome(cur)
		if note := w.insideRule(cur); note != "" {
			description += "\n" + note
		}
		if problem != "" {
			description += "\n✗ " + problem
			problem = ""
		}
		choices, first := w.dirChoices(cur, cur == home)
		pick, err := w.Prompt.Select(title, description, choices, first)
		if err != nil {
			return err
		}
		switch {
		case pick == "default":
			p.MakeDefault = true
			return nil
		case pick == "use":
			if err := w.validateDir(cur); err != nil {
				problem = err.Error()
				continue
			}
			p.BindDir = dirRulePath(cur)
			return nil
		case pick == "new":
			name, err := w.Prompt.Input("New folder in "+config.ContractHome(cur), "It is created after you confirm the summary. Leave empty to go back.", "", func(s string) error {
				if s == "" {
					return nil
				}
				if s == "." || s == ".." || strings.ContainsAny(s, "/\\") {
					return fmt.Errorf("enter a folder name without /")
				}
				target := filepath.Join(cur, s)
				if fileExists(target) {
					return fmt.Errorf("%s already exists", config.ContractHome(target))
				}
				return w.validateDir(target)
			})
			if err != nil {
				return err
			}
			if name == "" {
				continue
			}
			p.BindDir, p.CreateDir = dirRulePath(filepath.Join(cur, name)), true
			return nil
		case pick == "type":
			done, err := w.typeDir(p)
			if err != nil || done {
				return err
			}
		case pick == "skip":
			return nil
		case pick == "up":
			cur = filepath.Dir(cur)
		default:
			cur = filepath.Join(cur, strings.TrimPrefix(pick, "dir:"))
		}
	}
}

func (w *Wizard) dirChoices(cur string, atHome bool) ([]Choice, string) {
	choices := []Choice{{"skip", "⨯ Skip (use it nowhere for now)"}}
	first := "use"
	if atHome {
		label := "★ Use as the default (every folder without an identity)"
		if w.Config.Default != "" {
			label += ", replaces " + w.Config.Default
		}
		choices = append(choices, Choice{"default", label})
		first = "skip"
	} else {
		choices = append(choices, Choice{"use", "✓ Use " + config.ContractHome(cur)})
	}
	choices = append(choices,
		Choice{"new", "+ New folder here…"},
		Choice{"type", "✎ Type a path…"},
	)
	if filepath.Dir(cur) != cur {
		choices = append(choices, Choice{"up", "↑ .."})
	}
	for _, e := range subdirs(cur) {
		if first == "use" {
			first = "dir:" + e
		}
		choices = append(choices, Choice{"dir:" + e, fmt.Sprintf("%-28s %s", e+"/", w.describeDir(filepath.Join(cur, e)))})
	}
	return choices, first
}

func (w *Wizard) describeDir(dir string) string {
	rule := dirRulePath(dir)
	for _, r := range w.Config.Rules {
		if r.Path == rule {
			return "bound to " + r.Identity
		}
	}
	if fileExists(filepath.Join(dir, ".git")) {
		return "git repo"
	}
	repos := 0
	entries, _ := os.ReadDir(dir)
	if len(entries) > 500 {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && fileExists(filepath.Join(dir, e.Name(), ".git")) {
			repos++
		}
	}
	switch repos {
	case 0:
		return ""
	case 1:
		return "1 repo"
	}
	return fmt.Sprintf("%d repos", repos)
}

func (w *Wizard) insideRule(dir string) string {
	best := config.Rule{}
	path := dirRulePath(dir)
	for _, r := range w.Config.Rules {
		if r.Path == path {
			return fmt.Sprintf("This folder is bound to %s. Pick a folder inside it, or another folder.", r.Identity)
		}
		if r.Path != path && strings.HasPrefix(path, r.Path) && len(r.Path) > len(best.Path) {
			best = r
		}
	}
	if best.Path == "" {
		return ""
	}
	return fmt.Sprintf("Inside %s (%s). A rule here wins for repos below it.", best.Path, best.Identity)
}

func (w *Wizard) validateDir(dir string) error {
	rule := config.Rule{Path: dirRulePath(dir)}
	if err := rule.Validate(); err != nil {
		return err
	}
	for _, r := range w.Config.Rules {
		if r.Path == rule.Path {
			return fmt.Errorf("%s is already bound to %q", r.Path, r.Identity)
		}
	}
	return nil
}

func (w *Wizard) typeDir(p *Plan) (bool, error) {
	validate := func(s string) error {
		if s == "" {
			return nil
		}
		if !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "~") {
			return fmt.Errorf("use a full path, for example ~/Clients")
		}
		if info, err := os.Stat(config.ExpandHome(s)); err == nil && !info.IsDir() {
			return fmt.Errorf("%s is a file, not a directory", s)
		}
		if dirRulePath(s) == "~/" {
			return fmt.Errorf("your home folder means the default; go back and choose ★ Use as the default")
		}
		return w.validateDir(config.ExpandHome(s))
	}
	dir, err := w.Prompt.PathInput(PathQuestion{
		Title:       "Directory path",
		Description: "Every repo under it uses " + p.Name + ". Leave empty to go back.",
		Validate:    validate,
		Complete:    CompleteDir,
		Status:      w.pathStatus,
		Describe:    func(s string) string { return w.describeDir(config.ExpandHome(s)) },
	})
	if err != nil || dir == "" {
		return false, err
	}
	p.BindDir = dirRulePath(dir)
	p.CreateDir = !fileExists(config.ExpandHome(p.BindDir))
	return true, nil
}

func (w *Wizard) pathStatus(s string) string {
	if s == "" || (!strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "~")) {
		return ""
	}
	path := config.ExpandHome(s)
	if dirRulePath(s) == "~/" {
		return ""
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && !info.IsDir():
		return "✗ this is a file"
	case err == nil:
		if verr := w.validateDir(path); verr != nil {
			return "✗ " + verr.Error()
		}
		if d := w.describeDir(path); d != "" {
			return "✓ exists · " + d
		}
		return "✓ exists"
	}
	if !strings.HasSuffix(s, "/") && len(CompleteDir(s)) > 0 {
		return ""
	}
	return "+ new folder, created after you confirm"
}

func CompleteDir(input string) []string {
	if input == "" {
		input = "~/"
	}
	if !strings.HasPrefix(input, "/") && !strings.HasPrefix(input, "~") {
		return nil
	}
	if input == "~" {
		input = "~/"
	}
	parent, prefix := input, ""
	if !strings.HasSuffix(input, "/") {
		i := strings.LastIndex(input, "/")
		parent, prefix = input[:i+1], input[i+1:]
	}
	entries, err := os.ReadDir(config.ExpandHome(parent))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		if e.IsDir() || (e.Type()&os.ModeSymlink != 0 && isDir(filepath.Join(config.ExpandHome(parent), name))) {
			out = append(out, parent+name+"/")
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "Library" {
			continue
		}
		if e.IsDir() || (e.Type()&os.ModeSymlink != 0 && isDir(filepath.Join(dir, name))) {
			out = append(out, name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
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
	if p.GHUser != "" {
		fmt.Fprintf(&b, "gh     logged in as %s\n", p.GHUser)
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
	switch {
	case p.MakeDefault:
		fmt.Fprintf(&b, "dir    default for every folder without an identity\n")
	case p.BindDir != "":
		created := ""
		if p.CreateDir {
			created = " (created)"
		}
		fmt.Fprintf(&b, "dir    %s%s\n", p.BindDir, created)
	default:
		fmt.Fprintf(&b, "dir    none, used nowhere (later: git-idm bind %s <dir>)\n", p.Name)
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
