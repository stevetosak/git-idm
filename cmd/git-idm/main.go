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
		os.Exit(1)
	}
	notifyUpdate(cmd, os.Stderr)
}
