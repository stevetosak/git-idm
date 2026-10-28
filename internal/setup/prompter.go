package setup

import (
	"errors"
	"io"

	"github.com/charmbracelet/huh"
)

var ErrAborted = errors.New("setup cancelled, nothing was changed")

type Choice struct {
	Key   string
	Label string
}

type Prompter interface {
	Input(title, description, value string, validate func(string) error) (string, error)
	Select(title, description string, choices []Choice, value string) (string, error)
	Confirm(title, description string, value bool) (bool, error)
	Note(title, body string) error
}

type HuhPrompter struct {
	In         io.Reader
	Out        io.Writer
	Accessible bool
}

func (p HuhPrompter) run(field huh.Field) error {
	form := huh.NewForm(huh.NewGroup(field)).WithShowHelp(true)
	if p.Accessible {
		form = form.WithAccessible(true)
	}
	if p.In != nil {
		form = form.WithInput(p.In)
	}
	if p.Out != nil {
		form = form.WithOutput(p.Out)
	}
	err := form.Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

func (p HuhPrompter) Input(title, description, value string, validate func(string) error) (string, error) {
	if validate == nil {
		validate = func(string) error { return nil }
	}
	if p.Accessible && value != "" {
		inner, fallback := validate, value
		validate = func(s string) error {
			if s == "" {
				return inner(fallback)
			}
			return inner(s)
		}
	}
	err := p.run(huh.NewInput().Title(title).Description(description).Value(&value).Validate(validate))
	return value, err
}

func (p HuhPrompter) Select(title, description string, choices []Choice, value string) (string, error) {
	options := make([]huh.Option[string], len(choices))
	for i, c := range choices {
		options[i] = huh.NewOption(c.Label, c.Key)
	}
	if value == "" && len(choices) > 0 {
		value = choices[0].Key
	}
	field := huh.NewSelect[string]().Title(title).Description(description).Options(options...).Value(&value)
	if len(choices) > 12 {
		field = field.Height(16)
	}
	err := p.run(field)
	return value, err
}

func (p HuhPrompter) Confirm(title, description string, value bool) (bool, error) {
	err := p.run(huh.NewConfirm().Title(title).Description(description).Affirmative("Yes").Negative("No").Value(&value))
	return value, err
}

func (p HuhPrompter) Note(title, body string) error {
	return p.run(huh.NewNote().Title(title).Description(body).Next(true).NextLabel("Continue"))
}
