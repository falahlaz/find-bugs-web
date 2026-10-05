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
# Trace sessions: streamed events, a session ID or resume, the repos
# readable through --add-dir and the logs in the working directory.
case "$*" in *"--add-dir "*)
	case "$*" in *"--output-format stream-json --verbose "*) ;; *) echo "stream flags missing" >&2; exit 7 ;; esac
	case "$*" in *"--session-id "*|*"--resume "*) ;; *) echo "session flag missing" >&2; exit 8 ;; esac
	case "$*" in *"--no-session-persistence"*) echo "session not persisted" >&2; exit 9 ;; esac
	grep -q "ERROR 504" logs.txt || { echo "logs.txt missing" >&2; exit 5; }
	dir=${*##*--add-dir }
	echo '{"type":"system","subtype":"init"}'
	echo "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"thinking\"},{\"type\":\"tool_use\",\"name\":\"Read\",\"input\":{\"file_path\":\"$dir/server/a.js\"}}]}}"
	echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Grep","input":{"pattern":"ERROR 504","path":"logs.txt"}}]}}'
	# Newer CLIs emit events whose fields clash with the assistant shape.
	echo '{"type":"system","subtype":"permission_denied","message":"Bash is not allowed"}'
	case "$input" in
	*"An engineer asks:"*)
		case "$input" in *"was restarted"*) r="Jawaban dengan recap" ;; *) r="Jawaban: baris 12" ;; esac
		echo "{\"type\":\"result\",\"is_error\":false,\"result\":\"$r\",\"modelUsage\":{\"m\":{}}}"
		exit 0 ;;
	*"another version of"*) ;;
	*"Service checkouts"*"grp/svc → "*) ;;
	*) echo "trace prompt missing" >&2; exit 6 ;;
	esac
	cat <<J
{"type":"result","is_error":false,"result":"{\"status\":\"found\",\"project\":\"grp/svc\",\"file\":\"$dir/server/a.js\",\"line\":12,\"function\":\"pay\",\"snippet\":\"x()\",\"explanation\":\"null\"}","modelUsage":{"m":{}}}
J
	exit 0 ;;
esac
case "$input" in *"Transaction ID: abc-1"*"./logs.txt"*) ;; *) echo "prompt missing" >&2; exit 4 ;; esac
grep -q "ERROR 504" logs.txt || { echo "logs.txt missing" >&2; exit 5; }
cat <<'J'
{"type":"result","subtype":"success","is_error":false,"result":"```json\n{\"summary\":\"ESB timeout\",\"error_type\":\"504\",\"failed_component\":\"esb/x\",\"likely_cause\":\"slow\",\"severity\":\"HIGH\",\"suggested_action\":\"retry\",\"relevant_logs\":[\"ERROR 504\"],\"error_source\":\"esb\"}\n```","modelUsage":{"claude-haiku-4-5-20251001":{"inputTokens":10,"outputTokens":5}}}
J
