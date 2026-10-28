package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/importer"
)

func newInitCmd() *cobra.Command {
	var write, force bool
	var defaultName string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Import your current git, SSH and gh setup",
		Long:  "Read your current setup and show the identities git-idm would create.\nNothing is written unless --write is given. --write only writes the git-idm config file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := importer.Import(importer.Options{DefaultName: defaultName})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if err := printImport(out, res); err != nil {
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
	return cmd
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
	if len(res.Legacy) > 0 {
		fmt.Fprintln(out, "\nTo replace later (when git-idm generates its own config):")
		for _, l := range res.Legacy {
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
