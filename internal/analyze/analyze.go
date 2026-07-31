package analyze

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Since    time.Time
	MaxGap   time.Duration
	MaxFiles int
}

type Category struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Duration    time.Duration `json:"duration_ns"`
	Recoverable bool          `json:"recoverable"`
}

type Segment struct {
	Start      time.Time     `json:"start"`
	End        time.Time     `json:"end"`
	Category   string        `json:"category"`
	Label      string        `json:"label"`
	Provider   string        `json:"provider"`
	SessionID  string        `json:"session_id"`
	Confidence float64       `json:"confidence"`
	Duration   time.Duration `json:"duration_ns"`
}

type Session struct {
	ID         string        `json:"id"`
	Provider   string        `json:"provider"`
	Project    string        `json:"project"`
	Path       string        `json:"-"`
	Start      time.Time     `json:"start"`
	End        time.Time     `json:"end"`
	Duration   time.Duration `json:"duration_ns"`
	Throughput float64       `json:"throughput"`
	Segments   []Segment     `json:"segments"`
}

type Finding struct {
	Title       string        `json:"title"`
	Detail      string        `json:"detail"`
	Action      string        `json:"action"`
	Category    string        `json:"category"`
	Recoverable time.Duration `json:"recoverable_ns"`
}

type Source struct {
	Provider string `json:"provider"`
	Files    int    `json:"files"`
}

type Report struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Since       time.Time     `json:"since"`
	Observed    time.Duration `json:"observed_ns"`
	Recoverable time.Duration `json:"recoverable_ns"`
	Throughput  float64       `json:"throughput"`
	Sessions    []Session     `json:"sessions"`
	Categories  []Category    `json:"categories"`
	Findings    []Finding     `json:"findings"`
	Sources     []Source      `json:"sources"`
	Scanned     int           `json:"files_scanned"`
	Skipped     int           `json:"files_skipped"`
	IsDemo      bool          `json:"is_demo"`
}

type candidate struct {
	Path     string
	Provider string
	ModTime  time.Time
}

type event struct {
	At       time.Time
	Category string
	Label    string
	Key      string
}

var categoryOrder = []string{
	"reasoning", "explore", "edit", "verify", "tool", "ci_wait",
	"agent_wait", "human_wait", "dependency_wait", "retry", "unknown",
}

var categoryLabels = map[string]string{
	"reasoning":       "Model work",
	"explore":         "Read & search",
	"edit":            "Code changes",
	"verify":          "Local verify",
	"tool":            "Other tool work",
	"ci_wait":         "Waiting for CI",
	"agent_wait":      "Waiting for agents",
	"human_wait":      "Waiting for human",
	"dependency_wait": "Tool / network wait",
	"retry":           "Retries",
	"unknown":         "Unclassified",
}

func Run(opts Options) (Report, error) {
	if opts.MaxGap <= 0 {
		opts.MaxGap = 30 * time.Minute
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 600
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Report{}, err
	}

	candidates := discover(home, opts.Since)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ModTime.After(candidates[j].ModTime) })
	skipped := 0
	if len(candidates) > opts.MaxFiles {
		skipped = len(candidates) - opts.MaxFiles
		candidates = candidates[:opts.MaxFiles]
	}

	report := Report{GeneratedAt: time.Now(), Since: opts.Since, Skipped: skipped}
	sourceCounts := map[string]int{}
	for _, file := range candidates {
		session, ok := parseSession(file, opts)
		if !ok {
			continue
		}
		report.Sessions = append(report.Sessions, session)
		sourceCounts[file.Provider]++
	}
	report.Scanned = len(candidates)
	sort.Slice(report.Sessions, func(i, j int) bool { return report.Sessions[i].End.After(report.Sessions[j].End) })
	for provider, files := range sourceCounts {
		report.Sources = append(report.Sources, Source{Provider: provider, Files: files})
	}
	sort.Slice(report.Sources, func(i, j int) bool { return report.Sources[i].Provider < report.Sources[j].Provider })
	finalize(&report)
	return report, nil
}

