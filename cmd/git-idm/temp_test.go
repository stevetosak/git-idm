package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemporaryBindingEndToEnd(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not installed")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin", "git-idm")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cfg := `[identities.work]
  name = "W"
  email = "w@x.example"
[identities.client]
  name = "C"
  email = "c@x.example"
[[rules]]
  path = "~/Work/"
  identity = "work"
`
	os.MkdirAll(filepath.Join(home, ".config/git-idm"), 0o700)
	os.WriteFile(filepath.Join(home, ".config/git-idm/config.toml"), []byte(cfg), 0o600)
	script := `set -e
g=` + bin + `
mkdir -p ~/Work/acme ~/Other/repo && git init -q ~/Work/acme && git init -q ~/Other/repo
cd ~/Work/acme
$g apply >/dev/null
echo "before: $(git config user.email)"
$g bind client ~/Work/ --temp
echo "temp: $(git config user.email)"
$g guard block >/dev/null
git commit -q --allow-empty -m temp 2>&1
$g doctor 2>/dev/null | grep temporarily || true
$g guard off >/dev/null
$g unbind ~/Work/
echo "after: $(git config user.email)"
cd ~/Other/repo
$g bind client --temp
echo "new temp: $(git config user.email)"
$g unbind
echo "new after: $(git config user.email || echo none)"
`
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sh: %v\n%s", err, out)
	}
	for _, want := range []string{
		"before: w@x.example",
		"~/Work/ uses client for now, instead of work. End with: git-idm unbind ~/Work/",
		"temp: c@x.example",
		"git-idm: committing as client (temporary, was work)",
		"i ~/Work/ uses client temporarily (was work)",
		"ended the temporary binding: ~/Work/ uses work again",
		"after: w@x.example",
		"~/Other/repo/ uses client for now. End with: git-idm unbind ~/Other/repo/",
		"new temp: c@x.example",
		"ended the temporary binding of ~/Other/repo/ to client",
		"new after: none",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
