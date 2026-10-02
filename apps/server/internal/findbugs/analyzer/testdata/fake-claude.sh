#!/bin/sh
# Fake Claude Code CLI: echoes a result envelope. FAKE_CLAUDE=error|garbage|timeout changes behaviour.
input=$(cat)
case "$FAKE_CLAUDE" in
error) echo '{"type":"result","is_error":true,"result":"API Error: 401"}'; exit 0 ;;
garbage) echo 'not json'; exit 0 ;;
timeout) sleep 5 ;;
esac
# Prove only read-only tools are enabled, the prompt arrived on stdin and the
# log file sits in the working directory.
case "$*" in *"--tools Read,Grep,Glob --permission-mode dontAsk "*) ;; *) echo "tool flags missing" >&2; exit 3 ;; esac
case "$input" in *"Transaction ID: abc-1"*"./logs.txt"*) ;; *) echo "prompt missing" >&2; exit 4 ;; esac
grep -q "ERROR 504" logs.txt || { echo "logs.txt missing" >&2; exit 5; }
cat <<'J'
{"type":"result","subtype":"success","is_error":false,"result":"```json\n{\"summary\":\"ESB timeout\",\"error_type\":\"504\",\"failed_component\":\"esb/x\",\"likely_cause\":\"slow\",\"severity\":\"HIGH\",\"suggested_action\":\"retry\",\"relevant_logs\":[\"ERROR 504\"],\"error_source\":\"esb\"}\n```","modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":10,"outputTokens":5}}}
J
