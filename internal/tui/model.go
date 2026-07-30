package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/zozo123/wasted-cycles/internal/analyze"
)

const (
	tabOverview = iota
	tabHistogram
	tabRuns
	tabMethod
)

// Two data hues, not twelve: cool means the agent was working, warm means it was
// blocked on a machine. Both were picked by the palette validator against the
// panel surface — worst-pair CVD ΔE 10.4, normal-vision 20.7, contrast >= 3:1.
// Repeated work shares the warm hue and is separated by texture instead of a
// third hue, because no warm/red pair survives deuteranopia at this lightness.
var (
	ink     = lipgloss.Color("#F3F2ED")
	muted   = lipgloss.Color("#858780")
	faint   = lipgloss.Color("#353833")
	panel   = lipgloss.Color("#171916")
	brand   = lipgloss.Color("#FF6B35")
	working = lipgloss.Color("#05A388")
	blocked = lipgloss.Color("#C17938")
	outside = lipgloss.Color("#6B6E68")
)

type Model struct {
	report  analyze.Report
	version string
	width   int
	height  int
	tab     int
	window  analyze.Window
	demo    bool
	loading bool
	scanErr error
	gen     int
	scan    func(analyze.Window) (analyze.Report, error)
}

// Config wires the TUI to a named lookback window and an optional custom scanner
// (tests inject a fake; production leaves Scan nil and hits the local traces).
type Config struct {
	Window analyze.Window
	Demo   bool
	Scan   func(analyze.Window) (analyze.Report, error)
}

type scannedMsg struct {
	gen    int
	window analyze.Window
	report analyze.Report
	err    error
}

func New(report analyze.Report, version string, cfg Config) Model {
	window := cfg.Window
	if !window.Valid() {
		if report.Window.Valid() {
			window = report.Window
		} else {
			window = analyze.Window7d
		}
	}
	return Model{
		report:  report,
		version: version,
		width:   100,
		height:  30,
		window:  window,
		demo:    cfg.Demo || report.IsDemo,
		scan:    cfg.Scan,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case scannedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		m.window = msg.window
		if msg.err != nil {
			m.scanErr = msg.err
			return m, nil
		}
		m.scanErr = nil
		m.report = msg.report
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "right", "l", "tab":
			m.tab = (m.tab + 1) % 4
		case "left", "h", "shift+tab":
			m.tab = (m.tab + 3) % 4
		case "1":
			m.tab = tabOverview
		case "2":
			m.tab = tabHistogram
		case "3":
			m.tab = tabRuns
		case "4":
			m.tab = tabMethod
		case "7":
			return m.setWindow(analyze.Window7d)
		case "0":
			return m.setWindow(analyze.Window30d)
		case "y":
			return m.setWindow(analyze.WindowYTD)
		case "]":
			return m.setWindow(m.window.Next())
		case "[":
			return m.setWindow(m.window.Prev())
		}
	}
	return m, nil
}

func (m Model) setWindow(window analyze.Window) (tea.Model, tea.Cmd) {
	if !window.Valid() || window == m.window && !m.loading {
		return m, nil
	}
	m.window = window
	m.loading = true
	m.scanErr = nil
	m.gen++
	gen := m.gen
	scan := m.scan
	demo := m.demo
	return m, func() tea.Msg {
		if scan != nil {
			report, err := scan(window)
			return scannedMsg{gen: gen, window: window, report: report, err: err}
		}
		if demo {
			report := analyze.DemoReport()
			report.Window = window
			report.Since = window.Since(time.Now())
			return scannedMsg{gen: gen, window: window, report: report}
		}
		report, err := defaultScan(window)
		return scannedMsg{gen: gen, window: window, report: report, err: err}
	}
}

func defaultScan(window analyze.Window) (analyze.Report, error) {
	return analyze.Run(analyze.Options{
		Since:  window.Since(time.Now()),
		Window: window,
		MaxGap: 30 * time.Minute,
	})
}

func (m Model) View() string {
	width := max(m.width, 60)
	body := min(width-4, 110)

	var content string
	switch m.tab {
	case tabHistogram:
		content = m.histogram(body)
	case tabRuns:
		content = m.runs(body)
	case tabMethod:
		content = m.method(body)
	default:
		content = m.overview(body)
	}
	parts := []string{m.header(body)}
	if banner := m.statusBanner(body); banner != "" {
		parts = append(parts, banner)
	}
	parts = append(parts, content, m.footer(body))
	page := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().MarginLeft(2).MarginTop(1).Render(page)
}

