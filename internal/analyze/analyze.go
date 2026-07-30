package analyze

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Since    time.Time
	MaxGap   time.Duration
	IdleGap  time.Duration
	MaxFiles int
	Workers  int
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
	Clamped    bool          `json:"clamped"`
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
	Resolution string        `json:"resolution"`
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
	Inferred    time.Duration `json:"inferred_ns"`
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
	At         time.Time
	Kind       eventKind
	Category   string
	Label      string
	Key        string
	Confidence float64
}

const (
	resolutionEvent = "event"
	resolutionTurn  = "turn"
	maxLineBytes    = 4 << 20
)

var categoryOrder = []string{
	"reasoning", "explore", "edit", "verify", "tool_other", "ci_wait",
	"agent_wait", "human_wait", "dependency_wait", "retry", "unknown",
}

var categoryLabels = map[string]string{
	"reasoning":       "Model work",
	"explore":         "Read & search",
	"edit":            "Code changes",
	"verify":          "Local verify",
	"tool_other":      "Other tool work",
	"ci_wait":         "Waiting for CI",
	"agent_wait":      "Waiting for agents",
	"human_wait":      "Waiting for human",
	"dependency_wait": "Tool / network wait",
	"retry":           "Retries",
	"unknown":         "Unclassified",
}

func (o Options) withDefaults() Options {
	if o.MaxGap <= 0 {
		o.MaxGap = 30 * time.Minute
	}
	if o.IdleGap <= o.MaxGap {
		o.IdleGap = max(2*time.Hour, 4*o.MaxGap)
	}
	if o.MaxFiles <= 0 {
		o.MaxFiles = 600
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	return o
}

func Run(opts Options) (Report, error) {
	opts = opts.withDefaults()
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

	report := Report{GeneratedAt: time.Now(), Since: opts.Since, Skipped: skipped, Scanned: len(candidates)}
	for _, session := range parseAll(candidates, opts) {
		report.Sessions = append(report.Sessions, session)
	}

	sourceCounts := map[string]int{}
	for _, session := range report.Sessions {
		sourceCounts[session.Provider]++
	}
	sort.SliceStable(report.Sessions, func(i, j int) bool { return report.Sessions[i].End.After(report.Sessions[j].End) })
	for provider, files := range sourceCounts {
		report.Sources = append(report.Sources, Source{Provider: provider, Files: files})
	}
	sort.Slice(report.Sources, func(i, j int) bool { return report.Sources[i].Provider < report.Sources[j].Provider })
	finalize(&report)
	return report, nil
}

func parseAll(candidates []candidate, opts Options) []Session {
	results := make([]Session, len(candidates))
	found := make([]bool, len(candidates))

	queue := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < opts.Workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range queue {
				if session, ok := parseSession(candidates[index], opts); ok {
					results[index] = session
					found[index] = true
				}
			}
		}()
	}
	for index := range candidates {
		queue <- index
	}
	close(queue)
	group.Wait()

	sessions := make([]Session, 0, len(candidates))
	for index, ok := range found {
		if ok {
			sessions = append(sessions, results[index])
		}
	}
	return sessions
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
				if skipDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			name := strings.ToLower(entry.Name())
			if !strings.HasSuffix(name, ".jsonl") {
				return nil
			}
			if root.Provider == "grok" && name != "updates.jsonl" {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(since) {
				return nil
			}
			out = append(out, candidate{Path: path, Provider: root.Provider, ModTime: info.ModTime()})
			return nil
		})
	}
	return out
}

func skipDir(name string) bool {
	switch name {
	case "subagents", "node_modules", ".git", "shell-snapshots", "statsig":
		return true
	}
	return false
}

