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
			{"verify", "test suite", 9 * time.Minute},
			{"ci_wait", "CI feedback", 24 * time.Minute},
			{"retry", "repeated test suite", 8 * time.Minute},
		}},
		{"claude-a92c", "claude", "billing-webhooks", []demoSegment{
			{"explore", "read / search", 14 * time.Minute},
			{"edit", "code change", 21 * time.Minute},
			{"verify", "test suite", 12 * time.Minute},
			{"human_wait", "handoff to human", 17 * time.Minute},
			{"reasoning", "model response", 16 * time.Minute},
		}},
		{"cursor-44de", "cursor", "admin-console", []demoSegment{
			{"reasoning", "model response", 13 * time.Minute},
			{"explore", "read / search", 9 * time.Minute},
			{"edit", "code change", 19 * time.Minute},
			{"dependency_wait", "dependency / network", 7 * time.Minute},
			{"verify", "test suite", 11 * time.Minute},
		}},
		{"grok-19aa", "grok", "worker-runtime", []demoSegment{
			{"reasoning", "Grok Build session", 17 * time.Minute},
			{"agent_wait", "agent join", 12 * time.Minute},
			{"edit", "code change", 14 * time.Minute},
			{"verify", "test suite", 8 * time.Minute},
		}},
	}

	report := Report{GeneratedAt: now, Since: now.Add(-7 * 24 * time.Hour), IsDemo: true}
	cursor := now.Add(-5 * time.Hour)
	for _, row := range rows {
		session := Session{ID: row.id, Provider: row.provider, Project: row.project, Start: cursor}
		for _, item := range row.segments {
			segment := Segment{
				Start: cursor, End: cursor.Add(item.duration), Duration: item.duration,
				Category: item.category, Label: item.label, Provider: row.provider,
				SessionID: row.id, Confidence: .92,
			}
			session.Segments = append(session.Segments, segment)
			session.Duration += item.duration
			cursor = segment.End
		}
		session.End = cursor
		session.Throughput = float64(activeDuration(session.Segments)) / float64(session.Duration)
		report.Sessions = append(report.Sessions, session)
		cursor = cursor.Add(9 * time.Minute)
	}
	report.Sources = []Source{{"claude", 1}, {"codex", 1}, {"cursor", 1}, {"grok", 1}}
	report.Scanned = 4
	finalize(&report)
	return report
}
