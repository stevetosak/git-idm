package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "git-idm",
		Short:         "Git identity manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          runStatus,
	}
	root.AddCommand(newAddCmd(), newLsCmd(), newInitCmd(), newApplyCmd(), newBindCmd(), newUnbindCmd(), newMigrateCmd(),
		newHookCmd(), newEnvCmd(), newUseCmd(), newPromptCmd(), newEditCmd(), newRmCmd(), newGuardCmd(), newAllowCmd(), newCheckCommitCmd(), newDoctorCmd())
	return root
}

func newAddCmd() *cobra.Command {
	var id config.Identity
	var protocol string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "add [identity]",
		Short: "Add an identity (asks questions when --email is not given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if errors.Is(err, config.ErrNotFound) {
				c, err = config.New(), nil
			}
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			if !cmd.Flags().Changed("email") {
				return runAddWizard(cmd, c, name)
			}
			if name == "" {
				return fmt.Errorf("give an identity name, or leave out the flags to answer questions")
			}
			if id.Name == "" {
				return fmt.Errorf("--name is required with --email")
			}
			if err := config.ValidateIdentityName(name); err != nil {
				return err
			}
			if _, exists := c.Identities[name]; exists {
				return fmt.Errorf("identity %q already exists", name)
			}
			id.Protocol = config.Protocol(protocol)
			id.SSHKey = config.ContractHome(id.SSHKey)
			id.GHConfigDir = config.ContractHome(id.GHConfigDir)
			c.Identities[name] = id
			if makeDefault {
				c.Default = name
			}
			if err := c.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s <%s>\n", name, id.Email)
			return applyConfig(cmd.OutOrStdout(), c)
		},
	}
	f := cmd.Flags()
	f.StringVar(&id.Name, "name", "", "commit author name")
	f.StringVar(&id.Email, "email", "", "commit author email (without it, add asks questions)")
	f.StringVar(&id.SSHKey, "ssh-key", "", "path to SSH private key")
	f.StringVar(&id.SigningKey, "signing-key", "", "commit signing key")
	f.StringVar(&id.GHUser, "gh-user", "", "GitHub login")
	f.StringVar(&id.GHConfigDir, "gh-config-dir", "", "gh config directory for this identity")
	f.StringVar(&protocol, "protocol", "", "git protocol: https or ssh")
	f.StringVar(&id.Color, "color", "", "prompt color")
	f.BoolVar(&makeDefault, "default", false, "use when no rule matches")
	return cmd
}

func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List identities and their directories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatList(c))
			return nil
		},
	}
}

func formatList(c *config.Config) string {
	var b strings.Builder
	for _, name := range c.Names() {
		id := c.Identities[name]
		marker := " "
		if name == c.Default {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s %-12s %s <%s>\n", marker, name, id.Name, id.Email)
		if id.GHUser != "" {
			fmt.Fprintf(&b, "    gh:   %s\n", id.GHUser)
		}
		if id.SSHKey != "" {
			fmt.Fprintf(&b, "    ssh:  %s\n", id.SSHKey)
		}
		for _, r := range c.RulesFor(name) {
			fmt.Fprintf(&b, "    dir:  %s\n", r.Path)
		}
		if name == c.Default {
			fmt.Fprintf(&b, "    dir:  (default, when no rule matches)\n")
		}
	}
	return b.String()
}
