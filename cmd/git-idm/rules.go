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
	return &cobra.Command{
		Use:   "bind <identity> [dir]",
		Short: "Use an identity for every repo in a folder",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := c.Identities[name]; !ok {
				return fmt.Errorf("identity %q does not exist", name)
			}
			rulePath, err := rulePathFor(args[1:])
			if err != nil {
				return err
			}
			replaced := false
			for i, r := range c.Rules {
				if r.Path == rulePath {
					c.Rules[i].Identity = name
					replaced = true
				}
			}
			if !replaced {
				c.Rules = append(c.Rules, config.Rule{Path: rulePath, Identity: name})
			}
			if err := c.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "bound %s to %s\n", rulePath, name)
			return applyConfig(cmd.OutOrStdout(), c)
		},
	}
}

func newUnbindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unbind [dir]",
		Short: "Stop using an identity in a folder",
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
			kept := c.Rules[:0]
			for _, r := range c.Rules {
				if r.Path != rulePath {
					kept = append(kept, r)
				}
			}
			if len(kept) == len(c.Rules) {
				return fmt.Errorf("no rule for %s", rulePath)
			}
			c.Rules = kept
			if err := c.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unbound %s\n", rulePath)
			return applyConfig(cmd.OutOrStdout(), c)
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
