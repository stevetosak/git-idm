package ui

import (
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var colors = map[string]string{
	"red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
}

const Hint = "add · edit · rm · ls · help"

type Theme struct {
	r *lipgloss.Renderer
}

func New(w io.Writer) *Theme {
	return &Theme{r: lipgloss.NewRenderer(w)}
}

func color(name string) lipgloss.Color {
	if c, ok := colors[name]; ok {
		return lipgloss.Color(c)
	}
	return lipgloss.Color(colors["cyan"])
}

func (t *Theme) Identity(name, colorName string) string {
	return t.r.NewStyle().Bold(true).Foreground(color(colorName)).Render(name)
}

func (t *Theme) Colored(s, colorName string) string {
	return t.r.NewStyle().Foreground(color(colorName)).Render(s)
}

func (t *Theme) Bold(s string) string {
	return t.r.NewStyle().Bold(true).Render(s)
}

func (t *Theme) Dim(s string) string {
	return t.r.NewStyle().Faint(true).Render(s)
}

func (t *Theme) Warn(s string) string {
	return t.r.NewStyle().Foreground(color("yellow")).Render(s)
}

func (t *Theme) Alert(s string) string {
	return t.r.NewStyle().Bold(true).Foreground(color("red")).Render(s)
}

func (t *Theme) Card(body, borderColor string) string {
	return t.r.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color(borderColor)).
		Padding(0, 1).
		Render(body)
}

func (t *Theme) Footer() string {
	return "  " + t.Dim(Hint)
}

func Rows(t *Theme, rows [][2]string) string {
	width := 0
	for _, r := range rows {
		width = max(width, lipgloss.Width(r[0]))
	}
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(t.Dim(pad(r[0], width)) + "  " + r[1])
	}
	return b.String()
}

func Table(t *Theme, header []string, rows [][]string) string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	line := func(cells []string, style func(string) string) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = pad(c, widths[i])
			if style != nil {
				parts[i] = style(parts[i])
			}
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	var b strings.Builder
	b.WriteString(line(header, t.Dim))
	for _, row := range rows {
		b.WriteString("\n" + line(row, nil))
	}
	return b.String()
}

func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
