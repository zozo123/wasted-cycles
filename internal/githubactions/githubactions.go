package githubactions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxRuns = 1000
	perPage        = 100
)

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Options struct {
	Repository string
	Since      time.Time
	Window     string
	MaxRuns    int

	fetcher runFetcher
	now     func() time.Time
}

type Report struct {
	GeneratedAt       time.Time     `json:"generated_at"`
	Since             time.Time     `json:"since"`
	Window            string        `json:"window"`
	Repository        string        `json:"repository"`
	RepositoryURL     string        `json:"repository_url"`
	Source            string        `json:"source"`
	Runs              int           `json:"runs"`
	CompletedRuns     int           `json:"completed_runs"`
	RunningRuns       int           `json:"running_runs"`
	SuccessfulRuns    int           `json:"successful_runs"`
	UnsuccessfulRuns  int           `json:"unsuccessful_runs"`
	SkippedRuns       int           `json:"skipped_runs"`
	SuccessRate       float64       `json:"success_rate"`
	CIWait            time.Duration `json:"ci_wait_ns"`
	WorkflowExecution time.Duration `json:"workflow_execution_ns"`
	QueueWait         time.Duration `json:"queue_wait_ns"`
	UnsuccessfulTime  time.Duration `json:"unsuccessful_ns"`
	AverageRun        time.Duration `json:"average_run_ns"`
	MedianRun         time.Duration `json:"median_run_ns"`
	P95Run            time.Duration `json:"p95_run_ns"`
	Workflows         []Workflow    `json:"workflows"`
	LongestRuns       []Run         `json:"longest_runs"`
	Truncated         bool          `json:"truncated"`
}

type Workflow struct {
	Name             string        `json:"name"`
	Runs             int           `json:"runs"`
	UnsuccessfulRuns int           `json:"unsuccessful_runs"`
	CIWait           time.Duration `json:"ci_wait_ns"`
	UnsuccessfulTime time.Duration `json:"unsuccessful_ns"`
}

type Run struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Title       string        `json:"title"`
	Event       string        `json:"event"`
	Conclusion  string        `json:"conclusion"`
	Attempt     int           `json:"attempt"`
	CreatedAt   time.Time     `json:"created_at"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	CIWait      time.Duration `json:"ci_wait_ns"`
	QueueWait   time.Duration `json:"queue_wait_ns"`
	URL         string        `json:"url"`
}

type apiRuns struct {
	TotalCount   int      `json:"total_count"`
	WorkflowRuns []apiRun `json:"workflow_runs"`
}

type apiRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	RunAttempt   int       `json:"run_attempt"`
	CreatedAt    time.Time `json:"created_at"`
	RunStartedAt time.Time `json:"run_started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	HTMLURL      string    `json:"html_url"`
}

type runFetcher interface {
	Fetch(context.Context, string, time.Time, int, int) (apiRuns, error)
	Source() string
}

// ParseRepository accepts owner/name, a github.com URL, or Git's common
// github.com HTTPS/SSH remote forms and returns the canonical owner/name.
func ParseRepository(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("repository is required")
	}

	switch {
	case strings.HasPrefix(value, "git@github.com:"):
		value = strings.TrimPrefix(value, "git@github.com:")
	case strings.HasPrefix(value, "ssh://git@github.com/"):
		value = strings.TrimPrefix(value, "ssh://git@github.com/")
	case strings.Contains(value, "://"):
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("invalid repository URL: %w", err)
		}
		if !strings.EqualFold(parsed.Hostname(), "github.com") {
			return "", fmt.Errorf("repository host must be github.com, got %q", parsed.Hostname())
		}
		value = strings.TrimPrefix(parsed.Path, "/")
	default:
		value = strings.TrimPrefix(value, "github.com/")
	}

	value = strings.TrimSuffix(strings.TrimSuffix(value, "/"), ".git")
	parts := strings.Split(value, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repository must be owner/name or a github.com URL")
	}
	owner, name := parts[0], strings.TrimSuffix(parts[1], ".git")
	if !repositoryPart.MatchString(owner) || !repositoryPart.MatchString(name) {
		return "", fmt.Errorf("invalid GitHub repository %q", value)
	}
	return owner + "/" + name, nil
}

