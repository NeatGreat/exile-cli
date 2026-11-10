#!/bin/sh
# Usage: exile-resume AGENT [ARGS...]
#
# Starts AGENT and continues its most recent session in the current
# directory, if there is one. "exile start" uses it unless --new is given.
# The script replaces itself with the agent, so signals reach the agent
# directly.
set -u
agent=$1
shift

# Claude Code keeps one transcript per session in a directory named after
# the working directory, with every character but letters and digits
# replaced by "-" and long names cut to 200 characters plus a hash.
claude_has_session() {
	name=$(pwd | sed 's/[^a-zA-Z0-9]/-/g')
	if [ "${#name}" -gt 200 ]; then
		name=$(printf %s "$name" | cut -c1-200)-*
	fi
	grep -qs '"type":"user"' "$HOME"/.claude/projects/$name/*.jsonl
}

# Codex records the working directory in each session file.
codex_has_session() {
	grep -rqsF --include='rollout-*.jsonl' "\"cwd\":\"$(pwd)\"" "$HOME/.codex/sessions"
}

# Cline lists its sessions with their working directory, newest first.
cline_last_session() {
	cline history --json --limit 100 2>/dev/null |
		jq -r --arg cwd "$(pwd)" 'first(.[] | select(.cwd == $cwd and (.isSubagent | not))).sessionId // empty' 2>/dev/null
}

case $agent in
claude)
	if claude_has_session; then
		exec claude --continue "$@"
	fi
	;;
codex)
	if codex_has_session; then
		exec codex resume --last "$@"
	fi
	;;
cline)
	id=$(cline_last_session)
	if [ -n "$id" ]; then
		exec cline --id "$id" "$@"
	fi
	;;
esac
exec "$agent" "$@"
