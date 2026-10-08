#!/bin/sh
# Fake tmux: `ls` prints $RC_SESSION_ROOT/.fake-tmux-ls (no server when it
# is missing); capture-pane shows a connected screen with both a claude.ai
# and an antigravity link, so each engine must pick its own.
case "$1" in
ls) [ -f "$RC_SESSION_ROOT/.fake-tmux-ls" ] || { echo "no server running on /tmp/tmux-1000/default" >&2; exit 1; }
	cat "$RC_SESSION_ROOT/.fake-tmux-ls" ;;
capture-pane) n=$(echo "$5" | tr -d "=:")
	echo "· Connected · https://claude.ai/code?environment=env_$n"
	echo "Open https://antigravity.google.com/r/inst_$n?p=c%2Fconv-1 on another device to continue this conversation." ;;
esac
