package analyze

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func record(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return value
}

func TestClassifyClaudeRecords(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		kind     eventKind
		category string
	}{
		{
			"bash test command",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"cd /repo && go test ./..."}}]}}`,
			kindToolCall, "test_wait",
		},
		{
			"bash ci watch",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"gh pr checks --watch"}}]}}`,
			kindToolCall, "ci_wait",
		},
		{
			"edit tool",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/a/b.go"}}]}}`,
			kindToolCall, "edit",
		},
		{
			"read tool",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a/b.go"}}]}}`,
			kindToolCall, "explore",
		},
		{
			"subagent delegation",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Task","input":{"prompt":"go"}}]}}`,
			kindToolCall, "agent_wait",
		},
		{
			"mcp tool keeps last segment",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"mcp__server__read_file","input":{}}]}}`,
			kindToolCall, "explore",
		},
		{
			"tool result",
			`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
			kindToolResult, "",
		},
		{
			"thinking block",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"…"}]}}`,
			kindThinking, "",
		},
		{
			"human prompt",
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"ship it"}]}}`,
			kindUserInput, "",
		},
		{
			"assistant prose",
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
			kindAssistantText, "",
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			result, ok := classifyRecord(record(t, item.line), "claude")
			if !ok {
				t.Fatal("record was not classified")
			}
			if result.Kind != item.kind {
				t.Fatalf("kind = %d, want %d", result.Kind, item.kind)
			}
			if result.Category != item.category {
				t.Fatalf("category = %q, want %q", result.Category, item.category)
			}
		})
	}
}

func TestClassifyIgnoresMetadataRecords(t *testing.T) {
	for _, line := range []string{
		`{"type":"attachment","attachment":{"type":"deferred_tools_delta"}}`,
		`{"type":"file-history-snapshot","snapshot":{}}`,
		`{"type":"ai-title","title":"something"}`,
	} {
		if _, ok := classifyRecord(record(t, line), "claude"); ok {
			t.Fatalf("metadata record should not become an event: %s", line)
		}
	}
}

func TestClassifyCodexRecords(t *testing.T) {
	cases := []struct {
		line     string
		kind     eventKind
		category string
	}{
		{`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"command\":[\"bash\",\"-lc\",\"pytest -q\"]}"}}`, kindToolCall, "test_wait"},
		{`{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch"}}`, kindToolCall, "edit"},
		{`{"type":"response_item","payload":{"type":"reasoning","summary":[]}}`, kindThinking, ""},
		{`{"type":"response_item","payload":{"type":"function_call_output","output":"ok"}}`, kindToolResult, ""},
		{`{"type":"response_item","payload":{"type":"message","role":"user","content":[]}}`, kindUserInput, ""},
	}
	for _, item := range cases {
		result, ok := classifyRecord(record(t, item.line), "codex")
		if !ok {
			t.Fatalf("not classified: %s", item.line)
		}
		if result.Kind != item.kind || result.Category != item.category {
			t.Fatalf("got kind=%d category=%q, want kind=%d category=%q", result.Kind, result.Category, item.kind, item.category)
		}
	}
}

