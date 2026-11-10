#!/bin/sh
# Claude Code status line: marks the session as running in exile. Claude Code
# passes the session as JSON on standard input.
input=$(cat)
dir=$(printf '%s' "$input" | jq -r '.workspace.current_dir // .cwd // empty' 2>/dev/null)
model=$(printf '%s' "$input" | jq -r '.model.display_name // empty' 2>/dev/null)
printf '\033[1;38;5;231;48;5;202m EXILED \033[0m \033[2m%s · network %s%s\033[0m' \
	"${dir##*/}" "${EXILE_NETWORK:-filtered}" "${model:+ · $model}"
