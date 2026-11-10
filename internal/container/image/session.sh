#!/bin/sh
# Usage: exile-session COMMAND [ARGS...]
#
# Runs COMMAND in tmux below a status bar that marks the session as exiled
# and, if exile serves its settings page (EXILE_UI=1), opens it when
# clicked. Exits with COMMAND's status.
set -u
dir=$(mktemp -d)
sock=$dir/tmux.sock

# esc makes a value safe inside a double-quoted tmux format string.
esc() {
	printf %s "$1" | tr -d '"$\\' | sed 's/#/##/g'
}

{
	cat /etc/exile/tmux.conf
	printf 'set -g status-left "#[bg=colour202,fg=colour231,bold] EXILED #[default] %s · %s "\n' \
		"$(esc "${EXILE_AGENT:-agent}")" "$(esc "${EXILE_PROJECT:-}")"
	if [ "${EXILE_UI:-}" = 1 ]; then
		printf 'set -g status-right "manage agent file and folder access  #[fg=colour231,bg=colour25,bold] open settings ↗ #[default]"\n'
	else
		printf 'set -g status-right "manage agent file and folder access: run exile ui "\n'
	fi
} > "$dir/tmux.conf"

# tini passes stop signals to this process group, which reaches the tmux
# client but not the tmux server. Ending the server hangs up on the agent.
trap 'tmux -S "$sock" kill-server 2>/dev/null' TERM HUP INT

tmux -S "$sock" -f "$dir/tmux.conf" new-session -s exile \
	sh -c '"$@"; echo $? > "$0"' "$dir/status" "$@"
exit "$(cat "$dir/status" 2>/dev/null || echo 0)"
