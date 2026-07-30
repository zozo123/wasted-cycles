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
