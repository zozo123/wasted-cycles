# Wasted Cycles

[![CI](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml/badge.svg)](https://github.com/zozo123/wasted-cycles/actions/workflows/ci.yml)

**Find the machines your coding agent is waiting on.**

A wasted cycle is time your agent spends blocked on compute it does not
control — compiling, running tests, waiting on CI, provisioning containers,
fetching packages, joining sub-agents. Wasted Cycles reads the traces already on
your machine and shows you how much of a run was that, and which machine to fix
first. It can also inspect a GitHub repository and total the wall-clock time
spent in GitHub Actions.

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
- Roughly how much time and money those waits might be worth to recover?
- How much wall-clock time did this repository spend in GitHub Actions?

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

# Analyze GitHub Actions for a public repository
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- github nanoporetech/dorado

# The repository can also be a URL; flags come before it
curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run |
  sh -s -- github --days 30 --json https://github.com/nanoporetech/dorado
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

## GitHub Actions

Pass `github` followed by `owner/name` or a GitHub repository URL to measure the
repository's Actions latency:

```sh
wasted-cycles github nanoporetech/dorado
wasted-cycles github https://github.com/nanoporetech/dorado
wasted-cycles github --days 30 --json owner/private-repo
```

Public repositories work without credentials through the GitHub REST API. When
`gh auth status` succeeds, the command uses the GitHub CLI, which also supports
private repositories your account can read. `GH_TOKEN` or `GITHUB_TOKEN` can be
used instead.

| Flag | Meaning |
| --- | --- |
| `--days N` | Days of workflow runs to inspect (default 7, max 365) |
| `--ytd` | Inspect runs created since January 1 |
| `--max-runs N` | Cap fetched workflow runs (default 1,000, max 10,000) |
| `--json` | Print the full GitHub Actions report as JSON |

The headline **CI wait time** is the sum of `created_at → updated_at` for every
completed workflow run created in the window. Queue time is included and shown
separately. Overlapping runs are counted separately. This measures repository CI
latency; it is not GitHub's billed runner-minutes and does not claim a person or
agent waited for every run. Unsuccessful time is broken out so failed,
cancelled, timed-out, and other non-successful runs are easy to see.

### Anonymized demo

This illustrative seven-day snapshot repeats a public sample **21×** (a randomly
selected factor between 10 and 22). It preserves the sample's proportions while
removing repository, commit, and session names.

```text
WASTED CYCLES · GITHUB ACTIONS
example/repository · last 7 days · 84 completed runs

CI wait time          22h 29m   summed workflow latency
Unsuccessful time          0m   0% of CI wait
Queue time                 0m   0% of CI wait
Average run           16m 04s
Success rate             100%   84 of 84 completed runs

WORKFLOW SHAPE
Build matrix          22h 06m   42 runs  ████████████████████  98.3%
Policy checks             23m   42 runs  ▍                      1.7%
```

These are scaled demo figures, not a claim about any repository. The live
command always calculates from the repository and window you provide.

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

When accelerateable waits are material, Overview and `--json` also show an
**illustrative** recovery estimate (time + engineer $/ CI $). It applies
conservative fractions to build, test, CI, container, package, and retry waits,
using labelled unit rates — **not a quote or guarantee**. Options like
[Incredibuild Build Runner](https://www.incredibuild.com/product/build-runner),
[Blacksmith](https://www.blacksmith.sh/), and [CircleCI](https://circleci.com/)
are listed for comparison only.

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
