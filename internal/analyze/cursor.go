package analyze

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cursor agent transcripts carry no per-event timestamps. The only wall-clock
// markers are <timestamp> tags on user turns. We reconstruct a turn-resolution
// timeline from those markers, classify the tools that fired inside each turn,
// and reject metronomic sessions (scheduled agents that tick on a fixed
// interval) which would otherwise inflate agent time into multi-day walls.

var cursorStamp = regexp.MustCompile(`(?i)<timestamp>([^<]{5,80})</timestamp>`)

func cursorEvents(path string) ([]event, string) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, ""
	}
	defer handle.Close()

	var (
		events  []event
		pending []action
		open    bool
		openAt  time.Time
	)
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
		if at, ok := cursorTurnStart(record); ok {
			if open {
				events = append(events, cursorTurn(openAt, pending))
			}
			open, openAt, pending = true, at, nil
			continue
		}
		if !open {
			continue
		}
		pending = append(pending, cursorToolActions(record)...)
	}
	if !open {
		return nil, ""
	}
	events = append(events, cursorTurn(openAt, pending))

	end := lastModified(path)
	if len(events) > 0 && end.After(events[len(events)-1].At) {
		events = append(events, event{At: end, Kind: kindOther, Label: "session end", Confidence: .3})
	}
	if len(events) < 2 || isMetronomic(events) {
		return nil, ""
	}
	return events, ""
}

func cursorTurnStart(record map[string]any) (time.Time, bool) {
	if text(record["role"]) != "user" && text(record["type"]) != "user" {
		return time.Time{}, false
	}
	message, _ := record["message"].(map[string]any)
	if message == nil {
		return time.Time{}, false
	}
	blocks, _ := message["content"].([]any)
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		body, _ := block["text"].(string)
		match := cursorStamp.FindStringSubmatch(body)
		if match == nil {
			continue
		}
		if at := parseCursorTime(match[1]); !at.IsZero() {
			return at, true
		}
	}
	return time.Time{}, false
}

func cursorToolActions(record map[string]any) []action {
	message, _ := record["message"].(map[string]any)
	var blocks []any
	if message != nil {
		blocks, _ = message["content"].([]any)
	}
	if blocks == nil {
		blocks, _ = record["content"].([]any)
	}
	var out []action
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch text(block["type"]) {
		case "tool_use", "server_tool_use", "mcp_tool_use":
			out = append(out, toolAction(text(block["name"]), mapOf(block["input"])))
		}
	}
	return out
}

func cursorTurn(at time.Time, pending []action) event {
	if len(pending) == 0 {
		return event{At: at, Kind: kindToolCall, Category: "reasoning", Label: "model response", Confidence: .4}
	}
	best := pending[0]
	for _, item := range pending[1:] {
		if blockRank[item.Category] > blockRank[best.Category] {
			best = item
		}
	}
	best.Confidence = min(best.Confidence, .5)
	return event{
		At: at, Kind: kindToolCall, Category: best.Category,
		Label: best.Label, Key: best.Key, Confidence: best.Confidence,
	}
}

// A scheduled agent that ticks every N minutes leaves a gap histogram with one
// dominant bucket. Counting those gaps as work produced the infamous 120-hour
// Cursor session; rejecting the pattern is cheaper than inventing timestamps.
func isMetronomic(events []event) bool {
	if len(events) < 12 {
		return false
	}
	counts := map[int]int{}
	gaps := 0
	for i := 1; i < len(events); i++ {
		minutes := int(events[i].At.Sub(events[i-1].At).Round(time.Minute) / time.Minute)
		if minutes < 5 || minutes > 120 {
			continue
		}
		counts[minutes]++
		gaps++
	}
	if gaps < 10 {
		return false
	}
	best := 0
	for _, count := range counts {
		if count > best {
			best = count
		}
	}
	return float64(best)/float64(gaps) >= 0.5
}

func lastModified(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

var cursorLayouts = []string{
	"Monday, Jan 2, 2006, 3:04 PM",
	"Monday, Jan 2, 2006, 3:04:05 PM",
	"Monday, Jan 2, 2006 3:04 PM",
	"Jan 2, 2006, 3:04 PM",
}

func parseCursorTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	zone := time.UTC
	if open := strings.LastIndex(raw, "("); open >= 0 {
		if parsed, ok := parseUTCOffset(strings.TrimSuffix(raw[open+1:], ")")); ok {
			zone = parsed
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw[:open]), ",")
	}
	for _, layout := range cursorLayouts {
		if parsed, err := time.ParseInLocation(layout, raw, zone); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func parseUTCOffset(raw string) (*time.Location, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "UTC") && !strings.HasPrefix(raw, "GMT") {
		return nil, false
	}
	offset := strings.TrimSpace(raw[3:])
	if offset == "" {
		return time.UTC, true
	}
	sign := 1
	switch offset[0] {
	case '-':
		sign = -1
	case '+':
	default:
		return nil, false
	}
	parts := strings.SplitN(offset[1:], ":", 2)
	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours > 14 {
		return nil, false
	}
	minutes := 0
	if len(parts) == 2 {
		if minutes, err = strconv.Atoi(parts[1]); err != nil || minutes > 59 {
			return nil, false
		}
	}
	return time.FixedZone(raw, sign*(hours*3600+minutes*60)), true
}