func parseSession(file candidate, opts Options) (Session, bool) {
	opts = opts.withDefaults()
	events, project, resolution := readEvents(file)
	if len(events) < 2 && file.Provider == "grok" {
		events, resolution = grokBoundaryEvents(file.Path), resolutionTurn
	}
	if len(events) < 2 {
		return Session{}, false
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	events = clampToWindow(events, opts.Since)
	if len(events) < 2 {
		return Session{}, false
	}

	id := strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))
	if file.Provider == "grok" {
		id = filepath.Base(filepath.Dir(file.Path))
	}
	if project == "" {
		project = inferProject(file)
	}
	session := Session{
		ID: shortID(id), Provider: file.Provider, Project: project,
		Path: file.Path, Resolution: resolution,
		Start: events[0].At, End: events[len(events)-1].At,
	}

	seen := map[string]int{}
	for i := 0; i < len(events)-1; i++ {
		current, next := events[i], events[i+1]
		duration := next.At.Sub(current.At)
		if duration <= 0 || duration > opts.IdleGap {
			continue
		}
		clamped := duration > opts.MaxGap
		if clamped {
			duration = opts.MaxGap
		}
		category, label, confidence := interval(current, next)
		if clamped {
			confidence = .3
		}
		if current.Key != "" {
			seen[current.Key]++
			if seen[current.Key] > 1 && (category == "verify" || category == "ci_wait") {
				category, label = "retry", "repeated "+label
			}
		}
		session.Segments = append(session.Segments, Segment{
			Start: current.At, End: current.At.Add(duration), Duration: duration,
			Category: category, Label: label, Provider: file.Provider,
			SessionID: session.ID, Confidence: confidence, Clamped: clamped,
		})
		session.Duration += duration
	}
	if session.Duration < time.Second {
		return Session{}, false
	}
	session.Throughput = float64(activeDuration(session.Segments)) / float64(session.Duration)
	return session, true
}

func clampToWindow(events []event, since time.Time) []event {
	if since.IsZero() {
		return events
	}
	start := 0
	for index, item := range events {
		if item.At.Before(since) {
			start = index
			continue
		}
		break
	}
	if events[start].At.Before(since) {
		if start == len(events)-1 {
			return nil
		}
		events[start].At = since
	}
	return events[start:]
}

func interval(current, next event) (string, string, float64) {
	switch current.Kind {
	case kindToolCall:
		return current.Category, current.Label, current.Confidence
	case kindUserInput:
		return "reasoning", "model response", .85
	case kindToolResult:
		return "reasoning", "model response", .8
	case kindThinking:
		return "reasoning", "extended thinking", .9
	case kindAssistantText:
		if next.Kind == kindUserInput {
			return "human_wait", "handoff to human", .8
		}
		return "reasoning", "model response", .7
	default:
		return "unknown", "unclassified event", .35
	}
}

func readEvents(file candidate) ([]event, string, string) {
	if file.Provider == "cursor" {
		events, project := cursorEvents(file.Path)
		return events, project, resolutionTurn
	}
	handle, err := os.Open(file.Path)
	if err != nil {
		return nil, "", resolutionEvent
	}
	defer handle.Close()

	var events []event
	project := ""
	scan := newLineScanner(handle)
	for {
		line, ok := scan()
		if !ok {
			break
		}
		record, ok := decodeRecord(line)
		if !ok {
			continue
		}
		if project == "" {
			project = cleanProject(recordProject(record))
		}
		at := recordTime(record)
		if at.IsZero() {
			continue
		}
		result, ok := classifyRecord(record, file.Provider)
		if !ok {
			continue
		}
		events = append(events, event{
			At: at, Kind: result.Kind, Category: result.Category,
			Label: result.Label, Key: result.Key, Confidence: result.Confidence,
		})
	}
	return events, project, resolutionEvent
}

func newLineScanner(handle io.Reader) func() ([]byte, bool) {
	reader := bufio.NewReaderSize(handle, 64<<10)
	return func() ([]byte, bool) {
		var line []byte
		for {
			chunk, err := reader.ReadSlice('\n')
			if len(line)+len(chunk) <= maxLineBytes {
				line = append(line, chunk...)
			}
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				return line, len(line) > 0
			}
			return line, true
		}
	}
}

func decodeRecord(line []byte) (map[string]any, bool) {
	line = trimSpaceBytes(line)
	if len(line) == 0 || line[0] != '{' {
		return nil, false
	}
	var record map[string]any
	if json.Unmarshal(line, &record) != nil {
		return nil, false
	}
	return record, true
}