func Analyze(ctx context.Context, options Options) (Report, error) {
	repository, err := ParseRepository(options.Repository)
	if err != nil {
		return Report{}, err
	}
	if options.Since.IsZero() {
		return Report{}, errors.New("analysis start time is required")
	}
	if options.MaxRuns <= 0 {
		options.MaxRuns = defaultMaxRuns
	}
	if options.now == nil {
		options.now = time.Now
	}
	if options.fetcher == nil {
		options.fetcher = defaultFetcher(ctx)
	}

	report := Report{
		GeneratedAt:   options.now(),
		Since:         options.Since,
		Window:        options.Window,
		Repository:    repository,
		RepositoryURL: "https://github.com/" + repository,
		Source:        options.fetcher.Source(),
	}

	var rawRuns []apiRun
	seen := make(map[int64]bool)
	totalCount := 0
	pageSize := min(perPage, options.MaxRuns)
	for page := 1; len(rawRuns) < options.MaxRuns; page++ {
		response, fetchErr := options.fetcher.Fetch(ctx, repository, options.Since, page, pageSize)
		if fetchErr != nil {
			if report.Source == "public-api" {
				return Report{}, fmt.Errorf("%w; if the repository is private, authenticate with `gh auth login`", fetchErr)
			}
			return Report{}, fetchErr
		}
		if page == 1 {
			totalCount = response.TotalCount
		}
		if len(response.WorkflowRuns) == 0 {
			break
		}
		for _, raw := range response.WorkflowRuns {
			if seen[raw.ID] {
				continue
			}
			seen[raw.ID] = true
			rawRuns = append(rawRuns, raw)
			if len(rawRuns) == options.MaxRuns {
				break
			}
		}
		if len(response.WorkflowRuns) < pageSize {
			break
		}
		if totalCount > 0 && page*pageSize >= totalCount {
			break
		}
	}
	report.Runs = len(rawRuns)
	report.Truncated = totalCount > len(rawRuns)
	finalize(&report, rawRuns)
	return report, nil
}

func finalize(report *Report, rawRuns []apiRun) {
	workflows := make(map[string]*Workflow)
	runs := make([]Run, 0, len(rawRuns))
	var durations []time.Duration

	for _, raw := range rawRuns {
		if raw.Status != "completed" {
			report.RunningRuns++
			continue
		}
		started := raw.RunStartedAt
		if started.IsZero() {
			started = raw.CreatedAt
		}
		if started.IsZero() || raw.UpdatedAt.IsZero() || raw.UpdatedAt.Before(started) {
			report.SkippedRuns++
			continue
		}

		execution := raw.UpdatedAt.Sub(started)
		queue := time.Duration(0)
		if !raw.CreatedAt.IsZero() && started.After(raw.CreatedAt) {
			queue = started.Sub(raw.CreatedAt)
		}
		wait := execution + queue
		name := strings.TrimSpace(raw.Name)
		if name == "" {
			name = "(unnamed workflow)"
		}

		run := Run{
			ID: raw.ID, Name: name, Title: raw.DisplayTitle, Event: raw.Event,
			Conclusion: raw.Conclusion, Attempt: raw.RunAttempt, CreatedAt: raw.CreatedAt,
			StartedAt: started, CompletedAt: raw.UpdatedAt, CIWait: wait, QueueWait: queue,
			URL: raw.HTMLURL,
		}
		runs = append(runs, run)
		durations = append(durations, wait)

		workflow := workflows[name]
		if workflow == nil {
			workflow = &Workflow{Name: name}
			workflows[name] = workflow
		}
		workflow.Runs++
		workflow.CIWait += wait

		report.CompletedRuns++
		report.CIWait += wait
		report.WorkflowExecution += execution
		report.QueueWait += queue
		if successful(raw.Conclusion) {
			report.SuccessfulRuns++
		} else {
			report.UnsuccessfulRuns++
			report.UnsuccessfulTime += wait
			workflow.UnsuccessfulRuns++
			workflow.UnsuccessfulTime += wait
		}
	}

	if report.CompletedRuns > 0 {
		report.SuccessRate = float64(report.SuccessfulRuns) / float64(report.CompletedRuns)
		report.AverageRun = report.CIWait / time.Duration(report.CompletedRuns)
		report.MedianRun = percentile(durations, .5)
		report.P95Run = percentile(durations, .95)
	}
	for _, workflow := range workflows {
		report.Workflows = append(report.Workflows, *workflow)
	}
	sort.Slice(report.Workflows, func(i, j int) bool {
		if report.Workflows[i].CIWait == report.Workflows[j].CIWait {
			return report.Workflows[i].Name < report.Workflows[j].Name
		}
		return report.Workflows[i].CIWait > report.Workflows[j].CIWait
	})
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CIWait == runs[j].CIWait {
			return runs[i].ID > runs[j].ID
		}
		return runs[i].CIWait > runs[j].CIWait
	})
	if len(runs) > 10 {
		runs = runs[:10]
	}
	report.LongestRuns = runs
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	index = max(0, min(index, len(sorted)-1))
	return sorted[index]
}

