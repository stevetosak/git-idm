package migrate

import (
	"fmt"
	"os"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
)

type Step struct {
	Legacy importer.Legacy
	Done   string
	Kept   string
}

type Options struct {
	Envrc  bool
	DryRun bool
}

func Run(c *config.Config, a *gitgen.Applier, legacy []importer.Legacy, opts Options) ([]Step, error) {
	if !a.Installed() {
		return nil, fmt.Errorf("git-idm is not applied yet (run `git-idm apply` first)")
	}
	covered := map[string]bool{}
	for _, r := range c.Rules {
		covered[r.Path] = true
	}
	var steps []Step
	for _, l := range legacy {
		step := Step{Legacy: l}
		switch {
		case !covered[l.RulePath]:
			step.Kept = fmt.Sprintf("no git-idm rule for %s", l.RulePath)
		case l.Kind == importer.LegacyEnvrc && !opts.Envrc:
			step.Kept = "needs the shell hook (use --envrc after `git-idm hook` is set up)"
		case opts.DryRun:
			step.Done = "would " + l.Detail
		default:
			done, err := apply(a, l)
			if err != nil {
				return steps, fmt.Errorf("%s %s: %w", l.Kind, l.Path, err)
			}
			step.Done = done
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func apply(a *gitgen.Applier, l importer.Legacy) (string, error) {
	path := config.ExpandHome(l.Path)
	switch l.Kind {
	case importer.LegacyInclude:
		backup, removed, err := a.RemoveLegacySection("includeIf", l.Condition)
		if err != nil {
			return "", err
		}
		if removed == 0 {
			return "already gone", nil
		}
		return fmt.Sprintf("removed includeIf %q (backup: %s)", l.Condition, config.ContractHome(backup)), nil
	case importer.LegacyFile, importer.LegacyEnvrc:
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "already gone", nil
		}
		backup, err := a.Backup(path)
		if err != nil {
			return "", err
		}
		if l.Kind == importer.LegacyEnvrc && !strings.HasPrefix(l.Detail, "delete") {
			if err := removeGHConfigDirLine(path); err != nil {
				return "", err
			}
			return fmt.Sprintf("removed GH_CONFIG_DIR line, run `direnv allow` there (backup: %s)", config.ContractHome(backup)), nil
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted (backup: %s)", config.ContractHome(backup)), nil
	}
	return "", fmt.Errorf("unknown legacy kind %q", l.Kind)
}

func removeGHConfigDirLine(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if !strings.Contains(line, "GH_CONFIG_DIR=") {
			kept = append(kept, line)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "")), info.Mode().Perm())
}
