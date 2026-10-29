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
	"github.com/stevetosak/git-idm/internal/importer"
)

const (
	ghConfigEnv = "GH_CONFIG_DIR"
	ghCheckEnv  = "GIT_IDM_GH_CHECK"
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
			real, err := gitgen.FindGH(os.Getenv("PATH"), os.Getenv(gitgen.ShimEnv), self)
			if err != nil {
				return err
			}
			env, target := ghEnv(os.Environ(), cmd.ErrOrStderr())
			if err := checkGHLogin(args, target, envValue(env, ghConfigEnv)); err != nil {
				return err
			}
			return execGH(real, append([]string{"gh"}, args...), env)
		},
	}
}

type ghTarget struct {
	name     string
	identity config.Identity
	managed  bool
}

func ghEnv(environ []string, stderr io.Writer) ([]string, ghTarget) {
	c, res, err := current()
	if err != nil {
		fmt.Fprintln(stderr, "git-idm: gh runs without a git-idm identity:", err)
		return environ, ghTarget{}
	}
	if got := envValue(environ, ghConfigEnv); got != "" && !ownedGHDir(c, got) {
		return environ, ghTarget{}
	}
	id, ok := c.Identities[res.Identity]
	if !ok {
		return withEnv(environ, ghConfigEnv, ""), ghTarget{}
	}
	return withEnv(environ, ghConfigEnv, config.ExpandHome(id.GHConfigDir)), ghTarget{name: res.Identity, identity: id, managed: true}
}

var ghAuthoring = map[string]bool{
	"pr create": true, "pr comment": true, "pr review": true, "pr merge": true,
	"issue create": true, "issue comment": true, "release create": true,
}

func checkGHLogin(args []string, target ghTarget, configDir string) error {
	if os.Getenv(ghCheckEnv) == "0" || len(args) < 2 || !ghAuthoring[args[0]+" "+args[1]] {
		return nil
	}
	if !target.managed || target.identity.GHUser == "" {
		return nil
	}
	if configDir == "" {
		configDir = importer.DefaultGHConfigDir()
	}
	user := importer.GHUserFromConfigDir(configDir)
	if user == "" || strings.EqualFold(user, target.identity.GHUser) {
		return nil
	}
	return fmt.Errorf("gh is logged in as %s, but %s uses %s, so `gh %s %s` stops here\n"+
		"  fix:       git-idm login %s\n"+
		"  skip once: %s=0 gh %s %s ...",
		user, target.name, target.identity.GHUser, args[0], args[1], target.name, ghCheckEnv, args[0], args[1])
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
