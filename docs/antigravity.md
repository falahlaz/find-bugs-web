# Antigravity log analyzer

`LOG_ANALYZER=antigravity` sends the Splunk log diagnosis to the Antigravity
CLI (`agy`) instead of Claude, using the Antigravity subscription of the
Google account that `agy` is signed in with. Code tracing, trace chat and RC
sessions keep using Claude.

## Setup (once, as the user running the server)

1. Install: `curl -fsSL https://antigravity.google/cli/install.sh | bash`
   (installs `~/.local/bin/agy`).
2. Sign in: run `agy` in a terminal, pick **Google OAuth**, open the URL,
   paste the code back. `agy models` lists the model slugs for `AGY_MODEL`.
3. Lock its tools down. `agy` has no per-run tool flag, so the deny rules live
   in the global `~/.gemini/antigravity-cli/settings.json` (this also applies
   to any interactive `agy` use on the host):

   ```json
   {
     "permissions": {
       "deny": [
         "command(*)", "unsandboxed(*)", "read_url(*)", "execute_url(*)", "mcp(*)", "write_file(*)",
         "read_file(/home/)", "read_file(/root/)", "read_file(/etc/)", "read_file(/proc/)", "read_file(/var/)",
         "read_file(/opt/)", "read_file(/srv/)", "read_file(/run/)", "read_file(/sys/)", "read_file(/dev/)",
         "read_file(/mnt/)", "read_file(/media/)", "read_file(/boot/)", "read_file(/usr/)"
       ]
     }
   }
   ```

   The server checks this list (`analyzer.AgyRequiredDeny`) at start-up and
   keeps Claude if any rule is missing.
4. Set `LOG_ANALYZER=antigravity` in the env file and restart.

## How a run is isolated

- The logs are copied into a fresh temp directory holding only `logs.txt`;
  `agy` runs there with `--sandbox` and `--json-schema` for the diagnosis.
- Commands, web/URL access, MCP and writes are denied; reads outside `/tmp`
  are denied. `search_web` cannot be denied but only reaches Google, which
  already receives the logs.
- `agy` stores each conversation under `~/.gemini/antigravity-cli`; the
  analyzer deletes it after the run.
- If `agy` fails (quota, sign-in expired, timeout) and `AGY_FALLBACK` is on,
  the job is diagnosed by Claude instead and a warning is logged.
