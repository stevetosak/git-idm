package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	cmd, err := newRootCmd().ExecuteC()
	if err != nil {
		if !errors.Is(err, errBlocked) {
			fmt.Fprintln(os.Stderr, "git-idm:", err)
		}
		if note := agentNote(cmd, err); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		os.Exit(1)
	}
	notifyUpdate(cmd, os.Stderr)
}
