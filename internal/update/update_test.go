package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.2", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v1.0.0-rc.1", "v1.0.0", false},
		{"v0.2.0", "dev", false},
		{"v0.2.0", "v0.0.0-20261028101010-abcdefabcdef", false},
		{"v0.2.0", "v0.0.0-20261028101010-abcdefabcdef+dirty", false},
		{"0.2.0", "v0.1.0", false},
		{"v0.2.0; rm -rf", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestSummary(t *testing.T) {
	notes := "## Changelog\n" +
		"* 0636538 release | feature: add version command\n" +
		"* 356a2e5 fix: keep \x1b[31mcolors\x1b[0m out\n" +
		"- plain line\n" +
		"* abcdef1 three\n* abcdef2 four\n* abcdef3 five\n* abcdef4 six\n\n" +
		"**Full Changelog**: https://github.com/x/y/compare/v0.1.0...v0.2.0\n"
	lines, more := Summary(notes, 5)
	want := []string{"feature: add version command", "fix: keep colors out", "plain line", "three", "four"}
	if strings.Join(lines, "|") != strings.Join(want, "|") || more != 2 {
		t.Fatalf("got %q more=%d", lines, more)
	}
}

func TestStateNoticeOncePerRelease(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	var s State
	if !s.CheckDue(t0) {
		t.Fatal("a new state must be due for a check")
	}
	s.Record(Release{Tag: "v0.2.0"}, t0)
	if s.CheckDue(t0.Add(time.Hour)) || !s.CheckDue(t0.Add(25*time.Hour)) {
		t.Fatal("the check must run once a day")
	}
	if !s.NoticeDue("v0.1.0", t0) || s.NoticeDue("v0.2.0", t0) {
		t.Fatal("the notice must show only for an older version")
	}
	s.NotifiedAt = t0
	if s.NoticeDue("v0.1.0", t0.Add(time.Hour)) {
		t.Fatal("the notice must show at most once a day")
	}
	s.Record(Release{Tag: "v0.3.0"}, t0.Add(2*time.Hour))
	if !s.NoticeDue("v0.1.0", t0.Add(2*time.Hour)) {
		t.Fatal("a new release must show at once")
	}
}

func TestStateSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "update.json")
	s := State{Latest: "v0.2.0", Notes: "n", CheckedAt: time.Unix(100, 0).UTC()}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadState(path); got.Latest != "v0.2.0" || !got.CheckedAt.Equal(s.CheckedAt) {
		t.Fatalf("got %+v", got)
	}
	os.WriteFile(path, []byte(`{"latest":"$(boom)"}`), 0o644)
	if got := LoadState(path); got.Latest != "" {
		t.Fatalf("an invalid tag must be dropped, got %+v", got)
	}
}

type fakeRelease struct {
	latest   string
	tag      string
	script   string
	tamper   bool
	requests []string
}

func (f *fakeRelease) server(t *testing.T) *Client {
	archive := tarGz(t, f.script)
	sum := sha256.Sum256(archive)
	if f.tamper {
		archive = append(archive, 0)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		switch r.URL.Path {
		case "/api/repos/" + Repo + "/releases/latest":
			if f.latest == "" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(f.latest))
		case "/dl/" + f.tag + "/checksums.txt":
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + AssetName() + "\n"))
		case "/dl/" + f.tag + "/" + AssetName():
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{API: srv.URL + "/api", Download: srv.URL + "/dl", HTTP: srv.Client()}
}

func tarGz(t *testing.T, script string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "git-idm", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestLatest(t *testing.T) {
	f := &fakeRelease{latest: `{"tag_name":"v0.2.0","body":"* abc1234 fix: x","html_url":"https://evil.example/x"}`}
	r, err := f.server(t).Latest(context.Background())
	if err != nil || r.Tag != "v0.2.0" || r.URL != "https://github.com/"+Repo+"/releases/tag/v0.2.0" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if _, err := (&fakeRelease{}).server(t).Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("got %v, want ErrNoRelease", err)
	}
	bad := &fakeRelease{latest: `{"tag_name":"v1/../../x"}`}
	if _, err := bad.server(t).Latest(context.Background()); err == nil {
		t.Fatal("an invalid tag must be rejected")
	}
}

func oldBinary(t *testing.T) string {
	exe := filepath.Join(t.TempDir(), "git-idm")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 'git-idm v0.1.0'\n"), 0o755)
	return exe
}

const newScript = "#!/bin/sh\necho 'git-idm v0.2.0'\n"

func TestInstallReplacesBinary(t *testing.T) {
	exe := oldBinary(t)
	f := &fakeRelease{tag: "v0.2.0", script: newScript}
	if err := f.server(t).Install(context.Background(), "v0.2.0", exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != newScript {
		t.Fatalf("the binary was not replaced: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".git-idm-update-*")); len(left) > 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestInstallKeepsBinaryOnFailure(t *testing.T) {
	cases := map[string]*fakeRelease{
		"checksum":      {tag: "v0.2.0", script: newScript, tamper: true},
		"wrong version": {tag: "v0.2.0", script: "#!/bin/sh\necho 'git-idm v0.1.5'\n"},
		"broken binary": {tag: "v0.2.0", script: "#!/bin/sh\nexit 3\n"},
	}
	for name, f := range cases {
		exe := oldBinary(t)
		before, _ := os.ReadFile(exe)
		if err := f.server(t).Install(context.Background(), "v0.2.0", exe); err == nil {
			t.Errorf("%s: want an error", name)
		}
		if after, _ := os.ReadFile(exe); !bytes.Equal(before, after) {
			t.Errorf("%s: the binary changed", name)
		}
		if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".git-idm-update-*")); len(left) > 0 {
			t.Errorf("%s: temp files left: %v", name, left)
		}
	}
}

func TestInstallWithoutWriteAccess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only folders")
	}
	exe := oldBinary(t)
	dir := filepath.Dir(exe)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	f := &fakeRelease{tag: "v0.2.0", script: newScript}
	if err := f.server(t).Install(context.Background(), "v0.2.0", exe); !errors.Is(err, ErrNoWriteAccess) {
		t.Fatalf("got %v, want ErrNoWriteAccess", err)
	}
}
