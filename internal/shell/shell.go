package shell

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"github.com/stevetosak/git-idm/internal/gitgen"
	"github.com/stevetosak/git-idm/internal/resolve"
)

type Shell string

const (
	Zsh  Shell = "zsh"
	Bash Shell = "bash"
	Fish Shell = "fish"
)

var Supported = []Shell{Zsh, Bash, Fish}

func Parse(name string) (Shell, error) {
	for _, sh := range Supported {
		if string(sh) == name {
			return sh, nil
		}
	}
	return "", fmt.Errorf("shell %q is not supported (supported: zsh, bash, fish)", name)
}

const (
	SegmentVar = "_GIT_IDM_SEGMENT"
	ghEnv      = "GH_CONFIG_DIR"
	countEnv   = "GIT_CONFIG_COUNT"
)

func Sync(sh Shell, c *config.Config, res resolve.Result) string {
	var b strings.Builder
	id, known := c.Identities[res.Identity]
	if known && id.GHConfigDir != "" {
		export(sh, &b, ghEnv, config.ExpandHome(id.GHConfigDir))
	} else {
		unset(sh, &b, ghEnv)
	}
	export(sh, &b, SegmentVar, Segment(sh, c, res))
	return b.String()
}

func Use(sh Shell, c *config.Config, name string, res resolve.Result) (string, error) {
	id, ok := c.Identities[name]
	if !ok {
		return "", fmt.Errorf("identity %q does not exist", name)
	}
	if os.Getenv(countEnv) != "" && os.Getenv(resolve.OverrideEnv) == "" {
		return "", fmt.Errorf("%s is already set in this shell, git-idm cannot override safely", countEnv)
	}
	var b strings.Builder
	b.WriteString(clearOverride(sh))
	bin, _ := gitgen.Executable()
	entries := gitgen.IdentityEntries(name, id, bin, gitgen.UsesGH(c))
	for i, e := range entries {
		export(sh, &b, fmt.Sprintf("GIT_CONFIG_KEY_%d", i), e.Name())
		export(sh, &b, fmt.Sprintf("GIT_CONFIG_VALUE_%d", i), e.Value)
	}
	export(sh, &b, countEnv, strconv.Itoa(len(entries)))
	export(sh, &b, resolve.OverrideEnv, name)
	b.WriteString(Sync(sh, c, res))
	return b.String(), nil
}

func Clear(sh Shell, c *config.Config, res resolve.Result) string {
	return clearOverride(sh) + Sync(sh, c, res)
}

func clearOverride(sh Shell) string {
	if os.Getenv(resolve.OverrideEnv) == "" {
		return ""
	}
	var b strings.Builder
	n, _ := strconv.Atoi(os.Getenv(countEnv))
	for i := 0; i < n; i++ {
		unset(sh, &b, fmt.Sprintf("GIT_CONFIG_KEY_%d", i), fmt.Sprintf("GIT_CONFIG_VALUE_%d", i))
	}
	unset(sh, &b, countEnv, resolve.OverrideEnv)
	return b.String()
}

func Segment(sh Shell, c *config.Config, res resolve.Result) string {
	if res.Identity == "" {
		return ""
	}
	color := c.Identities[res.Identity].Color
	if color == "" {
		color = "cyan"
	}
	label := res.Identity
	if res.Source == resolve.SourceOverride {
		return paint(sh, label+" (override)", "red", true)
	}
	if res.Temporary() {
		label += " (temp)"
	}
	if res.LocalEmail != "" && !strings.EqualFold(res.LocalEmail, c.Identities[res.Identity].Email) {
		label += " (repo user)"
		color = "yellow"
	}
	if res.Temporary() {
		return paint(sh, label, "yellow", true)
	}
	return paint(sh, label, color, false)
}

var ansiColors = map[string]string{"red": "31", "green": "32", "yellow": "33", "blue": "34", "magenta": "35", "cyan": "36", "white": "37"}

func paint(sh Shell, label, color string, bold bool) string {
	if sh == Zsh {
		label = "%F{" + color + "}" + strings.ReplaceAll(label, "%", "%%") + "%f"
		if bold {
			label = "%B" + label + "%b"
		}
		return label
	}
	code := ansiColors[color]
	if code == "" {
		code = ansiColors["cyan"]
	}
	if bold {
		code = "1;" + code
	}
	if sh == Fish {
		return "\x1b[" + code + "m" + label + "\x1b[0m"
	}
	return "\x01\x1b[" + code + "m\x02" + label + "\x01\x1b[0m\x02"
}

func export(sh Shell, b *strings.Builder, key, value string) {
	if sh == Fish {
		fmt.Fprintf(b, "set -gx %s %s\n", key, fishQuote(value))
		return
	}
	fmt.Fprintf(b, "export %s=%s\n", key, gitgen.ShellQuote(value))
}

func unset(sh Shell, b *strings.Builder, keys ...string) {
	if sh == Fish {
		fmt.Fprintf(b, "set -e %s\n", strings.Join(keys, " "))
		return
	}
	fmt.Fprintf(b, "unset %s\n", strings.Join(keys, " "))
}

func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
