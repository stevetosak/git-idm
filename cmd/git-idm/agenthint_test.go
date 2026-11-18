package main

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

func TestAgentNote(t *testing.T) {
	other := errors.New("bad flag")
	cases := []struct {
		cmd  string
		err  error
		want string
	}{
		{"check-commit", errBlocked, agentHint},
		{"gh", other, agentHint},
		{"ssh", other, agentHint},
		{"bind", other, ""},
		{"gh", nil, ""},
	}
	for _, tc := range cases {
		if got := agentNote(&cobra.Command{Use: tc.cmd}, tc.err); got != tc.want {
			t.Errorf("agentNote(%s, %v) = %q, want %q", tc.cmd, tc.err, got, tc.want)
		}
	}
}
