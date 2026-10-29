package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
)

const ghConfigEnv = "GH_CONFIG_DIR"

var execGH = syscall.Exec

func newGHCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "gh [gh arguments]",
		Short:              "Run gh with the gh login of the current folder",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			self, _ := gitgen.Executable()
			real, err := gitgen.FindGH(os.Getenv("PATH"), os.Getenv(gitgen.ShimEnv), self)
			if err != nil {
				return err
			}
			env := ghEnv(os.Environ(), cmd.ErrOrStderr())
			return execGH(real, append([]string{"gh"}, args...), env)
		},
	}
}

func ghEnv(environ []string, stderr io.Writer) []string {
	c, res, err := current()
	if err != nil {
		fmt.Fprintln(stderr, "git-idm: gh runs without a git-idm identity:", err)
		return environ
	}
	if got := envValue(environ, ghConfigEnv); got != "" && !ownedGHDir(c, got) {
		return environ
	}
	want := ""
	if id, ok := c.Identities[res.Identity]; ok {
		want = config.ExpandHome(id.GHConfigDir)
	}
	return withEnv(environ, ghConfigEnv, want)
}

func ownedGHDir(c *config.Config, dir string) bool {
	dir = filepath.Clean(dir)
	for _, id := range c.Identities {
		if id.GHConfigDir != "" && filepath.Clean(config.ExpandHome(id.GHConfigDir)) == dir {
			return true
		}
	}
	return false
}

func envValue(environ []string, key string) string {
	value := ""
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			value = v
		}
	}
	return value
}

func withEnv(environ []string, key, value string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	if value != "" {
		out = append(out, key+"="+value)
	}
	return out
}
