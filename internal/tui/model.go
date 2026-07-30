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
	tabTimeline
	tabSessions
	tabMethod
)

var (
	ink       = lipgloss.Color("#F3F2ED")
	muted     = lipgloss.Color("#858780")
	faint     = lipgloss.Color("#353833")
	panel     = lipgloss.Color("#171916")
	orange    = lipgloss.Color("#FF6B35")
	lime      = lipgloss.Color("#C8F56A")
	blue      = lipgloss.Color("#61A8FF")
	purple    = lipgloss.Color("#B197FC")
	yellow    = lipgloss.Color("#FFD166")
	red       = lipgloss.Color("#FF5D73")
	teal      = lipgloss.Color("#57D7C4")
	slate     = lipgloss.Color("#7C8B96")
	baseStyle = lipgloss.NewStyle().Foreground(ink)
)

type Model struct {
	report  analyze.Report
	version string
	width   int
	height  int
	tab     int
}

func New(report analyze.Report, version string) Model {
	return Model{report: report, version: version, width: 104, height: 32}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
			m.tab = tabTimeline
		case "3":
			m.tab = tabSessions
		case "4":
			m.tab = tabMethod
		}
	}
	return m, nil
}

func (m Model) View() string {
	width := m.width
	if width < 68 {
		width = 68
	}
	bodyWidth := min(width-4, 116)
	var body string
	switch m.tab {
	case tabTimeline:
		body = m.timeline(bodyWidth)
	case tabSessions:
		body = m.sessions(bodyWidth)
	case tabMethod:
		body = m.method(bodyWidth)
	default:
		body = m.overview(bodyWidth)
	}

	header := m.header(bodyWidth)
	left, right := "←/→ switch view   1–4 jump   q quit", "local only · no uploads"
	gap := bodyWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left, right = "←/→ · 1–4 · q", "local only"
		gap = bodyWidth - lipgloss.Width(left) - lipgloss.Width(right)
	}
	footer := lipgloss.NewStyle().Foreground(muted).Render(
		truncate(left+strings.Repeat(" ", max(1, gap))+right, bodyWidth),
	)
	page := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return lipgloss.NewStyle().MarginLeft(2).MarginTop(1).Render(page)
}

func (m Model) header(width int) string {
	wordmark := lipgloss.NewStyle().Bold(true).Foreground(orange).Render("WASTED")
	wordmark += lipgloss.NewStyle().Bold(true).Foreground(ink).Render(" CYCLES")
	scope := fmt.Sprintf("last %s · %d traces", relativePeriod(m.report.Since), m.report.Scanned)
	if m.report.IsDemo {
		scope = "demo dataset · Codex + Claude + Cursor + Grok"
	}
	top := lipgloss.JoinHorizontal(lipgloss.Top,
		wordmark,
		strings.Repeat(" ", max(1, width-lipgloss.Width(wordmark)-lipgloss.Width(scope))),
		lipgloss.NewStyle().Foreground(muted).Render(scope),
	)

	labels := []string{"1  OVERVIEW", "2  HISTOGRAM", "3  RUNS", "4  METHOD"}
	var tabs []string
	for index, label := range labels {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(muted)
		if index == m.tab {
			style = style.Foreground(ink).Bold(true).Background(faint)
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, "", lipgloss.JoinHorizontal(lipgloss.Left, tabs...))
}

func (m Model) overview(width int) string {
	if len(m.report.Sessions) == 0 {
		return m.empty(width)
	}
	perRow := 4
	if width < 76 {
		perRow = 2
	}
	cardWidth := max(18, (width-perRow+1)/perRow)
	cards := []string{
		statCard(cardWidth, "OBSERVED", duration(m.report.Observed), "wall-clock in traces", ink),
		statCard(cardWidth, "REASONING SHARE", percent(categoryDuration(m.report, "reasoning"), m.report.Observed), "model inference proxy", purple),
		statCard(cardWidth, "RECOVERABLE", duration(m.report.Recoverable), "blocked + repeated", red),
		statCard(cardWidth, "THROUGHPUT", fmt.Sprintf("%.0f%%", m.report.Throughput*100), throughputLabel(m.report.Throughput), lime),
	}
	var rows []string
	for start := 0; start < len(cards); start += perRow {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards[start:min(start+perRow, len(cards))]...))
	}
	cardsRow := lipgloss.JoinVertical(lipgloss.Left, rows...)

	leftWidth := max(38, int(float64(width)*0.58))
	rightWidth := width - leftWidth - 2
	histogram := panelStyle(leftWidth).Render(m.histogram(leftWidth - 4))
	findings := panelStyle(rightWidth).Render(m.topFindings(rightWidth - 4))
	lower := lipgloss.JoinHorizontal(lipgloss.Top, histogram, "  ", findings)
	return lipgloss.JoinVertical(lipgloss.Left, cardsRow, "", lower)
}

