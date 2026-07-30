package analyze

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCursorTime(t *testing.T) {
	got := parseCursorTime("Thursday, Jul 30, 2026, 6:41 PM (UTC+3)")
	if got.IsZero() {
		t.Fatal("failed to parse cursor timestamp")
	}
	// 6:41 PM UTC+3 == 15:41 UTC
	if got.Hour() != 15 || got.Minute() != 41 {
		t.Fatalf("got %s, want 15:41 UTC", got)
	}
}

func TestCursorEventsClassifyShellAndRejectMetronome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.jsonl")
	lines := []string{
		`{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Thursday, Jul 30, 2026, 1:00 PM (UTC)</timestamp>\n<user_query>build</user_query>"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"cargo build --release"}}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"ReadFile","input":{"path":"main.rs"}}]}}`,
		`{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Thursday, Jul 30, 2026, 1:12 PM (UTC)</timestamp>\n<user_query>next</user_query>"}]}}`,
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pytest -q"}}]}}`,
	}
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o600); err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 7, 30, 13, 20, 0, 0, time.UTC)
	if err := os.Chtimes(path, end, end); err != nil {
		t.Fatal(err)
	}

	events, _ := cursorEvents(path)
	if len(events) < 2 {
		t.Fatalf("expected turn events, got %d", len(events))
	}
	if events[0].Category != "build_wait" {
		t.Fatalf("first turn should be build_wait (most blocking), got %s", events[0].Category)
	}

	session, ok := parseSession(candidate{Path: path, Provider: "cursor", ModTime: end}, Options{
		Since:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		MaxGap: 30 * time.Minute,
	})
	if !ok {
		t.Fatal("cursor session should parse")
	}
	if session.Resolution != resolutionTurn {
		t.Fatalf("resolution = %q", session.Resolution)
	}
	if session.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestCursorMetronomeRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.jsonl")
	var lines []string
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		at := start.Add(time.Duration(i) * 30 * time.Minute)
		stamp := at.Format("Monday, Jan 2, 2006, 3:04 PM") + " (UTC)"
		lines = append(lines,
			`{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>`+stamp+`</timestamp>\n<user_query>tick</user_query>"}]}}`,
			`{"role":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`,
		)
	}
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o600); err != nil {
		t.Fatal(err)
	}
	end := start.Add(20 * 30 * time.Minute)
	_ = os.Chtimes(path, end, end)

	if events, _ := cursorEvents(path); len(events) != 0 {
		t.Fatalf("metronomic cursor session must be dropped, got %d events", len(events))
	}
}

func TestDiscoverIncludesCursorAgentTranscripts(t *testing.T) {
	home := t.TempDir()
	cursor := filepath.Join(home, ".cursor", "projects", "Users-demo-app", "agent-transcripts", "abcd1234")
	if err := os.MkdirAll(cursor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "projects", "Users-demo-app", "mcps"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(cursor, "abcd1234.jsonl")
	noise := filepath.Join(home, ".cursor", "projects", "Users-demo-app", "other.jsonl")
	for _, path := range []string{keep, noise} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	found := discover(home, time.Now().Add(-time.Hour))
	if len(found) != 1 || found[0].Provider != "cursor" || found[0].Path != keep {
		t.Fatalf("discover = %+v, want only agent-transcript", found)
	}
}

func TestWindowSince(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	if got := Window7d.Since(now); !got.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("7d since = %s", got)
	}
	if got := WindowYTD.Since(now); got.Year() != 2026 || got.Month() != 1 || got.Day() != 1 {
		t.Fatalf("ytd since = %s", got)
	}
	if WindowFromDays(7, now) != Window7d || WindowFromDays(30, now) != Window30d {
		t.Fatal("WindowFromDays presets")
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
