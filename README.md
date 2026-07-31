# Wasted Cycles

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

## Supported trace roots

| Harness | Local source |
| --- | --- |
| Codex | `~/.codex/sessions` |
| Claude Code | `~/.claude/projects` |
| Cursor Agent | `~/.cursor/projects` |
| Grok Build | `~/.grok/sessions` |

Wasted Cycles reads JSON, JSONL, and Cursor transcript files modified within
the selected period. Prompt text and source code are never stored or rendered.

## Method

The profiler reconstructs elapsed segments between timestamped trace events and
classifies the preceding activity. A repeated verification or CI command is
classified as retry time. Gaps longer than 30 minutes are capped so an
overnight pause does not dominate the report.

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
Linux, and Windows by GitHub Actions.

## Prior art

Wasted Cycles is deliberately narrower than
[CodeBurn](https://github.com/getagentseal/codeburn). CodeBurn explains token
usage and cost; Wasted Cycles profiles wall-clock throughput and the critical
path.

## License

MIT
