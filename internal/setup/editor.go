package setup

import (
	"fmt"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
)

type Editor struct {
	Prompt Prompter
	Config *config.Config
}

type Edit struct {
	Identity config.Identity
	Default  bool
	Changed  []string
}

func (e *Editor) Run(name string) (Edit, error) {
	original, ok := e.Config.Identities[name]
	if !ok {
		return Edit{}, fmt.Errorf("identity %q does not exist", name)
	}
	wasDefault := e.Config.Default == name
	edit := Edit{Identity: original, Default: wasDefault}
	last := "name"
	for {
		edit.Changed = changedFields(original, wasDefault, edit)
		pick, err := e.Prompt.Select("Edit "+name, editDescription(edit.Changed), e.menu(edit), last)
		if err != nil {
			return Edit{}, err
		}
		last = pick
		switch pick {
		case "done":
			return edit, nil
		case "cancel":
			return Edit{}, ErrAborted
		}
		if err := e.ask(name, pick, &edit); err != nil {
			return Edit{}, err
		}
	}
}

func (e *Editor) ask(name, field string, edit *Edit) error {
	id := &edit.Identity
	var err error
	switch field {
	case "name":
		id.Name, err = e.Prompt.Input("Author name", "The name on your commits.", id.Name, required("author name"))
	case "email":
		id.Email, err = e.Prompt.Input("Email", "The email on your commits. GitHub uses it to link commits to your account.", id.Email,
			func(s string) error { return validateEmail(e.Config, name, s) })
	case "color":
		id.Color, err = e.Prompt.Select("Prompt color", "The color of this identity in your shell prompt.", colorOptions(), colorOf(*id))
	case "default":
		edit.Default, err = e.Prompt.Confirm("Use "+name+" as the default?", "The default applies in every folder without a rule.", edit.Default)
	}
	return err
}

func (e *Editor) menu(edit Edit) []Choice {
	id := edit.Identity
	row := func(key, label, value string) Choice {
		return Choice{key, fmt.Sprintf("%-12s %s", label, value)}
	}
	return []Choice{
		row("name", "Author name", id.Name),
		row("email", "Email", id.Email),
		row("color", "Color", colorOf(id)),
		row("default", "Default", yesNo(edit.Default)),
		{"done", "Done"},
		{"cancel", "Cancel"},
	}
}

func changedFields(original config.Identity, wasDefault bool, edit Edit) []string {
	var changed []string
	id := edit.Identity
	if id.Name != original.Name {
		changed = append(changed, "author name")
	}
	if id.Email != original.Email {
		changed = append(changed, "email")
	}
	if colorOf(id) != colorOf(original) {
		changed = append(changed, "color")
	}
	if edit.Default != wasDefault {
		changed = append(changed, "default")
	}
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
