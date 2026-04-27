#!/usr/bin/env bash
#
# capture-events.sh — drive each registered runtime with a single prompt and
# record every raw stdout JSON line as a JSONL snapshot under the runtime's
# testdata directory. The snapshots feed the per-runtime parser tests so the
# parser is exercised against real CLI output, not just hand-written
# fixtures.
#
# Re-run after upstream CLI schema changes and review the diff.
#
# Requirements: bash 3.2+, jq.

set -u
set -o pipefail

DEFAULT_PROMPT="Read and summary this repository"
DEFAULT_NAME="summary_repo"
DEFAULT_TIMEOUT=300   # seconds (5 minutes)

usage() {
  cat <<USAGE
Usage: $0 [options]

Options:
  -r, --runtime <name>   Runtime to capture: claude-code|codex|opencode|all (default: all)
  -p, --prompt <text>    Prompt to send (default: "$DEFAULT_PROMPT")
  -w, --workdir <dir>    Working directory for the session (default: repo root)
  -o, --outdir <dir>     Override output dir (default: internal/runtime/<runtime>/testdata)
  -n, --name <basename>  Snapshot file basename without extension (default: $DEFAULT_NAME)
  -t, --timeout <sec>    Max seconds to wait for a session to finish (default: $DEFAULT_TIMEOUT)
  -h, --help             Show this help
USAGE
}

die() { printf '%s\n' "$*" >&2; exit 1; }

# Resolve the repo root by walking up from this script's location until we
# find go.mod. Avoids depending on the caller's PWD.
repo_root() {
  local dir
  dir="$(cd "$(dirname "$0")" >/dev/null 2>&1 && pwd)"
  while [[ "$dir" != "/" ]]; do
    if [[ -f "$dir/go.mod" ]]; then
      printf '%s\n' "$dir"
      return 0
    fi
    dir="$(dirname "$dir")"
  done
  return 1
}

runtime_dir() {
  case "$1" in
    claude-code) printf 'claudecode\n' ;;
    codex)       printf 'codex\n' ;;
    opencode)    printf 'opencode\n' ;;
    *)           return 1 ;;
  esac
}

# Compact a single JSON line: tolerate pretty-printed input, reject invalid
# JSON. Stdout receives one minified line per input record.
compact_jsonl() {
  jq -c '.'
}

# Run a command with a wall-clock timeout. Sends SIGTERM first, then
# SIGKILL after a short grace period. Returns the command's exit code, or
# 124 on timeout (matching coreutils `timeout`).
run_with_timeout() {
  local seconds="$1"; shift
  if command -v timeout >/dev/null 2>&1; then
    timeout --foreground "${seconds}s" "$@"
    return $?
  fi
  if command -v gtimeout >/dev/null 2>&1; then
    gtimeout --foreground "${seconds}s" "$@"
    return $?
  fi
  # Fallback: spawn child, watchdog kills on timeout. Avoids requiring
  # GNU coreutils on bare-bones macOS installs.
  "$@" &
  local child=$!
  ( sleep "$seconds" && kill -TERM "$child" 2>/dev/null && \
    sleep 5 && kill -KILL "$child" 2>/dev/null ) &
  local watchdog=$!
  local rc=0
  wait "$child" || rc=$?
  kill "$watchdog" 2>/dev/null || true
  wait "$watchdog" 2>/dev/null || true
  return "$rc"
}

# capture_claude_code <prompt> <workdir> <output>
capture_claude_code() {
  local prompt="$1" workdir="$2" output="$3"
  ( cd "$workdir" && \
    run_with_timeout "$TIMEOUT" \
      claude -p --output-format stream-json --verbose \
        --dangerously-skip-permissions "$prompt" ) | compact_jsonl > "$output"
}

# capture_opencode <prompt> <workdir> <output>
capture_opencode() {
  local prompt="$1" workdir="$2" output="$3"
  ( cd "$workdir" && \
    run_with_timeout "$TIMEOUT" \
      opencode run --format json --thinking \
        --dangerously-skip-permissions "$prompt" ) | compact_jsonl > "$output"
}

