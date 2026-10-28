package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func send(m pathModel, keys ...tea.KeyMsg) pathModel {
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(pathModel)
	}
	return m
}

func typed(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var (
	tab   = tea.KeyMsg{Type: tea.KeyTab}
	down  = tea.KeyMsg{Type: tea.KeyDown}
	enter = tea.KeyMsg{Type: tea.KeyEnter}
	esc   = tea.KeyMsg{Type: tea.KeyEsc}
	ctrlC = tea.KeyMsg{Type: tea.KeyCtrlC}
)

func question() PathQuestion {
	return PathQuestion{
		Title: "Directory path", Description: "d",
		Validate: func(s string) error {
			if strings.Contains(s, "Bound") {
				return errors.New("already bound")
			}
			return nil
		},
		Complete: CompleteDir,
		Status:   func(s string) string { return "status:" + s },
		Describe: func(s string) string {
			if strings.HasSuffix(s, "Projects/") {
				return "2 repos"
			}
			return ""
		},
	}
}

func TestPathInputFiltersAndCompletes(t *testing.T) {
	home := sandbox(t)
	for _, d := range []string{"Projects/api", "Projects/web", "Personal", "Bound", "Music"} {
		os.MkdirAll(filepath.Join(home, d), 0o700)
	}
	m := newPathModel(question())
	if got := strings.Join(m.matches, ","); got != "~/Bound/,~/Music/,~/Personal/,~/Projects/" {
		t.Fatalf("empty input must list home folders, got %s", got)
	}

	m = send(m, typed("~/P"))
	if got := strings.Join(m.matches, ","); got != "~/Personal/,~/Projects/" {
		t.Fatalf("typing must filter, got %s", got)
	}
	view := m.View()
	if !strings.Contains(view, "~/Projects/  2 repos") || strings.Contains(view, "Music") || !strings.Contains(view, "status:~/P") {
		t.Fatalf("view:\n%s", view)
	}

	m = send(m, down, tab)
	if m.input.Value() != "~/Projects/" {
		t.Fatalf("down + tab must complete the chosen entry, value = %q", m.input.Value())
	}
	if got := strings.Join(m.matches, ","); got != "~/Projects/api/,~/Projects/web/" {
		t.Fatalf("after completion the list must show subfolders, got %s", got)
	}

	m = send(m, typed("w"), tab, enter)
	if !m.done || m.value != "~/Projects/web/" {
		t.Fatalf("done=%v value=%q", m.done, m.value)
	}
}

func TestPathInputValidationBackAndAbort(t *testing.T) {
	home := sandbox(t)
	os.MkdirAll(filepath.Join(home, "Bound"), 0o700)
	m := send(newPathModel(question()), typed("~/Bound"), enter)
	if m.done || !strings.Contains(m.View(), "✗ already bound") {
		t.Fatalf("an invalid path must stay open with the error:\n%s", m.View())
	}
	m = send(m, typed("x"))
	if m.err != "" {
		t.Fatal("typing must clear the error")
	}
	if m = send(m, esc); !m.done || m.value != "" {
		t.Fatalf("esc must go back: done=%v value=%q", m.done, m.value)
	}
	if m = send(newPathModel(question()), enter); !m.done || m.value != "" {
		t.Fatal("enter on an empty input must go back")
	}
	if m = send(newPathModel(question()), ctrlC); !m.aborted {
		t.Fatal("ctrl+c must abort")
	}
}
