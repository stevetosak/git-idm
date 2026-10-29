package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/doctor"
	"github.com/stevetosak/git-idm/internal/ghauth"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/shell"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Find problems with keys, gh logins, hooks and the shell",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, res, err := current()
			if err != nil {
				return err
			}
			a, err := gitgen.NewApplier()
			if err != nil {
				return err
			}
			_, hook := os.LookupEnv(shell.SegmentVar)
			env := doctor.Env{Getenv: os.Getenv, HookHint: hookHint(), ShellHook: hook, LookPath: exec.LookPath}
			if gh := gitgen.RealGH(); gh != "" {
				env.GHAccount = func(dir string) (ghauth.Account, error) { return ghauth.Fetch(gh, dir) }
			}
			checks := doctor.Run(c, a, res, env)
			out := cmd.OutOrStdout()
			color := colorEnabled(out)
			marks := map[doctor.Level][2]string{
				doctor.OK: {"32", "✓"}, doctor.Info: {"36", "i"}, doctor.Warn: {"33", "!"}, doctor.Fail: {"31", "✗"},
			}
			failed := 0
			for _, ch := range checks {
				m := marks[ch.Level]
				mark := m[1]
				if color {
					mark = "\x1b[" + m[0] + "m" + mark + "\x1b[0m"
				}
				fmt.Fprintf(out, "%s %s\n", mark, ch.Text)
				if ch.Fix != "" && ch.Level != doctor.OK {
					fmt.Fprintf(out, "    fix: %s\n", ch.Fix)
				}
				if ch.Level == doctor.Fail {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d problem(s) found", failed)
			}
			return nil
		},
	}
}
