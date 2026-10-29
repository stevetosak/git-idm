package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
	"github.com/stevetosak/git-idm/internal/migrate"
	"github.com/stevetosak/git-idm/internal/resolve"
	"github.com/stevetosak/git-idm/internal/setup"
)

func newApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Write the git config again from config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			return applyConfig(cmd.OutOrStdout(), c)
		},
	}
}

func newBindCmd() *cobra.Command {
	var temporary bool
	cmd := &cobra.Command{
		Use:   "bind [identity] [dir]",
		Short: "Use an identity for every repo in a folder",
		Long: "Use an identity for every repo in a folder. Without a folder, bind the current repo\n" +
			"(or the current folder outside a repo). Without an identity, use the default identity;\n" +
			"in a terminal, pick one from a list with the default first.",
		Example: "  git-idm bind                 bind this repo to the default identity\n" +
			"  git-idm bind work ~/Projects\n" +
			"  git-idm bind client ~/Projects/acme --temp   end with: git-idm unbind ~/Projects/acme",
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			var rulePath string
			if len(args) == 2 {
				rulePath, err = explicitBindPath(args[1])
			} else {
				rulePath, err = currentBindPath(cmd.OutOrStdout())
			}
			if err != nil {
				return err
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			} else if name, err = pickBindIdentity(cmd, c, rulePath); err != nil {
				return err
			}
			rule, err := c.Bind(rulePath, name, temporary)
			if err != nil {
				return err
			}
			if err := c.Save(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case rule.Temporary && rule.Was != "":
				fmt.Fprintf(out, "%s uses %s for now, instead of %s. End with: git-idm unbind %s\n", rulePath, rule.Identity, rule.Was, rulePath)
			case rule.Temporary:
				fmt.Fprintf(out, "%s uses %s for now. End with: git-idm unbind %s\n", rulePath, rule.Identity, rulePath)
			default:
				fmt.Fprintf(out, "bound %s to %s\n", rulePath, rule.Identity)
			}
			return applyConfig(out, c)
		},
	}
	cmd.Flags().BoolVar(&temporary, "temp", false, "until you run git-idm unbind; then the folder uses its old identity again")
	return cmd
}

func newUnbindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unbind [dir]",
		Short: "Stop using an identity in a folder",
		Long:  "Stop using an identity in a folder. For a temporary binding, the folder uses its old identity again.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			rulePath, err := rulePathFor(args)
			if err != nil {
				return err
			}
			old, err := c.Unbind(rulePath)
			if err != nil {
				return err
			}
			if err := c.Save(); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case old.Temporary && old.Was != "":
				fmt.Fprintf(out, "ended the temporary binding: %s uses %s again\n", rulePath, old.Was)
			case old.Temporary:
				fmt.Fprintf(out, "ended the temporary binding of %s to %s\n", rulePath, old.Identity)
			default:
				fmt.Fprintf(out, "unbound %s\n", rulePath)
			}
			return applyConfig(out, c)
		},
	}
}

func newMigrateCmd() *cobra.Command {
	var opts migrate.Options
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Remove the old setup that git-idm replaced",
		Long:  "Remove old includeIf entries, identity files, .envrc lines and repo author keys that git-idm rules now cover.\nEvery changed file is backed up first. Then offer to bind repos that have no identity yet.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			a, err := gitgen.NewApplier()
			if err != nil {
				return err
			}
			res, err := importer.Import(importer.Options{DefaultName: c.Default})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			scan := importer.ScanRules(c.Rules)
			if scan.Truncated {
				fmt.Fprintln(out, "repo scan stopped early, run migrate again for the rest")
			}
			legacy := mergeLegacy(res.Legacy, importer.EnvrcLegacy(c.Rules), importer.LocalLegacy(c, scan.Repos))
			home := importer.ScanHome()
			groups := importer.UnboundGroups(c, home.Repos)
			if len(legacy) == 0 && len(groups) == 0 {
				fmt.Fprintln(out, "nothing to migrate")
				return nil
			}
			if len(legacy) > 0 {
				steps, err := migrate.Run(c, a, legacy, opts)
				printSteps(out, steps)
				if err != nil {
					return err
				}
			}
			return bindUnbound(cmd, out, c, groups, opts.DryRun)
		},
	}
	cmd.Flags().BoolVar(&opts.Envrc, "envrc", false, "also migrate GH_CONFIG_DIR out of .envrc files")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show what would change")
	return cmd
}

