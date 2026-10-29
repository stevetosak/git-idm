package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/migrate"
	"github.com/stevetosak/git-idm/internal/setup"
	"github.com/stevetosak/git-idm/internal/shell"
)

func newInitCmd() *cobra.Command {
	var write, force bool
	var defaultName string
	var scan []string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Import your current git, SSH and gh setup",
		Long: "Read your current git, SSH and gh setup and turn it into git-idm identities.\n" +
			"init also reads the git repos in your home folder (and in --scan folders): repos that set their\n" +
			"own author become identities and folder rules.\n" +
			"In a terminal, init shows each identity it found and asks what to keep. Without a terminal,\n" +
			"init only prints the result; --write then saves the config file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := importOptions(defaultName, scan)
			if err != nil {
				return err
			}
			if !write && !force {
				if prompter, err := newPrompter(cmd); err == nil {
					return runInitReview(cmd, prompter, opts)
				}
			}
			res, err := importer.Import(opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if err := printImport(out, res); err != nil {
				return err
			}
			if err := bindUnbound(cmd, out, res.Config, importer.UnboundGroups(res.Config, res.Repos), true); err != nil {
				return err
			}
			path, err := config.Path()
			if err != nil {
				return err
			}
			if !write {
				fmt.Fprintf(out, "\nNothing written. Run `git-idm init --write` to save %s\n", config.ContractHome(path))
				return nil
			}
			if importer.Exists() && !force {
				return fmt.Errorf("%s already exists (use --force to replace it)", config.ContractHome(path))
			}
			if err := res.Config.Save(); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nWrote %s. Your git, SSH, gh and direnv files are not changed.\nNext: `git-idm apply`, check a commit, then `git-idm migrate`.\n", config.ContractHome(path))
			return nil
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "save the imported config")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing config")
	cmd.Flags().StringVar(&defaultName, "default-name", "work", "name for the identity from your global gitconfig")
	cmd.Flags().StringArrayVar(&scan, "scan", nil, "also read git repos in this folder (repeatable)")
	return cmd
}

func importOptions(defaultName string, scan []string) (importer.Options, error) {
	roots, exclude := importer.HomeScan()
	for _, dir := range scan {
		path := config.ExpandHome(strings.TrimSpace(dir))
		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return importer.Options{}, fmt.Errorf("--scan %q: %w", dir, err)
			}
			path = abs
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return importer.Options{}, fmt.Errorf("--scan %q: not a folder", dir)
		}
		roots = append(roots, filepath.Clean(path))
	}
	return importer.Options{DefaultName: defaultName, Scan: roots, Exclude: exclude}, nil
}

func printImport(out io.Writer, res *importer.Result) error {
	fmt.Fprintln(out, "Found identities:")
	for _, f := range res.Findings {
		fmt.Fprintf(out, "  %s\n", f)
	}
	if len(res.Skipped) > 0 {
		fmt.Fprintln(out, "\nSkipped:")
		for _, s := range res.Skipped {
			fmt.Fprintf(out, "  %s\n", s)
		}
	}
	legacy := append(append([]importer.Legacy{}, res.Legacy...), importer.LocalLegacy(res.Config, res.Repos)...)
	if len(legacy) > 0 {
		fmt.Fprintln(out, "\nTo replace later (when git-idm generates its own config):")
		for _, l := range legacy {
			fmt.Fprintf(out, "  %-7s %s: %s\n", l.Kind, l.Path, l.Detail)
		}
	}
	data, err := res.Config.Encode()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nProposed config.toml:\n%s", data)
	return nil
}

func runInitReview(cmd *cobra.Command, prompter setup.Prompter, opts importer.Options) error {
	out := cmd.OutOrStdout()
	path, err := config.Path()
	if err != nil {
		return err
	}
	if importer.Exists() {
		fmt.Fprintf(out, "git-idm is already set up (%s).\nAdd an identity with: git-idm add\n", config.ContractHome(path))
		return nil
	}
	res, err := importer.Import(opts)
	if err != nil {
		return err
	}
	if len(res.Config.Identities) == 0 {
		fmt.Fprintln(out, "No git identity found to import. Create one with: git-idm add")
		return nil
	}
	c, err := (&setup.Reviewer{Prompt: prompter}).Run(res.Config)
	if errors.Is(err, setup.ErrAborted) {
		fmt.Fprintln(out, "nothing was imported")
		return nil
	}
	if err != nil {
		return err
	}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Fprintf(out, "  ✓ saved %s\n", config.ContractHome(path))
	if err := applyConfig(io.Discard, c); err != nil {
		return err
	}
	fmt.Fprintln(out, "  ✓ git config applied")
	if len(res.Skipped) > 0 {
		fmt.Fprintln(out, "\nNot imported:")
		for _, s := range res.Skipped {
			fmt.Fprintf(out, "  %s\n", s)
		}
	}
	if err := offerMigrate(out, prompter, c, mergeLegacy(res.Legacy, importer.EnvrcLegacy(c.Rules), importer.LocalLegacy(c, res.Repos))); err != nil {
		return err
	}
	if err := bindUnbound(cmd, out, c, importer.UnboundGroups(c, res.Repos), false); err != nil {
		return err
	}
	fmt.Fprintln(out, "\nDone. Check any time with: git-idm")
	return nil
}

func offerMigrate(out io.Writer, prompter setup.Prompter, c *config.Config, legacy []importer.Legacy) error {
	covered := map[string]bool{}
	for _, r := range c.Rules {
		covered[r.Path] = true
	}
	var removable []importer.Legacy
	var lines []string
	envrc := false
	for _, l := range legacy {
		if !covered[l.RulePath] {
			continue
		}
		removable = append(removable, l)
		lines = append(lines, fmt.Sprintf("%s: %s", l.Path, l.Detail))
		envrc = envrc || l.Kind == importer.LegacyEnvrc
	}
	if len(removable) == 0 {
		return nil
	}
	later := "\nYour old setup is still in place. Remove it later with: git-idm migrate"
	if err := prompter.Note("Old setup", "git-idm now does the job of these entries:\n\n"+strings.Join(lines, "\n")); err != nil {
		return err
	}
	remove, err := prompter.Confirm("Remove the old setup now?", "git-idm makes a backup of each file first.", true)
	if err != nil || !remove {
		fmt.Fprintln(out, later)
		return ignoreAbort(err)
	}
	opts := migrate.Options{}
	if envrc {
		_, hook := os.LookupEnv(shell.SegmentVar)
		opts.Envrc, err = prompter.Confirm("Also remove GH_CONFIG_DIR from .envrc files?",
			"The git-idm shell hook sets GH_CONFIG_DIR instead. Say yes only when the hook is in your ~/.zshrc.", hook)
		if err != nil {
			fmt.Fprintln(out, later)
			return ignoreAbort(err)
		}
	}
	a, err := gitgen.NewApplier()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "\nRemoving the old setup")
	steps, err := migrate.Run(c, a, removable, opts)
	printSteps(out, steps)
	return err
}

func ignoreAbort(err error) error {
	if errors.Is(err, setup.ErrAborted) {
		return nil
	}
	return err
}
