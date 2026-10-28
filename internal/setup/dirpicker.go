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

type DirKind string

const (
	DirSkip    DirKind = "skip"
	DirDefault DirKind = "default"
	DirUse     DirKind = "use"
	DirCreate  DirKind = "create"
	DirFolder  DirKind = "dir"
)

type DirEntry struct {
	Kind  DirKind
	Path  string
	Label string
	Note  string
	Err   string
}

type DirAnswer struct {
	Kind DirKind
	Path string
}

type DirQuestion struct {
	Title       string
	Description string
	Start       string
	Entries     func(input string) []DirEntry
	Status      func(input string) string
}

const dirListSize = 10

type dirModel struct {
	q       DirQuestion
	input   textinput.Model
	entries []DirEntry
	sel     int
	offset  int
	err     string
	answer  DirAnswer
	done    bool
	aborted bool
	styles  *huh.FieldStyles
}

func newDirModel(q DirQuestion) dirModel {
	styles := huh.ThemeCharm().Focused
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = styles.TextInput.Prompt
	ti.TextStyle = styles.TextInput.Text
	ti.Cursor.Style = styles.TextInput.Cursor
	ti.SetValue(q.Start)
	ti.CursorEnd()
	ti.Focus()
	m := dirModel{q: q, input: ti, styles: &styles}
	m.entries = q.Entries(q.Start)
	m.sel = m.index(DirSkip)
	return m
}

func (m *dirModel) index(kind DirKind) int {
	for i, e := range m.entries {
		if e.Kind == kind {
			return i
		}
	}
	return 0
}

func (m *dirModel) setInput(v string) {
	m.input.SetValue(v)
	m.input.CursorEnd()
	m.refresh()
}

func (m *dirModel) refresh() {
	m.err = ""
	m.entries = m.q.Entries(m.input.Value())
	m.sel, m.offset = 0, 0
	for i, e := range m.entries {
		if e.Kind == DirFolder {
			m.sel = i
			break
		}
	}
	m.scroll()
}

func (m *dirModel) scroll() {
	if m.sel < m.offset {
		m.offset = m.sel
	}
	if m.sel >= m.offset+dirListSize {
		m.offset = m.sel - dirListSize + 1
	}
}

func (m dirModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m dirModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.answer, m.done = DirAnswer{Kind: DirSkip}, true
		return m, tea.Quit
	case "up", "ctrl+p":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n":
		m.move(1)
		return m, nil
	case "tab":
		target := -1
		if len(m.entries) > 0 && m.entries[m.sel].Kind == DirFolder {
			target = m.sel
		} else {
			for i, e := range m.entries {
				if e.Kind == DirFolder {
					target = i
					break
				}
			}
		}
		if target >= 0 {
			m.setInput(m.entries[target].Path)
		}
		return m, nil
	case "enter":
		if len(m.entries) == 0 {
			return m, nil
		}
		e := m.entries[m.sel]
		switch {
		case e.Kind == DirFolder:
			m.setInput(e.Path)
		case e.Err != "":
			m.err = e.Err
		default:
			m.answer, m.done = DirAnswer{Kind: e.Kind, Path: e.Path}, true
			return m, tea.Quit
		}
		return m, nil
	case "backspace":
		v := m.input.Value()
		if v == "" {
			m.setInput("~/")
			return m, nil
		}
		if strings.HasSuffix(v, "/") && len(v) > 1 && m.input.Position() == len(v) {
			trimmed := strings.TrimSuffix(v, "/")
			m.setInput(trimmed[:strings.LastIndex(trimmed, "/")+1])
			return m, nil
		}
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.refresh()
	}
	return m, cmd
}

func (m *dirModel) move(delta int) {
	if len(m.entries) == 0 {
		return
	}
	m.err = ""
	m.sel = (m.sel + delta + len(m.entries)) % len(m.entries)
	m.scroll()
}

func (m dirModel) View() string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Title.Render(m.q.Title) + "\n")
	b.WriteString(s.Description.Render(m.q.Description) + "\n")
	b.WriteString(m.input.View() + "\n")
	if m.done {
		return s.Base.Render(b.String())
	}
	if m.err != "" {
		b.WriteString(s.ErrorMessage.Render("  ✗ "+m.err) + "\n")
	} else if m.q.Status != nil {
		if line := m.q.Status(m.input.Value()); line != "" {
			b.WriteString(s.Description.Render("  "+line) + "\n")
		}
	}
	end := min(m.offset+dirListSize, len(m.entries))
	width := 0
	for _, e := range m.entries[m.offset:end] {
		width = max(width, len([]rune(e.Label)))
	}
	for i := m.offset; i < end; i++ {
		e := m.entries[i]
		label := e.Label
		if e.Note != "" {
			label = fmt.Sprintf("%-*s  %s", width, label, e.Note)
		}
		switch {
		case i == m.sel:
			b.WriteString(s.SelectSelector.Render("▸ ") + s.SelectedOption.Render(label) + "\n")
		case e.Err != "":
			b.WriteString("  " + s.Description.Render(label) + "\n")
		default:
			b.WriteString("  " + s.Option.Render(label) + "\n")
		}
	}
	if rest := len(m.entries) - end; rest > 0 {
		b.WriteString(s.Description.Render(fmt.Sprintf("  … %d more, keep typing to filter", rest)) + "\n")
	}
	b.WriteString(s.Description.Render("tab complete · ↑/↓ choose · enter select · backspace on / goes up · esc skip"))
	return s.Base.Render(b.String())
}

func runDirPicker(q DirQuestion, in io.Reader, out io.Writer) (DirAnswer, error) {
	opts := []tea.ProgramOption{tea.WithOutput(os.Stderr)}
	if out != nil {
		opts = []tea.ProgramOption{tea.WithOutput(out)}
	}
	if in != nil {
		opts = append(opts, tea.WithInput(in))
	}
	final, err := tea.NewProgram(newDirModel(q), opts...).Run()
	if err != nil {
		return DirAnswer{}, err
	}
	m := final.(dirModel)
	if m.aborted {
		return DirAnswer{}, ErrAborted
	}
	return m.answer, nil
}

func answerFromText(q DirQuestion, text string) (DirAnswer, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return DirAnswer{Kind: DirSkip}, nil
	}
	if text == "~" {
		text = "~/"
	}
	for _, e := range q.Entries(text) {
		if e.Kind == DirDefault || e.Kind == DirUse || e.Kind == DirCreate {
			if e.Err != "" {
				return DirAnswer{}, fmt.Errorf("%s", e.Err)
			}
			return DirAnswer{Kind: e.Kind, Path: e.Path}, nil
		}
	}
	if line := q.Status(text); line != "" {
		return DirAnswer{}, fmt.Errorf("%s", strings.TrimPrefix(line, "✗ "))
	}
	return DirAnswer{}, fmt.Errorf("enter a full folder path, for example ~/Clients")
}
