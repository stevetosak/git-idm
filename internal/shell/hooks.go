package shell

import "github.com/stevetosak/git-idm/internal/gitgen"

func Hook(sh Shell, bin, stamp string) string {
	switch sh {
	case Bash:
		return bashHook(bin, stamp)
	case Fish:
		return fishHook(bin, stamp)
	}
	return zshHook(bin, stamp)
}

func zshHook(bin, stamp string) string {
	return `typeset -g _GIT_IDM_BIN=` + gitgen.ShellQuote(bin) + `
typeset -g _GIT_IDM_STAMP=` + gitgen.ShellQuote(stamp) + `
typeset -g _GIT_IDM_SEEN
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
  local before="$_GIT_IDM_SEEN" out
  _GIT_IDM_SEEN=
  { read -r _GIT_IDM_SEEN < "$_GIT_IDM_STAMP" } 2>/dev/null
  [[ "$_GIT_IDM_SEEN" == "$before" ]] || rehash
  out="$("$_GIT_IDM_BIN" env --shell=zsh 2>/dev/null)" && eval "$out"
}
_git_idm_precmd() {
  local stamp=
  { read -r stamp < "$_GIT_IDM_STAMP" } 2>/dev/null
  [[ "$stamp" == "$_GIT_IDM_SEEN" ]] || _git_idm_sync
}
autoload -Uz add-zsh-hook
add-zsh-hook chpwd _git_idm_sync
add-zsh-hook precmd _git_idm_precmd
_git_idm_sync
setopt prompt_subst
if [[ "$RPROMPT" != *` + SegmentVar + `* ]]; then
  RPROMPT='${` + SegmentVar + `}'"${RPROMPT:+ $RPROMPT}"
fi
`
}

func bashHook(bin, stamp string) string {
	return `_GIT_IDM_BIN=` + gitgen.ShellQuote(bin) + `
_GIT_IDM_STAMP=` + gitgen.ShellQuote(stamp) + `
_GIT_IDM_SEEN=
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
  local before="$_GIT_IDM_SEEN" out
  _GIT_IDM_SEEN=
  { read -r _GIT_IDM_SEEN < "$_GIT_IDM_STAMP"; } 2>/dev/null
  if [[ "$_GIT_IDM_SEEN" != "$before" ]]; then
    hash -r
  fi
  out="$("$_GIT_IDM_BIN" env --shell=bash 2>/dev/null)" && eval "$out"
}
_git_idm_prompt() {
  local stamp=
  { read -r stamp < "$_GIT_IDM_STAMP"; } 2>/dev/null
  if [[ "$PWD" != "${_GIT_IDM_PWD-}" || "$stamp" != "$_GIT_IDM_SEEN" ]]; then
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

func fishHook(bin, stamp string) string {
	return `set -g _GIT_IDM_BIN ` + fishQuote(bin) + `
set -g _GIT_IDM_STAMP ` + fishQuote(stamp) + `
set -g _GIT_IDM_SEEN ''
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
function _git_idm_stamp
    test -r $_GIT_IDM_STAMP; and read -l stamp < $_GIT_IDM_STAMP; and echo $stamp
end
function _git_idm_sync
    set -g _GIT_IDM_SEEN (_git_idm_stamp)
    $_GIT_IDM_BIN env --shell=fish 2>/dev/null | source
end
function _git_idm_on_pwd --on-variable PWD
    _git_idm_sync
end
function _git_idm_on_prompt --on-event fish_prompt
    set -l stamp (_git_idm_stamp)
    test "$stamp" = "$_GIT_IDM_SEEN"; or _git_idm_sync
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