func (m Model) histogram(width int) string {
	title := sectionTitle("WHERE THE TIME WENT", fmt.Sprintf("%d classified buckets", len(m.report.Categories)))
	maxDuration := time.Duration(0)
	for _, category := range m.report.Categories {
		if category.Duration > maxDuration {
			maxDuration = category.Duration
		}
	}
	labelWidth := min(18, max(13, width/3))
	barWidth := max(8, width-labelWidth-13)
	lines := []string{title, ""}
	for _, category := range m.report.Categories {
		ratio := 0.0
		if maxDuration > 0 {
			ratio = float64(category.Duration) / float64(maxDuration)
		}
		fill := int(math.Round(ratio * float64(barWidth)))
		color := categoryColor(category.ID)
		bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", fill))
		bar += lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("░", max(0, barWidth-fill)))
		label := lipgloss.NewStyle().Width(labelWidth).Foreground(ink).Render(category.Label)
		value := lipgloss.NewStyle().Width(9).Align(lipgloss.Right).Foreground(color).Bold(true).Render(duration(category.Duration))
		lines = append(lines, label+" "+bar+" "+value)
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(muted).Render(
		"Time between timestamped trace events · breaks over 2h dropped, shorter gaps capped at 30m",
	))
	return strings.Join(lines, "\n")
}

