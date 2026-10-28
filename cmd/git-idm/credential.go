package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
)

func newCredentialHintCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "credential-hint <identity> <action>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, action := args[0], args[1]
			if action != "erase" || config.ValidateIdentityName(name) != nil {
				return nil
			}
			attrs := readCredential(cmd.InOrStdin())
			if attrs["host"] != "" && attrs["host"] != "github.com" {
				return nil
			}
			account := ""
			if c, err := config.Load(); err == nil {
				id, ok := c.Identities[name]
				if !ok {
					return nil
				}
				if id.GHUser != "" {
					account = " (" + id.GHUser + ")"
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "git-idm: GitHub refused the token of %s%s. Log in again: git-idm login %s\n", name, account, name)
			return nil
		},
	}
}

func readCredential(r io.Reader) map[string]string {
	attrs := map[string]string{}
	scanner := bufio.NewScanner(io.LimitReader(r, 64<<10))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			attrs[key] = value
		}
	}
	return attrs
}
