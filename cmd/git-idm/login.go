package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/setup"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login [identity]",
		Short: "Log in to GitHub again when the gh token stops working",
		Long: "Log in again in the browser when the gh token is invalid, revoked or cannot add SSH keys.\n" +
			"The gh folder and its settings stay the same. With no identity, use the identity that is active here.",
		Example: "  git-idm login personal",
		Args:    cobra.MaximumNArgs(1),
		RunE:    runLogin,
	}
}

func runLogin(cmd *cobra.Command, args []string) error {
	c, res, err := current()
	if err != nil {
		return err
	}
	name := res.Identity
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return fmt.Errorf("no identity is active here, give one: git-idm login <identity>")
	}
	if _, ok := c.Identities[name]; !ok {
		return fmt.Errorf("identity %q does not exist", name)
	}
	ex, err := newExecutor(cmd)
	if err != nil {
		return err
	}
	if ex.Tools.GH == "" {
		return fmt.Errorf("gh is not installed (https://cli.github.com)")
	}
	if c.Identities[name].GHConfigDir == "" {
		return firstLogin(cmd, c, name, ex)
	}
	return relogin(cmd, c, name, ex)
}

func relogin(cmd *cobra.Command, c *config.Config, name string, ex *setup.Executor) error {
	out := cmd.OutOrStdout()
	id := c.Identities[name]
	shared := identitiesUsing(c, id.GHConfigDir)
	fmt.Fprintf(out, "Log in to GitHub again for %s (%s).\n", name, id.GHConfigDir)
	if id.GHUser != "" {
		fmt.Fprintf(out, "Use the account %s in the browser.\n", id.GHUser)
	}
	if len(shared) > 1 {
		fmt.Fprintf(out, "This login is also used by %s.\n", strings.Join(without(shared, name), ", "))
	}
	before := importer.GHUserFromConfigDir(id.GHConfigDir)
	account, err := ex.Relogin(id.GHConfigDir)
	if err != nil {
		return fmt.Errorf("login failed, nothing was changed: %w", err)
	}
	if id.GHUser != "" && !strings.EqualFold(account.Login, id.GHUser) {
		use, err := confirmOtherAccount(cmd, account.Login, id.GHUser, shared)
		if err != nil || !use {
			ex.LogoutUser(id.GHConfigDir, account.Login)
			fmt.Fprintf(out, "logged out of %s again, %s still uses %s\n", account.Login, name, id.GHUser)
			return err
		}
		if before != "" && !strings.EqualFold(before, account.Login) {
			ex.LogoutUser(id.GHConfigDir, before)
		}
	}
	changed := false
	for _, n := range shared {
		other := c.Identities[n]
		if other.GHUser != account.Login {
			other.GHUser = account.Login
			c.Identities[n] = other
			changed = true
		}
	}
	if changed {
		if err := c.Save(); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "✓ %s uses the gh login %s again. Check with: git-idm doctor\n", strings.Join(shared, ", "), account.Login)
	return nil
}

func confirmOtherAccount(cmd *cobra.Command, got, want string, shared []string) (bool, error) {
	prompter, err := newPrompter(cmd)
	if err != nil {
		return false, nil
	}
	use, err := prompter.Confirm(
		fmt.Sprintf("Use %s for %s?", got, strings.Join(shared, ", ")),
		fmt.Sprintf("You logged in as %s, but the identity uses %s. No logs out of %s.", got, want, got),
		false)
	if errors.Is(err, setup.ErrAborted) {
		return false, nil
	}
	return use, err
}

func firstLogin(cmd *cobra.Command, c *config.Config, name string, ex *setup.Executor) error {
	out := cmd.OutOrStdout()
	prompter, err := newPrompter(cmd)
	if err != nil {
		return fmt.Errorf("%s has no gh login and login asks questions, run it in a terminal", name)
	}
	w := setup.Wizard{Prompt: prompter, Config: c, Login: ex.Login, Verify: ex.Account,
		Logins: setup.ExistingLogins(c, importer.DefaultGHConfigDir())}
	plan, err := w.AskGitHub(name)
	if errors.Is(err, setup.ErrAborted) || (err == nil && plan.GHUser == "") {
		fmt.Fprintln(out, "nothing was changed")
		return nil
	}
	if err != nil {
		return err
	}
	undo := func(err error) error {
		if plan.NewLogin {
			ex.Logout(plan.GHConfigDir, plan.GHUser)
		}
		return err
	}
	id := c.Identities[name]
	if plan.NewLogin && id.Protocol == config.ProtocolSSH {
		if err := ex.UseSSHProtocol(plan.GHConfigDir); err != nil {
			return undo(err)
		}
	}
	id.GHUser, id.GHConfigDir = plan.GHUser, config.ContractHome(config.ExpandHome(plan.GHConfigDir))
	c.Identities[name] = id
	if err := c.Save(); err != nil {
		return undo(err)
	}
	if err := applyConfig(io.Discard, c); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ %s uses the gh login %s (%s)\n", name, plan.GHUser, id.GHConfigDir)
	return nil
}

func identitiesUsing(c *config.Config, dir string) []string {
	var names []string
	for _, n := range c.Names() {
		if other := c.Identities[n].GHConfigDir; other != "" && config.ExpandHome(other) == config.ExpandHome(dir) {
			names = append(names, n)
		}
	}
	return names
}

func without(names []string, name string) []string {
	var rest []string
	for _, n := range names {
		if n != name {
			rest = append(rest, n)
		}
	}
	return rest
}
