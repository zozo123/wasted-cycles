# Wasted Cycles

[![CI](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml/badge.svg)](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zozo123/wasted-cycles)](https://github.com/zozo123/wasted-cycles/releases/latest)
[![Pages](https://img.shields.io/badge/demo-GitHub%20Pages-05A388)](https://zozo123.github.io/wasted-cycles/)

**Find the machine time blocking agent work.**

Wasted Cycles is a local-first wall-clock profiler for Codex, Claude Code,
Cursor, and Grok Build. It reads their existing traces, separates useful agent
work from machine waits, and ranks the build, test, CI, container, package, and
sub-agent stalls worth fixing.

It can also inspect a public or private GitHub repository and summarize GitHub
Actions latency.

```sh
curl -fsSL https://zozo123.github.io/w | sh
```

The runner downloads a checksum-verified release binary to a temporary
directory, runs it, and removes it. There is no install, account, daemon, API
key, or upload.

## Start here

```sh
# Explore the TUI without local traces
curl -fsSL https://zozo123.github.io/w |
  sh -s -- --demo

# Scan 30 days of local agent traces
curl -fsSL https://zozo123.github.io/w |
  sh -s -- --days 30

# Analyze a public repository
curl -fsSL https://zozo123.github.io/w |
  sh -s -- github nanoporetech/dorado

# A URL works too; options may come before or after it
wasted-cycles github https://github.com/nanoporetech/dorado --days 30
```

Use `←` / `→` to switch between Overview, Breakdown, Sessions, and Method.
Press `W`, `M`, or `Y` to change the lookback window and `q` to quit. Small
terminals get a compact summary automatically; pipes and redirects get plain
text.

## Local agent traces

The four headline values are deliberately narrow:

| Value | Meaning |
| --- | --- |
| **Agent loop** | elapsed machine-observed work, excluding human wait |
| **Blocked** | agent-loop time attributed to machine waits |
| **Flow** | share of the agent loop not blocked on a machine |
| **Human** | time waiting on a person, shown but excluded |

Every measured segment belongs to one group:

| Group | Categories |
| --- | --- |
| **Working** | model work, reads, searches, edits, other tools |
| **Blocked** | build, tests, CI, containers, packages, sub-agents, repeated work |
| **Excluded** | waiting on a person |

A repeated build, test, or CI command is counted as repeated machine work.
Wasted Cycles reports measured durations and actionable findings; it does not
invent dollar savings.

For reproducible production use, pin the release consumed by the installer:

```sh
curl -fsSL https://zozo123.github.io/w |
  WASTED_CYCLES_VERSION=v0.6.0 sh
```

### Supported sources

| Harness | Local trace root | Resolution |
| --- | --- | --- |
| Codex | `~/.codex/sessions` | event |
| Claude Code | `~/.claude/projects` | event |
| Cursor | `~/.cursor/projects/*/agent-transcripts` | turn |
| Grok Build | `~/.grok/sessions` | session |

Prompt text and source code are never stored, rendered, or uploaded.

## GitHub Actions

```sh
wasted-cycles github OWNER/REPO
wasted-cycles github OWNER/REPO --days 30
wasted-cycles github --ytd --json https://github.com/OWNER/REPO
```

Public repositories work without login. For private repositories, authenticate
with `gh auth login`, `GH_TOKEN`, or `GITHUB_TOKEN`.

The GitHub report shows:

- summed workflow latency across completed runs;
- unsuccessful and queued portions;
- median and p95 run latency;
- success rate and the workflows using the most elapsed time.

Workflow latency is `created_at → updated_at`. Overlapping runs count
separately. It is repository latency—not billed runner-minutes and not proof
that a person or agent waited for every run.

## Options

```text
wasted-cycles [--days N | --ytd] [--demo] [--plain | --json]
wasted-cycles github [--days N | --ytd] [--max-runs N] [--json] OWNER/REPO
```

Run `wasted-cycles --help` or `wasted-cycles help github` for the full, current
reference.

## Method and limits

The profiler reconstructs elapsed segments between timestamped trace events and
classifies each segment from the structured action that opened it. It parses
event fields rather than prompt text, so pasted logs and quoted commands are not
treated as executed work.

A gap over two hours starts a new session and is dropped. A shorter gap is
capped at 30 minutes, marked inferred, and exposed as `inferred_ns` in JSON.
“Model work” is the interval after a message or tool result, not measured GPU
time. Cursor and Grok expose coarser timestamps, so their lower-resolution
segments are labelled in the report. Use `--json` to inspect confidence and
segment-level details.

## Develop

Requires Go 1.24 or newer.

```sh
go test ./...
go run ./cmd/wasted-cycles --demo
go run ./cmd/wasted-cycles github nanoporetech/dorado
```

The GitHub Pages source is in `docs/`. Tags matching `v*` build checksum-verified
archives for macOS, Linux, and Windows.

## License

MIT
