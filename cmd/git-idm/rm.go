package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
)

func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <identity>",
		Short: "Remove an identity and its directory rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			name := args[0]
			id, ok := c.Identities[name]
			if !ok {
				return fmt.Errorf("identity %q does not exist", name)
			}
			if c.Default == name && len(c.Identities) > 1 {
				return fmt.Errorf("%q is the default identity; make another identity the default first", name)
			}
			delete(c.Identities, name)
			if c.Default == name {
				c.Default = ""
			}
			kept := c.Rules[:0]
			for _, r := range c.Rules {
				if r.Identity != name {
					kept = append(kept, r)
				}
			}
			c.Rules = kept
			if err := c.Save(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "removed %s\n", name)
			if err := applyConfig(out, c); err != nil {
				return err
			}
			var leftovers []string
			if id.SSHKey != "" {
				leftovers = append(leftovers, fmt.Sprintf("ssh key: rm %s %s.pub", id.SSHKey, id.SSHKey))
			}
			if id.GHConfigDir != "" {
				if dir, err := config.Dir(); err == nil && filepath.Dir(config.ExpandHome(id.GHConfigDir)) == filepath.Join(dir, "gh") {
					leftovers = append(leftovers, "gh login: rm -r "+id.GHConfigDir)
				}
			}
			if id.GHUser != "" && id.SSHKey != "" {
				leftovers = append(leftovers, "GitHub key \"git-idm "+name+"\": https://github.com/settings/keys")
			}
			if len(leftovers) > 0 {
				fmt.Fprintln(out, "Kept, so you can delete them yourself:")
				for _, l := range leftovers {
					fmt.Fprintf(out, "  %s\n", l)
				}
			}
			return nil
		},
	}
}
