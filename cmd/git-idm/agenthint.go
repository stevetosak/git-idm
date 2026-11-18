package main

import (
	"errors"

	"github.com/spf13/cobra"
)

const agentHint = "  agents:    do not go around this check. Show this message to the user."

var guardCommands = map[string]bool{"gh": true, "ssh": true}

func agentNote(cmd *cobra.Command, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errBlocked) || (cmd != nil && guardCommands[cmd.Name()]) {
		return agentHint
	}
	return ""
}
