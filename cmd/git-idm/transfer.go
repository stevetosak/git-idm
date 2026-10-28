package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
	"github.com/stevetosak/git-idm/internal/transfer"
)

func newExportCmd() *cobra.Command {
	var all, force bool
	cmd := &cobra.Command{
		Use:   "export <identity> [file]",
		Short: "Write identities to a file for another machine",
		Long: "Write the author, email, color, protocol, GitHub username and signing type to a file.\n" +
			"SSH keys, gh logins, tokens and folders are not exported. Use - as the file to print to stdout.",
		Example: "  git-idm export work\n  git-idm export work ~/Desktop/work.git-idm.toml\n  git-idm export --all",
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) > 1 {
					return fmt.Errorf("with --all, give only the file, got: %v", args)
				}
				return nil
			}
			return cobra.RangeArgs(1, 2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			names, path := c.Names(), "git-idm-identities.toml"
			if !all {
				names, path = args[:1], args[0]+".git-idm.toml"
				args = args[1:]
			}
			if len(args) > 0 {
				path = args[0]
			}
			if len(names) == 0 {
				return fmt.Errorf("there are no identities to export, create one with: git-idm add")
			}
			f, err := transfer.FromConfig(c, names)
			if err != nil {
				return err
			}
			data, err := f.Encode()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if path == "-" {
				_, err := out.Write(data)
				return err
			}
			if err := writeExport(path, data, force); err != nil {
				return err
			}
			fmt.Fprintf(out, "✓ exported %s to %s\n", joinNames(names), path)
			fmt.Fprintln(out, "  not included: SSH keys, gh logins and folders")
			fmt.Fprintf(out, "  on the other machine: git-idm import %s\n", path)
			for _, e := range f.Identities {
				if e.Signing == transfer.SigningGPG && e.GPGKey == "" {
					fmt.Fprintf(out, "  %s signs with gpg, but its key is not a key ID, so import turns signing off\n", e.ID)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "export every identity to one file")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it exists")
	return cmd
}

func writeExport(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	fh, err := os.OpenFile(path, flags, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists, use --force to overwrite it", path)
	}
	if err != nil {
		return err
	}
	_, werr := fh.Write(data)
	return errors.Join(werr, fh.Close())
}

func joinNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return fmt.Sprintf("%d identities", len(names))
}

func newImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file>",
		Short: "Create identities from an export file",
		Long: "Read a file from git-idm export and create each identity with the add steps.\n" +
			"The values from the file are filled in. You pick the folder, the SSH key and the GitHub login.",
		Example: "  git-idm import work.git-idm.toml",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := transfer.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read %s: %w", args[0], err)
			}
			if _, err := newPrompter(cmd); err != nil {
				return fmt.Errorf("import asks questions and needs a terminal")
			}
			c, err := config.Load()
			if errors.Is(err, config.ErrNotFound) {
				c, err = config.New(), nil
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for i := range f.Identities {
				e := f.Identities[i]
				if len(f.Identities) > 1 {
					ok, err := confirmImport(cmd, e, i+1, len(f.Identities))
					if err != nil {
						return err
					}
					if !ok {
						fmt.Fprintf(out, "skipped %s\n", e.ID)
						continue
					}
				}
				if err := runSetupWizard(cmd, c, func(w *setup.Wizard) { w.Import, w.DefaultName = &e, e.ID }); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func confirmImport(cmd *cobra.Command, e transfer.Entry, n, total int) (bool, error) {
	prompter, err := newPrompter(cmd)
	if err != nil {
		return false, err
	}
	ok, err := prompter.Confirm(fmt.Sprintf("Import %s? (%d of %d)", e.ID, n, total), e.Name+" <"+e.Email+">", true)
	if errors.Is(err, setup.ErrAborted) {
		return false, nil
	}
	return ok, err
}