func discover(home string, since time.Time) []candidate {
	roots := []struct {
		Provider string
		Path     string
	}{
		{"codex", filepath.Join(home, ".codex", "sessions")},
		{"claude", filepath.Join(home, ".claude", "projects")},
		{"cursor", filepath.Join(home, ".cursor", "projects")},
		{"grok", filepath.Join(home, ".grok", "sessions")},
	}
	var out []candidate
	for _, root := range roots {
		_ = filepath.WalkDir(root.Path, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, os.ErrPermission) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if strings.Contains(path, string(filepath.Separator)+"subagents") {
					return filepath.SkipDir
				}
				return nil
			}
			name := strings.ToLower(entry.Name())
			if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".txt") {
				return nil
			}
			if root.Provider == "grok" && name != "updates.jsonl" {
				return nil
			}
			info, err := entry.Info()
			if err == nil && !info.ModTime().Before(since) {
				out = append(out, candidate{Path: path, Provider: root.Provider, ModTime: info.ModTime()})
			}
			return nil
		})
	}
	return out
}

func parseSession(file candidate, opts Options) (Session, bool) {
	events, project := readEvents(file)
	if len(events) < 2 {
		if file.Provider == "grok" {
			events = grokBoundaryEvents(file.Path)
		}
	}
	if len(events) < 2 {
		return Session{}, false
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	id := strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))
	if file.Provider == "grok" {
		id = filepath.Base(filepath.Dir(file.Path))
	}
	if project == "" {
		project = inferProject(file)
	}
	session := Session{ID: shortID(id), Provider: file.Provider, Project: project, Path: file.Path}
	session.Start = events[0].At
	session.End = events[len(events)-1].At

	seen := map[string]int{}
	for i := 0; i < len(events)-1; i++ {
		current, next := events[i], events[i+1]
		duration := next.At.Sub(current.At)
		if duration <= 0 {
			continue
		}
		if duration > opts.MaxGap {
			duration = opts.MaxGap
		}
		category := current.Category
		if current.Key != "" {
			seen[current.Key]++
			if seen[current.Key] > 1 && (category == "verify" || category == "ci_wait") {
				category = "retry"
			}
		}
		segment := Segment{
			Start: current.At, End: current.At.Add(duration), Duration: duration,
			Category: category, Label: current.Label, Provider: file.Provider,
			SessionID: session.ID, Confidence: confidenceFor(category, current),
		}
		session.Segments = append(session.Segments, segment)
		session.Duration += duration
	}
	if session.Duration < time.Second {
		return Session{}, false
	}
	active := activeDuration(session.Segments)
	session.Throughput = float64(active) / float64(session.Duration)
	return session, true
}

