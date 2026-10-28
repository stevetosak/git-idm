package setup

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(m dirModel, keys ...tea.KeyMsg) dirModel {
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(dirModel)
	}
	return m
}

func typed(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var (
	tab       = tea.KeyMsg{Type: tea.KeyTab}
	up        = tea.KeyMsg{Type: tea.KeyUp}
	down      = tea.KeyMsg{Type: tea.KeyDown}
	enter     = tea.KeyMsg{Type: tea.KeyEnter}
	esc       = tea.KeyMsg{Type: tea.KeyEsc}
	backspace = tea.KeyMsg{Type: tea.KeyBackspace}
	ctrlC     = tea.KeyMsg{Type: tea.KeyCtrlC}
)

func picker(t *testing.T) dirModel {
	_, w := dirFixture(t)
	return newDirModel(DirQuestion{Title: "Directory for q", Start: "~/", Entries: w.dirEntries, Status: w.dirStatus})
}

func selected(m dirModel) DirEntry { return m.entries[m.sel] }

func TestDirPickerStartsOnSkip(t *testing.T) {
	m := picker(t)
	if m.input.Value() != "~/" || selected(m).Kind != DirSkip {
		t.Fatalf("value=%q selected=%+v", m.input.Value(), selected(m))
	}
	if m = press(m, enter); !m.done || m.answer.Kind != DirSkip {
		t.Fatalf("enter at start must skip: %+v", m.answer)
	}
}

func TestDirPickerTypeCompleteAndUse(t *testing.T) {
	m := press(picker(t), typed("Pro"))
	if selected(m).Path != "~/Projects/" {
		t.Fatalf("typing must select the first matching folder, got %+v", selected(m))
	}
	view := m.View()
	if strings.Contains(view, "Work") || !strings.Contains(view, "~/Projects/") {
		t.Fatalf("list must be filtered:\n%s", view)
	}
	m = press(m, tab)
	if m.input.Value() != "~/Projects/" || selected(m).Path != "~/Projects/a/" {
		t.Fatalf("tab must complete the whole path: value=%q selected=%+v", m.input.Value(), selected(m))
	}
	m = press(m, typed("b"), enter)
	if m.input.Value() != "~/Projects/b/" {
		t.Fatalf("enter on a folder must open it: %q", m.input.Value())
	}
	if selected(m).Kind != DirUse && selected(m).Kind != DirSkip {
		t.Fatalf("a folder without subfolders must offer use, got %+v", m.entries)
	}
	m.sel = m.index(DirUse)
	m = press(m, enter)
	if !m.done || m.answer != (DirAnswer{Kind: DirUse, Path: "~/Projects/b/"}) {
		t.Fatalf("answer = %+v", m.answer)
	}
}

func TestDirPickerBackspaceGoesUp(t *testing.T) {
	m := press(picker(t), typed("Projects/a/"))
	m = press(m, backspace)
	if m.input.Value() != "~/Projects/" {
		t.Fatalf("backspace after / must go up one folder: %q", m.input.Value())
	}
	m = press(m, typed("x"), backspace)
	if m.input.Value() != "~/Projects/" {
		t.Fatalf("backspace inside a name must delete one character: %q", m.input.Value())
	}
}

func TestDirPickerCreateDefaultErrorsAndExit(t *testing.T) {
	m := press(picker(t), typed("Clients"))
	if selected(m).Kind != DirCreate {
		t.Fatalf("a new path must select create, got %+v", m.entries)
	}
	if m = press(m, enter); m.answer != (DirAnswer{Kind: DirCreate, Path: "~/Clients/"}) {
		t.Fatalf("answer = %+v", m.answer)
	}

	m = press(picker(t), up, up)
	for m.entries[m.sel].Kind != DirDefault {
		m = press(m, down)
	}
	if m = press(m, enter); m.answer.Kind != DirDefault {
		t.Fatalf("answer = %+v", m.answer)
	}

	m = press(picker(t), typed("Work/"))
	m.sel = m.index(DirUse)
	m = press(m, enter)
	if m.done || !strings.Contains(m.View(), "✗ ~/Work/ is already bound") {
		t.Fatalf("a bound folder must show an error:\n%s", m.View())
	}

	if m = press(picker(t), typed("Pro"), esc); !m.done || m.answer.Kind != DirSkip {
		t.Fatal("esc must skip")
	}
	if m = press(picker(t), ctrlC); !m.aborted {
		t.Fatal("ctrl+c must abort")
	}
}

func TestDirPickerEmptyFieldReturnsHome(t *testing.T) {
	m := press(picker(t), backspace)
	if m.input.Value() != "" {
		t.Fatalf("backspace on ~/ must empty the field: %q", m.input.Value())
	}
	if !strings.Contains(m.View(), "~/Projects/") {
		t.Fatalf("an empty field must still list the home folders:\n%s", m.View())
	}
	if m = press(m, backspace); m.input.Value() != "~/" {
		t.Fatalf("backspace on an empty field must return to ~/: %q", m.input.Value())
	}
}
