package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/guard"
)

var confirmOnTerminal = askTerminal

func askTerminal(question string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer tty.Close()
	fmt.Fprint(tty, question)
	answer, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && answer == "" {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func newAcceptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accept",
		Short: "Approve the commits that the push guard blocked",
		Long: "Shows the commits that the last blocked push stopped and asks you to approve them.\n" +
			"The next push accepts only these commits. The account checks still run.\n" +
			"It asks on the terminal, so an agent or script cannot approve.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			pending, err := guard.Pending(dir)
			if err != nil {
				return err
			}
			if len(pending) == 0 {
				return errors.New("no blocked commits to accept: run git push first")
			}
			summary, err := guard.DescribeCommits(dir, pending)
			if err != nil {
				return errors.New("the blocked commits changed since the push: run git push again")
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "The last push was blocked for %d commit(s):\n%s\n", len(pending), indent(summary, "  "))
			ok, err := confirmOnTerminal(fmt.Sprintf("Accept these %d commit(s) for push? [y/N] ", len(pending)))
			if err != nil {
				return fmt.Errorf("git-idm accept must ask on a terminal, so only a person can approve (%v)\n  agents: ask the user to run git-idm accept", err)
			}
			if !ok {
				fmt.Fprintln(out, "nothing accepted")
				return nil
			}
			if err := guard.AcceptPending(dir, pending); err != nil {
				return err
			}
			fmt.Fprintf(out, "accepted %d commit(s), run git push again\n", len(pending))
			return nil
		},
	}
}