func bindUnbound(cmd *cobra.Command, out io.Writer, c *config.Config, groups []importer.UnboundGroup, dryRun bool) error {
	if len(groups) == 0 {
		return nil
	}
	fmt.Fprintln(out, "\nRepos without an identity (git refuses to commit there while the guard is on):")
	for _, g := range groups {
		fmt.Fprintf(out, "  %s  %s\n", g.RulePath(), repoCount(g))
	}
	prompter, err := newPrompter(cmd)
	if dryRun || err != nil {
		fmt.Fprintln(out, "\nBind them with:")
		for _, g := range groups {
			name := g.Suggested
			if name == "" {
				name = "<identity>"
			}
			fmt.Fprintf(out, "  git-idm bind %s %s\n", name, g.RulePath())
		}
		return nil
	}
	var bound []string
	for _, g := range groups {
		var choices []setup.Choice
		if g.Suggested != "" {
			choices = append(choices, setup.Choice{Key: g.Suggested, Label: g.Suggested + " (suggested)  " + c.Identities[g.Suggested].Email})
		}
		for _, name := range c.Names() {
			if name != g.Suggested {
				choices = append(choices, setup.Choice{Key: name, Label: name + "  " + c.Identities[name].Email})
			}
		}
		choices = append(choices, setup.Choice{Key: "", Label: "Skip, leave it without an identity"})
		pick, err := prompter.Select("Bind "+g.RulePath(), repoCount(g)+": "+strings.Join(contractAll(g.Repos, 3), ", "), choices, choices[0].Key)
		if err != nil {
			if len(bound) == 0 {
				return ignoreAbort(err)
			}
			break
		}
		if pick == "" {
			continue
		}
		if _, err := c.Bind(g.RulePath(), pick, false); err != nil {
			return err
		}
		bound = append(bound, fmt.Sprintf("bound %s to %s", g.RulePath(), pick))
	}
	if len(bound) == 0 {
		return nil
	}
	if err := c.Save(); err != nil {
		return err
	}
	for _, line := range bound {
		fmt.Fprintln(out, "  "+line)
	}
	return applyConfig(out, c)
}

func repoCount(g importer.UnboundGroup) string {
	if len(g.Repos) == 1 {
		return "1 repo"
	}
	return fmt.Sprintf("%d repos", len(g.Repos))
}

func contractAll(paths []string, max int) []string {
	var out []string
	for i, p := range paths {
		if i == max {
			out = append(out, fmt.Sprintf("+%d more", len(paths)-max))
			break
		}
		out = append(out, config.ContractHome(p))
	}
	return out
}

func printSteps(out io.Writer, steps []migrate.Step) {
	for _, s := range steps {
		if s.Done != "" {
			fmt.Fprintf(out, "  done  %-7s %s: %s\n", s.Legacy.Kind, s.Legacy.Path, s.Done)
		} else {
			fmt.Fprintf(out, "  kept  %-7s %s: %s\n", s.Legacy.Kind, s.Legacy.Path, s.Kept)
		}
	}
}

func mergeLegacy(lists ...[]importer.Legacy) []importer.Legacy {
	seen := map[string]bool{}
	var out []importer.Legacy
	for _, list := range lists {
		for _, l := range list {
			key := string(l.Kind) + "|" + l.Path + "|" + l.Condition
			if !seen[key] {
				seen[key] = true
				out = append(out, l)
			}
		}
	}
	return out
}

func applyConfig(out io.Writer, c *config.Config) error {
	a, err := gitgen.NewApplier()
	if err != nil {
		return err
	}
	changes, err := a.Apply(c)
	for _, ch := range changes {
		fmt.Fprintf(out, "  %-8s %s\n", ch.Action, config.ContractHome(ch.Path))
	}
	if err == nil && len(changes) == 0 {
		fmt.Fprintln(out, "  git config is up to date")
	}
	return err
}

func currentBindPath(out io.Writer) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if root, gitdir := resolve.RepoAt(cwd); root != "" {
		cwd = resolve.BindRoot(root, gitdir)
		if cwd != root {
			fmt.Fprintf(out, "%s keeps its git data in %s, so git uses the rule of %s\n", config.ContractHome(root), config.ContractHome(gitdir), config.ContractHome(cwd))
		}
	}
	return rulePathFor([]string{cwd})
}

func explicitBindPath(dir string) (string, error) {
	abs, err := filepath.Abs(config.ExpandHome(dir))
	if err != nil {
		return "", err
	}
	if root, gitdir := resolve.RepoAt(abs); root == abs {
		if owner := resolve.BindRoot(root, gitdir); owner != root {
			return "", fmt.Errorf("%s keeps its git data in %s, so a rule for it has no effect; bind %s instead", config.ContractHome(root), config.ContractHome(gitdir), config.ContractHome(owner))
		}
	}
	return rulePathFor([]string{dir})
}

func pickBindIdentity(cmd *cobra.Command, c *config.Config, rulePath string) (string, error) {
	if len(c.Identities) == 0 {
		return "", fmt.Errorf("no identities yet: git-idm add")
	}
	prompter, err := newPrompter(cmd)
	if err != nil {
		if c.Default == "" {
			return "", fmt.Errorf("no default identity: git-idm bind <identity>")
		}
		return c.Default, nil
	}
	var choices []setup.Choice
	if c.Default != "" {
		choices = append(choices, setup.Choice{Key: c.Default, Label: c.Default + " (default)  " + c.Identities[c.Default].Email})
	}
	for _, name := range c.Names() {
		if name != c.Default {
			choices = append(choices, setup.Choice{Key: name, Label: name + "  " + c.Identities[name].Email})
		}
	}
	return prompter.Select("Bind "+rulePath+" to", "git-idm uses this identity for commits, pushes and gh in this folder.", choices, choices[0].Key)
}

func rulePathFor(args []string) (string, error) {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	abs, err := filepath.Abs(config.ExpandHome(dir))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	rule := config.Rule{Path: config.ContractHome(abs) + "/"}
	if abs == "/" {
		rule.Path = "/"
	}
	return rule.Path, rule.Validate()
}
