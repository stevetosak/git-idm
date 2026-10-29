package gitgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stevetosak/git-idm/internal/config"
)

func script(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func shimSetup(t *testing.T) (string, *Applier, *config.Config) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	bin := filepath.Join(home, "opt", "git-idm")
	script(t, bin, `echo "git-idm $*" "shim=$GIT_IDM_GH_SHIM"`+"\n")
	script(t, filepath.Join(home, "real", "gh"), `echo "real gh $*"`+"\n")
	binDir := filepath.Join(home, ".local", "bin")
	os.MkdirAll(binDir, 0o755)
	t.Setenv("PATH", binDir+":"+filepath.Join(home, "real")+":/usr/bin:/bin")
	a := &Applier{ConfigDir: filepath.Join(home, ".config", "git-idm"), GlobalConfig: filepath.Join(home, ".gitconfig"), Bin: bin, BinDir: binDir, Now: time.Now}
	c := config.New()
	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/gh-work"}
	return home, a, c
}

func actions(changes []Change, path string) string {
	var out []string
	for _, ch := range changes {
		if ch.Path == path {
			out = append(out, ch.Action)
		}
	}
	return strings.Join(out, ",")
}

func TestApplyWritesAndRemovesShim(t *testing.T) {
	_, a, c := shimSetup(t)
	shim := a.ShimPath()

	changes, err := a.Apply(c)
	if err != nil || actions(changes, shim) != "created" {
		t.Fatalf("first apply: %v %+v", err, changes)
	}
	if info, _ := os.Stat(shim); info.Mode().Perm() != 0o755 || !IsShim(shim) {
		t.Fatalf("shim mode %v", info.Mode())
	}
	if changes, _ := a.Apply(c); actions(changes, shim) != "" {
		t.Fatalf("second apply must not touch the shim: %+v", changes)
	}

	c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example"}
	if changes, _ := a.Apply(c); actions(changes, shim) != "removed" {
		t.Fatalf("no gh logins: %+v", changes)
	}
	if _, err := os.Stat(shim); !os.IsNotExist(err) {
		t.Fatal("shim must be removed")
	}
}

func TestApplyKeepsForeignGH(t *testing.T) {
	_, a, c := shimSetup(t)
	script(t, a.ShimPath(), "echo mine\n")
	changes, err := a.Apply(c)
	if err != nil || !strings.HasPrefix(actions(changes, a.ShimPath()), "kept") {
		t.Fatalf("%v %+v", err, changes)
	}
	if data, _ := os.ReadFile(a.ShimPath()); string(data) != "#!/bin/sh\necho mine\n" {
		t.Fatalf("foreign gh changed: %q", data)
	}
}

func TestApplySkipsShimWithoutRealGH(t *testing.T) {
	home, a, c := shimSetup(t)
	os.Remove(filepath.Join(home, "real", "gh"))
	t.Setenv("PATH", a.BinDir+":"+filepath.Join(home, "real"))
	changes, err := a.Apply(c)
	if err != nil || actions(changes, a.ShimPath()) != "" {
		t.Fatalf("%v %+v", err, changes)
	}
}

func TestShimRunsGitIDMOrFallsBack(t *testing.T) {
	_, a, c := shimSetup(t)
	if _, err := a.Apply(c); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("gh", "pr", "create", "--title", "a b").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "git-idm gh pr create --title a b shim="+a.ShimPath() {
		t.Fatalf("shim: %v %q", err, out)
	}

	os.Remove(a.Bin)
	out, err = exec.Command("gh", "status").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "not found (run: git-idm doctor)") || !strings.HasSuffix(strings.TrimSpace(string(out)), "real gh status") {
		t.Fatalf("fallback: %v %q", err, out)
	}
}

func TestInternalGHSkipsShim(t *testing.T) {
	home, a, c := shimSetup(t)
	if _, err := a.Apply(c); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(home, "real", "gh")
	if got := RealGH(); got != real {
		t.Fatalf("RealGH = %q, want %q", got, real)
	}
	helper := ""
	for _, e := range IdentityEntries("work", c.Identities["work"], "", false) {
		if e.Name() == "credential.https://github.com.helper" && strings.Contains(e.Value, "git-credential") {
			helper = e.Value
		}
	}
	if !strings.Contains(helper, ShellQuote(real)+" auth git-credential") {
		t.Fatalf("credential helper = %q", helper)
	}
	if changes, err := a.Apply(c); err != nil || len(changes) != 0 {
		t.Fatalf("apply after the shim exists must not change files: %v %+v", err, changes)
	}
}
