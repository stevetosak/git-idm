package ghauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyLogin(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "gh")
	os.MkdirAll(from, 0o755)
	os.WriteFile(filepath.Join(from, "hosts.yml"), []byte("github.com:\n    user: u\n"), 0o644)
	os.WriteFile(filepath.Join(from, "config.yml"), []byte("git_protocol: ssh\n"), 0o644)
	to := filepath.Join(dir, "own", "work")

	if err := CopyLogin(from, to); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hosts.yml", "config.yml"} {
		info, err := os.Stat(filepath.Join(to, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, err, info)
		}
	}
	if info, _ := os.Stat(to); info.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", info.Mode())
	}
	if err := CopyLogin(from, to); err == nil {
		t.Fatal("an existing login must never be overwritten")
	}
	if err := CopyLogin("relative", to+"2"); err == nil {
		t.Fatal("relative paths must be refused")
	}
	if err := CopyLogin(from, from); err == nil {
		t.Fatal("same folder must be refused")
	}
	empty := filepath.Join(dir, "empty")
	os.MkdirAll(empty, 0o700)
	if err := CopyLogin(empty, filepath.Join(dir, "x")); err == nil {
		t.Fatal("a folder without a login must be refused")
	}
	linked := filepath.Join(dir, "linked")
	os.MkdirAll(linked, 0o700)
	os.Symlink(filepath.Join(from, "hosts.yml"), filepath.Join(linked, "hosts.yml"))
	if err := CopyLogin(linked, filepath.Join(dir, "y")); err == nil {
		t.Fatal("a symlinked hosts.yml must be refused")
	}
}
