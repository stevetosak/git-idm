package setup

import (
	"fmt"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

type Reviewer struct {
	Prompt Prompter
}

func (r *Reviewer) Run(found *config.Config) (*config.Config, error) {
	names := found.Names()
	if err := r.Prompt.Note("Found your git setup", foundSummary(found)); err != nil {
		return nil, err
	}
	renamed := map[string]string{}
	taken := map[string]bool{}
	for _, name := range names {
		newName, err := r.review(found, name, taken)
		if err != nil {
			return nil, err
		}
		if newName != "" {
			renamed[name] = newName
			taken[newName] = true
		}
	}
	result := config.New()
	result.Guard = found.Guard
	for _, name := range names {
		if newName, ok := renamed[name]; ok {
			result.Identities[newName] = found.Identities[name]
		}
	}
	for _, rule := range found.Rules {
		if newName, ok := renamed[rule.Identity]; ok {
			rule.Identity = newName
			result.Rules = append(result.Rules, rule)
		}
	}
	if len(result.Identities) == 0 {
		return nil, ErrAborted
	}
	if err := r.askDefault(result, renamed[found.Default]); err != nil {
		return nil, err
	}
	if err := r.Prompt.Note("Summary", importSummary(result)); err != nil {
		return nil, err
	}
	ok, err := r.Prompt.Confirm("Save this config and apply it?", "git-idm writes its config and a marked block in ~/.gitconfig. Your old setup stays until you remove it.", true)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAborted
	}
	return result, nil
}

func (r *Reviewer) review(found *config.Config, name string, taken map[string]bool) (string, error) {
	pick, err := r.Prompt.Select("Identity "+name, identityDetails(found, name, "everywhere (global ~/.gitconfig)"),
		[]Choice{{"keep", "Keep as " + name}, {"rename", "Rename"}, {"skip", "Skip, do not import"}}, "keep")
	if err != nil || pick == "skip" {
		return "", err
	}
	if pick == "keep" && !taken[name] {
		return name, nil
	}
	return r.Prompt.Input("New name for "+name, "Use 1-32 characters: a-z, 0-9, - or _.", name, func(s string) error {
		if err := config.ValidateIdentityName(s); err != nil {
			return fmt.Errorf("use 1-32 characters: a-z, 0-9, - or _")
		}
		if taken[s] {
			return fmt.Errorf("another imported identity is already called %s", s)
		}
		return nil
	})
}

func (r *Reviewer) askDefault(c *config.Config, current string) error {
	choices := make([]Choice, 0, len(c.Identities)+1)
	for _, name := range c.Names() {
		choices = append(choices, Choice{name, name + " (" + c.Identities[name].Email + ")"})
	}
	choices = append(choices, Choice{"none", "No default"})
	if current == "" {
		current = "none"
	}
	pick, err := r.Prompt.Select("Default identity", "The default applies in every folder without a rule. Today your global ~/.gitconfig identity does this.", choices, current)
	if err != nil {
		return err
	}
	c.Default = ""
	if pick != "none" {
		c.Default = pick
	}
	return nil
}

func identityDetails(c *config.Config, name, defaultLabel string) string {
	id := c.Identities[name]
	rows := [][2]string{{"author", id.Name + " <" + id.Email + ">"}}
	var where []string
	if c.Default == name {
		where = append(where, defaultLabel)
	}
	for _, r := range c.RulesFor(name) {
		where = append(where, r.Path)
	}
	if len(where) > 0 {
		rows = append(rows, [2]string{"used in", strings.Join(where, ", ")})
	}
	if id.SSHKey != "" {
		rows = append(rows, [2]string{"ssh", id.SSHKey})
	}
	if id.SigningKey != "" {
		rows = append(rows, [2]string{"signs", id.SigningKey})
	}
	if id.GHConfigDir != "" {
		gh := id.GHUser
		if gh == "" {
			gh = "not logged in"
		}
		rows = append(rows, [2]string{"gh", gh + " (" + id.GHConfigDir + ")"})
	}
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%-8s %s", row[0], row[1])
	}
	return b.String()
}

func foundSummary(c *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "git-idm found %d identities. Review each one: keep it, rename it or skip it.\n", len(c.Identities))
	for _, name := range c.Names() {
		fmt.Fprintf(&b, "\n  %s  %s", name, c.Identities[name].Email)
	}
	return b.String()
}

func importSummary(c *config.Config) string {
	var b strings.Builder
	for i, name := range c.Names() {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(name + "\n" + indentLines(identityDetails(c, name, "★ default (folders without a rule)"), "  "))
	}
	if c.Default == "" {
		b.WriteString("\n\nNo default: folders without a rule use no identity.")
	}
	return b.String()
}

func indentLines(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