func (m Model) header(width int) string {
	mark := lipgloss.NewStyle().Bold(true).Foreground(brand).Render("WASTED") +
		lipgloss.NewStyle().Bold(true).Foreground(ink).Render(" CYCLES")
	top := spread(mark, m.rangeChips(), width)

	var tabs []string
	for index, label := range []string{"1 OVERVIEW", "2 HISTOGRAM", "3 RUNS", "4 METHOD"} {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(muted)
		if index == m.tab {
			style = style.Foreground(ink).Bold(true).Background(faint)
		}
		tabs = append(tabs, style.Render(label))
	}
	meta := m.scopeMeta()
	tabLine := truncate(strings.Join(tabs, ""), width)
	if meta != "" && lipgloss.Width(tabLine)+1+lipgloss.Width(meta) <= width {
		tabLine = spread(tabLine, meta, width)
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, "", tabLine)
}

func (m Model) rangeChips() string {
	var chips []string
	for _, window := range analyze.Windows {
		label := " " + window.Label() + " "
		style := lipgloss.NewStyle().Foreground(muted)
		if window == m.window {
			style = style.Foreground(ink).Bold(true).Background(faint)
		}
		chips = append(chips, style.Render(label))
	}
	return strings.Join(chips, " ")
}

func (m Model) scopeMeta() string {
	if m.demo {
		return lipgloss.NewStyle().Foreground(muted).Render("demo dataset")
	}
	return lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf("%d traces", m.report.Scanned))
}

func (m Model) statusBanner(width int) string {
	switch {
	case m.loading:
		return lipgloss.NewStyle().Foreground(brand).Render(truncate("rescanning Codex · Claude · Cursor · Grok…", width))
	case m.scanErr != nil:
		return lipgloss.NewStyle().Foreground(blocked).Render(truncate("scan failed: "+m.scanErr.Error(), width))
	default:
		return ""
	}
}

func (m Model) footer(width int) string {
	left, right := "←/→ view   7/0/y range   q quit", "local only · no uploads"
	if lipgloss.Width(left)+lipgloss.Width(right)+1 > width {
		left, right = "←/→ · 7/0/y · q", "local only"
	}
	return lipgloss.NewStyle().Foreground(muted).Render(spread(left, right, width))
}

func (m Model) overview(width int) string {
	if len(m.report.Sessions) == 0 {
		return m.empty(width)
	}
	report := m.report

	perRow := 4
	if width < 72 {
		perRow = 2
	}
	cardWidth := max(17, (width-perRow+1)/perRow)
	cards := []string{
		card(cardWidth, "AGENT TIME", duration(report.Observed), "excl. human", ink),
		card(cardWidth, "BLOCKED", duration(report.Blocked), share(report.Blocked, report.Observed)+" of agent", blocked),
		card(cardWidth, "THROUGHPUT", fmt.Sprintf("%.0f%%", report.Throughput*100), verdict(report.Throughput), working),
		card(cardWidth, "HUMAN TIME", duration(report.Human), "not counted", outside),
	}
	var rows []string
	for start := 0; start < len(cards); start += perRow {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards[start:min(start+perRow, len(cards))]...))
	}

	left := max(34, int(float64(width)*.5))
	right := width - left - 2
	lower := lipgloss.JoinHorizontal(lipgloss.Top,
		box(left).Render(m.summary(left-4)),
		"  ",
		box(right).Render(m.leaks(right-4)),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinVertical(lipgloss.Left, rows...), "", lower)
}

func (m Model) histogram(width int) string {
	if len(m.report.Sessions) == 0 {
		return m.empty(width)
	}
	return box(width).Render(m.bars(width - 4))
}