# capture_codex <prompt> <workdir> <output>
#
# Drives the codex JSON-RPC stdio app-server. Mirrors internal/runtime/codex
# session.go exactly:
#   1. send initialize (id=1), discard response
#   2. send initialized notification
#   3. send thread/start (id=2); forward any interleaved server-pushed
#      events to the snapshot, parse threadId from the matching response,
#      do NOT forward the response itself
#   4. send turn/start (id=3); forward every subsequent line to the
#      snapshot until method == "turn/completed"
#
# Bidirectional comms use a FIFO for the child's stdin so we can write to
# it from multiple steps without losing the connection.
capture_codex() {
  local prompt="$1" workdir="$2" output="$3"

  local tmpdir
  tmpdir="$(mktemp -d -t cula-codex.XXXXXX)" || die "mktemp failed"
  local fifo="$tmpdir/stdin"
  mkfifo "$fifo" || { rm -rf "$tmpdir"; die "mkfifo failed"; }

  # Empty/truncate the snapshot up front so partial captures are obvious.
  : > "$output"

  # Open the FIFO for writing on fd 9. Holding it open keeps stdin alive
  # for the codex child across multiple send steps.
  exec 9>"$fifo"

  # Start codex reading from the FIFO; pipe its stdout into our processing
  # loop. Use a subshell to scope the cd.
  (
    cd "$workdir" && \
    run_with_timeout "$TIMEOUT" codex app-server --listen stdio:// < "$fifo"
  ) | _codex_drive "$prompt" "$output" &
  local pipeline_pid=$!

  # Wait for the pipeline (codex | _codex_drive). _codex_drive exits as
  # soon as it sees turn/completed; that closes the pipe and codex
  # receives EOF on its stdout writer, which causes it to terminate.
  wait "$pipeline_pid" || true

  # Release stdin and clean up.
  exec 9>&-
  rm -rf "$tmpdir"
}

# _codex_drive <prompt> <output>
#
# Runs as the right-hand side of `codex | _codex_drive`. Reads JSON-RPC
# messages from codex on stdin, and uses fd 9 (the FIFO we opened in
# capture_codex) to write requests back. State machine has three phases:
# init → thread → turn.
_codex_drive() {
  local prompt="$1" output="$2"
  local phase="init"
  local thread_id=""
  local cwd
  cwd="$(pwd)"

  # Send initialize. id=1.
  printf '%s\n' \
    "$(jq -nc --arg name cula --arg version 0.1.0 \
        '{jsonrpc:"2.0",id:1,method:"initialize",params:{clientInfo:{name:$name,version:$version}}}')" \
    >&9

  while IFS= read -r line; do
    # Skip blank lines and any non-JSON noise (codex shouldn't emit any,
    # but be defensive).
    [[ -z "$line" ]] && continue
    if ! printf '%s' "$line" | jq -e . >/dev/null 2>&1; then
      continue
    fi

    local id method
    id="$(printf '%s' "$line" | jq -r '.id // empty')"
    method="$(printf '%s' "$line" | jq -r '.method // empty')"

    case "$phase" in
      init)
        if [[ "$id" == "1" ]]; then
          # Initialize response: send 'initialized' notification, then
          # thread/start (id=2). Do NOT forward the init response.
          printf '%s\n' '{"jsonrpc":"2.0","method":"initialized"}' >&9
          local start_params
          start_params="$(jq -nc \
            --arg cwd "$cwd" \
            --arg approval "${PERMISSION:-never}" \
            --arg sandbox "${SANDBOX:-danger-full-access}" \
            '{cwd:$cwd, approvalPolicy:$approval, sandbox:$sandbox}')"
          printf '%s\n' \
            "$(jq -nc --argjson params "$start_params" \
              '{jsonrpc:"2.0",id:2,method:"thread/start",params:$params}')" \
            >&9
          phase="thread"
        fi
        ;;
      thread)
        if [[ "$id" == "2" ]]; then
          # thread/start response: extract threadId, send turn/start
          # (id=3). Do NOT forward this response — it's the matched
          # JSON-RPC response, not an event.
          thread_id="$(printf '%s' "$line" | jq -r '.result.thread.id // empty')"
          if [[ -z "$thread_id" ]]; then
            printf 'thread/start returned empty thread id: %s\n' "$line" >&2
            return 1
          fi
          local turn_params
          turn_params="$(jq -nc \
            --arg tid "$thread_id" \
            --arg text "$prompt" \
            '{threadId:$tid, input:[{type:"text", text:$text}]}')"
          printf '%s\n' \
            "$(jq -nc --argjson params "$turn_params" \
              '{jsonrpc:"2.0",id:3,method:"turn/start",params:$params}')" \
            >&9
          phase="turn"
          continue
        fi
        # Anything else arriving while we wait for the thread/start
        # response is a server-pushed event (thread/started, status
        # changes, etc.) — forward to the snapshot.
        printf '%s\n' "$line" >> "$output"
        ;;
      turn)
        # Forward every line during the turn, including the id=3
        # response and any deltas.
        printf '%s\n' "$line" >> "$output"
        if [[ "$method" == "turn/completed" ]]; then
          return 0
        fi
        ;;
    esac
  done
}