func readEvents(file candidate) ([]event, string) {
	handle, err := os.Open(file.Path)
	if err != nil {
		return nil, ""
	}
	defer handle.Close()

	var events []event
	project := ""
	reader := bufio.NewReaderSize(io.LimitReader(handle, 64<<20), 64<<10)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var value any
			if json.Unmarshal(line, &value) == nil {
				at := findTime(value)
				if !at.IsZero() {
					flat := strings.ToLower(flatten(value, 0, ""))
					category, label, key := classify(flat)
					if category == "unknown" && os.Getenv("WASTED_CYCLES_DEBUG") != "" {
						fmt.Fprintln(os.Stderr, "unclassified:", flat)
					}
					if category != "meta" {
						events = append(events, event{At: at, Category: category, Label: label, Key: key})
					}
				}
				if project == "" {
					project = findString(value, "cwd", "projectPath", "workspace", "project")
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return events, cleanProject(project)
}

func classify(flat string) (string, string, string) {
	switch {
	case containsAny(flat, "token_count", "session_meta", "turn_context", "world_state", "thread_settings_applied", "task_started", "role:developer", "role:system"):
		return "meta", "trace metadata", ""
	case containsAny(flat, "wait_agent", "wait agent", "waiting on agent", "join agents"):
		return "agent_wait", "agent join", ""
	case containsAny(flat, "retry_state", "type:retrying"):
		return "retry", "harness retry", ""
	case containsAny(flat, "gh run watch", "gh pr checks --watch", "waiting for ci", "github actions", "buildkite-agent", "circleci"):
		return "ci_wait", "CI feedback", commandKey(flat)
	case containsAny(flat, "go test", "cargo test", "pytest", "npm test", "pnpm test", "yarn test", "vitest", "jest", "rspec", "mvn test", "gradle test"):
		return "verify", "test suite", commandKey(flat)
	case containsAny(flat, "apply_patch", "patch_apply", "write_file", "edit_file", "str_replace", "search_replace", "\"name\":\"edit", "\"name\": \"edit"):
		return "edit", "code change", ""
	case containsAny(flat, "read_file", "list_dir", "\"name\":\"read", "\"name\": \"read", "\"name\":\"grep", "\"name\": \"grep", "search_query", "glob"):
		return "explore", "read / search", ""
	case containsAny(flat, "npm install", "pnpm install", "yarn install", "cargo fetch", "go mod download", "pip install", "curl ", "wget "):
		return "dependency_wait", "dependency / network", commandKey(flat)
	case containsAny(flat, "\"role\":\"assistant", "\"role\": \"assistant", "role:assistant", "agent_message", "final_answer"):
		return "human_wait", "handoff to human", ""
	case containsAny(flat, "sessionupdate:turn_completed"):
		return "human_wait", "turn complete", ""
	case containsAny(flat, "\"role\":\"user", "\"role\": \"user", "role:user", "user_message", "tool_result", "function_call_output"):
		return "reasoning", "model response", ""
	case containsAny(flat, "custom_tool_call_output", "tool_search_output", "mcp_tool_call_end", "web_search_end", "image_generation_end", "type:reasoning", "agent_thought_chunk", "sessionupdate:plan", "session_recap"):
		return "reasoning", "model response", ""
	case containsAny(flat, "sessionupdate:task_completed"):
		return "tool", "tool execution", commandKey(flat)
	case containsAny(flat, "custom_tool_call", "function_call", "tool_use", "tool_call", "write_stdin", "tool_search_call"):
		return "tool", "tool execution", commandKey(flat)
	default:
		return "unknown", "unclassified event", ""
	}
}

func findTime(value any) time.Time {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"timestamp", "created_at", "updated_at", "time", "ts"} {
			if raw, ok := v[key]; ok {
				if parsed := parseTime(raw); !parsed.IsZero() {
					return parsed
				}
			}
		}
		for _, child := range v {
			if parsed := findTime(child); !parsed.IsZero() {
				return parsed
			}
		}
	case []any:
		for _, child := range v {
			if parsed := findTime(child); !parsed.IsZero() {
				return parsed
			}
		}
	}
	return time.Time{}
}

func parseTime(value any) time.Time {
	switch v := value.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return parsed
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return unixTime(n)
		}
	case float64:
		return unixTime(int64(v))
	}
	return time.Time{}
}

func unixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value < 1_000_000_000_000 {
		return time.Unix(value, 0)
	}
	return time.UnixMilli(value)
}

func flatten(value any, depth int, parentKey string) string {
	if depth > 7 {
		return ""
	}
	switch v := value.(type) {
	case map[string]any:
		var builder strings.Builder
		for key, child := range v {
			builder.WriteString(key)
			builder.WriteByte(':')
			builder.WriteString(flatten(child, depth+1, key))
			builder.WriteByte(' ')
		}
		return builder.String()
	case []any:
		var builder strings.Builder
		for _, child := range v {
			builder.WriteString(flatten(child, depth+1, parentKey))
			builder.WriteByte(' ')
		}
		return builder.String()
	case string:
		if !classificationField(parentKey) {
			return ""
		}
		if len(v) > 800 {
			return v[:800]
		}
		return v
	case float64, bool:
		return fmt.Sprint(v)
	default:
		return ""
	}
}

func classificationField(key string) bool {
	switch strings.ToLower(key) {
	case "type", "role", "name", "command", "cmd", "title", "sessionupdate", "tool", "function", "arguments", "input", "rawinput":
		return true
	default:
		return false
	}
}