// The Overview answers one question — how much of the run was blocked — so it
// shows the three group totals. Per-category detail is one keystroke away on the
// Histogram tab.
func (m Model) summary(width int) string {
	totals := map[string]time.Duration{}
	for _, category := range m.report.Categories {
		totals[category.Group] += category.Duration
	}
	peak := time.Duration(0)
	for _, value := range totals {
		if value > peak {
			peak = value
		}
	}

	labelWidth := min(20, max(12, width/3))
	barWidth := max(6, width-labelWidth-15)
	lines := []string{heading("WHERE THE TIME WENT", "by group"), ""}
	for _, group := range []struct {
		id, label string
		glyph     string
		colour    lipgloss.Color
	}{
		{analyze.GroupWorking, "Coding", "█", working},
		{analyze.GroupBlocked, "Blocked", "█", blocked},
		{analyze.GroupExcluded, "Not counted", "░", outside},
	} {
		value := totals[group.id]
		if value == 0 {
			continue
		}
		fill := 0
		if peak > 0 {
			fill = max(1, int(math.Round(float64(value)/float64(peak)*float64(barWidth))))
		}
		bar := lipgloss.NewStyle().Foreground(group.colour).Render(strings.Repeat(group.glyph, fill)) +
			lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("·", max(0, barWidth-fill)))
		lines = append(lines,
			lipgloss.NewStyle().Width(labelWidth).Foreground(ink).Render(truncate(group.label, labelWidth))+
				bar+
				lipgloss.NewStyle().Width(8).Align(lipgloss.Right).Foreground(ink).Render(duration(value)))
	}
	return strings.Join(lines, "\n")
}

// One row per category, grouped: the eye should land on the warm block and know
// immediately how much of the run was spent waiting on a machine.
func (m Model) bars(width int) string {
	title := heading("WHERE THE TIME WENT", fmt.Sprintf("%s of %s blocked",
		duration(m.report.Blocked), duration(m.report.Observed)))

	peak := time.Duration(0)
	for _, category := range m.report.Categories {
		if category.Duration > peak {
			peak = category.Duration
		}
	}
	labelWidth := min(22, max(14, width/3))
	barWidth := max(6, width-labelWidth-16)

	lines := []string{title, ""}
	groups := []struct{ id, title string }{
		{analyze.GroupWorking, "AGENT WORKING"},
		{analyze.GroupBlocked, "BLOCKED ON COMPUTE"},
		{analyze.GroupExcluded, "NOT COUNTED"},
	}
	for _, group := range groups {
		rows := 0
		for _, category := range m.report.Categories {
			if category.Group != group.id || category.Duration == 0 {
				continue
			}
			if rows == 0 {
				if len(lines) > 2 {
					lines = append(lines, "")
				}
				lines = append(lines, lipgloss.NewStyle().Foreground(muted).Bold(true).Render(group.title))
			}
			rows++
			lines = append(lines, barRow(category, peak, labelWidth, barWidth))
		}
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(muted).Render(truncate(legend(), width)))
	return strings.Join(lines, "\n")
}

func barRow(category analyze.Category, peak time.Duration, labelWidth, barWidth int) string {
	ratio := 0.0
	if peak > 0 {
		ratio = float64(category.Duration) / float64(peak)
	}
	fill := int(math.Round(ratio * float64(barWidth)))
	if fill == 0 && category.Duration > 0 {
		fill = 1
	}

	glyph, colour := "█", working
	switch category.Group {
	case analyze.GroupBlocked:
		colour = blocked
		if category.ID == "retry" {
			glyph = "▒"
		}
	case analyze.GroupExcluded:
		glyph, colour = "░", outside
	}

	bar := lipgloss.NewStyle().Foreground(colour).Render(strings.Repeat(glyph, max(0, fill))) +
		lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("·", max(0, barWidth-fill)))

	// Values stay in text ink; the bar beside them carries the identity.
	value := lipgloss.NewStyle().Width(8).Align(lipgloss.Right).Foreground(ink).Render(duration(category.Duration))
	row := lipgloss.NewStyle().Width(labelWidth).Foreground(ink).Render(truncate("  "+category.Label, labelWidth)) + bar + value
	if category.Group != analyze.GroupExcluded {
		row += lipgloss.NewStyle().Width(5).Align(lipgloss.Right).Foreground(muted).Render(fmt.Sprintf("%.0f%%", category.Share*100))
	}
	return row
}

func legend() string {
	return lipgloss.NewStyle().Foreground(working).Render("█") +
		lipgloss.NewStyle().Foreground(muted).Render(" coding   ") +
		lipgloss.NewStyle().Foreground(blocked).Render("█") +
		lipgloss.NewStyle().Foreground(muted).Render(" blocked on compute   ") +
		lipgloss.NewStyle().Foreground(blocked).Render("▒") +
		lipgloss.NewStyle().Foreground(muted).Render(" repeated   ") +
		lipgloss.NewStyle().Foreground(outside).Render("░") +
		lipgloss.NewStyle().Foreground(muted).Render(" not counted")
}

