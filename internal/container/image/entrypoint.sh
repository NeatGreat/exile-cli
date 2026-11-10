#!/bin/sh
# Aligns the agent user's UID/GID with the host user (EXILE_UID/EXILE_GID) so
# files written to bind mounts get the right owner, loads the agent
# configuration copied in by exile, prepares the agents and drops root.
set -eu

if [ "$(id -u)" != 0 ]; then
	exec "$@"
fi

gid="${EXILE_GID:-}"
uid="${EXILE_UID:-}"

if [ -n "$gid" ] && [ "$gid" != 0 ] && [ "$gid" != "$(id -g agent)" ]; then
	groupmod --non-unique --gid "$gid" agent
fi
if [ -n "$uid" ] && [ "$uid" != 0 ] && [ "$uid" != "$(id -u agent)" ]; then
	usermod --non-unique --uid "$uid" agent
fi

as_agent() {
	setpriv --reuid=agent --regid=agent --init-groups -- "$@"
}

if [ -f /run/exile/agent.env ]; then
	. /run/exile/agent.env
	rm -f /run/exile/agent.env
fi

# Tell the agents what they can access: Claude Code reads its managed
# settings and memory file, Codex its global AGENTS.md unless the user wrote
# their own, Cline every file in its global rules directory.
if [ -f /run/exile/claude-settings.json ]; then
	install -m 644 /run/exile/claude-settings.json /etc/claude-code/managed-settings.json
	rm -f /run/exile/claude-settings.json
fi
if [ -f /run/exile/access.md ]; then
	install -m 644 /run/exile/access.md /etc/claude-code/CLAUDE.md
	marker='<!-- managed by exile -->'
	if [ ! -e "$HOME/.codex/AGENTS.md" ] || [ "$(head -n1 "$HOME/.codex/AGENTS.md")" = "$marker" ]; then
		{ echo "$marker"; cat /run/exile/access.md; } |
			as_agent sh -c 'mkdir -p "$HOME/.codex" && cat > "$HOME/.codex/AGENTS.md"'
	fi
	as_agent sh -c 'mkdir -p "$HOME/.cline/rules" && cat > "$HOME/.cline/rules/exile.md"' < /run/exile/access.md
	rm -f /run/exile/access.md
fi

# With credentials configured, skip Claude Code's first-run setup.
if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}${ANTHROPIC_API_KEY:-}" ] && [ ! -e "$HOME/.claude.json" ]; then
	as_agent sh -c 'echo "{\"hasCompletedOnboarding\": true}" > "$HOME/.claude.json"'
fi

# Codex takes API keys from its login, not from the environment.
if [ -n "${OPENAI_API_KEY:-}" ]; then
	printenv OPENAI_API_KEY | as_agent codex login --with-api-key >/dev/null 2>&1 || true
fi

# Cline takes them from its provider settings.
if [ -n "${CLINE_API_KEY:-}" ]; then
	as_agent cline auth --provider "${CLINE_PROVIDER:-cline}" --apikey "$CLINE_API_KEY" \
		--modelid "${CLINE_MODEL:-}" </dev/null >/dev/null || true
fi

# Switch off Cline's telemetry, as Claude Code's is.
as_agent sh -c 'f=$HOME/.cline/data/settings/global-settings.json
	mkdir -p "${f%/*}"
	{ cat "$f" 2>/dev/null || echo "{}"; } | jq ".telemetryOptOut = true" > "$f.new" 2>/dev/null &&
		mv "$f.new" "$f" || rm -f "$f.new"'

exec setpriv --reuid=agent --regid=agent --init-groups -- "$@"
