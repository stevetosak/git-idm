package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stevetosak/git-idm/internal/config"
)

func runCredentialHint(t *testing.T, input string, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	root := newRootCmd()
	root.SetIn(strings.NewReader(input))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"credential-hint"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.Len() > 0 {
		t.Fatalf("a credential helper must not print to stdout: %q", out.String())
	}
	return errOut.String()
}

func TestCredentialHint(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := config.New()
	c.Identities["personal"] = config.Identity{Name: "Jane", Email: "jane@example.com", GHUser: "jane-gh", GHConfigDir: "~/gh"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	github := "protocol=https\nhost=github.com\nusername=x\npassword=y\n\n"
	if got := runCredentialHint(t, github, "personal", "erase"); got != "git-idm: GitHub refused the token of personal (jane-gh). Log in again: git-idm login personal\n" {
		t.Errorf("erase: %q", got)
	}
	quiet := map[string][]string{
		"get":          {"personal", "get"},
		"store":        {"personal", "store"},
		"bad name":     {"../x", "erase"},
		"unknown name": {"nope", "erase"},
	}
	for name, args := range quiet {
		if got := runCredentialHint(t, github, args...); got != "" {
			t.Errorf("%s: want no output, got %q", name, got)
		}
	}
	if got := runCredentialHint(t, "protocol=https\nhost=gitlab.com\n\n", "personal", "erase"); got != "" {
		t.Errorf("other host: %q", got)
	}
}

func TestCredentialGivesOnlyTheIdentityAccount(t *testing.T) {
	github := "protocol=https\nhost=github.com\npath=o/r.git\n\n"
	cases := []struct {
		name, input, identity, action, ghOut string
		ghErr                                bool
		setup                                func(t *testing.T, dir string)
		want, stderr                         string
	}{
		{name: "right account", identity: "work", ghOut: "protocol=https\nhost=github.com\nusername=W-GH\npassword=tok\n",
			want: "protocol=https\nhost=github.com\nusername=W-GH\npassword=tok\n"},
		{name: "fallback token", identity: "work", ghOut: "username=x-access-token\npassword=tok\n",
			want: "quit=1\n", stderr: "gh gives a token of x-access-token, but work uses w-gh, so git stops here\n  fix: git-idm login work"},
		{name: "other account", identity: "work", ghOut: "username=martin\npassword=tok\n", want: "quit=1\n", stderr: "gh gives a token of martin"},
		{name: "no token", identity: "work", ghOut: "username=w-gh\n", want: "quit=1\n", stderr: "gh has no token for work (~/gh-work)"},
		{name: "gh fails", identity: "work", ghErr: true, want: "quit=1\n", stderr: "gh has no token for work"},
		{name: "no gh login", identity: "plain", want: "quit=1\n", stderr: "plain has no gh login"},
		{name: "no gh_user", identity: "nouser", want: "quit=1\n", stderr: "nouser has no gh_user"},
		{name: "removed identity", identity: "gone", want: "quit=1\n", stderr: "gone does not exist any more"},
		{name: "bad name", identity: "../x", want: "quit=1\n", stderr: "is not valid"},
		{name: "broken config", identity: "work", want: "quit=1\n", stderr: "git-idm cannot read its config",
			setup: func(t *testing.T, dir string) {
				os.WriteFile(filepath.Join(dir, "git-idm", "config.toml"), []byte("x ["), 0o600)
			}},
		{name: "gh missing", identity: "work", want: "quit=1\n", stderr: "gh is not installed",
			setup: func(t *testing.T, dir string) { t.Setenv("PATH", dir) }},
		{name: "other host", identity: "work", input: "protocol=https\nhost=gitlab.com\n\n"},
		{name: "store", identity: "work", action: "store"},
		{name: "erase", identity: "work", action: "erase", stderr: "GitHub refused the token of work (w-gh)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			c := config.New()
			c.Identities["work"] = config.Identity{Name: "W", Email: "w@x.example", GHConfigDir: "~/gh-work", GHUser: "w-gh"}
			c.Identities["plain"] = config.Identity{Name: "P", Email: "p@x.example"}
			c.Identities["nouser"] = config.Identity{Name: "N", Email: "n@x.example", GHConfigDir: "~/gh-n"}
			if err := c.Save(); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(dir, "tools", "gh")
			executable(t, real)
			t.Setenv("PATH", filepath.Dir(real))
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			old := runGHCredential
			t.Cleanup(func() { runGHCredential = old })
			runGHCredential = func(gh, configDir string, input []byte) ([]byte, error) {
				if gh != real || configDir != config.ExpandHome("~/gh-work") || string(input) != github {
					t.Errorf("gh %s dir %s input %q", gh, configDir, input)
				}
				if tc.ghErr {
					return nil, errors.New("exit 1")
				}
				return []byte(tc.ghOut), nil
			}
			input, action := tc.input, tc.action
			if input == "" {
				input = github
			}
			if action == "" {
				action = "get"
			}
			var out, errOut bytes.Buffer
			if err := runCredential(strings.NewReader(input), &out, &errOut, tc.identity, action); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Errorf("stdout = %q, want %q", out.String(), tc.want)
			}
			if !strings.Contains(errOut.String(), tc.stderr) || (tc.stderr == "" && errOut.Len() > 0) {
				t.Errorf("stderr = %q, want %q", errOut.String(), tc.stderr)
			}
		})
	}
}

func TestGHCredentialDropsTokenVariables(t *testing.T) {
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\ncat >/dev/null\necho \"args=$*\"\necho \"dir=$GH_CONFIG_DIR\"\necho \"token=$GH_TOKEN$GITHUB_TOKEN\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_TOKEN", "a")
	t.Setenv("GITHUB_TOKEN", "b")
	t.Setenv(ghConfigEnv, "/inherited")
	out, err := runGHCredential(gh, "/identity/gh", []byte("host=github.com\n\n"))
	if err != nil || string(out) != "args=auth git-credential get\ndir=/identity/gh\ntoken=\n" {
		t.Fatalf("%v %q", err, out)
	}
}
