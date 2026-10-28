package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
)

func newEditCmd() *cobra.Command {
	var v config.Identity
	var protocol string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "edit <identity>",
		Short: "Change an identity, for example its signing key",
		Example: "  git-idm edit client --signing-key ~/.ssh/git-idm_client.pub\n" +
			"  git-idm edit client --email jane@client.com --color magenta",
		Args: cobra.ExactArgs(1),
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
			f := cmd.Flags()
			if f.NFlag() == 0 {
				return fmt.Errorf("give at least one field to change, see: git-idm edit --help")
			}
			set := func(flag string, dst *string, value string, path bool) error {
				if !f.Changed(flag) {
					return nil
				}
				if path && value != "" {
					if _, err := os.Stat(config.ExpandHome(value)); err != nil {
						return fmt.Errorf("--%s: %s does not exist", flag, value)
					}
					value = config.ContractHome(config.ExpandHome(value))
				}
				*dst = value
				return nil
			}
			for _, s := range []struct {
				flag  string
				dst   *string
				value string
				path  bool
			}{
				{"name", &id.Name, v.Name, false},
				{"email", &id.Email, v.Email, false},
				{"ssh-key", &id.SSHKey, v.SSHKey, true},
				{"signing-key", &id.SigningKey, v.SigningKey, looksLikePath(v.SigningKey)},
				{"gh-user", &id.GHUser, v.GHUser, false},
				{"gh-config-dir", &id.GHConfigDir, v.GHConfigDir, true},
				{"color", &id.Color, v.Color, false},
			} {
				if err := set(s.flag, s.dst, s.value, s.path); err != nil {
					return err
				}
			}
			if f.Changed("protocol") {
				id.Protocol = config.Protocol(protocol)
			}
			c.Identities[name] = id
			if makeDefault {
				c.Default = name
			}
			if err := c.Save(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "updated %s\n", name)
			if err := applyConfig(out, c); err != nil {
				return err
			}
			if f.Changed("signing-key") && id.SigningKey != "" && strings.HasSuffix(id.SigningKey, ".pub") && id.GHConfigDir != "" {
				fmt.Fprintf(out, "Commits are now signed. For a Verified badge on GitHub, add the key as a signing key:\n  GH_CONFIG_DIR=%s gh auth refresh -h github.com -s admin:ssh_signing_key\n  GH_CONFIG_DIR=%s gh ssh-key add %s --type signing\n",
					id.GHConfigDir, id.GHConfigDir, id.SigningKey)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&v.Name, "name", "", "commit author name")
	f.StringVar(&v.Email, "email", "", "commit author email")
	f.StringVar(&v.SSHKey, "ssh-key", "", "path to SSH private key (empty to remove)")
	f.StringVar(&v.SigningKey, "signing-key", "", "SSH public key file or GPG key ID that signs commits (empty to stop signing)")
	f.StringVar(&v.GHUser, "gh-user", "", "GitHub login")
	f.StringVar(&v.GHConfigDir, "gh-config-dir", "", "gh config directory")
	f.StringVar(&protocol, "protocol", "", "git protocol for GitHub: https or ssh")
	f.StringVar(&v.Color, "color", "", "prompt color")
	f.BoolVar(&makeDefault, "default", false, "use when no rule matches")
	return cmd
}

func looksLikePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, ".")
}
