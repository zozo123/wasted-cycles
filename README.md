# Wasted Cycles

[![CI](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml/badge.svg)](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml)

**Find the machines your coding agent is waiting on.**

A wasted cycle is time your agent spends blocked on compute it does not
control — compiling, running tests, waiting on CI, provisioning containers,
fetching packages, joining sub-agents. Wasted Cycles reads the traces already on
your machine and shows you how much of a run was that, and which machine to fix
first.

Time spent waiting on a *person* is not a wasted cycle. It is reported
separately and never enters the metric.

```sh
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run | sh
```

The runner downloads a checksum-verified binary into a temporary directory,
runs it, and deletes it on exit. There is no install, account, daemon, API key,
or upload.

## What it answers

- How much of a run was spent blocked on a machine instead of coding?
- Which one — the compiler, the test suite, CI, containers, or the registry?
- Is the same build or test running more than once?
- Which coding harness keeps moving on your workload?
- What is the highest-impact bottleneck to fix next?

Use the arrow keys to switch between Overview, Histogram, Runs, and Method.
Press `W`, `M`, or `Y` to switch the lookback window. Press `q` to quit.

```sh
# Explore with a realistic built-in dataset
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --demo

# Scan a wider window
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --days 30

# Year to date
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --ytd

# Machine-readable output
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- --json
```

| Flag | Meaning |
| --- | --- |
| `--days N` | Days of history to scan (default 7, max 365) |
| `--ytd` | Scan from January 1 of this year |
| `--demo` | Open the built-in demo dataset instead of your traces |
| `--json` | Print the full report as JSON |
| `--plain` | Print a plain-text summary instead of the TUI |
| `--no-alt-screen` | Render without the terminal alternate screen |
| `--version` | Print the version |

In the TUI, press `W` (week), `M` (month), or `Y` (YTD) — or `[` / `]` — to switch
between **7d**, **30d**, and **YTD** without restarting. The header chips show the
active window.

The TUI is used when stdout is a terminal. Piped or redirected output falls
back to the plain-text summary automatically, so `wasted-cycles > report.txt`
and CI usage both behave.

## How time is counted

Every segment lands in one of three groups.

| Group | Categories | Counted? |
| --- | --- | --- |
| **Agent working** | model work, read & search, code changes, other tool work | yes |
| **Blocked on compute** | build, tests, CI, containers, packages, sub-agents, repeated work | yes — **these are the wasted cycles** |
| **Not counted** | waiting on a human | no |

`agent time` is working + blocked. `throughput` is the share of agent time that
was not spent waiting on a machine. Human time is reported beside those numbers
so you can see it, and excluded from both so it cannot flatter or distort them.

A build, test, or CI command that runs more than once in a session is
reclassified as repeated work, because the machine did the same job twice.

## Supported trace roots

| Harness | Local source | Resolution |
| --- | --- | --- |
| Codex | `~/.codex/sessions` | per event |
| Claude Code | `~/.claude/projects` | per event |
| Cursor | `~/.cursor/projects/*/agent-transcripts` | per turn |
| Grok Build | `~/.grok/sessions` | per session |

Wasted Cycles reads JSONL trace files modified within the selected period.
Prompt text and source code are never stored or rendered.

Cursor transcripts only stamp wall-clock time on user turns, so each segment
spans a whole turn rather than a single tool call. Scheduled Cursor agents that
tick on a fixed interval (the pattern that once inflated a session to 120 hours)
are detected and dropped.

## Method

The profiler reconstructs elapsed segments between timestamped trace events and
classifies each segment by the structured action that opened it: the tool that
was called, the command that tool ran, or the message that ended a turn. It
reads parsed event structure rather than matching text, so a pasted log or a
quoted command in a prompt cannot be mistaken for real activity. Records it
cannot identify are skipped instead of guessed, which leaves their elapsed time
attributed to the last recognized action.

Idle time uses two thresholds, because a wait and a walk-away are not the same
thing. A gap longer than 2 hours is treated as a session break and is not
counted at all. A shorter gap is capped at 30 minutes; those segments are marked
`"clamped": true` in JSON, carry a confidence of 0.3, and the total clamped share
is reported on the Method screen and as `inferred_ns`, so you can see how much of
the headline is measured versus inferred.

“Model work” is an inference proxy: the interval after a user message or tool
result and before the next emitted action. It is not claimed as GPU compute time
unless a harness exposes exact duration. The Method screen and JSON output keep
that limitation visible.

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
usage and cost; Wasted Cycles profiles wall-clock throughput and the machines on
the critical path.

## License

MIT
