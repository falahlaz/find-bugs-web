#!/bin/sh
# Fake tmux: `ls` prints $RC_SESSION_ROOT/.fake-tmux-ls (no server when it
# is missing); capture-pane shows a connected rc screen.
case "$1" in
ls) [ -f "$RC_SESSION_ROOT/.fake-tmux-ls" ] || { echo "no server running on /tmp/tmux-1000/default" >&2; exit 1; }
	cat "$RC_SESSION_ROOT/.fake-tmux-ls" ;;
capture-pane) echo "· Connected · https://claude.ai/code?environment=env_${5#=}" ;;
esac
