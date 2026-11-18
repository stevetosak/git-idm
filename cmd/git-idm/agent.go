package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/agent"
	"github.com/stevetosak/git-idm/internal/config"
)

var skillPath = agent.DefaultPath

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent [install|remove]",
		Short: "Teach AI agents the git-idm rules",
		Long: "With no argument, print the rules for agents (add them to AGENTS.md or a prompt).\n" +
			"install: write a Claude Code skill to ~/.claude/skills/git-idm/SKILL.md. git-idm apply keeps it up to date.\n" +
			"remove: remove that skill.",
		Example:   "  git-idm agent install\n  git-idm agent >> AGENTS.md",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"install", "remove"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				fmt.Fprint(out, agent.Instructions())
				return nil
			}
			path := skillPath()
			if path == "" {
				return errors.New("cannot find the home folder")
			}
			shown := config.ContractHome(path)
			switch args[0] {
			case "install":
				changed, err := agent.Install(path)
				if err != nil {
					return err
				}
				if !changed {
					fmt.Fprintf(out, "%s is up to date\n", shown)
					return nil
				}
				fmt.Fprintf(out, "wrote %s\nnew Claude Code sessions load it\n", shown)
			case "remove":
				removed, err := agent.Remove(path)
				if err != nil {
					return err
				}
				if !removed {
					fmt.Fprintf(out, "no git-idm skill at %s\n", shown)
					return nil
				}
				fmt.Fprintf(out, "removed %s\n", shown)
			default:
				return usageError(cmd, "use install or remove")
			}
			return nil
		},
	}
	return cmd
}
