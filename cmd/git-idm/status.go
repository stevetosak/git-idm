package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/guard"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/resolve"
	"github.com/stevetosak/git-idm/internal/shell"
	"github.com/stevetosak/git-idm/internal/ui"
)

func runStatus(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	t := ui.New(out)
	c, err := config.Load()
	if errors.Is(err, config.ErrNotFound) || (err == nil && len(c.Identities) == 0) {
		fmt.Fprintln(out, welcome(t, importable()))
		return nil
	}
	c, res, err := current()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, statusView(t, c, res))
	return nil
}

func welcome(t *ui.Theme, found []string) string {
	if len(found) == 0 {
		return t.Bold("No identities yet.") + "\n\n" + ui.Rows(t, [][2]string{
			{"git-idm add", "create an identity (asks a few questions)"},
			{"git-idm help", "all commands"},
		})
	}
	return t.Bold("No identities yet.") + " Your git setup already has " + strings.Join(found, ", ") + ".\n\n" + ui.Rows(t, [][2]string{
		{"git-idm init", "import them (recommended)"},
		{"git-idm add", "create a new identity instead"},
		{"git-idm help", "all commands"},
	})
}

func importable() []string {
	res, err := importer.Import(importer.Options{})
	if err != nil {
		return nil
	}
	var emails []string
	for _, name := range res.Config.Names() {
		emails = append(emails, res.Config.Identities[name].Email)
	}
	return emails
}

func statusView(t *ui.Theme, c *config.Config, res resolve.Result) string {
	var b strings.Builder
	if res.Identity == "" {
		body := t.Bold("○ no identity here") + "\n\n" + ui.Rows(t, [][2]string{
			{"why", "no rule matches and there is no default"},
			{"fix", "git-idm bind <identity> .  or  git-idm edit <identity> --default"},
		})
		if res.RepoRoot != "" {
			body += "\n" + ui.Rows(t, [][2]string{{"repo", config.ContractHome(res.RepoRoot)}})
		}
		b.WriteString(t.Card(body, "white") + "\n" + t.Footer())
		return b.String()
	}
	id := c.Identities[res.Identity]
	border := id.Color
	header := t.Colored("●", id.Color) + " " + t.Identity(res.Identity, id.Color)
	rows := [][2]string{{"why", whyText(t, res)}}
	switch {
	case res.Source == resolve.SourceOverride:
		border = "red"
		header = t.Alert("● "+res.Identity) + "  " + t.Alert("override")
	case res.Temporary():
		border = "yellow"
		header = t.Identity("● "+res.Identity, "yellow") + "  " + t.Identity("temporary", "yellow")
		if res.Rule.Was != "" {
			rows = append(rows, [2]string{"was", res.Rule.Was})
		}
		rows = append(rows, [2]string{"end", "git-idm unbind " + res.Rule.Path})
	}
	if res.RepoRoot != "" {
		rows = append(rows, [2]string{"repo", config.ContractHome(res.RepoRoot)})
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
			gh = t.Warn("not logged in")
		}
		rows = append(rows, [2]string{"gh", gh})
	}
	body := header + "\n" + "  " + id.Name + " <" + id.Email + ">\n\n" + indent(ui.Rows(t, rows), "  ")
	b.WriteString(t.Card(body, border))
	for _, note := range notes(c, res) {
		b.WriteString("\n  " + t.Warn("!") + " " + note)
	}
	b.WriteString("\n" + t.Footer())
	return b.String()
}

func whyText(t *ui.Theme, res resolve.Result) string {
	switch res.Source {
	case resolve.SourceOverride:
		s := "override in this shell"
		if res.RuleIdentity != "" {
			s += ", the rule says " + res.RuleIdentity
		}
		return s + "\n" + t.Dim("clear: git-idm use --clear")
	case resolve.SourceRule:
		if res.Temporary() {
			return "temporary rule " + res.Rule.Path
		}
		return "rule " + res.Rule.Path
	case resolve.SourceDefault:
		return "★ default (no rule matches here)"
	}
	return "none"
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

func newPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Print the identity name for custom prompts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, res, err := current()
			if err != nil {
				return nil
			}
			label := res.Identity
			switch {
			case res.Source == resolve.SourceOverride:
				label += " (override)"
			case res.Temporary():
				label += " (temp)"
			}
			fmt.Fprintln(cmd.OutOrStdout(), label)
			return nil
		},
	}
}

func reason(res resolve.Result) string {
	switch res.Source {
	case resolve.SourceOverride:
		s := "override in this shell"
		if res.RuleIdentity != "" {
			s += fmt.Sprintf(" (rule %s says: %s)", res.Rule.Path, res.RuleIdentity)
		}
		return s + ", clear with: git-idm use --clear"
	case resolve.SourceRule:
		if res.Temporary() {
			return "temporary rule " + res.Rule.Path
		}
		return "rule " + res.Rule.Path
	case resolve.SourceDefault:
		return "default (no rule matches)"
	}
	return "none"
}

func notes(c *config.Config, res resolve.Result) []string {
	var out []string
	if res.LocalEmail != "" && res.Source != resolve.SourceOverride && !strings.EqualFold(res.LocalEmail, c.Identities[res.Identity].Email) {
		user := res.LocalEmail
		if res.LocalUser != "" {
			user = res.LocalUser + " <" + res.LocalEmail + ">"
		}
		if strings.EqualFold(guard.Allowed(res.RepoRoot), res.LocalEmail) {
			out = append(out, "commits here use "+user+" from .git/config (accepted with git-idm allow)")
		} else {
			out = append(out, "commits here use "+user+" from .git/config, not "+res.Identity)
		}
	}
	want := config.ExpandHome(c.Identities[res.Identity].GHConfigDir)
	if got := os.Getenv("GH_CONFIG_DIR"); want != "" && got != want {
		if got == "" {
			got = "(not set)"
		}
		fix := "open a new shell"
		if os.Getenv(shell.SegmentVar) == "" {
			fix = "the shell hook is not active: " + hookHint
		}
		out = append(out, fmt.Sprintf("gh uses %s here, expected %s; %s", config.ContractHome(got), config.ContractHome(want), fix))
	}
	return out
}

func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && !strings.EqualFold(os.Getenv("TERM"), "dumb")
}
