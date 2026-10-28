package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

func newRmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rm <identity>",
		Short: "Remove an identity and clean up its keys and gh login",
		Long: "In a terminal, rm asks what to delete. With --yes it deletes what git-idm created\n" +
			"(git-idm_* keys, their GitHub entry, and logins under ~/.config/git-idm/gh). Without a terminal\n" +
			"and without --yes, rm changes only the config.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			dir, err := config.Dir()
			if err != nil {
				return err
			}
			ex, err := newExecutor(cmd)
			if err != nil {
				return err
			}
			name := args[0]
			rem := setup.Remover{Config: c, ManagedGHRoot: filepath.Join(dir, "gh")}
			if ex.Tools.GH != "" {
				rem.FindKey = ex.FindGitHubKey
			}
			out := cmd.OutOrStdout()
			var plan setup.RemovePlan
			prompter, promptErr := newPrompter(cmd)
			switch {
			case yes:
				plan, err = rem.Recommended(name)
			case promptErr == nil:
				rem.Prompt = prompter
				plan, err = rem.Ask(name)
			default:
				plan, err = rem.ConfigOnly(name)
			}
			if errors.Is(err, setup.ErrAborted) {
				fmt.Fprintln(out, "nothing was removed")
				return nil
			}
			if err != nil {
				return err
			}
			wasDefault := c.Default == name
			failed := ex.Remove(c, plan)
			removeFromConfig(c, plan)
			if err := c.Save(); err != nil {
				return err
			}
			if err := applyConfig(io.Discard, c); err != nil {
				return err
			}
			fmt.Fprintf(out, "  ✓ removed %s from the config\n", name)
			if plan.NewDefault != "" {
				fmt.Fprintf(out, "  ✓ %s is now the default\n", plan.NewDefault)
			} else if c.Default == "" && wasDefault {
				fmt.Fprintln(out, "  i no default identity now; set one with: git-idm edit <identity> --default")
			}
			if len(plan.Kept) > 0 {
				fmt.Fprintln(out, "Kept:")
				for _, k := range plan.Kept {
					fmt.Fprintf(out, "  %s\n", k)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("%d cleanup step(s) failed", len(failed))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "delete what git-idm created, without questions")
	return cmd
}

func removeFromConfig(c *config.Config, p setup.RemovePlan) {
	delete(c.Identities, p.Name)
	if c.Default == p.Name {
		c.Default = p.NewDefault
	}
	c.RemoveRulesFor(p.Name)
}