func (m Model) leaks(width int) string {
	lines := []string{heading("BIGGEST STALLS", "ranked by machine time"), ""}
	if len(m.report.Findings) == 0 {
		return strings.Join(append(lines,
			lipgloss.NewStyle().Foreground(working).Bold(true).Render("Nothing is blocking runs"),
			lipgloss.NewStyle().Foreground(muted).Render(wrap("No build, test, CI, container, or package wait is material in this window.", width)),
		), "\n")
	}
	for index, finding := range m.report.Findings {
		if index >= 3 {
			break
		}
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines,
			lipgloss.NewStyle().Foreground(blocked).Bold(true).Render(fmt.Sprintf("%d ", index+1))+
				lipgloss.NewStyle().Bold(true).Foreground(ink).Render(truncate(finding.Title, width-2)),
			lipgloss.NewStyle().Foreground(muted).Render(wrap(duration(finding.Recoverable)+" on the critical path", width)),
		)
	}
	return strings.Join(lines, "\n")
}

func (m Model) runs(width int) string {
	if len(m.report.Sessions) == 0 {
		return m.empty(width)
	}
	inner := width - 4
	showEnded := inner >= 68
	showDetail := inner >= 80
	project := max(8, min(22, inner-46))

	layout := "%-9s %-" + fmt.Sprint(project) + "s %9s %9s %7s"
	heads := []any{"HARNESS", "PROJECT", "AGENT", "BLOCKED", "THRUPUT"}
	if showEnded {
		layout += " %11s"
		heads = append(heads, "ENDED")
	}
	if showDetail {
		layout += " %6s"
		heads = append(heads, "DETAIL")
	}

	lines := []string{
		heading("RUN COMPARISON", "which harness keeps moving?"), "",
		lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf(layout, heads...)),
		lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("─", max(1, min(inner, 88)))),
	}

	coarse := false
	limit := max(4, m.height-16)
	for index, session := range m.report.Sessions {
		if index >= limit {
			lines = append(lines, lipgloss.NewStyle().Foreground(muted).
				Render(fmt.Sprintf("  … %d more runs (--json for all)", len(m.report.Sessions)-index)))
			break
		}
		detail := "event"
		if session.Resolution == "turn" {
			detail, coarse = "turn", true
		}
		stalled := session.Duration - time.Duration(session.Throughput*float64(session.Duration))
		values := []any{
			truncate(session.Provider, 9), truncate(session.Project, project),
			duration(session.Duration), duration(stalled),
			fmt.Sprintf("%.0f%%", session.Throughput*100),
		}
		if showEnded {
			values = append(values, session.End.Format("Jan 02 15:04"))
		}
		if showDetail {
			values = append(values, detail)
		}
		colour := ink
		if session.Throughput < .9 {
			colour = blocked
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colour).Render(fmt.Sprintf(layout, values...)))
	}

	note := "Throughput = agent time that was not spent waiting on a machine. Human time is excluded."
	if coarse && showDetail {
		note += "\nturn = the harness stamps no per-event times, so a segment spans a whole turn."
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(muted).Render(wrap(note, inner)))
	return box(width).Render(strings.Join(lines, "\n"))
}

