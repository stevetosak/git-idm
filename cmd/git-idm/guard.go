package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/guard"
	"github.com/stevetosak/git-idm/internal/resolve"
)

var errBlocked = errors.New("commit blocked by git-idm guard")

func newGuardCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "guard [block|warn|off]",
		Short:     "Stop commits and pushes with the wrong identity",
		Long:      "block: stop commits and pushes with the wrong identity. warn: print a warning only. off: no check.\nWith no argument, show the current mode.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{string(config.GuardBlock), string(config.GuardWarn), string(config.GuardOff)},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				mode := c.Guard
				if mode == "" {
					mode = config.GuardOff
				}
				fmt.Fprintf(out, "guard: %s\n", mode)
				return nil
			}
			c.Guard = config.GuardMode(args[0])
			if err := c.Validate(); err != nil {
				return err
			}
			if c.GuardEnabled() {
				if hp := globalHooksPathOutsideBlock(); hp != "" {
					return fmt.Errorf("your global gitconfig already sets core.hooksPath = %s; remove it first, git-idm runs repo hooks itself", hp)
				}
			}
			if err := c.Save(); err != nil {
				return err
			}
			fmt.Fprintf(out, "guard: %s\n", c.Guard)
			return applyConfig(out, c)
		},
	}
}

func newAllowCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "allow",
		Short: "Accept the commit author that this repo sets itself",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, res, err := current()
			if err != nil {
				return err
			}
			if res.RepoRoot == "" {
				return fmt.Errorf("not inside a git repository")
			}
			out := cmd.OutOrStdout()
			if remove {
				if err := guard.Disallow(res.RepoRoot); err != nil {
					return err
				}
				fmt.Fprintln(out, "removed the accepted author for this repo")
				return nil
			}
			v, err := guard.Check(c, res, res.RepoRoot)
			if err != nil {
				return err
			}
			if strings.EqualFold(v.Expected, v.Actual) {
				return fmt.Errorf("the author %s is already the expected identity, nothing to allow", v.Actual)
			}
			if err := guard.Allow(res.RepoRoot, v.Actual); err != nil {
				return err
			}
			fmt.Fprintf(out, "commits in %s may use %s (undo: git-idm allow --remove)\n", config.ContractHome(res.RepoRoot), v.Actual)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "stop accepting the author")
	return cmd
}

func newCheckCommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:    gitgen.CheckCommand,
		Short:  "Check the commit author (used by the pre-commit hook)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, res, err := current()
			if errors.Is(err, config.ErrNotFound) {
				fmt.Fprintln(cmd.ErrOrStderr(), "git-idm: guard skipped:", err)
				return nil
			}
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "git-idm blocked: the guard cannot read its config:", err, "\n  skip once: git commit --no-verify")
				return errBlocked
			}
			if !c.GuardEnabled() {
				return nil
			}
			dir, _ := os.Getwd()
			v, err := guard.Check(c, res, dir)
			if err != nil {
				if c.Guard == config.GuardBlock {
					fmt.Fprintln(cmd.ErrOrStderr(), "git-idm blocked: the guard cannot check this commit:", err, "\n  skip once: git commit --no-verify")
					return errBlocked
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "git-idm: guard skipped:", err)
				return nil
			}
			if v.OK() {
				if res.Temporary() {
					fmt.Fprintln(cmd.ErrOrStderr(), "git-idm: committing as "+res.Identity+" ("+temporaryText(res.Rule)+")")
				}
				return nil
			}
			printMismatch(cmd.ErrOrStderr(), c, res, v)
			if c.Guard == config.GuardBlock {
				return errBlocked
			}
			return nil
		},
	}
}

func printMismatch(w io.Writer, c *config.Config, res resolve.Result, v guard.Verdict) {
	from := "git config"
	switch {
	case os.Getenv("GIT_AUTHOR_EMAIL") != "":
		from = "GIT_AUTHOR_EMAIL or --author"
	case strings.EqualFold(res.LocalEmail, v.Actual):
		from = "this repo's .git/config"
	}
	label := "warning"
	if c.Guard == config.GuardBlock {
		label = "blocked"
	}
	fmt.Fprintf(w, "git-idm %s: this commit uses the wrong identity\n", label)
	if !v.Accepts(v.Actual) {
		fmt.Fprintf(w, "  author:   %s  (from %s)\n", v.Actual, from)
	}
	if v.Committer != "" && !v.Accepts(v.Committer) {
		committerFrom := "git config"
		if os.Getenv("GIT_COMMITTER_EMAIL") != "" {
			committerFrom = "GIT_COMMITTER_EMAIL"
		}
		fmt.Fprintf(w, "  committer: %s  (from %s)\n", v.Committer, committerFrom)
	}
	fmt.Fprintf(w, "  expected: %s <%s>  (%s)\n", v.ExpectedFrom, v.Expected, reason(res))
	fmt.Fprintln(w, "  fix one of:")
	fix := func(command, text string) { fmt.Fprintf(w, "    %-42s %s\n", command, text) }
	if from == "this repo's .git/config" {
		if res.Rule != nil {
			fix("git-idm migrate", "remove author keys from repo configs under rules")
		} else {
			fix("git config --local --unset-all user.email", "use the git-idm identity")
		}
	}
	fix("git-idm allow", "accept this author in this repo")
	fix("git commit --no-verify", "skip the check once")
}

func globalHooksPathOutsideBlock() string {
	path, err := gitgen.GlobalConfigPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return gitgen.HooksPathOutsideBlock(string(data))
}

func temporaryText(r *config.Rule) string {
	if r.Was != "" {
		return "temporary, was " + r.Was
	}
	return "temporary"
}
