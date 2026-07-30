package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/zozo123/wasted-cycles/internal/analyze"
)

func reports() map[string]analyze.Report {
	return map[string]analyze.Report{
		"demo":  analyze.DemoReport(),
		"empty": {GeneratedAt: time.Now(), Since: time.Now().Add(-7 * 24 * time.Hour)},
	}
}

func TestViewNeverPanicsAcrossTerminalSizes(t *testing.T) {
	sizes := []struct{ width, height int }{
		{0, 0}, {1, 1}, {10, 3}, {20, 5}, {40, 12}, {68, 24}, {104, 32}, {400, 120},
	}
	for name, report := range reports() {
		for _, size := range sizes {
			for tab := tabOverview; tab <= tabMethod; tab++ {
				model, _ := New(report, "test", Config{}).Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				view := model.(Model)
				view.tab = tab
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							t.Fatalf("%s report, %dx%d, tab %d panicked: %v", name, size.width, size.height, tab, recovered)
						}
					}()
					if view.View() == "" {
						t.Fatalf("%s report, %dx%d, tab %d rendered nothing", name, size.width, size.height, tab)
					}
				}()
			}
		}
	}
}

func TestNoRenderedLineExceedsTheTerminal(t *testing.T) {
	// A single over-wide line wraps and shifts everything below it, which is how
	// the footer used to break every view at every width.
	for name, report := range reports() {
		for _, width := range []int{68, 74, 80, 100, 140} {
			for tab := tabOverview; tab <= tabMethod; tab++ {
				model, _ := New(report, "test", Config{}).Update(tea.WindowSizeMsg{Width: width, Height: 40})
				view := model.(Model)
				view.tab = tab
				for index, line := range strings.Split(view.View(), "\n") {
					if got := ansi.StringWidth(line); got > width {
						t.Fatalf("%s report, width %d, tab %d: line %d is %d columns\n%q",
							name, width, tab, index, got, line)
					}
				}
			}
		}
	}
}

func TestTabNavigationWraps(t *testing.T) {
	model := New(analyze.DemoReport(), "test", Config{})
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := next.(Model).tab; got != tabMethod {
		t.Fatalf("left from the first tab should wrap to the last, got %d", got)
	}
	forward := model
	for i := 0; i < 4; i++ {
		updated, _ := forward.Update(tea.KeyMsg{Type: tea.KeyRight})
		forward = updated.(Model)
	}
	if forward.tab != tabOverview {
		t.Fatalf("four steps right should return to the first tab, got %d", forward.tab)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
	} {
		if _, cmd := New(analyze.DemoReport(), "test", Config{}).Update(key); cmd == nil {
			t.Fatalf("%s should quit", key)
		}
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	if got := truncate("日本語のプロジェクト", 6); strings.Contains(got, "\ufffd") {
		t.Fatalf("truncate split a multi-byte rune: %q", got)
	}
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate(abcdef, 4) = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("short strings should pass through, got %q", got)
	}
	if got := truncate("abc", 0); got != "" {
		t.Fatalf("zero width should render nothing, got %q", got)
	}
}

func TestPlainSummary(t *testing.T) {
	output := Plain(analyze.DemoReport())
	for _, want := range []string{"WASTED CYCLES", "WHERE THE TIME WENT", "Model work", "RUNS", "turn resolution", "cursor"} {
		if !strings.Contains(output, want) {
			t.Fatalf("plain output is missing %q:\n%s", want, output)
		}
	}
	empty := Plain(analyze.Report{Since: time.Now().Add(-24 * time.Hour)})
	if !strings.Contains(empty, "No recent supported traces") {
		t.Fatalf("empty report should explain itself:\n%s", empty)
	}
}

func TestRangeKeysRescan(t *testing.T) {
	calls := 0
	model := New(analyze.DemoReport(), "test", Config{
		Window: analyze.Window7d,
		Scan: func(window analyze.Window) (analyze.Report, error) {
			calls++
			report := analyze.DemoReport()
			report.Window = window
			report.Since = window.Since(time.Now())
			report.Scanned = 42
			return report, nil
		},
	})
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	view := updated.(Model)
	if view.window != analyze.Window30d || !view.loading || cmd == nil {
		t.Fatalf("pressing M should start a 30d rescan, got window=%s loading=%v cmd=%v", view.window, view.loading, cmd)
	}
	msg := cmd()
	scanned, ok := msg.(scannedMsg)
	if !ok || scanned.err != nil || scanned.window != analyze.Window30d {
		t.Fatalf("unexpected scan result: %#v", msg)
	}
	final, _ := view.Update(scanned)
	done := final.(Model)
	if done.loading || done.report.Scanned != 42 || done.window != analyze.Window30d {
		t.Fatalf("after scan: loading=%v scanned=%d window=%s", done.loading, done.report.Scanned, done.window)
	}
	if calls != 1 {
		t.Fatalf("scan called %d times", calls)
	}
	rendered := done.View()
	if !strings.Contains(ansi.Strip(rendered), "30d") {
		t.Fatalf("view should show the 30d chip:\n%s", ansi.Strip(rendered))
	}
}

func TestDurationFormatting(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:             "30s",
		5 * time.Minute:              "5m",
		90 * time.Minute:             "1h 30m",
		25*time.Hour + 3*time.Minute: "25h 03m",
		0:                            "0s",
	}
	for value, want := range cases {
		if got := duration(value); got != want {
			t.Fatalf("duration(%s) = %q, want %q", value, got, want)
		}
	}
}