func successful(conclusion string) bool {
	switch strings.ToLower(conclusion) {
	case "success", "neutral", "skipped":
		return true
	default:
		return false
	}
}

type httpFetcher struct {
	client *http.Client
	base   string
	token  string
	source string
}

func (fetcher *httpFetcher) Source() string { return fetcher.source }

func (fetcher *httpFetcher) Fetch(ctx context.Context, repository string, since time.Time, page, limit int) (apiRuns, error) {
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	query.Set("created", ">="+since.UTC().Format(time.RFC3339))
	endpoint := strings.TrimSuffix(fetcher.base, "/") + "/repos/" + repository + "/actions/runs?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return apiRuns{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "wasted-cycles")
	if fetcher.token != "" {
		request.Header.Set("Authorization", "Bearer "+fetcher.token)
	}

	response, err := fetcher.client.Do(request)
	if err != nil {
		return apiRuns{}, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer response.Body.Close()
	return decodeResponse(response.StatusCode, response.Body)
}

type ghFetcher struct{}

func (ghFetcher) Source() string { return "github-cli" }

func (ghFetcher) Fetch(ctx context.Context, repository string, since time.Time, page, limit int) (apiRuns, error) {
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(limit))
	query.Set("page", strconv.Itoa(page))
	query.Set("created", ">="+since.UTC().Format(time.RFC3339))
	endpoint := "repos/" + repository + "/actions/runs?" + query.Encode()
	command := exec.CommandContext(ctx, "gh", "api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		endpoint,
	)
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(exitErr.Stderr))
			if message != "" {
				return apiRuns{}, fmt.Errorf("gh api failed: %s", message)
			}
		}
		return apiRuns{}, fmt.Errorf("gh api failed: %w", err)
	}
	return decodeResponse(http.StatusOK, bytes.NewReader(output))
}

func decodeResponse(status int, body io.Reader) (apiRuns, error) {
	if status < 200 || status >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&failure)
		if failure.Message == "" {
			failure.Message = http.StatusText(status)
		}
		return apiRuns{}, fmt.Errorf("GitHub API returned %d: %s", status, failure.Message)
	}
	var response apiRuns
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return apiRuns{}, fmt.Errorf("decode GitHub Actions response: %w", err)
	}
	return response, nil
}

func defaultFetcher(ctx context.Context) runFetcher {
	if token := firstNonEmpty(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN")); token != "" {
		return &httpFetcher{
			client: &http.Client{Timeout: 30 * time.Second},
			base:   "https://api.github.com",
			token:  token,
			source: "authenticated-api",
		}
	}
	if _, err := exec.LookPath("gh"); err == nil {
		command := exec.CommandContext(ctx, "gh", "auth", "status", "--hostname", "github.com")
		if err := command.Run(); err == nil {
			return ghFetcher{}
		}
	}
	return &httpFetcher{
		client: &http.Client{Timeout: 30 * time.Second},
		base:   "https://api.github.com",
		source: "public-api",
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
