package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
)

func helpfulArgs(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		helpfulArgs(sub)
	}
	validate := cmd.Args
	if validate == nil {
		return
	}
	cmd.Args = func(c *cobra.Command, args []string) error {
		if err := validate(c, args); err != nil {
			return usageError(c, argsProblem(c, args, err))
		}
		return nil
	}
}

func argsProblem(cmd *cobra.Command, args []string, err error) string {
	required, optional := argNames(cmd.Use)
	switch {
	case len(args) < len(required):
		return cmd.Name() + " needs " + strings.Join(required[len(args):], " and ")
	case len(args) > len(required)+len(optional):
		extra := args[len(required)+len(optional):]
		if len(required)+len(optional) == 0 {
			return cmd.Name() + " takes no arguments, got: " + strings.Join(extra, " ")
		}
		return cmd.Name() + " got too many arguments: " + strings.Join(extra, " ")
	}
	return err.Error()
}

func argNames(use string) (required, optional []string) {
	fields := strings.Fields(use)
	if len(fields) == 0 {
		return nil, nil
	}
	for _, f := range fields[1:] {
		switch {
		case f == "|" || strings.HasPrefix(f, "-"):
		case strings.HasPrefix(f, "["):
			optional = append(optional, f)
		default:
			required = append(required, f)
		}
	}
	return required, optional
}

func usageError(cmd *cobra.Command, problem string) error {
	var b strings.Builder
	b.WriteString(problem)
	fmt.Fprintf(&b, "\n  usage:       %s", cmd.UseLine())
	if example := firstExample(cmd); example != "" {
		fmt.Fprintf(&b, "\n  example:     %s", example)
	}
	if strings.Contains(cmd.Use, "identity>") {
		if c, err := config.Load(); err == nil && len(c.Identities) > 0 {
			fmt.Fprintf(&b, "\n  identities:  %s", strings.Join(c.Names(), ", "))
		}
	}
	fmt.Fprintf(&b, "\n  help:        %s --help", cmd.CommandPath())
	return fmt.Errorf("%s", b.String())
}

func firstExample(cmd *cobra.Command) string {
	line, _, _ := strings.Cut(strings.TrimSpace(cmd.Example), "\n")
	return strings.TrimSpace(line)
}
