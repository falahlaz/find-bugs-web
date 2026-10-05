#!/bin/sh
# Fake rc-session.sh: records its arguments and environment in
# $RC_SESSION_ROOT/.fake-log (the caller's environment does not reach it).
{ echo "args: $*"; env | sort; } > "$RC_SESSION_ROOT/.fake-log"
case "$1" in
start) printf 'Session: %s\nFolder:  %s\nStatus:  Connected\nURL:     https://claude.ai/code?environment=env_1\n' "$(basename "$2")" "$2" ;;
stop) [ -d "$2/fail" ] && { echo "WARNING: leftover claude rc processes: password=abc"; exit 1; }
	echo "Killed tmux session '$(basename "$2")'" ;;
esac
