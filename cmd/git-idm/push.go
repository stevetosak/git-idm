package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/guard"
)

func newCheckPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:    gitgen.PushCheckCommand + " [remote] [url]",
		Short:  "Check the identity of a push (used by the pre-push hook)",
		Hidden: true,
		Args:   cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := ""
			if len(args) == 2 {
				url = args[1]
			}
			return runCheckPush(cmd.InOrStdin(), cmd.ErrOrStderr(), url)
		},
	}
}

func runCheckPush(in io.Reader, w io.Writer, url string) error {
	block := func(text string) error {
		fmt.Fprintf(w, "git-idm blocked: %s\n  skip once: git push --no-verify\n", text)
		return errBlocked
	}
	c, res, err := current()
	if errors.Is(err, config.ErrNotFound) {
		return nil
	}
	if err != nil {
		return block("the push guard cannot read its config: " + err.Error())
	}
	if !c.GuardEnabled() {
		return nil
	}
	id, ok := c.Identities[res.Identity]
	if !ok {
		if c.Guard != config.GuardBlock {
			fmt.Fprintf(w, "git-idm warning: no identity is bound to this folder\n  fix: %s\n", bindHint(res))
			return nil
		}
		return block("no identity is bound to this folder\n  fix: " + bindHint(res))
	}
	updates, err := guard.ParsePushInput(in)
	if err != nil {
		return block(err.Error())
	}
	dir, _ := os.Getwd()
	bin, _ := gitgen.Executable()
	problems, err := guard.CheckPush(guard.Push{
		Dir: dir, URL: url, Name: res.Identity, Identity: id, Allowed: guard.Allowed(dir),
		Bin: bin, Strict: gitgen.UsesGH(c), Getenv: os.Getenv,
	}, updates)
	if err != nil {
		if c.Guard == config.GuardBlock {
			return block("the push guard cannot check this push: " + err.Error())
		}
		fmt.Fprintln(w, "git-idm: push guard skipped:", err)
		return nil
	}
	if len(problems) == 0 {
		return nil
	}
	label := "warning"
	if c.Guard == config.GuardBlock {
		label = "blocked"
	}
	fmt.Fprintf(w, "git-idm %s: this push does not match %s (%s)\n", label, res.Identity, reason(res))
	for _, p := range problems {
		fmt.Fprintf(w, "  - %s\n    fix: %s\n", p.Text, p.Fix)
	}
	if c.Guard != config.GuardBlock {
		return nil
	}
	fmt.Fprintln(w, "  skip once: git push --no-verify")
	return errBlocked
}
