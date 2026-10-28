package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/resolve"
	"github.com/stevetosak/git-idm/internal/shell"
)

const hookHint = "add this line to ~/.zshrc: eval \"$(git-idm hook zsh)\""

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "hook zsh",
		Example:   `  echo 'eval "$(git-idm hook zsh)"' >> ~/.zshrc`,
		Short:     "Print the shell hook for ~/.zshrc",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"zsh"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "zsh" {
				return fmt.Errorf("shell %q is not supported yet (supported: zsh)", args[0])
			}
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			if real, err := filepath.EvalSymlinks(bin); err == nil {
				bin = real
			}
			fmt.Fprint(cmd.OutOrStdout(), shell.ZshHook(bin))
			return nil
		},
	}
}

func newEnvCmd() *cobra.Command {
	var forShell bool
	cmd := &cobra.Command{
		Use:    "env",
		Short:  "Print shell code for the current directory (used by the hook)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, res, err := current()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), shell.Sync(c, res))
			return nil
		},
	}
	cmd.Flags().BoolVar(&forShell, "shell", false, "")
	return cmd
}

func newUseCmd() *cobra.Command {
	var forShell, clear bool
	cmd := &cobra.Command{
		Use:     "use <identity> | --clear",
		Short:   "Switch identity in this shell only",
		Example: "  git-idm use client\n  git-idm use --clear",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !forShell {
				return fmt.Errorf("`use` needs the shell hook: %s", hookHint)
			}
			if clear == (len(args) == 1) {
				return usageError(cmd, "use needs <identity> or --clear, not both")
			}
			c, err := config.Load()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			errOut := cmd.ErrOrStderr()
			if clear {
				res := resolve.Resolve(c, cwd, "")
				fmt.Fprint(cmd.OutOrStdout(), shell.Clear(c, res))
				fmt.Fprintf(errOut, "override cleared, %s is active: %s\n", res.Identity, reason(res))
				return nil
			}
			name := args[0]
			res := resolve.Resolve(c, cwd, name)
			code, err := shell.Use(c, name, res)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), code)
			fmt.Fprintf(errOut, "%s is active in this shell only. Clear with: git-idm use --clear\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&forShell, "shell", false, "print shell code (used by the hook)")
	_ = cmd.Flags().MarkHidden("shell")
	cmd.Flags().BoolVar(&clear, "clear", false, "go back to directory rules")
	return cmd
}

func current() (*config.Config, resolve.Result, error) {
	c, err := config.Load()
	if err != nil {
		return nil, resolve.Result{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, resolve.Result{}, err
	}
	override := os.Getenv(resolve.OverrideEnv)
	if override != "" {
		if _, ok := c.Identities[override]; !ok {
			override = ""
		}
	}
	return c, resolve.Resolve(c, cwd, override), nil
}
