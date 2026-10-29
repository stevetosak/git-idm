package main

import (
	"errors"
	"fmt"
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

var ghTokenEnvs = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

var ghUnchecked = map[string]bool{
	"auth": true, "help": true, "version": true, "config": true, "completion": true,
	"--version": true, "--help": true, "-h": true,
}

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
			env, target := ghEnv(os.Environ())
			if err := checkGH(args, target, env); err != nil {
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
	err      error
}

func ghEnv(environ []string) ([]string, ghTarget) {
	c, res, err := current()
	if errors.Is(err, config.ErrNotFound) {
		return environ, ghTarget{}
	}
	if err != nil {
		return environ, ghTarget{err: fmt.Errorf("git-idm cannot read its config, so it cannot check the gh login: %w", err)}
	}
	if got := envValue(environ, ghConfigEnv); got != "" && !ownedGHDir(c, got) {
		return environ, ghTarget{}
	}
	id, ok := c.Identities[res.Identity]
	if !ok {
		return withEnv(environ, ghConfigEnv, ""), ghTarget{managed: true}
	}
	return withEnv(environ, ghConfigEnv, config.ExpandHome(id.GHConfigDir)), ghTarget{name: res.Identity, identity: id, managed: true}
}

func checkGH(args []string, target ghTarget, environ []string) error {
	if len(args) == 0 || ghUnchecked[args[0]] || os.Getenv(ghCheckEnv) == "0" {
		return nil
	}
	refuse := func(reason, fix string) error {
		msg := reason + ", so gh stops here"
		if fix != "" {
			msg += "\n  fix:       " + fix
		}
		return errors.New(msg + "\n  skip once: " + ghCheckEnv + "=0 gh " + args[0] + " ...")
	}
	if target.err != nil {
		return refuse(target.err.Error(), "git-idm doctor")
	}
	if !target.managed {
		return nil
	}
	for _, key := range ghTokenEnvs {
		if envValue(environ, key) != "" {
			return refuse(fmt.Sprintf("%s is set: gh would use that token, not the gh login of %s", key, orNone(target.name)), "unset "+key)
		}
	}
	name, id := target.name, target.identity
	login := "git-idm login " + name
	switch {
	case name == "":
		return refuse("no git-idm identity uses this folder", "git-idm bind <identity>, or set a default identity")
	case id.GHConfigDir == "":
		return refuse(name+" has no gh login", login)
	case id.GHUser == "":
		return refuse(name+" has no gh_user, git-idm cannot check which account gh uses", login)
	}
	user := importer.GHUserFromConfigDir(id.GHConfigDir)
	switch {
	case user == "":
		return refuse(fmt.Sprintf("gh is not logged in for %s (%s)", name, id.GHConfigDir), login)
	case !strings.EqualFold(user, id.GHUser):
		return refuse(fmt.Sprintf("gh is logged in as %s, but %s uses %s", user, name, id.GHUser), login)
	}
	return nil
}

func orNone(name string) string {
	if name == "" {
		return "this folder"
	}
	return name
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
