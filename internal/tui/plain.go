package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/zozo123/wasted-cycles/internal/analyze"
)

func Plain(report analyze.Report) string {
	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }

	scope := fmt.Sprintf("last %s · %d traces scanned", relativePeriod(report.Since), report.Scanned)
	if report.IsDemo {
		scope = "demo dataset"
	}
	add("WASTED CYCLES  (%s)", scope)
	add("")
	if len(report.Sessions) == 0 {
		add("No recent supported traces found in ~/.codex/sessions, ~/.claude/projects,")
		add("~/.cursor/projects, or ~/.grok/sessions.")
		add("Try a wider window with --days 30, or explore the interface with --demo.")
		return strings.Join(lines, "\n")
	}

	add("Observed        %s", duration(report.Observed))
	add("Model work      %s of observed", percent(categoryDuration(report, "reasoning"), report.Observed))
	add("Recoverable     %s (blocked + repeated)", duration(report.Recoverable))
	add("Throughput      %.0f%% (%s)", report.Throughput*100, throughputLabel(report.Throughput))
	add("")
	add("WHERE THE TIME WENT")
	maximum := time.Duration(0)
	for _, category := range report.Categories {
		if category.Duration > maximum {
			maximum = category.Duration
		}
	}
	for _, category := range report.Categories {
		fill := 0
		if maximum > 0 {
			fill = int(float64(category.Duration) / float64(maximum) * 32)
		}
		add("  %-20s %9s  %s", category.Label, duration(category.Duration), strings.Repeat("#", fill))
	}

	if len(report.Findings) > 0 {
		add("")
		add("BIGGEST LEAKS")
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
	add("Model work is an inference proxy, not measured GPU time.")
	add("%s", inferredNote(report))
	add("Run with --json for the full report, or in an interactive terminal for the TUI.")
	return strings.Join(lines, "\n")
}
