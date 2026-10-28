package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/ui"
	"github.com/stevetosak/git-idm/internal/version"
)

const quickStart = `git-idm keeps one git identity per folder: commit author, SSH key and gh account.

Quick start:
  git-idm add          create an identity (asks a few questions)
  git-idm              show the identity for this folder
  git-idm ls           list all identities
  git-idm use <id>     switch identity in this shell only`

var commandGroups = []struct {
	id, title string
	commands  []string
}{
	{"essentials", "Essentials:", []string{"add", "edit", "rm", "ls", "help"}},
	{"switching", "Switch identity:", []string{"use", "bind", "unbind"}},
	{"safety", "Check and protect:", []string{"doctor", "login", "guard", "allow"}},
	{"setup", "Setup:", []string{"init", "migrate", "export", "import", "apply", "hook", "prompt", "completion", "version", "update"}},
}

func newRootCmd() *cobra.Command {
	cobra.EnableCommandSorting = false
	root := &cobra.Command{
		Use:           "git-idm",
		Short:         "Git identity manager",
		Long:          quickStart,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return unknownCommand(cmd, args[0])
			}
			return runStatus(cmd, args)
		},
	}
	root.AddCommand(
		newAddCmd(), newEditCmd(), newRmCmd(), newLsCmd(),
		newUseCmd(), newBindCmd(), newUnbindCmd(),
		newDoctorCmd(), newLoginCmd(), newGuardCmd(), newAllowCmd(),
		newInitCmd(), newMigrateCmd(), newExportCmd(), newImportCmd(), newApplyCmd(), newHookCmd(), newPromptCmd(),
		newVersionCmd(), newUpdateCmd(), newUpdateCheckCmd(), newEnvCmd(), newCheckCommitCmd(), newCredentialHintCmd(),
	)
	root.SuggestionsMinimumDistance = 2
	root.Version = version.Current()
	root.SetVersionTemplate("git-idm {{.Version}}\n")
	group := map[string]string{}
	for _, g := range commandGroups {
		root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		for _, name := range g.commands {
			group[name] = g.id
		}
	}
	for _, c := range root.Commands() {
		c.GroupID = group[c.Name()]
	}
	helpfulArgs(root)
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error { return usageError(c, err.Error()) })
	root.SetHelpCommandGroupID("essentials")
	root.SetCompletionCommandGroupID("setup")
	return root
}

func unknownCommand(cmd *cobra.Command, name string) error {
	msg := fmt.Sprintf("unknown command %q", name)
	if suggestions := cmd.SuggestionsFor(name); len(suggestions) > 0 {
		msg += ", did you mean: " + strings.Join(suggestions, ", ")
	}
	return fmt.Errorf("%s\nsee all commands: git-idm help", msg)
}

func newAddCmd() *cobra.Command {
	var id config.Identity
	var protocol string
	var makeDefault bool
	cmd := &cobra.Command{
		Use:   "add [identity]",
		Short: "Create an identity (asks a few questions)",
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
	var long bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List identities and where they are used",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			t := ui.New(out)
			c, err := config.Load()
			if errors.Is(err, config.ErrNotFound) || (err == nil && len(c.Identities) == 0) {
				fmt.Fprintln(out, welcome(t, importable()))
				return nil
			}
			if err != nil {
				return err
			}
			active := ""
			if _, res, err := current(); err == nil {
				active = res.Identity
			}
			fmt.Fprintln(out, listView(t, c, active, long))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&long, "long", "l", false, "also show SSH and signing keys")
	return cmd
}

func listView(t *ui.Theme, c *config.Config, active string, long bool) string {
	header := []string{"", "IDENTITY", "AUTHOR", "GITHUB", "USED IN"}
	if long {
		header = append(header, "SSH KEY", "SIGNS WITH")
	}
	var rows [][]string
	for _, name := range c.Names() {
		id := c.Identities[name]
		marker := " "
		if name == active {
			marker = t.Colored("●", id.Color)
		}
		var used []string
		if name == c.Default {
			used = append(used, "★ default")
		}
		for _, r := range c.RulesFor(name) {
			used = append(used, r.Path+temporaryNote(t, r))
		}
		where := strings.Join(used, ", ")
		if where == "" {
			where = t.Dim("nowhere (git-idm bind " + name + " <dir>)")
		}
		gh := id.GHUser
		if gh == "" {
			gh = t.Dim("-")
		}
		row := []string{marker, t.Identity(name, id.Color), id.Name + " <" + id.Email + ">", gh, where}
		if long {
			row = append(row, orDash(t, id.SSHKey), orDash(t, id.SigningKey))
		}
		rows = append(rows, row)
	}
	legend := "● active here  ★ default for folders without a rule"
	return ui.Table(t, header, rows) + "\n\n  " + t.Dim(legend) + "\n" + t.Footer()
}

func orDash(t *ui.Theme, s string) string {
	if s == "" {
		return t.Dim("-")
	}
	return s
}

func temporaryNote(t *ui.Theme, r config.Rule) string {
	if !r.Temporary {
		return ""
	}
	return " " + t.Warn("("+temporaryText(&r)+")")
}
