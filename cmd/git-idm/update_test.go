package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/update"
	"github.com/stevetosak/git-idm/internal/version"
)

const releaseNotes = "## Changelog\\n* 1111111 release | feature: self update\\n* 2222222 fix: login hint\\n"

func newUpdateEnv(t *testing.T, current, latest string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	oldVersion, oldClient, oldExe := version.Version, newUpdateClient, executablePath
	t.Cleanup(func() { version.Version, newUpdateClient, executablePath = oldVersion, oldClient, oldExe })
	version.Version = current

	script := "#!/bin/sh\necho 'git-idm " + latest + "'\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "git-idm", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/" + update.Repo + "/releases/latest":
			w.Write([]byte(`{"tag_name":"` + latest + `","body":"` + releaseNotes + `"}`))
		case "/dl/" + latest + "/checksums.txt":
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + update.AssetName() + "\n"))
		case "/dl/" + latest + "/" + update.AssetName():
			w.Write(buf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	newUpdateClient = func() *update.Client {
		return &update.Client{API: srv.URL + "/api", Download: srv.URL + "/dl", HTTP: srv.Client()}
	}
	exe := filepath.Join(t.TempDir(), "git-idm")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 'git-idm "+current+"'\n"), 0o755)
	executablePath = func() (string, error) { return exe, nil }
	return exe
}

func runUpdateCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"update"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("update %v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestUpdateCheckShowsNotes(t *testing.T) {
	exe := newUpdateEnv(t, "v0.1.0", "v0.2.0")
	out := runUpdateCmd(t, "--check")
	for _, want := range []string{"git-idm v0.2.0 is available", "you have v0.1.0", "• feature: self update", "• fix: login hint", "git-idm update"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got, _ := os.ReadFile(exe); !strings.Contains(string(got), "v0.1.0") {
		t.Error("--check must not replace the binary")
	}
	path, _ := updateStatePath()
	if s := update.LoadState(path); s.Latest != "v0.2.0" {
		t.Errorf("the check must save the latest release, got %+v", s)
	}
}

func TestUpdateReplacesBinary(t *testing.T) {
	exe := newUpdateEnv(t, "v0.1.0", "v0.2.0")
	out := runUpdateCmd(t)
	if !strings.Contains(out, "Updating git-idm v0.1.0 → v0.2.0") || !strings.Contains(out, "✓ updated to git-idm v0.2.0") {
		t.Errorf("unexpected output:\n%s", out)
	}
	if got, _ := os.ReadFile(exe); !strings.Contains(string(got), "v0.2.0") {
		t.Errorf("the binary was not replaced: %s", got)
	}
}

func TestUpdateWhenLatest(t *testing.T) {
	newUpdateEnv(t, "v0.2.0", "v0.2.0")
	if out := runUpdateCmd(t); !strings.Contains(out, "✓ git-idm v0.2.0 is the latest version") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestUpdateDevBuild(t *testing.T) {
	exe := newUpdateEnv(t, "", "v0.2.0")
	out := runUpdateCmd(t)
	if !strings.Contains(out, "local build") || !strings.Contains(out, update.InstallLine) {
		t.Errorf("unexpected output:\n%s", out)
	}
	if got, _ := os.ReadFile(exe); strings.Contains(string(got), "v0.2.0") {
		t.Error("a dev build must not replace itself")
	}
}

func TestManagedUpdateHint(t *testing.T) {
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "/home/u/go")
	cases := map[string]string{
		"/opt/homebrew/Cellar/git-idm/0.1.0/bin/git-idm": "brew upgrade git-idm",
		"/home/u/go/bin/git-idm":                         "go install github.com/stevetosak/git-idm/cmd/git-idm@latest",
		"/home/u/.local/bin/git-idm":                     "",
	}
	for exe, want := range cases {
		if got := managedUpdateHint(exe); got != want {
			t.Errorf("%s: got %q, want %q", exe, got, want)
		}
	}
}
