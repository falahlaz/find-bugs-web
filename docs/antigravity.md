# Antigravity CLI (agy)

The server can use the Antigravity CLI (`agy`), signed in with a Google
account that has an Antigravity subscription, in two places:

- **Log diagnosis** (`LOG_ANALYZER=antigravity`): the Splunk logs are
  diagnosed by Gemini instead of Claude. Code tracing and trace chat always
  use Claude.
- **Repo page sessions**: next to "Mulai sesi Claude" a repo gets "Mulai sesi
  agy", which runs `agy --remote-control --dangerously-skip-permissions` in
  tmux through the rc-session skill script (`rc-session.sh start <dir> --agy`).
  The link opens the conversation on the Antigravity Remote Control dashboard.
  The button shows when `AGY_BIN` exists.

## Setup (once, as the user running the server)

1. Install: `curl -fsSL https://antigravity.google/cli/install.sh | bash`
   (installs `~/.local/bin/agy`).
2. Sign in: run `agy` in a terminal, pick **Google OAuth**, open the URL,
   paste the code back, and finish the first-run screens (the data-sharing
   opt-in is ticked by default). `agy models` lists the slugs for `AGY_MODEL`.
3. For log diagnosis set `LOG_ANALYZER=antigravity` in the env file and
   restart.

Keep `~/.gemini/antigravity-cli/settings.json` free of deny rules that would
cripple Remote Control sessions; log diagnosis does not use it.

## How a log diagnosis is isolated

- Each run gets a throwaway `HOME` holding a copy of the sign-in token and
  its own `settings.json`, which denies commands, web/URL access, MCP, writes
  and reads outside `/tmp`. `search_web` cannot be denied but only reaches
  Google, which already receives the logs.
- The logs are copied into a fresh temp directory holding only `logs.txt`;
  `agy` runs there with `--sandbox` and `--json-schema` for the diagnosis.
- The throwaway `HOME`, and with it the stored conversation, is deleted
  after the run.
- If `agy` fails (quota, sign-in expired, timeout) and `AGY_FALLBACK` is on,
  the job is diagnosed by Claude instead and a warning is logged.

## Model speed

On a two-event log: `gemini-3.8-flash-high` ~230 s (it thinks a lot),
`-medium` ~97 s, `-low` ~17 s with a shallower answer. The default is
`-medium` with `AGY_TIMEOUT=4m`.
