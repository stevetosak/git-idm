package config

import (
	"os"
	"path/filepath"
	"strings"
)

func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")) + trailingSlash(p)
}

func ContractHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func trailingSlash(p string) string {
	if strings.HasSuffix(p, "/") && p != "~/" {
		return "/"
	}
	return ""
}
