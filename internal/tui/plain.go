package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/zozo123/wasted-cycles/internal/analyze"
)

// Plain is what a pipe, a redirect, or a CI job gets instead of the TUI.
func Plain(report analyze.Report) string {
	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }

	scope := fmt.Sprintf("%s · %d traces", windowLabel(report), report.Scanned)
	if report.IsDemo {
		scope = "demo dataset"
	}
	add("WASTED CYCLES  (%s)", scope)
	add("")
	if len(report.Sessions) == 0 {
		add("No recent supported traces found in ~/.codex/sessions, ~/.claude/projects,")
		add("~/.cursor/projects, or ~/.grok/sessions.")
		add("Widen the window with --days 30 or --ytd, or see the interface with --demo.")
		return strings.Join(lines, "\n")
	}

	add("Agent loop  %9s   machine-observed time", duration(report.Observed))
	add("Blocked     %9s   %s of the agent loop", duration(report.Blocked), share(report.Blocked, report.Observed))
	add("Flow        %8.0f%%   %s", report.Throughput*100, verdict(report.Throughput))
	add("Human       %9s   outside the metric", duration(report.Human))
	add("")

	add("WHERE THE TIME WENT")
	peak := time.Duration(0)
	for _, category := range report.Categories {
		if category.Duration > peak {
			peak = category.Duration
		}
	}
	for _, group := range []struct{ id, title string }{
		{analyze.GroupWorking, "agent working"},
		{analyze.GroupBlocked, "blocked on compute"},
		{analyze.GroupExcluded, "not counted"},
	} {
		first := true
		for _, category := range report.Categories {
			if category.Group != group.id || category.Duration == 0 {
				continue
			}
			if first {
				add("  [%s]", group.title)
				first = false
			}
			fill := 0
			if peak > 0 {
				fill = int(float64(category.Duration) / float64(peak) * 28)
			}
			glyph := "#"
			if group.id == analyze.GroupBlocked {
				glyph = "="
			} else if group.id == analyze.GroupExcluded {
				glyph = "."
			}
			add("    %-24s %9s  %s", category.Label, duration(category.Duration), strings.Repeat(glyph, fill))
		}
	}

	if len(report.Findings) > 0 {
		add("")
		add("BIGGEST STALLS")
		for index, finding := range report.Findings {
			if index >= 3 {
				break
			}
			add("  %d. %s (%s blocked)", index+1, finding.Title, duration(finding.Duration))
			add("     %s", finding.Action)
		}
	}

	add("")
	add("SESSIONS")
	for index, session := range report.Sessions {
		if index >= 8 {
			add("  … and %d more", len(report.Sessions)-8)
			break
		}
		note := ""
		if session.Resolution == "turn" {
			note = "  (turn resolution)"
		}
		add("  %-8s %-24s %9s  %3.0f%% flow%s",
			session.Provider, truncate(session.Project, 24), duration(session.Duration),
			session.Throughput*100, note)
	}

	add("")
	add("Blocked time is elapsed agent-loop time attributed to builds, tests, CI,")
	add("containers, packages, sub-agents, or repeated machine work.")
	add("Human wait is reported separately and never counted as blocked.")
	add("%s", inferredNote(report))
	add("Run with --json for the full report, or in a terminal for the TUI.")
	return strings.Join(lines, "\n")
}
