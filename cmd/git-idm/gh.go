package main

import (
	"errors"
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

const (
	ghConfigEnv = "GH_CONFIG_DIR"
	ghShimEnv   = "GIT_IDM_GH_SHIM"
)

var execGH = syscall.Exec

func newGHCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "gh [gh arguments]",
		Short:              "Run gh with the gh login of the current folder",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			self, _ := gitgen.Executable()
			real, err := findGH(os.Getenv("PATH"), os.Getenv(ghShimEnv), self)
			if err != nil {
				return err
			}
			env := ghEnv(os.Environ(), cmd.ErrOrStderr())
			return execGH(real, append([]string{"gh"}, args...), env)
		},
	}
}

func findGH(path string, skip ...string) (string, error) {
	skipped := map[string]bool{}
	for _, s := range skip {
		if s == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(s); err == nil {
			skipped[resolved] = true
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "gh")
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || skipped[resolved] {
			continue
		}
		return candidate, nil
	}
	return "", errors.New("gh is not installed or not in PATH")
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
