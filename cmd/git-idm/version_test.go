package main

import (
	"bytes"
	"testing"

	"github.com/stevetosak/git-idm/internal/version"
)

func TestVersionCommandAndFlag(t *testing.T) {
	old := version.Version
	t.Cleanup(func() { version.Version = old })
	version.Version = "1.4.0"
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var out bytes.Buffer
		root := newRootCmd()
		root.SetOut(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got := out.String(); got != "git-idm v1.4.0\n" {
			t.Errorf("%v: got %q", args, got)
		}
	}
}
