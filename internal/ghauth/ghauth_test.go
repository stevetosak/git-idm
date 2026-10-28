package ghauth

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseAccountScopes(t *testing.T) {
	body := `{"login":"jane","id":7}`
	cases := []struct {
		name     string
		response string
		scopes   []string
		canAdd   bool
	}{
		{"no headers", body, nil, true},
		{"missing scope", "HTTP/2.0 200 OK\nX-Oauth-Scopes: gist, read:org, repo\n\n" + body, []string{"gist", "read:org", "repo"}, false},
		{"admin scope", "HTTP/2.0 200 OK\r\nX-OAuth-Scopes: repo, admin:public_key\r\n\r\n" + body, []string{"repo", "admin:public_key"}, true},
		{"write scope", "HTTP/2.0 200 OK\nX-Oauth-Scopes: write:public_key\n\n" + body, []string{"write:public_key"}, true},
		{"empty scopes", "HTTP/2.0 200 OK\nX-Oauth-Scopes: \n\n" + body, []string{}, false},
	}
	for _, tc := range cases {
		a, err := Parse(tc.response)
		if err != nil || a.Login != "jane" || !slices.Equal(a.Scopes, tc.scopes) || a.CanAddKeys() != tc.canAdd {
			t.Errorf("%s: %+v %v canAdd=%v", tc.name, a, err, a.CanAddKeys())
		}
	}
	if _, err := Parse("HTTP/2.0 401 Unauthorized\n\n{\"message\":\"Bad credentials\"}"); err == nil {
		t.Error("want error without a login")
	}
}

func fakeGH(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFetch(t *testing.T) {
	ok := fakeGH(t, `[ "$GH_CONFIG_DIR" = "/cfg" ] || exit 9
printf 'HTTP/2.0 200 OK\nX-Oauth-Scopes: repo\n\n{"login":"jane","id":7}'
`)
	a, err := Fetch(ok, "/cfg")
	if err != nil || a.Login != "jane" || a.CanAddKeys() {
		t.Fatalf("%+v %v", a, err)
	}

	revoked := fakeGH(t, `printf 'HTTP/2.0 401 Unauthorized\n\n{"message":"Bad credentials"}'
echo "gh: Bad credentials (HTTP 401)" >&2
exit 1
`)
	if _, err := Fetch(revoked, "/cfg"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("revoked: %v", err)
	}

	noToken := fakeGH(t, `echo "To get started with GitHub CLI, please run:  gh auth login" >&2
exit 4
`)
	if _, err := Fetch(noToken, "/cfg"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("no token: %v", err)
	}

	offline := fakeGH(t, `echo 'error connecting to api.github.com' >&2
echo 'check your internet connection or https://githubstatus.com' >&2
exit 1
`)
	if _, err := Fetch(offline, "/cfg"); !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "error connecting") {
		t.Errorf("offline: %v", err)
	}

	serverError := fakeGH(t, `printf 'HTTP/2.0 500 Internal Server Error\n\n{}'
echo "gh: HTTP 500" >&2
exit 1
`)
	if _, err := Fetch(serverError, "/cfg"); err == nil || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrUnreachable) {
		t.Errorf("server error: %v", err)
	}

	defer func(old time.Duration) { Timeout = old }(Timeout)
	Timeout = 200 * time.Millisecond
	slow := fakeGH(t, "exec sleep 5\n")
	start := time.Now()
	if _, err := Fetch(slow, "/cfg"); !errors.Is(err, ErrUnreachable) || time.Since(start) > 3*time.Second {
		t.Errorf("slow: %v after %s", err, time.Since(start))
	}
}
