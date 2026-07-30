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

	scope := fmt.Sprintf("last %s · %d traces", relativePeriod(report.Since), report.Scanned)
	if report.IsDemo {
		scope = "demo dataset"
	}
	add("WASTED CYCLES  (%s)", scope)
	add("")
	if len(report.Sessions) == 0 {
		add("No recent supported traces found in ~/.codex/sessions, ~/.claude/projects,")
		add("~/.cursor/projects, or ~/.grok/sessions.")
		add("Widen the window with --days 30, or see the interface with --demo.")
		return strings.Join(lines, "\n")
	}

	add("Agent time         %s   (excludes time waiting on you)", duration(report.Observed))
	add("Blocked on compute %s   %s of agent time", duration(report.Blocked), share(report.Blocked, report.Observed))
	add("Throughput         %-9s %s", fmt.Sprintf("%.0f%%", report.Throughput*100), verdict(report.Throughput))
	add("Outside the loop   %s   waiting on you, not counted", duration(report.Human))
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
			add("  %d. %s (%s)", index+1, finding.Title, duration(finding.Recoverable))
			add("     %s", finding.Action)
		}
	}

	add("")
	add("RUNS")
	for index, session := range report.Sessions {
		if index >= 12 {
			add("  … and %d more", len(report.Sessions)-12)
			break
		}
		note := ""
		if session.Resolution == "turn" {
			note = "  (turn resolution)"
		}
		add("  %-8s %-24s %9s  %3.0f%% throughput%s",
			session.Provider, truncate(session.Project, 24), duration(session.Duration),
			session.Throughput*100, note)
	}

	add("")
	add("A wasted cycle is time blocked on a machine: builds, tests, CI, containers,")
	add("packages, sub-agents. Time waiting on a person is reported but never counted.")
	add("%s", inferredNote(report))
	add("Run with --json for the full report, or in a terminal for the TUI.")
	return strings.Join(lines, "\n")
}