func findString(value any, keys ...string) string {
	keySet := map[string]bool{}
	for _, key := range keys {
		keySet[strings.ToLower(key)] = true
	}
	var walk func(any) string
	walk = func(current any) string {
		switch v := current.(type) {
		case map[string]any:
			for key, child := range v {
				if keySet[strings.ToLower(key)] {
					if text, ok := child.(string); ok {
						return text
					}
				}
			}
			for _, child := range v {
				if found := walk(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range v {
				if found := walk(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(value)
}

func grokBoundaryEvents(path string) []event {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "summary.json"))
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	start := parseTime(findRaw(value, "created_at"))
	end := parseTime(findRaw(value, "updated_at"))
	if end.IsZero() {
		end = parseTime(findRaw(value, "last_active_at"))
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil
	}
	return []event{
		{At: start, Category: "reasoning", Label: "Grok Build session"},
		{At: end, Category: "unknown", Label: "session end"},
	}
}

func findRaw(value any, key string) any {
	if object, ok := value.(map[string]any); ok {
		if raw, found := object[key]; found {
			return raw
		}
		for _, child := range object {
			if raw := findRaw(child, key); raw != nil {
				return raw
			}
		}
	}
	return nil
}

func finalize(report *Report) {
	totals := map[string]time.Duration{}
	for _, session := range report.Sessions {
		report.Observed += session.Duration
		for _, segment := range session.Segments {
			totals[segment.Category] += segment.Duration
		}
	}
	for _, id := range categoryOrder {
		if totals[id] == 0 {
			continue
		}
		recoverable := isRecoverable(id)
		report.Categories = append(report.Categories, Category{
			ID: id, Label: categoryLabels[id], Duration: totals[id], Recoverable: recoverable,
		})
		if recoverable {
			report.Recoverable += totals[id]
		}
	}
	if report.Observed > 0 {
		report.Throughput = float64(report.Observed-report.Recoverable) / float64(report.Observed)
	}
	report.Findings = buildFindings(totals)
}

func buildFindings(totals map[string]time.Duration) []Finding {
	type spec struct {
		ID, Title, Detail, Action string
		Min                       time.Duration
	}
	specs := []spec{
		{"ci_wait", "CI is your longest feedback loop", "Agent runs are blocked on remote checks instead of continuing useful work.", "Mirror the slow gate locally and split independent checks into parallel jobs.", 2 * time.Minute},
		{"human_wait", "Handoffs are stalling runs", "The harness reaches a human decision and stops its critical path.", "Front-load acceptance criteria and pre-approve safe tools for unattended runs.", 2 * time.Minute},
		{"retry", "Verification is repeating", "The same verification command appears more than once in a session.", "Capture the first failure, run the narrowest affected test, and quarantine flakes.", time.Minute},
		{"agent_wait", "Delegation is serializing", "A parent agent waits while delegated work runs.", "Fan out independent tasks together and keep a local task ready during joins.", time.Minute},
		{"dependency_wait", "Setup is inside the hot loop", "Package or network work is consuming observed session time.", "Cache dependencies in the harness image and warm them before the agent starts.", time.Minute},
	}
	var out []Finding
	for _, item := range specs {
		if totals[item.ID] < item.Min {
			continue
		}
		out = append(out, Finding{
			Title: item.Title, Detail: item.Detail, Action: item.Action,
			Category: item.ID, Recoverable: totals[item.ID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Recoverable > out[j].Recoverable })
	return out
}

func activeDuration(segments []Segment) time.Duration {
	var total time.Duration
	for _, segment := range segments {
		if !isRecoverable(segment.Category) {
			total += segment.Duration
		}
	}
	return total
}

func isRecoverable(category string) bool {
	switch category {
	case "ci_wait", "agent_wait", "human_wait", "dependency_wait", "retry":
		return true
	default:
		return false
	}
}

func inferProject(file candidate) string {
	dir := filepath.Dir(file.Path)
	if file.Provider == "grok" {
		dir = filepath.Dir(filepath.Dir(file.Path))
	}
	return cleanProject(filepath.Base(dir))
}

func cleanProject(raw string) string {
	if raw == "" {
		return "unknown"
	}
	raw = strings.TrimPrefix(raw, "file://")
	raw = strings.TrimSuffix(raw, string(filepath.Separator))
	base := filepath.Base(raw)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "unknown"
	}
	if strings.HasPrefix(base, "-Users-") {
		parts := strings.Split(strings.Trim(base, "-"), "-")
		return parts[len(parts)-1]
	}
	return base
}

func shortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func commandKey(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 180 {
		value = value[:180]
	}
	return value
}

func confidenceFor(category string, current event) float64 {
	if category == "unknown" {
		return 0.35
	}
	if current.Key != "" {
		return 0.9
	}
	return 0.72
}
