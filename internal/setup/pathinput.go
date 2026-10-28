package setup

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type PathQuestion struct {
	Title       string
	Description string
	Validate    func(string) error
	Complete    func(string) []string
	Status      func(string) string
	Describe    func(string) string
}

const pathListSize = 8

type pathModel struct {
	q       PathQuestion
	input   textinput.Model
	matches []string
	sel     int
	offset  int
	err     string
	value   string
	done    bool
	aborted bool
	styles  *huh.FieldStyles
}

func newPathModel(q PathQuestion) pathModel {
	styles := huh.ThemeCharm().Focused
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = styles.TextInput.Prompt
	ti.TextStyle = styles.TextInput.Text
	ti.Cursor.Style = styles.TextInput.Cursor
	ti.Focus()
	m := pathModel{q: q, input: ti, styles: &styles}
	m.refresh()
	return m
}

func (m *pathModel) refresh() {
	m.matches = m.q.Complete(m.input.Value())
	m.sel, m.offset = 0, 0
}

func (m pathModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m pathModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	switch key.String() {
	case "ctrl+c":
		m.aborted = true
		return m, tea.Quit
	case "esc":
		m.value, m.done = "", true
		return m, tea.Quit
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value != "" {
			if err := m.q.Validate(value); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		m.value, m.done = value, true
		return m, tea.Quit
	case "tab":
		if len(m.matches) > 0 {
			m.input.SetValue(m.matches[m.sel])
			m.input.CursorEnd()
			m.err = ""
			m.refresh()
		}
		return m, nil
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.err = ""
		m.refresh()
	}
	return m, cmd
}

func (m *pathModel) move(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.sel = (m.sel + delta + len(m.matches)) % len(m.matches)
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+pathListSize {
		m.offset = m.sel - pathListSize + 1
	}
}

func (m pathModel) View() string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Title.Render(m.q.Title) + "\n")
	b.WriteString(s.Description.Render(m.q.Description) + "\n")
	b.WriteString(m.input.View() + "\n")
	if m.done {
		return s.Base.Render(b.String())
	}
	switch {
	case m.err != "":
		b.WriteString(s.ErrorMessage.Render("  ✗ "+m.err) + "\n")
	case m.q.Status != nil:
		if line := m.q.Status(m.input.Value()); line != "" {
			b.WriteString(s.Description.Render("  "+line) + "\n")
		}
	}
	end := min(m.offset+pathListSize, len(m.matches))
	width := 0
	for _, p := range m.matches[m.offset:end] {
		width = max(width, len(p))
	}
	for i := m.offset; i < end; i++ {
		label := m.matches[i]
		if m.q.Describe != nil {
			if d := m.q.Describe(label); d != "" {
				label = fmt.Sprintf("%-*s  %s", width, label, d)
			}
		}
		if i == m.sel {
			b.WriteString(s.SelectSelector.Render("▸ ") + s.SelectedOption.Render(label) + "\n")
		} else {
			b.WriteString("  " + s.Option.Render(label) + "\n")
		}
	}
	if rest := len(m.matches) - end; rest > 0 {
		b.WriteString(s.Description.Render(fmt.Sprintf("  … %d more, keep typing to filter", rest)) + "\n")
	}
	b.WriteString(s.Description.Render("tab complete · ↑/↓ choose · enter select · esc back"))
	return s.Base.Render(b.String())
}

func runPathInput(q PathQuestion, in io.Reader, out io.Writer) (string, error) {
	opts := []tea.ProgramOption{tea.WithOutput(os.Stderr)}
	if out != nil {
		opts = []tea.ProgramOption{tea.WithOutput(out)}
	}
	if in != nil {
		opts = append(opts, tea.WithInput(in))
	}
	final, err := tea.NewProgram(newPathModel(q), opts...).Run()
	if err != nil {
		return "", err
	}
	m := final.(pathModel)
	if m.aborted {
		return "", ErrAborted
	}
	return m.value, nil
}
