package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/guard"
	"github.com/stevetosak/git-idm/internal/resolve"
)

var ansi = map[string]string{
	"red": "31", "green": "32", "yellow": "33", "blue": "34", "magenta": "35", "cyan": "36", "white": "37",
}

func runStatus(cmd *cobra.Command, args []string) error {
	c, res, err := current()
	if err != nil {
		return err
	}
	printStatus(cmd.OutOrStdout(), c, res, colorEnabled(cmd.OutOrStdout()))
	return nil
}

func newPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Print a short label for custom prompts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, res, err := current()
			if err != nil {
				return nil
			}
			label := res.Identity
			if res.Source == resolve.SourceOverride {
				label += " (override)"
			}
			fmt.Fprintln(cmd.OutOrStdout(), label)
			return nil
		},
	}
}

func printStatus(out io.Writer, c *config.Config, res resolve.Result, color bool) {
	paint := func(code, s string) string {
		if !color || code == "" {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	if res.Identity == "" {
		fmt.Fprintln(out, "no identity: no rule matches and no default is set")
		return
	}
	id := c.Identities[res.Identity]
	dot := ansi[id.Color]
	if dot == "" {
		dot = ansi["cyan"]
	}
	if res.Source == resolve.SourceOverride {
		dot = "1;31"
	}
	fmt.Fprintf(out, "%s %s  %s <%s>\n", paint(dot, "●"), paint("1", res.Identity), id.Name, id.Email)
	row := func(label, value string) { fmt.Fprintf(out, "  %-5s %s\n", label, value) }
	row("why", reason(res))
	if res.RepoRoot != "" {
		row("repo", config.ContractHome(res.RepoRoot))
	}
	if id.SSHKey != "" {
		row("ssh", id.SSHKey)
	}
	if id.GHConfigDir != "" {
		gh := id.GHUser
		if gh == "" {
			gh = "not logged in"
		}
		row("gh", fmt.Sprintf("%s (%s)", gh, id.GHConfigDir))
	}
	for _, note := range notes(c, res) {
		fmt.Fprintf(out, "  %s %s\n", paint("33", "note"), note)
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
			out = append(out, "this repo's .git/config sets the author to "+user+" (accepted with git-idm allow)")
		} else {
			out = append(out, "this repo's .git/config sets the author to "+user+", commits here use it")
		}
	}
	want := config.ExpandHome(c.Identities[res.Identity].GHConfigDir)
	if got := os.Getenv("GH_CONFIG_DIR"); want != "" && got != want {
		if got == "" {
			got = "(not set)"
		}
		out = append(out, fmt.Sprintf("gh in this shell uses GH_CONFIG_DIR=%s, expected %s; %s",
			config.ContractHome(got), config.ContractHome(want), hookHint))
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
