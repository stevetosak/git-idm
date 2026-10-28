package importer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type entry struct {
	Key   string
	Value string
}

type gitConfig []entry

func readGlobal() (gitConfig, error) {
	if !globalConfigExists() {
		return nil, nil
	}
	return runGitConfig("--global")
}

func globalConfigExists() bool {
	if p := os.Getenv("GIT_CONFIG_GLOBAL"); p != "" {
		return fileExists(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return fileExists(filepath.Join(home, ".gitconfig")) || fileExists(filepath.Join(xdg, "git", "config"))
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func readFile(path string) (gitConfig, error) {
	return runGitConfig("--file", path)
}

func runGitConfig(scope ...string) (gitConfig, error) {
	args := append([]string{"config"}, scope...)
	args = append(args, "--no-includes", "--null", "--list")
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return parseNullList(out), nil
}

func parseNullList(out []byte) gitConfig {
	var cfg gitConfig
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		key, value, _ := strings.Cut(string(rec), "\n")
		cfg = append(cfg, entry{Key: key, Value: value})
	}
	return cfg
}

func (c gitConfig) get(key string) (string, bool) {
	value, found := "", false
	for _, e := range c {
		if strings.EqualFold(e.Key, key) {
			value, found = e.Value, true
		}
	}
	return value, found
}

func (c gitConfig) value(key string) string {
	v, _ := c.get(key)
	return v
}

type include struct {
	Condition string
	Path      string
}

func (c gitConfig) includeIfs() []include {
	var out []include
	for _, e := range c {
		lower := strings.ToLower(e.Key)
		if !strings.HasPrefix(lower, "includeif.") || !strings.HasSuffix(lower, ".path") {
			continue
		}
		cond := e.Key[len("includeif.") : len(e.Key)-len(".path")]
		out = append(out, include{Condition: cond, Path: e.Value})
	}
	return out
}