count_lines() {
  awk 'NF{n++} END{print n+0}' "$1"
}

# capture_runtime <name> <prompt> <workdir> <outdir> <basename>
capture_runtime() {
  local kind="$1" prompt="$2" workdir="$3" outdir="$4" basename="$5"
  printf '==> capturing %s\n' "$kind"
  local target_dir
  if [[ -n "$outdir" ]]; then
    target_dir="$outdir"
  else
    local subdir
    subdir="$(runtime_dir "$kind")" || die "unknown runtime: $kind"
    target_dir="$REPO_ROOT/internal/runtime/$subdir/testdata"
  fi
  mkdir -p "$target_dir" || die "mkdir $target_dir failed"
  local out="$target_dir/$basename.jsonl"

  case "$kind" in
    claude-code) capture_claude_code "$prompt" "$workdir" "$out" ;;
    codex)       capture_codex       "$prompt" "$workdir" "$out" ;;
    opencode)    capture_opencode    "$prompt" "$workdir" "$out" ;;
    *)           die "unknown runtime: $kind" ;;
  esac

  local n
  n="$(count_lines "$out")"
  if [[ "$n" -eq 0 ]]; then
    rm -f "$out"
    die "no raw events captured for $kind (is the CLI authenticated?)"
  fi
  printf '    captured %s raw events\n' "$n"
  printf '    wrote %s\n' "$out"
}

main() {
  local runtime="all"
  local prompt="$DEFAULT_PROMPT"
  local workdir=""
  local outdir=""
  local basename="$DEFAULT_NAME"
  TIMEOUT="$DEFAULT_TIMEOUT"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -r|--runtime) runtime="$2"; shift 2 ;;
      -p|--prompt)  prompt="$2"; shift 2 ;;
      -w|--workdir) workdir="$2"; shift 2 ;;
      -o|--outdir)  outdir="$2"; shift 2 ;;
      -n|--name)    basename="$2"; shift 2 ;;
      -t|--timeout) TIMEOUT="$2"; shift 2 ;;
      -h|--help)    usage; exit 0 ;;
      *) die "unknown arg: $1 (try --help)" ;;
    esac
  done

  command -v jq >/dev/null 2>&1 || die "jq is required (brew install jq)"

  REPO_ROOT="$(repo_root)" || die "could not locate repo root (no go.mod found)"
  if [[ -z "$workdir" ]]; then
    workdir="$REPO_ROOT"
  fi

  local targets=()
  case "$runtime" in
    all|"") targets=(claude-code codex opencode) ;;
    claude-code|codex|opencode) targets=("$runtime") ;;
    *) die "unknown runtime: $runtime" ;;
  esac

  for kind in "${targets[@]}"; do
    capture_runtime "$kind" "$prompt" "$workdir" "$outdir" "$basename"
  done
}

main "$@"
