#!/bin/sh
# Fake Claude Code CLI: echoes a result envelope. FAKE_CLAUDE=error|garbage|timeout changes behaviour.
input=$(cat)
case "$FAKE_CLAUDE" in
error) echo '{"type":"result","is_error":true,"result":"API Error: 401"}'; exit 0 ;;
garbage) echo 'not json'; exit 0 ;;
timeout) sleep 5 ;;
esac
# Prove tools are disabled and the prompt arrived on stdin.
case "$*" in *"--tools "*) ;; *) echo "tools flag missing" >&2; exit 3 ;; esac
case "$input" in *"Transaction ID: abc-1"*) ;; *) echo "prompt missing" >&2; exit 4 ;; esac
cat <<'J'
{"type":"result","subtype":"success","is_error":false,"result":"```json\n{\"summary\":\"ESB timeout\",\"error_type\":\"504\",\"failed_component\":\"esb/x\",\"likely_cause\":\"slow\",\"severity\":\"HIGH\",\"suggested_action\":\"retry\",\"relevant_logs\":[\"ERROR 504\"],\"error_source\":\"esb\"}\n```"}
J
