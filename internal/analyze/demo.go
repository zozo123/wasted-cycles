package analyze

import "time"

func DemoReport() Report {
	now := time.Now().Truncate(time.Minute)
	type demoSegment struct {
		category string
		label    string
		duration time.Duration
	}
	rows := []struct {
		id, provider, project string
		segments              []demoSegment
	}{
		{"codex-7f31", "codex", "checkout-api", []demoSegment{
			{"reasoning", "model response", 18 * time.Minute},
			{"explore", "read / search", 11 * time.Minute},
			{"edit", "code change", 16 * time.Minute},
			{"build_wait", "cargo build", 21 * time.Minute},
			{"test_wait", "test suite", 9 * time.Minute},
			{"tool_other", "shell command", 6 * time.Minute},
			{"ci_wait", "CI feedback", 24 * time.Minute},
			{"retry", "repeated test suite", 8 * time.Minute},
			{"human_wait", "handoff to human", 31 * time.Minute},
		}},
		{"claude-a92c", "claude", "billing-webhooks", []demoSegment{
			{"explore", "read / search", 14 * time.Minute},
			{"edit", "code change", 21 * time.Minute},
			{"test_wait", "test suite", 12 * time.Minute},
			{"container_wait", "docker compose up", 13 * time.Minute},
			{"human_wait", "handoff to human", 17 * time.Minute},
			{"reasoning", "model response", 16 * time.Minute},
		}},
		{"cursor-3e91", "cursor", "admin-console", []demoSegment{
			{"reasoning", "model response", 13 * time.Minute},
			{"explore", "read / search", 9 * time.Minute},
			{"edit", "code change", 19 * time.Minute},
			{"dependency_wait", "pnpm install", 7 * time.Minute},
			{"build_wait", "vite build", 11 * time.Minute},
		}},
		{"grok-19aa", "grok", "worker-runtime", []demoSegment{
			{"reasoning", "Grok Build session", 17 * time.Minute},
			{"agent_wait", "agent join", 12 * time.Minute},
			{"edit", "code change", 14 * time.Minute},
			{"test_wait", "test suite", 8 * time.Minute},
		}},
	}

	report := Report{GeneratedAt: now, Since: Window7d.Since(now), Window: Window7d, IsDemo: true}
	cursor := now.Add(-5 * time.Hour)
	for _, row := range rows {
		resolution := resolutionEvent
		if row.provider == "grok" || row.provider == "cursor" {
			resolution = resolutionTurn
		}
		session := Session{ID: row.id, Provider: row.provider, Project: row.project, Start: cursor, Resolution: resolution}
		human := time.Duration(0)
		for _, item := range row.segments {
			segment := Segment{
				Start: cursor, End: cursor.Add(item.duration), Duration: item.duration,
				Category: item.category, Label: item.label, Provider: row.provider,
				SessionID: row.id, Confidence: .92,
			}
			session.Segments = append(session.Segments, segment)
			if CategoryGroup(item.category) == GroupExcluded {
				human += item.duration
			}
			cursor = segment.End
		}
		session.End = cursor
		total, blocked := machineTime(session.Segments)
		session.Duration = total
		session.Human = human
		session.Throughput = float64(total-blocked) / float64(total)
		report.Sessions = append(report.Sessions, session)
		cursor = cursor.Add(9 * time.Minute)
	}
	report.Sources = []Source{{"claude", 1}, {"codex", 1}, {"cursor", 1}, {"grok", 1}}
	report.Scanned = 4
	finalize(&report)
	return report
}
