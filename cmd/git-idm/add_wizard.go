package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/setup"
)

var newPrompter = func(cmd *cobra.Command) (setup.Prompter, error) {
	if !isTerminal(os.Stdin) && os.Getenv("TERM") != "dumb" {
		return nil, fmt.Errorf("add asks questions and needs a terminal; in scripts use: git-idm add <name> --name ... --email ...")
	}
	return setup.HuhPrompter{}, nil
}

var newExecutor = func(cmd *cobra.Command) (*setup.Executor, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	return &setup.Executor{
		Tools:     setup.FindTools(),
		ConfigDir: dir,
		Hostname:  strings.TrimSuffix(host, ".local"),
		In:        cmd.InOrStdin(),
		Out:       cmd.OutOrStdout(),
	}, nil
}

func runAddWizard(cmd *cobra.Command, c *config.Config, name string) error {
	out := cmd.OutOrStdout()
	prompter, err := newPrompter(cmd)
	if err != nil {
		return err
	}
	ex, err := newExecutor(cmd)
	if err != nil {
		return err
	}
	w := setup.Wizard{Prompt: prompter, Config: c, DefaultName: name, DefaultUser: defaultAuthor(c), Logout: ex.Logout}
	if ex.Tools.GH != "" {
		w.Login = ex.Login
	}
	plan, err := w.Run()
	if errors.Is(err, setup.ErrAborted) {
		if plan.GHUser != "" {
			fmt.Fprintln(out, "setup cancelled: logged out of", plan.GHUser, "and nothing else was changed")
		} else {
			fmt.Fprintln(out, err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err := ex.Check(plan); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nSetting up %s\n", plan.Name)
	id, report, err := ex.Run(plan)
	if err != nil {
		printPartial(out, report)
		return err
	}
	c.Identities[plan.Name] = id
	if c.Default == "" {
		c.Default = plan.Name
	}
	if plan.BindDir != "" {
		c.Rules = append(c.Rules, config.Rule{Path: plan.BindDir, Identity: plan.Name})
	}
	if err := c.Save(); err != nil {
		printPartial(out, report)
		return err
	}
	fmt.Fprintf(out, "  ✓ saved %s\n", plan.Name)
	if err := applyConfig(io.Discard, c); err != nil {
		return err
	}
	fmt.Fprintln(out, "  ✓ git config applied")
	if id.SSHKey != "" {
		testConnection(out, ex, id)
	}
	fmt.Fprintf(out, "\nDone.")
	if plan.BindDir != "" {
		fmt.Fprintf(out, " Repos under %s now use %s.", plan.BindDir, plan.Name)
	}
	fmt.Fprintln(out, " Check any time with: git-idm")
	return nil
}

func testConnection(out io.Writer, ex *setup.Executor, id config.Identity) {
	login, err := ex.TestConnection(id)
	switch {
	case err != nil:
		fmt.Fprintf(out, "  ! connection test failed: %v\n", err)
		if id.GHUser == "" {
			fmt.Fprintln(out, "    add the public key to your GitHub account: "+id.SSHKey+".pub")
		}
	case id.GHUser != "" && !strings.EqualFold(login, id.GHUser):
		fmt.Fprintf(out, "  ! GitHub knows this key as %s, but gh is logged in as %s\n", login, id.GHUser)
	default:
		fmt.Fprintf(out, "  ✓ GitHub accepts the key as %s\n", login)
	}
}

func printPartial(out io.Writer, r *setup.Report) {
	if r == nil {
		return
	}
	fmt.Fprintf(out, "\n  ✗ %s failed. The config was not changed.\n", r.Failed)
	if len(r.Done) > 0 {
		fmt.Fprintln(out, "  These steps were done and stay in place:")
		for _, d := range r.Done {
			fmt.Fprintf(out, "    - %s\n", d)
		}
	}
}

func defaultAuthor(c *config.Config) string {
	if id, ok := c.Identities[c.Default]; ok {
		return id.Name
	}
	for _, name := range c.Names() {
		return c.Identities[name].Name
	}
	return ""
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
