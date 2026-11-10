#!/bin/sh
# Usage: exile-copy < TEXT
#
# Asks exile on the host to put TEXT on the clipboard there. tmux pipes
# everything copied in the session to it: the terminal would only get it as
# OSC 52, which Apple's Terminal ignores.
set -eu
text=$(head -c 1000000 | base64 -w0)
if [ -n "$text" ]; then
	echo "copy $text" >> /run/exile-requests/requests
fi
