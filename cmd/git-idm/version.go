package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the git-idm version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "git-idm", version.Current())
			return nil
		},
	}
}
