package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/setup"
)

func newEditCmd() *cobra.Command {
	var v config.Identity
	var protocol string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "edit <identity>",
		Short: "Change an identity, for example its signing key",
		Long:  "Change an identity. With no flags, pick the fields to change from a menu.",
		Example: "  git-idm edit client\n" +
			"  git-idm edit client --signing-key ~/.ssh/git-idm_client.pub\n" +
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
				return runEditMenu(cmd, c, name)
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

func runEditMenu(cmd *cobra.Command, c *config.Config, name string) error {
	out := cmd.OutOrStdout()
	prompter, err := newPrompter(cmd)
	if err != nil {
		return fmt.Errorf("give at least one field to change, see: git-idm edit --help")
	}
	ex, err := newExecutor(cmd)
	if err != nil {
		return err
	}
	editor := setup.Editor{Prompt: prompter, Config: c, Logout: ex.Logout}
	if ex.Tools.GH != "" {
		editor.Login = func(string) (setup.Account, string, error) { return ex.Login(freeLoginName(ex, name)) }
		editor.Verify = ex.Account
		editor.Logins = setup.ExistingLogins(c, importer.DefaultGHConfigDir())
	}
	edit, err := editor.Run(name)
	if errors.Is(err, setup.ErrAborted) {
		fmt.Fprintln(out, "nothing was changed")
		return nil
	}
	if err != nil {
		return err
	}
	if len(edit.Changed) == 0 {
		fmt.Fprintln(out, "nothing was changed")
		return nil
	}
	plan := edit.Plan
	undo := func(err error) error {
		if plan.NewLogin {
			ex.Logout(plan.GHConfigDir, plan.GHUser)
		}
		return err
	}
	if err := ex.Check(plan); err != nil {
		return undo(err)
	}
	old := c.Identities[name]
	id, report, err := ex.Run(plan)
	if err != nil {
		printPartial(out, report)
		return undo(err)
	}
	id.SigningKey = old.SigningKey
	c.Identities[name] = id
	switch {
	case plan.MakeDefault:
		c.Default = name
	case c.Default == name:
		c.Default = ""
	}
	if err := c.Save(); err != nil {
		printPartial(out, report)
		return undo(err)
	}
	fmt.Fprintf(out, "updated %s: %s\n", name, strings.Join(edit.Changed, ", "))
	if err := applyConfig(out, c); err != nil {
		return err
	}
	if old.GHConfigDir != "" && config.ExpandHome(old.GHConfigDir) != config.ExpandHome(id.GHConfigDir) &&
		isManagedLogin(ex, old.GHConfigDir) && len(identitiesUsing(c, old.GHConfigDir)) == 0 {
		ex.Logout(old.GHConfigDir, old.GHUser)
		fmt.Fprintf(out, "  ✓ logged out of %s and deleted %s, no identity uses it now\n", old.GHUser, old.GHConfigDir)
	}
	if plan.Upload {
		testConnection(out, ex, id)
	}
	printKeyHint(out, plan, id)
	return nil
}

func freeLoginName(ex *setup.Executor, name string) string {
	for i := 1; ; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", name, i)
		}
		if _, err := os.Stat(ex.LoginDir(candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}

func isManagedLogin(ex *setup.Executor, dir string) bool {
	return filepath.Dir(config.ExpandHome(dir)) == filepath.Join(ex.ConfigDir, "gh")
}