func (m Model) topFindings(width int) string {
	title := sectionTitle("BIGGEST LEAKS", "ranked by elapsed time")
	lines := []string{title, ""}
	if len(m.report.Findings) == 0 {
		lines = append(lines,
			lipgloss.NewStyle().Foreground(lime).Bold(true).Render("No material wait pattern"),
			lipgloss.NewStyle().Foreground(muted).Render("This window looks healthy."),
		)
		return strings.Join(lines, "\n")
	}
	for index, finding := range m.report.Findings {
		if index >= 3 {
			break
		}
		number := lipgloss.NewStyle().Foreground(categoryColor(finding.Category)).Bold(true).Render(fmt.Sprintf("%d", index+1))
		saving := lipgloss.NewStyle().Foreground(red).Bold(true).Render(duration(finding.Recoverable))
		lines = append(lines, number+"  "+lipgloss.NewStyle().Bold(true).Render(finding.Title))
		lines = append(lines, "   "+saving+" "+lipgloss.NewStyle().Foreground(muted).Render("on the critical path"))
		if index < min(2, len(m.report.Findings)-1) {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) timeline(width int) string {
	title := sectionTitle("WALL-CLOCK HISTOGRAM", "each block ≈ one slice of observed time")
	lines := []string{title, ""}
	if len(m.report.Sessions) == 0 {
		return m.empty(width)
	}
	labelWidth := min(24, max(16, width/4))
	barWidth := max(24, width-labelWidth-16)
	for _, session := range m.report.Sessions {
		if len(lines) > max(11, m.height-9) {
			break
		}
		label := fmt.Sprintf("%s / %s", session.Provider, session.Project)
		label = truncate(label, labelWidth-1)
		bar := segmentBar(session.Segments, session.Duration, barWidth)
		value := lipgloss.NewStyle().Width(9).Align(lipgloss.Right).Foreground(muted).Render(duration(session.Duration))
		lines = append(lines, lipgloss.NewStyle().Width(labelWidth).Foreground(ink).Render(label)+bar+value)
	}
	lines = append(lines, "", legend(width-4))
	lines = append(lines, "", lipgloss.NewStyle().Foreground(muted).Render(
		"Long offline gaps are excluded. Repeated test/CI commands are reclassified as retries.",
	))
	return panelStyle(width).Render(strings.Join(lines, "\n"))
}

func (m Model) sessions(width int) string {
	title := sectionTitle("RUN COMPARISON", "which harness keeps moving?")
	lines := []string{title, ""}
	if len(m.report.Sessions) == 0 {
		return m.empty(width)
	}
	// Columns are dropped rather than wrapped as the terminal narrows: a wrapped
	// row is unreadable, a shorter row is not.
	inner := width - 4
	showDetail, showEnded := inner >= 78, inner >= 66
	project := max(8, min(22, inner-44))
	layout := "%-10s %-" + fmt.Sprint(project) + "s %10s %11.0f%%"
	heads := []any{"HARNESS", "PROJECT", "OBSERVED", "THROUGHPUT"}
	headLayout := "%-10s %-" + fmt.Sprint(project) + "s %10s %12s"
	if showEnded {
		layout, headLayout = layout+" %11s", headLayout+" %11s"
		heads = append(heads, "ENDED")
	}
	if showDetail {
		layout, headLayout = layout+" %6s", headLayout+" %6s"
		heads = append(heads, "DETAIL")
	}

	lines = append(lines,
		lipgloss.NewStyle().Foreground(muted).Render(fmt.Sprintf(headLayout, heads...)),
		lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("─", max(1, min(inner, 82)))),
	)
	coarse := false
	for _, session := range m.report.Sessions {
		detail := "event"
		if session.Resolution == "turn" {
			detail, coarse = "turn", true
		}
		values := []any{
			truncate(session.Provider, 10),
			truncate(session.Project, project),
			duration(session.Duration),
			session.Throughput * 100,
		}
		if showEnded {
			values = append(values, session.End.Format("Jan 02 15:04"))
		}
		if showDetail {
			values = append(values, detail)
		}
		row := fmt.Sprintf(layout, values...)
		color := lime
		if session.Throughput < .7 {
			color = yellow
		}
		if session.Throughput < .5 {
			color = red
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(color).Render(row))
	}
	note := "Throughput = observed time minus CI, human, agent, dependency, and retry waits."
	if coarse && showDetail {
		note += "\nturn = the harness records no per-event timestamps, so segments span a whole turn."
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(muted).Render(note))
	return panelStyle(width).Render(strings.Join(lines, "\n"))
}

func (m Model) method(width int) string {
	sourceText := "No supported trace files found."
	if len(m.report.Sources) > 0 {
		var sources []string
		for _, source := range m.report.Sources {
			sources = append(sources, fmt.Sprintf("%s (%d)", source.Provider, source.Files))
		}
		sourceText = strings.Join(sources, " · ")
	}
	copy := []string{
		sectionTitle("METHOD & TRUST", "use the number, know its limits"),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(ink).Render("What is measured"),
		wrap("Wasted Cycles reads local JSONL traces and reconstructs elapsed wall-clock segments between timestamped events. Each segment is classified by the structured action that opened it: the tool that was called, the command it ran, or the message that ended a turn. Records it cannot identify are skipped rather than guessed, so their elapsed time stays attributed to the last recognized action.", width-4),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(purple).Render("Inference is a proxy"),
		wrap("Harnesses expose different timing detail. “Model work” means the interval after a user message or tool result and before the next emitted action. It is not GPU compute time unless the trace explicitly records duration.", width-4),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(yellow).Render("Resolution differs by harness"),
		wrap("Codex, Claude Code, and Grok Build stamp individual events. Cursor does not, so its transcripts are reconstructed per user turn and each turn takes the dominant tool category it observed. Those runs are marked “turn” on the Runs screen and carry low confidence in JSON output.", width-4),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(red).Render("Guardrails"),
		wrap("A gap longer than 2 hours is treated as a session break and is not counted at all, so an overnight pause cannot become “waiting for human”. Shorter gaps are capped at 30 minutes; those segments are marked clamped with low confidence. "+inferredNote(m.report)+" Prompt text and source code are never retained, rendered, or uploaded.", width-4),
		"",
		lipgloss.NewStyle().Foreground(muted).Render("Detected  " + sourceText),
	}
	return panelStyle(width).Render(strings.Join(copy, "\n"))
}

func inferredNote(report analyze.Report) string {
	if report.Inferred <= 0 || report.Observed <= 0 {
		return "No time in this report came from a clamped gap."
	}
	return fmt.Sprintf("In this report %s of %s observed (%s) is clamped rather than measured.",
		duration(report.Inferred), duration(report.Observed), percent(report.Inferred, report.Observed))
}

func (m Model) empty(width int) string {
	lines := []string{
		lipgloss.NewStyle().Foreground(yellow).Bold(true).Render("No recent supported traces found"),
		"",
		wrap("Wasted Cycles looked in ~/.codex/sessions, ~/.claude/projects, ~/.cursor/projects, and ~/.grok/sessions.", width-6),
		"",
		lipgloss.NewStyle().Foreground(ink).Render("Try a wider window:"),
		lipgloss.NewStyle().Foreground(lime).Render("  wasted-cycles --days 30"),
		"",
		lipgloss.NewStyle().Foreground(ink).Render("Or explore the complete interface:"),
		lipgloss.NewStyle().Foreground(lime).Render("  wasted-cycles --demo"),
	}
	return panelStyle(width).Render(strings.Join(lines, "\n"))
}

func statCard(width int, label, value, note string, color lipgloss.Color) string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(muted).Bold(true).Render(label),
		lipgloss.NewStyle().Foreground(color).Bold(true).Render(value),
		lipgloss.NewStyle().Foreground(muted).Render(note),
	)
	return lipgloss.NewStyle().
		Width(width-1).
		Height(4).
		Padding(0, 1).
		MarginRight(1).
		Background(panel).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(faint).
		Render(content)
}

func panelStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(width).Padding(1, 2).Background(panel)
}

func sectionTitle(title, note string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(ink).Render(title) + "  " +
		lipgloss.NewStyle().Foreground(muted).Render(note)
}

func segmentBar(segments []analyze.Segment, total time.Duration, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	var builder strings.Builder
	used := 0
	for index, segment := range segments {
		size := int(math.Round(float64(segment.Duration) / float64(total) * float64(width)))
		if size < 1 {
			size = 1
		}
		if index == len(segments)-1 || used+size > width {
			size = width - used
		}
		if size <= 0 {
			break
		}
		builder.WriteString(lipgloss.NewStyle().Foreground(categoryColor(segment.Category)).Render(strings.Repeat("█", size)))
		used += size
	}
	if used < width {
		builder.WriteString(lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("░", width-used)))
	}
	return builder.String()
}

func legend(width int) string {
	items := []struct {
		id, label string
	}{
		{"reasoning", "model"}, {"explore", "read"}, {"edit", "edit"},
		{"verify", "test"}, {"tool_other", "other"}, {"ci_wait", "CI"},
		{"human_wait", "human"}, {"agent_wait", "agents"}, {"retry", "retry"},
	}
	var rendered []string
	for _, item := range items {
		rendered = append(rendered,
			lipgloss.NewStyle().Foreground(categoryColor(item.id)).Render("■")+" "+
				lipgloss.NewStyle().Foreground(muted).Render(item.label),
		)
	}
	return truncate(strings.Join(rendered, "   "), width)
}

func categoryColor(category string) lipgloss.Color {
	switch category {
	case "reasoning":
		return purple
	case "explore":
		return blue
	case "edit":
		return lime
	case "verify":
		return teal
	case "tool_other":
		return slate
	case "ci_wait":
		return orange
	case "agent_wait":
		return yellow
	case "human_wait":
		return red
	case "dependency_wait":
		return lipgloss.Color("#C084FC")
	case "retry":
		return lipgloss.Color("#FB7185")
	default:
		return muted
	}
}

func categoryDuration(report analyze.Report, id string) time.Duration {
	for _, category := range report.Categories {
		if category.ID == id {
			return category.Duration
		}
	}
	return 0
}

func duration(value time.Duration) string {
	if value < time.Minute {
		return fmt.Sprintf("%ds", int(value.Seconds()))
	}
	hours := int(value.Hours())
	minutes := int(value.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func percent(value, total time.Duration) string {
	if total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(value)/float64(total)*100)
}

func throughputLabel(value float64) string {
	switch {
	case value >= .8:
		return "strong flow"
	case value >= .6:
		return "room to recover"
	default:
		return "critical path blocked"
	}
}

func relativePeriod(since time.Time) string {
	days := int(time.Since(since).Hours() / 24)
	if days < 1 {
		return "24h"
	}
	return fmt.Sprintf("%dd", days)
}

func truncate(value string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(value) <= width {
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
	words := strings.Fields(value)
	current := ""
	for _, word := range words {
		if lipgloss.Width(current)+1+lipgloss.Width(word) > width && current != "" {
			lines = append(lines, current)
			current = word
		} else if current == "" {
			current = word
		} else {
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
