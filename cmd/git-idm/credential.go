package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
)

const credentialLimit = 64 << 10

var runGHCredential = func(gh, configDir string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "auth", "git-credential", "get")
	env := withEnv(os.Environ(), ghConfigEnv, configDir)
	for _, key := range ghTokenEnvs {
		env = withEnv(env, key, "")
	}
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = io.Discard
	return cmd.Output()
}

func newCredentialCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "credential <identity> <get|store|erase>",
		Short:  "Git credential helper that gives only the identity's GitHub account",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCredential(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], args[1])
		},
	}
}

func runCredential(in io.Reader, out, errOut io.Writer, name, action string) error {
	input, _ := io.ReadAll(io.LimitReader(in, credentialLimit))
	attrs := readCredential(bytes.NewReader(input))
	if attrs["host"] != "github.com" || (attrs["protocol"] != "" && attrs["protocol"] != "https") {
		return nil
	}
	switch action {
	case "erase":
		printRefusedToken(errOut, name)
		return nil
	case "get":
	default:
		return nil
	}
	stop := func(reason, fix string) error {
		fmt.Fprintf(errOut, "git-idm: %s, so git stops here\n  fix: %s\n", reason, fix)
		fmt.Fprintln(out, "quit=1")
		return nil
	}
	login := "git-idm login " + name
	if config.ValidateIdentityName(name) != nil {
		return stop(fmt.Sprintf("identity name %q is not valid", name), "git-idm apply")
	}
	c, err := config.Load()
	if err != nil {
		return stop("git-idm cannot read its config", "git-idm doctor")
	}
	id, ok := c.Identities[name]
	switch {
	case !ok:
		return stop(name+" does not exist any more", "git-idm apply")
	case id.GHConfigDir == "":
		return stop(name+" has no gh login, git-idm cannot give a GitHub account for it", login)
	case id.GHUser == "":
		return stop(name+" has no gh_user, git-idm cannot check the GitHub account", login)
	}
	gh := gitgen.RealGH()
	if gh == "" {
		return stop("gh is not installed or not in PATH", "install gh, then "+login)
	}
	output, err := runGHCredential(gh, config.ExpandHome(id.GHConfigDir), input)
	result := readCredential(bytes.NewReader(output))
	switch {
	case err != nil || result["password"] == "":
		return stop(fmt.Sprintf("gh has no token for %s (%s)", name, id.GHConfigDir), login)
	case !strings.EqualFold(result["username"], id.GHUser):
		return stop(fmt.Sprintf("gh gives a token of %s, but %s uses %s", result["username"], name, id.GHUser), login)
	}
	for _, key := range []string{"protocol", "host", "username", "password"} {
		if v := result[key]; v != "" && !strings.ContainsAny(v, "\n\x00") {
			fmt.Fprintf(out, "%s=%s\n", key, v)
		}
	}
	return nil
}

func newCredentialHintCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "credential-hint <identity> <action>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, action := args[0], args[1]
			if action != "erase" {
				return nil
			}
			attrs := readCredential(cmd.InOrStdin())
			if attrs["host"] != "" && attrs["host"] != "github.com" {
				return nil
			}
			printRefusedToken(cmd.ErrOrStderr(), name)
			return nil
		},
	}
}

func printRefusedToken(w io.Writer, name string) {
	if config.ValidateIdentityName(name) != nil {
		return
	}
	account := ""
	if c, err := config.Load(); err == nil {
		id, ok := c.Identities[name]
		if !ok {
			return
		}
		if id.GHUser != "" {
			account = " (" + id.GHUser + ")"
		}
	}
	fmt.Fprintf(w, "git-idm: GitHub refused the token of %s%s. Log in again: git-idm login %s\n", name, account, name)
}

func readCredential(r io.Reader) map[string]string {
	attrs := map[string]string{}
	scanner := bufio.NewScanner(io.LimitReader(r, credentialLimit))
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
