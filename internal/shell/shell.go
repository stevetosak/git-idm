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

const (
	SegmentVar = "_GIT_IDM_SEGMENT"
	ghEnv      = "GH_CONFIG_DIR"
	countEnv   = "GIT_CONFIG_COUNT"
)

func ZshHook(bin string) string {
	return `typeset -g _GIT_IDM_BIN=` + gitgen.ShellQuote(bin) + `
git-idm() {
  if [[ "$1" == use ]]; then
    local out
    out="$("$_GIT_IDM_BIN" use --shell "${@:2}")" && eval "$out"
  else
    "$_GIT_IDM_BIN" "$@"
    local rc=$?
    _git_idm_sync
    return $rc
  fi
}
_git_idm_sync() {
  local out
  out="$("$_GIT_IDM_BIN" env --shell 2>/dev/null)" && eval "$out"
}
autoload -Uz add-zsh-hook
add-zsh-hook chpwd _git_idm_sync
_git_idm_sync
setopt prompt_subst
if [[ "$RPROMPT" != *` + SegmentVar + `* ]]; then
  RPROMPT='${` + SegmentVar + `}'"${RPROMPT:+ $RPROMPT}"
fi
`
}

func Sync(c *config.Config, res resolve.Result) string {
	var b strings.Builder
	id, known := c.Identities[res.Identity]
	if known && id.GHConfigDir != "" {
		export(&b, ghEnv, config.ExpandHome(id.GHConfigDir))
	} else {
		fmt.Fprintf(&b, "unset %s\n", ghEnv)
	}
	export(&b, SegmentVar, Segment(c, res))
	return b.String()
}

func Use(c *config.Config, name string, res resolve.Result) (string, error) {
	id, ok := c.Identities[name]
	if !ok {
		return "", fmt.Errorf("identity %q does not exist", name)
	}
	if os.Getenv(countEnv) != "" && os.Getenv(resolve.OverrideEnv) == "" {
		return "", fmt.Errorf("%s is already set in this shell, git-idm cannot override safely", countEnv)
	}
	var b strings.Builder
	b.WriteString(clearOverride())
	entries := gitgen.IdentityEntries(id)
	for i, e := range entries {
		export(&b, fmt.Sprintf("GIT_CONFIG_KEY_%d", i), e.Name())
		export(&b, fmt.Sprintf("GIT_CONFIG_VALUE_%d", i), e.Value)
	}
	export(&b, countEnv, strconv.Itoa(len(entries)))
	export(&b, resolve.OverrideEnv, name)
	b.WriteString(Sync(c, res))
	return b.String(), nil
}

func Clear(c *config.Config, res resolve.Result) string {
	return clearOverride() + Sync(c, res)
}

func clearOverride() string {
	if os.Getenv(resolve.OverrideEnv) == "" {
		return ""
	}
	var b strings.Builder
	n, _ := strconv.Atoi(os.Getenv(countEnv))
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "unset GIT_CONFIG_KEY_%d GIT_CONFIG_VALUE_%d\n", i, i)
	}
	fmt.Fprintf(&b, "unset %s %s\n", countEnv, resolve.OverrideEnv)
	return b.String()
}

func Segment(c *config.Config, res resolve.Result) string {
	if res.Identity == "" {
		return ""
	}
	color := c.Identities[res.Identity].Color
	if color == "" {
		color = "cyan"
	}
	label := res.Identity
	if res.Source == resolve.SourceOverride {
		return "%B%F{red}" + escapePrompt(label) + " (override)%f%b"
	}
	if res.Temporary() {
		label += " (temp)"
	}
	if res.LocalEmail != "" && !strings.EqualFold(res.LocalEmail, c.Identities[res.Identity].Email) {
		label += " (repo user)"
		color = "yellow"
	}
	if res.Temporary() {
		return "%B%F{yellow}" + escapePrompt(label) + "%f%b"
	}
	return "%F{" + color + "}" + escapePrompt(label) + "%f"
}

func escapePrompt(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

func export(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "export %s=%s\n", key, gitgen.ShellQuote(value))
}