func trimSpaceBytes(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r' || line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
		line = line[:len(line)-1]
	}
	for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		line = line[1:]
	}
	return line
}

var timeKeys = []string{"timestamp", "created_at", "createdAt", "time", "ts", "started_at", "updated_at"}

func recordTime(record map[string]any) time.Time {
	if at := timeFrom(record); !at.IsZero() {
		return at
	}
	for _, key := range []string{"payload", "message", "meta"} {
		if nested, ok := record[key].(map[string]any); ok {
			if at := timeFrom(nested); !at.IsZero() {
				return at
			}
		}
	}
	return time.Time{}
}

func timeFrom(record map[string]any) time.Time {
	for _, key := range timeKeys {
		if raw, ok := record[key]; ok {
			if parsed := parseTime(raw); !parsed.IsZero() {
				return parsed
			}
		}
	}
	return time.Time{}
}

var projectKeys = []string{"cwd", "projectPath", "workspace", "project", "workspacePath"}

func recordProject(record map[string]any) string {
	for _, key := range projectKeys {
		if value, ok := record[key].(string); ok && value != "" {
			return value
		}
	}
	for _, key := range []string{"payload", "message", "meta"} {
		if nested, ok := record[key].(map[string]any); ok {
			for _, field := range projectKeys {
				if value, ok := nested[field].(string); ok && value != "" {
					return value
				}
			}
		}
	}
	return ""
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700", "2006-01-02 15:04:05.999999-07:00", "2006-01-02 15:04:05"}

func parseTime(value any) time.Time {
	switch v := value.(type) {
	case string:
		for _, layout := range timeLayouts {
			if parsed, err := time.Parse(layout, v); err == nil {
				return parsed.UTC()
			}
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return unixTime(n)
		}
	case float64:
		return unixTime(int64(v))
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return unixTime(n)
		}
	}
	return time.Time{}
}

func unixTime(value int64) time.Time {
	switch {
	case value <= 0:
		return time.Time{}
	case value < 1e11:
		return time.Unix(value, 0).UTC()
	case value < 1e14:
		return time.UnixMilli(value).UTC()
	case value < 1e17:
		return time.UnixMicro(value).UTC()
	default:
		return time.Unix(0, value).UTC()
	}
}

func grokBoundaryEvents(path string) []event {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "summary.json"))
	if err != nil {
		return nil
	}
	var record map[string]any
	if json.Unmarshal(raw, &record) != nil {
		return nil
	}
	start := parseTime(record["created_at"])
	end := parseTime(record["updated_at"])
	if end.IsZero() {
		end = parseTime(record["last_active_at"])
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil
	}
	return []event{
		{At: start, Kind: kindToolCall, Category: "reasoning", Label: "Grok Build session", Confidence: .4},
		{At: end, Kind: kindOther, Label: "session end", Confidence: .4},
	}
}

func finalize(report *Report) {
	totals := map[string]time.Duration{}
	for _, session := range report.Sessions {
		report.Observed += session.Duration
		for _, segment := range session.Segments {
			totals[segment.Category] += segment.Duration
			if segment.Clamped {
				report.Inferred += segment.Duration
			}
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
	raw = strings.TrimRight(raw, "/"+string(filepath.Separator))
	base := filepath.Base(raw)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "unknown"
	}
	if strings.HasPrefix(base, "-") || strings.HasPrefix(base, "Users-") {
		parts := strings.Split(strings.Trim(base, "-"), "-")
		if last := parts[len(parts)-1]; last != "" {
			return last
		}
	}
	return base
}

func shortID(id string) string {
	if token := longestHexRun(id); len(token) >= 8 {
		return token[:8]
	}
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func longestHexRun(id string) string {
	best, start := "", -1
	for index := 0; index <= len(id); index++ {
		if index < len(id) && isHexDigit(id[index]) {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			if run := id[start:index]; len(run) > len(best) {
				best = run
			}
			start = -1
		}
	}
	return best
}

func isHexDigit(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f')
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
