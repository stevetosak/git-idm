package main

import (
	"bytes"
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
