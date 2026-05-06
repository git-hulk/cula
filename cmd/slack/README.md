# Slack bot example

`cmd/slack` shows how to use cula outside the terminal UI by exposing a local
agent runtime through Slack. It uses Slack Socket Mode, so the bot can run on a
developer machine without a public HTTP endpoint.

## Behavior

The bot listens for app mentions in channels and direct messages. Each Slack
thread gets its own cula session so follow-up messages in the same thread keep
the local agent context.

For each user prompt, the bot writes two Slack messages in the thread:

- A temporary Block Kit activity message, initially showing a thinking status.
- A final Block Kit answer message containing the assistant text.

As cula events stream in, `activity` and tool events update the temporary
activity message. Assistant `text` events are accumulated into the final answer
message. When cula emits `done` or a terminal state, the bot flushes the final
answer and deletes the activity message. The Slack thread is left with the user
prompt and final assistant response, not the transient progress log.

## Slack app setup

Create a Slack app with:

- Socket Mode enabled.
- Interactivity enabled (no Request URL is needed in Socket Mode).
- An app-level token with the `connections:write` scope.
- A bot token with these bot scopes:
  - `app_mentions:read`
  - `chat:write`
  - `channels:history`
  - `groups:history`
  - `im:history`
  - `im:read`
  - `im:write`

Subscribe to these bot events:

- `app_mention`
- `message.im`

Install the app into the workspace and invite it to any channel where you want
to mention it.

## Run

```bash
# Required. The bot will refuse to start if either is missing.
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_APP_TOKEN=xapp-...

# Optional defaults shown in the setup wizard.
export CULA_RUNTIME=codex
export CULA_MODEL=
export CULA_WORKDIR=/path/to/project

go run ./cmd/slack
```

On startup, the command opens an interactive setup wizard for choosing the
installed runtime, model, and working directory for Slack requests. The Slack
bot/app tokens must be provided via the environment variables above.

Supported `CULA_RUNTIME` values are:

- `claude-code`
- `codex`
- `opencode`
- `copilot`

Optional execution controls map directly to `cula.SessionInput`:

```bash
export CULA_PERMISSION=never
export CULA_SANDBOX=workspace-write
```

Use `CULA_SLACK_DEBUG=true` to enable Slack client debug logging.

## Notes

The command is an example, not a hosted service. It assumes the selected agent
CLI is installed and already authenticated on the machine running the bot.

The example intentionally keeps progress transient in Slack. If you want a full
audit trail, handle `tool_call`, `tool_result`, and `activity` events differently
instead of deleting the activity message at the end of each turn.