func TestClassifyCommand(t *testing.T) {
	cases := map[string]string{
		"cd /repo && go test ./...":        "test_wait",
		"bash -lc 'npm install'":           "dependency_wait",
		"gh run watch 123":                 "ci_wait",
		"git status --short":               "explore",
		"git commit -m x":                  "edit",
		"python3 -c 'print(1)'":            "tool_other",
		"cd /a && cd /b && cargo clippy":   "test_wait",
		"docker build -t x .":              "build_wait",
		"golangci-lint run ./internal/...": "test_wait",
	}
	for command, want := range cases {
		if got, _, _ := classifyCommand(command); got != want {
			t.Fatalf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestRetryKeyIsStableAcrossFormatting(t *testing.T) {
	_, _, first := classifyCommand("go test   ./...")
	_, _, second := classifyCommand("bash -lc 'go test ./...'")
	if first == "" || first != second {
		t.Fatalf("retry keys should match: %q vs %q", first, second)
	}
}

func TestIntervalCategories(t *testing.T) {
	assistant := event{Kind: kindAssistantText}
	if category, _, _ := interval(assistant, event{Kind: kindUserInput}); category != "human_wait" {
		t.Fatalf("assistant text before a human turn should be human_wait, got %s", category)
	}
	if category, _, _ := interval(assistant, event{Kind: kindToolCall}); category != "reasoning" {
		t.Fatalf("assistant text before more tool work should be reasoning, got %s", category)
	}
	if category, _, _ := interval(event{Kind: kindToolResult}, assistant); category != "reasoning" {
		t.Fatalf("time after a tool result is model work, got %s", category)
	}
}

func writeTrace(t *testing.T, provider string, lines []string) candidate {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session-0123456789abcdef.jsonl")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return candidate{Path: path, Provider: provider, ModTime: info.ModTime()}
}

func at(minutes int) string {
	return time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339)
}

func TestParseSessionEndToEnd(t *testing.T) {
	file := writeTrace(t, "claude", []string{
		`{"timestamp":"` + at(0) + `","cwd":"/Users/x/checkout-api","type":"user","message":{"role":"user","content":[{"type":"text","text":"fix it"}]}}`,
		`{"timestamp":"` + at(2) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/a.go"}}]}}`,
		`{"timestamp":"` + at(5) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"timestamp":"` + at(9) + `","type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"FAIL"}]}}`,
		`{"timestamp":"` + at(10) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"timestamp":"` + at(14) + `","type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
	})

	session, ok := parseSession(file, Options{MaxGap: 30 * time.Minute})
	if !ok {
		t.Fatal("expected a session")
	}
	if session.Project != "checkout-api" {
		t.Fatalf("project = %q", session.Project)
	}
	if session.Resolution != resolutionEvent {
		t.Fatalf("resolution = %q", session.Resolution)
	}
	if session.Duration != 14*time.Minute {
		t.Fatalf("duration = %s, want 14m", session.Duration)
	}

	totals := map[string]time.Duration{}
	for _, segment := range session.Segments {
		totals[segment.Category] += segment.Duration
	}
	if totals["reasoning"] != 3*time.Minute {
		t.Fatalf("reasoning = %s, want 3m (2m after the prompt + 1m after the failure)", totals["reasoning"])
	}
	if totals["explore"] != 3*time.Minute {
		t.Fatalf("explore = %s, want 3m", totals["explore"])
	}
	if totals["test_wait"] != 4*time.Minute {
		t.Fatalf("test_wait = %s, want 4m for the first run only", totals["test_wait"])
	}
	if totals["retry"] != 4*time.Minute {
		t.Fatalf("retry = %s, want 4m for the repeated run", totals["retry"])
	}
}

func TestGapPolicy(t *testing.T) {
	// A 45m wait is a plausible block and is clamped to MaxGap. A 12h gap is a
	// session break, not a wait, and must not be counted at all — otherwise an
	// overnight pause invents 30 minutes of "waiting for human".
	file := writeTrace(t, "claude", []string{
		`{"timestamp":"` + at(0) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"gh run watch"}}]}}`,
		`{"timestamp":"` + at(45) + `","type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"timestamp":"` + at(50) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done for today"}]}}`,
		`{"timestamp":"` + at(12*60) + `","type":"user","message":{"role":"user","content":[{"type":"text","text":"back"}]}}`,
		`{"timestamp":"` + at(12*60+3) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{}}]}}`,
	})
	session, ok := parseSession(file, Options{MaxGap: 30 * time.Minute, IdleGap: 2 * time.Hour})
	if !ok {
		t.Fatal("expected a session")
	}

	totals := map[string]time.Duration{}
	clamped := 0
	for _, segment := range session.Segments {
		totals[segment.Category] += segment.Duration
		if segment.Clamped {
			clamped++
			if segment.Confidence > .4 {
				t.Fatalf("clamped segments must lose confidence, got %v", segment.Confidence)
			}
		}
	}
	if totals["ci_wait"] != 30*time.Minute {
		t.Fatalf("ci_wait = %s, want the 45m wait clamped to 30m", totals["ci_wait"])
	}
	if clamped != 1 {
		t.Fatalf("clamped segment count = %d, want 1", clamped)
	}
	if totals["human_wait"] != 0 {
		t.Fatalf("the overnight break must not become human_wait, got %s", totals["human_wait"])
	}
	if session.Duration != 30*time.Minute+5*time.Minute+3*time.Minute {
		t.Fatalf("duration = %s, want 38m (30m clamped + 5m + 3m, overnight dropped)", session.Duration)
	}
	if session.End.Sub(session.Start) != 12*time.Hour+3*time.Minute {
		t.Fatalf("session boundaries should keep the real span, got %s", session.End.Sub(session.Start))
	}
}

func TestEventsBeforeTheWindowAreClamped(t *testing.T) {
	// The file's mtime is recent, but most of its events are older than --days.
	since := time.Date(2026, 7, 30, 10, 30, 0, 0, time.UTC)
	file := writeTrace(t, "claude", []string{
		`{"timestamp":"` + at(0) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"timestamp":"` + at(20) + `","type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{}}]}}`,
		`{"timestamp":"` + at(40) + `","type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
	})
	session, ok := parseSession(file, Options{Since: since, MaxGap: 30 * time.Minute})
	if !ok {
		t.Fatal("expected a session")
	}
	if session.Start.Before(since) {
		t.Fatalf("session starts at %s, before the requested window %s", session.Start, since)
	}
	if session.Duration != 10*time.Minute {
		t.Fatalf("duration = %s, want only the 10m inside the window", session.Duration)
	}
	future := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	if _, ok := parseSession(file, Options{Since: future}); ok {
		t.Fatal("a file whose events all predate the window should produce no session")
	}
}

