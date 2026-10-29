package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/ui"
	"github.com/stevetosak/git-idm/internal/update"
	"github.com/stevetosak/git-idm/internal/version"
)

var (
	newUpdateClient = update.NewClient
	executablePath  = currentExecutable
	now             = time.Now
)

var quietCommands = map[string]bool{
	"check-commit": true, "check-push": true, "credential-hint": true, "credential": true, "ssh": true, "env": true, "gh": true, "prompt": true, "hook": true, "update": true, "update-check": true,
	"completion": true, cobra.ShellCompRequestCmd: true, cobra.ShellCompNoDescRequestCmd: true,
}

func newUpdateCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update git-idm to the latest release",
		Long: "Download the latest release from GitHub, check its checksum and replace this binary.\n" +
			"git-idm also checks for a new release once a day and shows a short notice.\n" +
			"Set GIT_IDM_NO_UPDATE_CHECK=1 to turn the daily check off.",
		Example: "  git-idm update\n  git-idm update --check",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only show if a new release is available")
	return cmd
}

func newUpdateCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "update-check",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			_, err := fetchLatest(ctx)
			return err
		},
	}
}

func runUpdate(cmd *cobra.Command, check bool) error {
	out := cmd.OutOrStdout()
	current := version.Current()
	if !update.IsRelease(current) {
		fmt.Fprintf(out, "git-idm %s is a local build, it does not update itself.\nInstall a release:\n  %s\n", current, update.InstallLine)
		return nil
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	latest, err := fetchLatest(ctx)
	if errors.Is(err, update.ErrNoRelease) {
		fmt.Fprintln(out, "No release is published yet.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	if !update.Newer(latest.Tag, current) {
		fmt.Fprintf(out, "✓ git-idm %s is the latest version\n", current)
		return nil
	}
	if check {
		printNotice(out, current, latest)
		return nil
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	if hint := managedUpdateHint(exe); hint != "" {
		fmt.Fprintf(out, "git-idm %s is available. This binary is managed by another tool, update it with:\n  %s\n", latest.Tag, hint)
		return nil
	}
	fmt.Fprintf(out, "Updating git-idm %s → %s\n", current, latest.Tag)
	err = newUpdateClient().Install(ctx, latest.Tag, exe)
	if errors.Is(err, update.ErrNoWriteAccess) {
		return fmt.Errorf("%w, run: sudo git-idm update", err)
	}
	if err != nil {
		return fmt.Errorf("update failed, git-idm %s is unchanged: %w", current, err)
	}
	printChanges(out, latest)
	fmt.Fprintf(out, "✓ updated to git-idm %s (%s)\n", latest.Tag, exe)
	return nil
}

func fetchLatest(ctx context.Context) (update.Release, error) {
	latest, err := newUpdateClient().Latest(ctx)
	if err != nil {
		return latest, err
	}
	if path, perr := updateStatePath(); perr == nil {
		state := update.LoadState(path)
		state.Record(latest, now())
		_ = state.Save(path)
	}
	return latest, nil
}

func printNotice(w io.Writer, current string, r update.Release) {
	t := ui.New(w)
	fmt.Fprintf(w, "%s (you have %s)\n", t.Bold("git-idm "+r.Tag+" is available"), current)
	printChanges(w, r)
	fmt.Fprintf(w, "Update: git-idm update\n")
}

func printChanges(w io.Writer, r update.Release) {
	lines, more := update.Summary(r.Notes, 5)
	for _, line := range lines {
		fmt.Fprintf(w, "  • %s\n", line)
	}
	if more > 0 {
		fmt.Fprintf(w, "  … %d more: %s\n", more, r.URL)
	} else if len(lines) == 0 && r.URL != "" {
		fmt.Fprintf(w, "  release notes: %s\n", r.URL)
	}
}

func notifyUpdate(cmd *cobra.Command, stderr *os.File) {
	current := version.Current()
	if cmd == nil || quietCommands[cmd.Name()] || !update.IsRelease(current) ||
		os.Getenv("GIT_IDM_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" || !isTerminal(stderr) {
		return
	}
	path, err := updateStatePath()
	if err != nil {
		return
	}
	state := update.LoadState(path)
	t := now()
	changed := false
	if state.NoticeDue(current, t) {
		fmt.Fprintln(stderr)
		printNotice(stderr, current, update.Release{Tag: state.Latest, Notes: state.Notes, URL: state.URL})
		state.NotifiedAt, changed = t, true
	}
	if state.CheckDue(t) {
		state.CheckedAt, changed = t, true
		defer startBackgroundCheck()
	}
	if changed {
		_ = state.Save(path)
	}
}

func startBackgroundCheck() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	c := exec.Command(exe, "update-check")
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if c.Start() == nil {
		_ = c.Process.Release()
	}
}

func updateStatePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update.json"), nil
}

func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func managedUpdateHint(exe string) string {
	if strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/") || strings.Contains(exe, "/linuxbrew/") {
		return "brew upgrade git-idm"
	}
	goBin := os.Getenv("GOBIN")
	if goBin == "" {
		goPath := os.Getenv("GOPATH")
		if goPath == "" {
			goPath = config.ExpandHome("~/go")
		}
		goBin = filepath.Join(goPath, "bin")
	}
	if filepath.Dir(exe) == filepath.Clean(goBin) {
		return "go install github.com/" + update.Repo + "/cmd/git-idm@latest"
	}
	return ""
}
