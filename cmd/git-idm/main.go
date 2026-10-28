package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		if !errors.Is(err, errBlocked) {
			fmt.Fprintln(os.Stderr, "git-idm:", err)
		}
		os.Exit(1)
	}
}
