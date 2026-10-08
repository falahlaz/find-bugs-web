#!/bin/sh
# Fake Antigravity CLI: echoes a JSON envelope. FAKE_AGY=error|garbage|timeout|crash|text changes behaviour.
id=0c271faf-538b-4bea-a942-3e2be6132bc7
case "$FAKE_AGY" in
error) echo "{\"conversation_id\":\"$id\",\"status\":\"ERROR\",\"error\":\"quota exhausted\"}"; exit 1 ;;
garbage) echo 'not json'; exit 0 ;;
timeout) sleep 5 ;;
crash) echo 'authentication required' >&2; exit 1 ;;
esac
# Prove the sandbox and schema flags are set, the prompt carries the
# instructions and the log file sits in the working directory.
case "$*" in *"--json-schema {"*"--sandbox --disable-slash-commands --print "*) ;; *) echo "flags missing" >&2; exit 3 ;; esac
case "$*" in *"Only use the view_file tool"*"Transaction ID: abc-1"*) ;; *) echo "prompt missing" >&2; exit 4 ;; esac
grep -q "ERROR 504" logs.txt || { echo "logs.txt missing" >&2; exit 5; }
# A throwaway HOME, signed in, with the locked-down settings.
cfg="$HOME/.gemini/antigravity-cli"
case "$HOME" in "$PWD"*) echo "HOME inside the model's directory" >&2; exit 6 ;; esac
[ "$(cat "$cfg/antigravity-oauth-token")" = "x-antigravity-oauth-token" ] || { echo "token missing" >&2; exit 8; }
grep -q '"read_file(/home/)"' "$cfg/settings.json" && grep -q '"command(\*)"' "$cfg/settings.json" || { echo "settings missing" >&2; exit 9; }
[ -n "$FAKE_AGY_HOMES" ] && echo "$HOME" >> "$FAKE_AGY_HOMES"
d='{"summary":"ESB timeout","error_type":"504","failed_component":"esb","likely_cause":"c","severity":"high","suggested_action":"a","relevant_logs":["#1 [t] ERROR 504"],"error_source":"esb"}'
if [ "$FAKE_AGY" = text ]; then
	printf '{"conversation_id":"%s","status":"SUCCESS","response":%s}\n' "$id" "$(printf '%s' "$d" | sed 's/"/\\"/g; s/^/"/; s/$/"/')"
else
	printf '{"conversation_id":"%s","status":"SUCCESS","response":"done","structured_output":%s}\n' "$id" "$d"
fi
