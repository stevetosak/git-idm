package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
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