func TestParseSessionRejectsUnusableTraces(t *testing.T) {
	for name, lines := range map[string][]string{
		"no timestamps": {`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`},
		"not json":      {`this is not json`, `neither is this`},
		"single event":  {`{"timestamp":"` + at(0) + `","type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`},
		"empty":         {},
	} {
		if _, ok := parseSession(writeTrace(t, "claude", lines), Options{MaxGap: time.Minute}); ok {
			t.Fatalf("%s should not produce a session", name)
		}
	}
}

func TestUnixTimeUnits(t *testing.T) {
	want := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	seconds := want.Unix()
	for name, value := range map[string]int64{
		"seconds": seconds,
		"millis":  seconds * 1e3,
		"micros":  seconds * 1e6,
		"nanos":   seconds * 1e9,
	} {
		if got := unixTime(value); !got.Equal(want) {
			t.Fatalf("%s: got %s, want %s", name, got, want)
		}
	}
	if !unixTime(0).IsZero() || !unixTime(-5).IsZero() {
		t.Fatal("non-positive epochs must be zero")
	}
}

func TestRecordTimeFindsNestedStamps(t *testing.T) {
	if recordTime(record(t, `{"payload":{"timestamp":"`+at(0)+`"}}`)).IsZero() {
		t.Fatal("payload timestamps should be found")
	}
	if !recordTime(record(t, `{"nested":{"deep":{"timestamp":"`+at(0)+`"}}}`)).IsZero() {
		t.Fatal("timestamps should not be harvested from arbitrary depth")
	}
}

func TestCleanProject(t *testing.T) {
	cases := map[string]string{
		"-Users-yossi-eliaz-tmp-wasted-cycles": "cycles",
		"/Users/x/checkout-api":                "checkout-api",
		"file:///Users/x/billing":              "billing",
		"":                                     "unknown",
	}
	for raw, want := range cases {
		if got := cleanProject(raw); got != want {
			t.Fatalf("cleanProject(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDiscoverOnlyTakesRecentTraceFiles(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude", "projects", "demo")
	if err := os.MkdirAll(filepath.Join(claude, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string) {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(claude, "keep.jsonl"))
	write(filepath.Join(claude, "mcp-auth.json"))
	write(filepath.Join(claude, "transcript.txt"))
	write(filepath.Join(claude, "subagents", "nested.jsonl"))

	stale := filepath.Join(claude, "stale.jsonl")
	write(stale)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	found := discover(home, time.Now().Add(-time.Hour))
	if len(found) != 1 || filepath.Base(found[0].Path) != "keep.jsonl" {
		names := []string{}
		for _, item := range found {
			names = append(names, filepath.Base(item.Path))
		}
		t.Fatalf("discover picked %v, want only keep.jsonl", names)
	}
}

func TestClassifyGrokThoughtAsReasoning(t *testing.T) {
	category, _, _ := classify("sessionupdate:agent_thought_chunk")
	if category != "reasoning" {
		t.Fatalf("expected reasoning, got %s", category)
	}
}

func TestPromptTextIsExcludedFromClassification(t *testing.T) {
	value := map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "user_message",
			"text": "please run gh pr checks --watch",
		},
	}
	flat := flatten(value, 0, "")
	category, _, _ := classify(flat)
	if category != "reasoning" {
		t.Fatalf("prompt content influenced classification: %s", category)
	}
}

func TestDemoReportTotals(t *testing.T) {
	report := DemoReport()
	if report.Observed <= 0 || report.Blocked <= 0 {
		t.Fatal("demo report should contain observed and recoverable time")
	}
	if report.Throughput <= 0 || report.Throughput >= 1 {
		t.Fatalf("unexpected throughput: %f", report.Throughput)
	}
	if len(report.Findings) < 3 {
		t.Fatalf("expected findings, got %d", len(report.Findings))
	}
	var counted, excluded time.Duration
	for _, category := range report.Categories {
		if category.Group == GroupExcluded {
			excluded += category.Duration
			continue
		}
		counted += category.Duration
	}
	if counted != report.Observed {
		t.Fatalf("counted categories sum to %s but observed is %s", counted, report.Observed)
	}
	if excluded != report.Human {
		t.Fatalf("excluded categories sum to %s but human is %s", excluded, report.Human)
	}
	if report.Human == 0 {
		t.Fatal("the demo should exercise excluded human time")
	}
}

func TestEveryCategoryHasALabel(t *testing.T) {
	for _, id := range categoryOrder {
		if categoryLabels[id] == "" {
			t.Fatalf("category %q has no label", id)
		}
	}
}
