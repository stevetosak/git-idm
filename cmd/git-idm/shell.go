package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/resolve"
	"github.com/stevetosak/git-idm/internal/shell"
)

func hookHint() string {
	switch filepath.Base(os.Getenv("SHELL")) {
	case string(shell.Bash):
		return "add this line to ~/.bashrc: eval \"$(git-idm hook bash)\""
	case string(shell.Fish):
		return "add this line to ~/.config/fish/config.fish: git-idm hook fish | source"
	}
	return "add this line to ~/.zshrc: eval \"$(git-idm hook zsh)\""
}

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use: "hook <zsh|bash|fish>",
		Example: "  echo 'eval \"$(git-idm hook zsh)\"' >> ~/.zshrc\n" +
			"  echo 'eval \"$(git-idm hook bash)\"' >> ~/.bashrc\n" +
			"  echo 'git-idm hook fish | source' >> ~/.config/fish/config.fish",
		Short:     "Print the shell hook for zsh, bash or fish",
		Long:      "The hook sets GH_CONFIG_DIR when you change folders, makes git-idm use work, and shows the identity in the prompt.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{string(shell.Zsh), string(shell.Bash), string(shell.Fish)},
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, err := shell.Parse(args[0])
			if err != nil {
				return err
			}
			bin, err := gitgen.Executable()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), shell.Hook(sh, bin))
			return nil
		},
	}
}

func addShellFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "shell", "", "print shell code for this shell (used by the hook)")
	cmd.Flags().Lookup("shell").NoOptDefVal = string(shell.Zsh)
	_ = cmd.Flags().MarkHidden("shell")
}

func newEnvCmd() *cobra.Command {
	var forShell string
	cmd := &cobra.Command{
		Use:    "env",
		Short:  "Print shell code for the current directory (used by the hook)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sh, err := shell.Parse(orDefault(forShell))
			if err != nil {
				return err
			}
			c, res, err := current()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), shell.Sync(sh, c, res))
			return nil
		},
	}
	addShellFlag(cmd, &forShell)
	return cmd
}

func newUseCmd() *cobra.Command {
	var forShell string
	var clear bool
	cmd := &cobra.Command{
		Use:     "use <identity> | --clear",
		Short:   "Switch identity in this shell only",
		Example: "  git-idm use client\n  git-idm use --clear",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if forShell == "" {
				return fmt.Errorf("`use` needs the shell hook: %s", hookHint())
			}
			sh, err := shell.Parse(forShell)
			if err != nil {
				return err
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
				fmt.Fprint(cmd.OutOrStdout(), shell.Clear(sh, c, res))
				fmt.Fprintf(errOut, "override cleared, %s is active: %s\n", res.Identity, reason(res))
				return nil
			}
			name := args[0]
			res := resolve.Resolve(c, cwd, name)
			code, err := shell.Use(sh, c, name, res)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), code)
			fmt.Fprintf(errOut, "%s is active in this shell only. Clear with: git-idm use --clear\n", name)
			return nil
		},
	}
	addShellFlag(cmd, &forShell)
	cmd.Flags().BoolVar(&clear, "clear", false, "go back to directory rules")
	return cmd
}

func orDefault(name string) string {
	if name == "" {
		return string(shell.Zsh)
	}
	return name
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
