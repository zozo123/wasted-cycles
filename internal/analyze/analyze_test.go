package analyze

import (
	"testing"
	"time"
)

func TestClassifyCI(t *testing.T) {
	category, _, _ := classify(`{"name":"exec_command","cmd":"gh pr checks --watch"}`)
	if category != "ci_wait" {
		t.Fatalf("expected ci_wait, got %s", category)
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
	if report.Observed <= 0 || report.Recoverable <= 0 {
		t.Fatal("demo report should contain observed and recoverable time")
	}
	if report.Throughput <= 0 || report.Throughput >= 1 {
		t.Fatalf("unexpected throughput: %f", report.Throughput)
	}
	if len(report.Findings) < 3 {
		t.Fatalf("expected findings, got %d", len(report.Findings))
	}
}

func TestMaxGap(t *testing.T) {
	now := time.Now()
	events := []event{
		{At: now, Category: "human_wait"},
		{At: now.Add(12 * time.Hour), Category: "reasoning"},
	}
	if got := events[1].At.Sub(events[0].At); got != 12*time.Hour {
		t.Fatal("test setup failed")
	}
}
