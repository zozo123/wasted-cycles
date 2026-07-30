# Wasted Cycles

[![CI](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml/badge.svg)](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml)

**Find where coding-agent runs stop coding.**

Wasted Cycles is a local wall-clock profiler for Codex, Claude Code, Cursor,
and Grok Build. It reads the traces already on your machine and turns them into
an interactive terminal histogram: model work, reads, edits, tests, CI waits,
human handoffs, agent joins, network stalls, and retries.

```sh
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run | sh
```

The runner downloads a checksum-verified binary into a temporary directory,
runs it, and deletes it on exit. There is no install, account, daemon, API key,
or upload.

## What it answers

- What share of elapsed time was actual model reasoning?
- Which waits dominate the critical path?
- Are retries, CI, humans, or sub-agents limiting throughput?
- Which coding harness stays productive for your workload?
- What is the highest-impact bottleneck to fix next?

Use the arrow keys to switch between Overview, Histogram, Runs, and Method.
Press `q` to quit.

```sh
# Explore with a realistic built-in dataset
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --demo

# Scan a wider window
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --days 30

# Machine-readable output
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --json
```

| Flag | Meaning |
| --- | --- |
| `--days N` | Days of history to scan (default 7, max 365) |
| `--demo` | Open the built-in demo dataset instead of your traces |
| `--json` | Print the full report as JSON |
| `--plain` | Print a plain-text summary instead of the TUI |
| `--no-alt-screen` | Render without the terminal alternate screen |
| `--version` | Print the version |

The TUI is used when stdout is a terminal. Piped or redirected output falls
back to the plain-text summary automatically, so `wasted-cycles > report.txt`
and CI usage both behave.

## Supported trace roots

| Harness | Local source | Resolution |
| --- | --- | --- |
| Codex | `~/.codex/sessions` | per event |
| Claude Code | `~/.claude/projects` | per event |
| Cursor Agent | `~/.cursor/projects` | per turn |
| Grok Build | `~/.grok/sessions` | per session |

Wasted Cycles reads JSONL trace files modified within the selected period.
Prompt text and source code are never stored or rendered.

## Method

The profiler reconstructs elapsed segments between timestamped trace events and
classifies each segment by the structured action that opened it: the tool that
was called, the command that tool ran, or the message that ended a turn. It
reads the parsed event structure rather than matching text, so a pasted log or
a quoted command in a prompt cannot be mistaken for real activity. Records it
cannot identify are skipped instead of guessed, which leaves their elapsed time
attributed to the last recognized action.

A verification or CI command that appears more than once in a session is
classified as retry time.

Idle time is handled with two thresholds, because a wait and a walk-away are
not the same thing. A gap longer than 2 hours is treated as a session break and
is not counted at all, so closing the laptop overnight cannot show up as
“waiting for human”. A shorter gap is capped at 30 minutes; those segments are
marked `"clamped": true` in JSON, carry a confidence of 0.3, and the total
clamped share is reported on the Method screen and as `inferred_ns` so you can
see how much of the headline is measured versus inferred.

“Model work” is an inference proxy: the interval after a user message or tool
result and before the next emitted action. It is not claimed as GPU compute time
unless a harness exposes exact duration. The Method screen and JSON output keep
that limitation visible.

Harnesses differ in what they record. Codex and Claude Code stamp individual
events. Cursor transcripts carry no per-event timestamps, so its runs are
reconstructed per user turn and each turn takes the dominant tool category
observed within it; those sessions are marked `turn` on the Runs screen and
carry low `confidence` in JSON output. Grok Build sessions are bounded by their
`summary.json` timestamps.

## Develop

Requires Go 1.24 or newer.

```sh
go test ./cmd/... ./internal/...
go run ./cmd/wasted-cycles --demo
```

The GitHub Pages site lives in `docs/`. Release assets are built for macOS,
Linux, and Windows by GitHub Actions on every `v*` tag.

## Prior art

Wasted Cycles is deliberately narrower than
[CodeBurn](https://github.com/getagentseal/codeburn). CodeBurn explains token
usage and cost; Wasted Cycles profiles wall-clock throughput and the critical
path.

## License

MIT
