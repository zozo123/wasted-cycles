package analyze

import (
	"testing"
	"time"
)

func TestBuildSavingsConservativeAndOptional(t *testing.T) {
	if got := buildSavings(map[string]time.Duration{"build_wait": 30 * time.Second}, time.Now().Add(-7*24*time.Hour)); got != nil {
		t.Fatalf("sub-minute addressable savings should be omitted, got %+v", got)
	}

	totals := map[string]time.Duration{
		"build_wait": 2 * time.Hour,  // 50% → 1h
		"test_wait":  2 * time.Hour,  // 40% → 48m
		"ci_wait":    1 * time.Hour,  // 50% → 30m
		"agent_wait": 3 * time.Hour,  // not accelerateable
		"human_wait": 10 * time.Hour, // excluded entirely
	}
	got := buildSavings(totals, time.Now().Add(-7*24*time.Hour))
	if got == nil {
		t.Fatal("expected savings")
	}
	want := time.Hour + 48*time.Minute + 30*time.Minute
	if got.Addressable != want {
		t.Fatalf("addressable = %s, want %s", got.Addressable, want)
	}
	if got.EngineerUSD <= 0 || got.ComputeUSD <= 0 {
		t.Fatalf("expected positive illustrative money, got eng=%v compute=%v", got.EngineerUSD, got.ComputeUSD)
	}
	if got.AnnualEngineerUSD <= got.EngineerUSD {
		t.Fatalf("annual eng should extrapolate above window eng: %+v", got)
	}
	if got.Disclaimer == "" || len(got.Options) != 3 {
		t.Fatalf("missing disclaimer or options: %+v", got)
	}
	for _, option := range got.Options {
		if option.URL == "" || option.Name == "" {
			t.Fatalf("incomplete option: %+v", option)
		}
	}
}

func TestDemoReportIncludesSavings(t *testing.T) {
	report := DemoReport()
	if report.Savings == nil || report.Savings.Addressable < time.Minute {
		t.Fatalf("demo should expose illustrative savings, got %+v", report.Savings)
	}
}
