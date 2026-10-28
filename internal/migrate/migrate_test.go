package migrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/importer"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sandbox(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL", "GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "EMAIL"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	write(t, filepath.Join(home, ".gitconfig"), `[user]
	name = Jane Doe
	email = jane@work.example
[init]
	defaultBranch = main
[includeIf "gitdir/i:~/Personal/"]
	path = ~/.gitconfig-personal
`)
	write(t, filepath.Join(home, ".gitconfig-personal"), `[user]
	email = jane@home.example
[core]
	sshCommand = ssh -F ~/.ssh/config-personal
[url "git@github.com:"]
	insteadOf = https://github.com/
`)
	write(t, filepath.Join(home, ".ssh", "config-personal"), "Host *\n  IdentityFile ~/.ssh/id_personal\n")
	write(t, filepath.Join(home, "Personal", ".envrc"), "export GH_CONFIG_DIR=\"$HOME/.config/gh-personal\"\n")
	write(t, filepath.Join(home, ".config", "gh-personal", "hosts.yml"), "github.com:\n    user: jane\n")
	return home
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitAuthor(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		run(t, dir, "init", "-q")
	}
	run(t, dir, "commit", "-q", "--allow-empty", "-m", "x")
	return run(t, dir, "log", "-1", "--format=%an <%ae>")
}

func TestApplyThenMigrateKeepsAuthors(t *testing.T) {
	home := sandbox(t)
	personalRepo := filepath.Join(home, "Personal", "repo")
	workRepo := filepath.Join(home, "Projects", "repo")
	check := func(stage string) {
		t.Helper()
		if got := commitAuthor(t, personalRepo); got != "Jane Doe <jane@home.example>" {
			t.Fatalf("%s: personal author = %q", stage, got)
		}
		if got := commitAuthor(t, workRepo); got != "Jane Doe <jane@work.example>" {
			t.Fatalf("%s: work author = %q", stage, got)
		}
	}
	check("before")

	res, err := importer.Import(importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config
	a, err := gitgen.NewApplier()
	if err != nil {
		t.Fatal(err)
	}
	a.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	if _, err := Run(c, a, res.Legacy, Options{}); err == nil {
		t.Fatal("migrate before apply must fail")
	}
	if _, err := a.Apply(c); err != nil {
		t.Fatal(err)
	}
	check("after apply")
	if changes, _ := a.Apply(c); len(changes) != 0 {
		t.Fatalf("second apply must not change anything: %+v", changes)
	}

	steps, err := Run(c, a, res.Legacy, Options{})
	if err != nil {
		t.Fatal(err)
	}
	check("after migrate")

	kept := 0
	for _, s := range steps {
		if s.Kept != "" {
			kept++
			if s.Legacy.Kind != importer.LegacyEnvrc {
				t.Fatalf("unexpected kept step: %+v", s)
			}
		}
	}
	if kept != 1 {
		t.Fatalf("want envrc kept, steps = %+v", steps)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig-personal")); !os.IsNotExist(err) {
		t.Fatal("legacy identity file must be removed")
	}
	global, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if strings.Contains(string(global), ".gitconfig-personal") {
		t.Fatalf("legacy include must be removed:\n%s", global)
	}
	backups, _ := filepath.Glob(filepath.Join(home, ".config", "git-idm", "backups", "*", "*"))
	if len(backups) < 2 {
		t.Fatalf("want backups of gitconfig and identity file, got %v", backups)
	}
	if got := run(t, personalRepo, "config", "core.sshCommand"); got != "ssh -i '"+home+"/.ssh/id_personal' -o IdentitiesOnly=yes" {
		t.Fatalf("sshCommand = %q", got)
	}
	if got := run(t, personalRepo, "config", "--get-regexp", `^url\..*insteadof$`); !strings.Contains(got, "https://github.com/") {
		t.Fatalf("insteadOf = %q", got)
	}

	steps, err = Run(c, a, res.Legacy, Options{Envrc: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Personal", ".envrc")); !os.IsNotExist(err) {
		t.Fatalf("envrc must be removed with --envrc: %+v", steps)
	}

	again, err := importer.Import(importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Legacy) != 0 {
		t.Fatalf("re-import must find no legacy setup: %+v", again.Legacy)
	}
}

func TestMigrateRemovesLocalAuthorKeys(t *testing.T) {
	home := sandbox(t)
	repo := filepath.Join(home, "Personal", "old")
	commitAuthor(t, repo)
	for _, args := range [][]string{
		{"config", "--add", "user.name", "Martin"}, {"config", "--add", "user.name", "Martin"},
		{"config", "user.email", "martin@old.example"}, {"config", "core.autocrlf", "input"},
	} {
		run(t, repo, args...)
	}

	res, err := importer.Import(importer.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config
	a, err := gitgen.NewApplier()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Apply(c); err != nil {
		t.Fatal(err)
	}
	legacy := importer.LocalLegacy(c, importer.ScanRules(c.Rules).Repos)
	if len(legacy) != 1 {
		t.Fatalf("legacy = %+v", legacy)
	}

	steps, err := Run(c, a, legacy, Options{DryRun: true})
	if err != nil || !strings.HasPrefix(steps[0].Done, "would remove") {
		t.Fatalf("dry run: %v %+v", err, steps)
	}
	if got := commitAuthor(t, repo); got != "Martin <martin@old.example>" {
		t.Fatalf("dry run changed the author: %q", got)
	}

	steps, err = Run(c, a, legacy, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(steps[0].Done, "removed user.name, user.email") {
		t.Fatalf("steps = %+v", steps)
	}
	if got := commitAuthor(t, repo); got != "Jane Doe <jane@home.example>" {
		t.Fatalf("author after migrate = %q", got)
	}
	if got := run(t, repo, "config", "--local", "core.autocrlf"); got != "input" {
		t.Fatalf("other keys must stay, core.autocrlf = %q", got)
	}
	backups, _ := filepath.Glob(filepath.Join(home, ".config", "git-idm", "backups", "*", "old.git-config"))
	if len(backups) != 1 {
		t.Fatalf("backup = %v", backups)
	}
	data, _ := os.ReadFile(backups[0])
	if !strings.Contains(string(data), "martin@old.example") {
		t.Fatalf("backup content:\n%s", data)
	}
	if again := importer.LocalLegacy(c, importer.ScanRules(c.Rules).Repos); len(again) != 0 {
		t.Fatalf("second scan = %+v", again)
	}
}
