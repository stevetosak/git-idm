package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/migrate"
)

func newApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Write the git config again from config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			return applyConfig(cmd.OutOrStdout(), c)
		},
	}
}

func newBindCmd() *cobra.Command {
	var temporary bool
	cmd := &cobra.Command{
		Use:   "bind <identity> [dir]",
		Short: "Use an identity for every repo in a folder",
		Example: "  git-idm bind work ~/Projects\n" +
			"  git-idm bind client ~/Projects/acme --temp   end with: git-idm unbind ~/Projects/acme",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			rulePath, err := rulePathFor(args[1:])
			if err != nil {
				return err
			}
			rule, err := c.Bind(rulePath, args[0], temporary)
			if err != nil {
				return err
			}
			if err := c.Save(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case rule.Temporary && rule.Was != "":
				fmt.Fprintf(out, "%s uses %s for now, instead of %s. End with: git-idm unbind %s\n", rulePath, rule.Identity, rule.Was, rulePath)
			case rule.Temporary:
				fmt.Fprintf(out, "%s uses %s for now. End with: git-idm unbind %s\n", rulePath, rule.Identity, rulePath)
			default:
				fmt.Fprintf(out, "bound %s to %s\n", rulePath, rule.Identity)
			}
			return applyConfig(out, c)
		},
	}
	cmd.Flags().BoolVar(&temporary, "temp", false, "until you run git-idm unbind; then the folder uses its old identity again")
	return cmd
}

func newUnbindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unbind [dir]",
		Short: "Stop using an identity in a folder",
		Long:  "Stop using an identity in a folder. For a temporary binding, the folder uses its old identity again.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			rulePath, err := rulePathFor(args)
			if err != nil {
				return err
			}
			old, err := c.Unbind(rulePath)
			if err != nil {
				return err
			}
			if err := c.Save(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case old.Temporary && old.Was != "":
				fmt.Fprintf(out, "ended the temporary binding: %s uses %s again\n", rulePath, old.Was)
			case old.Temporary:
				fmt.Fprintf(out, "ended the temporary binding of %s to %s\n", rulePath, old.Identity)
			default:
				fmt.Fprintf(out, "unbound %s\n", rulePath)
			}
			return applyConfig(out, c)
		},
	}
}

func newMigrateCmd() *cobra.Command {
	var opts migrate.Options
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Remove the old setup that git-idm replaced",
		Long:  "Remove old includeIf entries, identity files and .envrc lines that git-idm rules now cover.\nEvery changed file is backed up first.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			a, err := gitgen.NewApplier()
			if err != nil {
				return err
			}
			res, err := importer.Import(importer.Options{DefaultName: c.Default})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			legacy := mergeLegacy(res.Legacy, importer.EnvrcLegacy(c.Rules))
			if len(legacy) == 0 {
				fmt.Fprintln(out, "nothing to migrate")
				return nil
			}
			steps, err := migrate.Run(c, a, legacy, opts)
			for _, s := range steps {
				if s.Done != "" {
					fmt.Fprintf(out, "  done  %-7s %s: %s\n", s.Legacy.Kind, s.Legacy.Path, s.Done)
				} else {
					fmt.Fprintf(out, "  kept  %-7s %s: %s\n", s.Legacy.Kind, s.Legacy.Path, s.Kept)
				}
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&opts.Envrc, "envrc", false, "also migrate GH_CONFIG_DIR out of .envrc files")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show what would change")
	return cmd
}

func mergeLegacy(lists ...[]importer.Legacy) []importer.Legacy {
	seen := map[string]bool{}
	var out []importer.Legacy
	for _, list := range lists {
		for _, l := range list {
			key := string(l.Kind) + "|" + l.Path + "|" + l.Condition
			if !seen[key] {
				seen[key] = true
				out = append(out, l)
			}
		}
	}
	return out
}

func applyConfig(out io.Writer, c *config.Config) error {
	a, err := gitgen.NewApplier()
	if err != nil {
		return err
	}
	changes, err := a.Apply(c)
	for _, ch := range changes {
		fmt.Fprintf(out, "  %-8s %s\n", ch.Action, config.ContractHome(ch.Path))
	}
	if err == nil && len(changes) == 0 {
		fmt.Fprintln(out, "  git config is up to date")
	}
	return err
}

func rulePathFor(args []string) (string, error) {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	abs, err := filepath.Abs(config.ExpandHome(dir))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	rule := config.Rule{Path: config.ContractHome(abs) + "/"}
	if abs == "/" {
		rule.Path = "/"
	}
	return rule.Path, rule.Validate()
}
