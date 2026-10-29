package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/ghauth"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
)

var verifyGHLogin = func(dir string) (string, error) {
	gh := gitgen.RealGH()
	if gh == "" {
		return "", errors.New("gh is not installed")
	}
	account, err := ghauth.Fetch(gh, dir)
	return account.Login, err
}

func defaultGHWork(c *config.Config) (user string, moving []string) {
	user = importer.GHUserFromConfigDir(importer.DefaultGHConfigDir())
	for _, name := range c.Names() {
		if importer.IsDefaultGHDir(c.Identities[name].GHConfigDir) {
			moving = append(moving, name)
		}
	}
	return user, moving
}

func isolateDefaultGH(cmd *cobra.Command, out io.Writer, c *config.Config, dryRun bool) error {
	user, moving := defaultGHWork(c)
	if user == "" && len(moving) == 0 {
		return nil
	}
	def := config.ExpandHome(importer.DefaultGHConfigDir())
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	target := func(name string) string { return filepath.Join(dir, "gh", name) }
	if user == "" {
		if dryRun {
			for _, name := range moving {
				fmt.Fprintf(out, "\n%s uses the default gh folder, which is not logged in: it would get %s\n", name, config.ContractHome(target(name)))
			}
			return nil
		}
		for _, name := range moving {
			id := c.Identities[name]
			id.GHConfigDir, id.GHUser = config.ContractHome(target(name)), ""
			c.Identities[name] = id
			fmt.Fprintf(out, "\n%s used the default gh folder, which is not logged in. It now uses %s (log in: git-idm login %s)\n", name, id.GHConfigDir, name)
		}
		if err := c.Save(); err != nil {
			return err
		}
		return applyConfig(out, c)
	}
	fmt.Fprintf(out, "\nThe default gh folder %s is logged in as %s. A gh that skips git-idm (a full path, or an app without ~/.local/bin in PATH) acts as %s.\n", config.ContractHome(def), user, user)
	for _, name := range moving {
		fmt.Fprintf(out, "  move the gh login of %s to %s\n", name, config.ContractHome(target(name)))
	}
	fmt.Fprintf(out, "  log out %s (hosts.yml goes to the backups, the token stays in the keychain)\n", config.ContractHome(def))
	prompter, perr := newPrompter(cmd)
	if dryRun || perr != nil {
		fmt.Fprintln(out, "Run git-idm migrate in a terminal to do this.")
		return nil
	}
	ok, err := prompter.Confirm("Move the gh logins and log out the default gh folder?", "git-idm checks each moved login with GitHub before it changes anything else.", true)
	if err != nil || !ok {
		return ignoreAbort(err)
	}
	var made []string
	rollback := func() {
		for _, d := range made {
			os.RemoveAll(d)
		}
	}
	for _, name := range moving {
		want := user
		if c.Identities[name].GHUser != "" {
			want = c.Identities[name].GHUser
		}
		to := target(name)
		if existing := importer.GHUserFromConfigDir(to); existing == "" {
			if err := ghauth.CopyLogin(def, to); err != nil {
				rollback()
				return fmt.Errorf("copy the gh login of %s: %w (nothing was changed)", name, err)
			}
			made = append(made, to)
		} else if !strings.EqualFold(existing, want) {
			rollback()
			return fmt.Errorf("%s already has a gh login of %s, not %s (nothing was changed)", config.ContractHome(to), existing, want)
		}
		login, err := verifyGHLogin(to)
		if err != nil || !strings.EqualFold(login, want) {
			rollback()
			if err == nil {
				err = fmt.Errorf("GitHub says %s, not %s", login, want)
			}
			return fmt.Errorf("the moved gh login of %s does not work (%v), nothing was changed", name, err)
		}
	}
	for _, name := range moving {
		id := c.Identities[name]
		id.GHConfigDir = config.ContractHome(target(name))
		c.Identities[name] = id
		fmt.Fprintf(out, "  moved the gh login of %s to %s\n", name, id.GHConfigDir)
	}
	if len(moving) > 0 {
		if err := c.Save(); err != nil {
			return err
		}
		if err := applyConfig(out, c); err != nil {
			return err
		}
	}
	a, err := gitgen.NewApplier()
	if err != nil {
		return err
	}
	hosts := filepath.Join(def, "hosts.yml")
	backup, err := a.Backup(hosts)
	if err != nil {
		return err
	}
	if err := os.Remove(hosts); err != nil {
		return err
	}
	fmt.Fprintf(out, "  logged out %s (backup: %s)\n", config.ContractHome(def), config.ContractHome(backup))
	return nil
}
