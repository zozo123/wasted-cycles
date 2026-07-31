package githubactions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRepository(t *testing.T) {
	for input, want := range map[string]string{
		"nanoporetech/dorado":                          "nanoporetech/dorado",
		"https://github.com/nanoporetech/dorado":       "nanoporetech/dorado",
		"https://github.com/nanoporetech/dorado/":      "nanoporetech/dorado",
		"https://github.com/nanoporetech/dorado.git":   "nanoporetech/dorado",
		"git@github.com:nanoporetech/dorado.git":       "nanoporetech/dorado",
		"ssh://git@github.com/nanoporetech/dorado.git": "nanoporetech/dorado",
		"github.com/nanoporetech/dorado/actions":       "nanoporetech/dorado",
	} {
		got, err := ParseRepository(input)
		if err != nil {
			t.Fatalf("ParseRepository(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseRepository(%q) = %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{"", "dorado", "https://example.com/a/b", "owner/bad repo"} {
		if _, err := ParseRepository(input); err == nil {
			t.Fatalf("ParseRepository(%q) should fail", input)
		}
	}
}

func TestAnalyzeAggregatesCompletedRuns(t *testing.T) {
	since := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	fetcher := &fixtureFetcher{response: apiRuns{
		TotalCount: 4,
		WorkflowRuns: []apiRun{
			{ID: 1, Name: "build", Status: "completed", Conclusion: "success", CreatedAt: start, RunStartedAt: start.Add(time.Minute), UpdatedAt: start.Add(11 * time.Minute)},
			{ID: 2, Name: "build", Status: "completed", Conclusion: "failure", CreatedAt: start, RunStartedAt: start, UpdatedAt: start.Add(20 * time.Minute)},
			{ID: 3, Name: "lint", Status: "completed", Conclusion: "skipped", CreatedAt: start, RunStartedAt: start, UpdatedAt: start.Add(2 * time.Minute)},
			{ID: 4, Name: "build", Status: "in_progress", CreatedAt: start, RunStartedAt: start},
		},
	}}

	report, err := Analyze(context.Background(), Options{
		Repository: "https://github.com/acme/widgets", Since: since, Window: "last 7 days",
		MaxRuns: 100, fetcher: fetcher, now: func() time.Time { return start.Add(time.Hour) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Repository != "acme/widgets" || report.Runs != 4 || report.CompletedRuns != 3 || report.RunningRuns != 1 {
		t.Fatalf("unexpected counts: %#v", report)
	}
	if report.CIWait != 33*time.Minute {
		t.Fatalf("CIWait = %s, want 33m", report.CIWait)
	}
	if report.WorkflowExecution != 32*time.Minute || report.QueueWait != time.Minute {
		t.Fatalf("execution=%s queue=%s", report.WorkflowExecution, report.QueueWait)
	}
	if report.UnsuccessfulTime != 20*time.Minute || report.UnsuccessfulRuns != 1 {
		t.Fatalf("unsuccessful=%s runs=%d", report.UnsuccessfulTime, report.UnsuccessfulRuns)
	}
	if report.SuccessRate != 2.0/3.0 {
		t.Fatalf("success rate = %v", report.SuccessRate)
	}
	if report.AverageRun != 11*time.Minute || report.MedianRun != 11*time.Minute || report.P95Run != 20*time.Minute {
		t.Fatalf("average=%s median=%s p95=%s", report.AverageRun, report.MedianRun, report.P95Run)
	}
	if len(report.Workflows) != 2 || report.Workflows[0].Name != "build" || report.Workflows[0].CIWait != 31*time.Minute {
		t.Fatalf("workflows = %#v", report.Workflows)
	}
}

func TestHTTPFetcherUsesWindowAndAuthentication(t *testing.T) {
	var authorization, created string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		created = request.URL.Query().Get("created")
		_ = json.NewEncoder(writer).Encode(apiRuns{TotalCount: 0, WorkflowRuns: []apiRun{}})
	}))
	defer server.Close()

	fetcher := &httpFetcher{client: server.Client(), base: server.URL, token: "secret", source: "authenticated-api"}
	_, err := fetcher.Fetch(context.Background(), "acme/widgets", time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC), 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer secret" {
		t.Fatalf("authorization = %q", authorization)
	}
	if created != ">=2026-07-24T01:02:03Z" {
		t.Fatalf("created filter = %q", created)
	}
}

func TestPlainExplainsMetric(t *testing.T) {
	report := Report{
		Repository: "acme/widgets", Window: "last 7 days", Source: "public-api",
		Runs: 2, CompletedRuns: 2, SuccessfulRuns: 1, UnsuccessfulRuns: 1,
		SuccessRate: .5, CIWait: time.Hour, UnsuccessfulTime: 20 * time.Minute,
		QueueWait: 5 * time.Minute, AverageRun: 30 * time.Minute,
		MedianRun: 25 * time.Minute, P95Run: 40 * time.Minute,
		Workflows:   []Workflow{{Name: "build", Runs: 2, UnsuccessfulRuns: 1, CIWait: time.Hour}},
		LongestRuns: []Run{{Title: "slow build", Conclusion: "failure", CIWait: 40 * time.Minute, URL: "https://github.com/acme/widgets/actions/runs/1"}},
	}
	output := Plain(report)
	for _, want := range []string{"WASTED CYCLES · GITHUB ACTIONS", "Workflow latency", "Median / p95", "MOST TIME BY WORKFLOW", "SLOWEST RUNS", "not", "billed runner-minutes"} {
		if !strings.Contains(output, want) {
			t.Fatalf("plain output missing %q:\n%s", want, output)
		}
	}
}

type fixtureFetcher struct {
	response apiRuns
}

func (fetcher *fixtureFetcher) Fetch(context.Context, string, time.Time, int, int) (apiRuns, error) {
	return fetcher.response, nil
}

func (*fixtureFetcher) Source() string { return "fixture" }