func (m Model) method(width int) string {
	inner := width - 4
	sources := "none found"
	if len(m.report.Sources) > 0 {
		var parts []string
		for _, source := range m.report.Sources {
			parts = append(parts, fmt.Sprintf("%s (%d)", source.Provider, source.Files))
		}
		sources = strings.Join(parts, " · ")
	}

	labelWidth := min(18, max(12, inner/4))
	rows := []struct {
		label, body string
		colour      lipgloss.Color
	}{
		{"A wasted cycle", "Time blocked on a machine you do not control: builds, tests, CI, containers, packages, sub-agents. A repeat counts twice — the machine did the work twice.", blocked},
		{"Not counted", "Waiting on a person. Reported beside the metric, never inside it.", outside},
		{"Gaps", "Over 2h is a session break and is dropped. Shorter gaps are capped at 30m and marked clamped. " + inferredNote(m.report), ink},
		{"Soft edges", "\u201cModel work\u201d is the interval after a message or tool result, not measured GPU time. Cursor and Grok only stamp turns, so a segment spans a whole turn. Scheduled Cursor agents that tick on a fixed interval are dropped. Per-segment confidence is in --json.", ink},
		{"Detected", sources, ink},
		{"Window", fmt.Sprintf("%s from %s. Press 7 / 0 / y or [ ] to rescan.", m.window.Label(), m.report.Since.Local().Format("2006-01-02")), ink},
	}

	lines := []string{heading("METHOD & LIMITS", "use the number, know its edges"), ""}
	for _, row := range rows {
		body := strings.Split(wrap(row.body, inner-labelWidth-1), "\n")
		for index, line := range body {
			label := ""
			if index == 0 {
				label = row.label
			}
			lines = append(lines,
				lipgloss.NewStyle().Width(labelWidth).Foreground(row.colour).Bold(index == 0).Render(label)+
					" "+lipgloss.NewStyle().Foreground(muted).Render(line))
		}
		lines = append(lines, "")
	}
	return box(width).Render(strings.Join(lines[:len(lines)-1], "\n"))
}

func (m Model) empty(width int) string {
	return box(width).Render(strings.Join([]string{
		lipgloss.NewStyle().Foreground(blocked).Bold(true).Render("No recent supported traces found"),
		"",
		lipgloss.NewStyle().Foreground(muted).Render(wrap(
			"Looked in ~/.codex/sessions, ~/.claude/projects, ~/.cursor/projects and ~/.grok/sessions.", width-4)),
		"",
		lipgloss.NewStyle().Foreground(ink).Render("Widen the window:") + lipgloss.NewStyle().Foreground(working).Render("  press 0 (30d) or y (YTD)"),
		lipgloss.NewStyle().Foreground(ink).Render("See the interface:") + lipgloss.NewStyle().Foreground(working).Render("  wasted-cycles --demo"),
	}, "\n"))
}

func inferredNote(report analyze.Report) string {
	if report.Inferred <= 0 || report.Observed <= 0 {
		return "Nothing in this report came from a capped gap."
	}
	return fmt.Sprintf("Here %s of %s agent time (%s) is capped rather than measured.",
		duration(report.Inferred), duration(report.Observed), share(report.Inferred, report.Observed))
}

func card(width int, label, value, note string, colour lipgloss.Color) string {
	return lipgloss.NewStyle().
		Width(width-1).Height(4).Padding(0, 1).MarginRight(1).
		Background(panel).
		Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(faint).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Foreground(muted).Bold(true).Render(truncate(label, width-3)),
			lipgloss.NewStyle().Foreground(colour).Bold(true).Render(truncate(value, width-3)),
			lipgloss.NewStyle().Foreground(muted).Render(truncate(note, width-3)),
		))
}

func box(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(width).Padding(1, 2).Background(panel)
}

func heading(title, note string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(ink).Render(title) + "  " +
		lipgloss.NewStyle().Foreground(muted).Render(note)
}

func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func duration(value time.Duration) string {
	switch {
	case value < time.Minute:
		return fmt.Sprintf("%ds", int(value.Seconds()))
	case value < time.Hour:
		return fmt.Sprintf("%dm", int(value.Minutes()))
	default:
		return fmt.Sprintf("%dh %02dm", int(value.Hours()), int(value.Minutes())%60)
	}
}

func share(value, total time.Duration) string {
	if total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(value)/float64(total)*100)
}

func verdict(value float64) string {
	switch {
	case value >= .95:
		return "rarely blocked"
	case value >= .8:
		return "room to recover"
	default:
		return "path blocked"
	}
}

func relativePeriod(since time.Time) string {
	if days := int(time.Since(since).Hours() / 24); days >= 1 {
		return fmt.Sprintf("%dd", days)
	}
	return "24h"
}

func windowLabel(report analyze.Report) string {
	if report.Window.Valid() {
		return report.Window.Label()
	}
	return "last " + relativePeriod(report.Since)
}

func truncate(value string, width int) string {
	if width < 1 {
		return ""
	}
	if ansi.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(value, width-1, "") + "…"
}

func wrap(value string, width int) string {
	if width < 8 {
		return value
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(value) {
		switch {
		case current == "":
			current = word
		case lipgloss.Width(current)+1+lipgloss.Width(word) > width:
			lines = append(lines, current)
			current = word
		default:
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}
