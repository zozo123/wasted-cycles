package githubactions

import (
	"fmt"
	"strings"
	"time"
)

func Plain(report Report) string {
	var lines []string
	add := func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}

	scope := report.Window
	if scope == "" {
		scope = "since " + report.Since.Format("2006-01-02")
	}
	add("WASTED CYCLES · GITHUB ACTIONS")
	add("%s · %s · %d runs via %s", report.Repository, scope, report.Runs, sourceLabel(report.Source))
	if report.Truncated {
		add("Warning: result capped; use a shorter window or raise --max-runs.")
	}
	add("")
	if report.Runs == 0 {
		add("No GitHub Actions runs were created in this window.")
		return strings.Join(lines, "\n")
	}
	if report.CompletedRuns == 0 {
		add("No completed GitHub Actions runs with usable timestamps were found.")
		if report.RunningRuns > 0 {
			add("%d run(s) are still in progress and excluded.", report.RunningRuns)
		}
		return strings.Join(lines, "\n")
	}

	add("Workflow latency  %9s   summed elapsed time", duration(report.CIWait))
	add("Unsuccessful     %9s   %s of workflow latency", duration(report.UnsuccessfulTime), share(report.UnsuccessfulTime, report.CIWait))
	add("Queue delay      %9s   %s of workflow latency", duration(report.QueueWait), share(report.QueueWait, report.CIWait))
	add("Median / p95    %9s / %s", duration(report.MedianRun), duration(report.P95Run))
	add("Success rate     %8.0f%%   %d of %d completed runs", report.SuccessRate*100, report.SuccessfulRuns, report.CompletedRuns)
	if report.RunningRuns > 0 || report.SkippedRuns > 0 {
		add("Excluded         %9s   %d running, %d missing/invalid timestamps", "", report.RunningRuns, report.SkippedRuns)
	}

	if len(report.Workflows) > 0 {
		add("")
		add("MOST TIME BY WORKFLOW")
		for index, workflow := range report.Workflows {
			if index >= 10 {
				break
			}
			add("  %-30s %9s   %d runs · %d unsuccessful",
				truncate(workflow.Name, 30), duration(workflow.CIWait), workflow.Runs, workflow.UnsuccessfulRuns)
		}
	}

	if len(report.LongestRuns) > 0 {
		add("")
		add("SLOWEST RUNS")
		for index, run := range report.LongestRuns {
			if index >= 5 {
				break
			}
			add("  %-30s %9s   %s", truncate(run.Name, 30), duration(run.CIWait), conclusionLabel(run.Conclusion))
		}
	}

	add("")
	add("METHOD")
	add("Workflow latency is created_at → updated_at for each completed run.")
	add("Overlapping runs count separately. This measures repository latency, not")
	add("billed runner-minutes or proof that a person waited for every run.")
	return strings.Join(lines, "\n")
}

func duration(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	value = value.Round(time.Second)
	days := value / (24 * time.Hour)
	value %= 24 * time.Hour
	hours := value / time.Hour
	value %= time.Hour
	minutes := value / time.Minute
	seconds := value % time.Minute
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds/time.Second)
	default:
		return fmt.Sprintf("%ds", seconds/time.Second)
	}
}

func share(part, total time.Duration) string {
	if total <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(part)/float64(total)*100)
}

func truncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:max(0, width)])
	}
	return string(runes[:width-1]) + "…"
}

func sourceLabel(source string) string {
	switch source {
	case "github-cli":
		return "gh"
	case "authenticated-api":
		return "GitHub API (authenticated)"
	default:
		return "GitHub public API"
	}
}

func conclusionLabel(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
