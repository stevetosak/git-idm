package shell

import "github.com/stevetosak/git-idm/internal/gitgen"

func Hook(sh Shell, bin string) string {
	switch sh {
	case Bash:
		return bashHook(bin)
	case Fish:
		return fishHook(bin)
	}
	return zshHook(bin)
}

func zshHook(bin string) string {
	return `typeset -g _GIT_IDM_BIN=` + gitgen.ShellQuote(bin) + `
git-idm() {
  if [[ "$1" == use ]]; then
    local out
    out="$("$_GIT_IDM_BIN" use --shell=zsh "${@:2}")" && eval "$out"
  else
    "$_GIT_IDM_BIN" "$@"
    local rc=$?
    _git_idm_sync
    return $rc
  fi
}
_git_idm_sync() {
  local out
  out="$("$_GIT_IDM_BIN" env --shell=zsh 2>/dev/null)" && eval "$out"
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

func bashHook(bin string) string {
	return `_GIT_IDM_BIN=` + gitgen.ShellQuote(bin) + `
git-idm() {
  if [[ "$1" == use ]]; then
    local out
    out="$("$_GIT_IDM_BIN" use --shell=bash "${@:2}")" && eval "$out"
  else
    "$_GIT_IDM_BIN" "$@"
    local rc=$?
    _git_idm_sync
    return $rc
  fi
}
_git_idm_sync() {
  local out
  out="$("$_GIT_IDM_BIN" env --shell=bash 2>/dev/null)" && eval "$out"
}
_git_idm_prompt() {
  if [[ "$PWD" != "${_GIT_IDM_PWD-}" ]]; then
    _GIT_IDM_PWD="$PWD"
    _git_idm_sync
  fi
}
if [[ " ${PROMPT_COMMAND[*]-} " != *_git_idm_prompt* ]]; then
  if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
    PROMPT_COMMAND=(_git_idm_prompt "${PROMPT_COMMAND[@]}")
  else
    PROMPT_COMMAND="_git_idm_prompt${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
  fi
fi
_git_idm_prompt
if [[ "$PS1" != *` + SegmentVar + `* ]]; then
  PS1='${` + SegmentVar + `:+$` + SegmentVar + ` }'"$PS1"
fi
`
}

func fishHook(bin string) string {
	return `set -g _GIT_IDM_BIN ` + fishQuote(bin) + `
function git-idm
    if test "$argv[1]" = use
        set -l code ($_GIT_IDM_BIN use --shell=fish $argv[2..-1])
        or return
        string join \n -- $code | source
    else
        $_GIT_IDM_BIN $argv
        set -l rc $status
        _git_idm_sync
        return $rc
    end
end
function _git_idm_sync
    $_GIT_IDM_BIN env --shell=fish 2>/dev/null | source
end
function _git_idm_on_pwd --on-variable PWD
    _git_idm_sync
end
_git_idm_sync
if not functions -q _git_idm_user_right_prompt
    if functions -q fish_right_prompt
        functions -c fish_right_prompt _git_idm_user_right_prompt
    else
        function _git_idm_user_right_prompt
        end
    end
    function fish_right_prompt
        set -l user (_git_idm_user_right_prompt)
        echo -n -s $` + SegmentVar + ` (test -n "$` + SegmentVar + `" -a -n "$user"; and echo -n " ") $user
    end
end
`
}
