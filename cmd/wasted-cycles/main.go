package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/zozo123/wasted-cycles/internal/analyze"
	"github.com/zozo123/wasted-cycles/internal/githubactions"
	"github.com/zozo123/wasted-cycles/internal/tui"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "github" {
		os.Exit(runGitHub(os.Args[2:], os.Stdout, os.Stderr))
	}

	var (
		days    = flag.Int("days", 7, "number of recent days to analyze (overridden by --ytd)")
		ytd     = flag.Bool("ytd", false, "analyze from January 1 of this year")
		demo    = flag.Bool("demo", false, "open the TUI with a realistic demo report")
		asJSON  = flag.Bool("json", false, "print the report as JSON")
		plain   = flag.Bool("plain", false, "print a plain-text summary instead of the TUI")
		noAlt   = flag.Bool("no-alt-screen", false, "render without the terminal alternate screen")
		showVer = flag.Bool("version", false, "print version")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}
	if *days < 1 || *days > 365 {
		fmt.Fprintln(os.Stderr, "--days must be between 1 and 365")
		os.Exit(2)
	}

	now := time.Now()
	window, since := resolveWindow(*ytd, *days, now)

	var report analyze.Report
	var err error
	if *demo {
		report = analyze.DemoReport()
		report.Window = window
		report.Since = since
	} else {
		report, err = analyze.Run(analyze.Options{
			Since:  since,
			Window: window,
			MaxGap: 30 * time.Minute,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "wasted-cycles: %v\n", err)
			os.Exit(1)
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "wasted-cycles: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *plain || !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Println(tui.Plain(report))
		return
	}

	programOptions := []tea.ProgramOption{tea.WithOutput(os.Stdout)}
	if !*noAlt {
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	model := tui.New(report, version, tui.Config{
		Window: window,
		Demo:   *demo,
	})
	if _, err := tea.NewProgram(model, programOptions...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wasted-cycles: interactive terminal unavailable, falling back to plain output")
		fmt.Println(tui.Plain(report))
	}
}

func runGitHub(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("wasted-cycles github", flag.ContinueOnError)
	flags.SetOutput(stderr)
	days := flags.Int("days", 7, "number of recent days to analyze (overridden by --ytd)")
	ytd := flags.Bool("ytd", false, "analyze runs created since January 1 of this year")
	asJSON := flags.Bool("json", false, "print the report as JSON")
	maxRuns := flags.Int("max-runs", 1000, "maximum workflow runs to fetch")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: wasted-cycles github [flags] OWNER/REPO")
		fmt.Fprintln(stderr, "   or: wasted-cycles github [flags] https://github.com/OWNER/REPO")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Measures elapsed GitHub Actions workflow time. Public repositories work")
		fmt.Fprintln(stderr, "without authentication; private repositories use an authenticated gh CLI.")
		fmt.Fprintln(stderr, "")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	if *days < 1 || *days > 365 {
		fmt.Fprintln(stderr, "--days must be between 1 and 365")
		return 2
	}
	if *maxRuns < 1 || *maxRuns > 10000 {
		fmt.Fprintln(stderr, "--max-runs must be between 1 and 10000")
		return 2
	}

	now := time.Now()
	window := fmt.Sprintf("last %d days", *days)
	since := now.Add(-time.Duration(*days) * 24 * time.Hour)
	if *ytd {
		since = time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, now.Location())
		window = fmt.Sprintf("YTD %d", now.Year())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := githubactions.Analyze(ctx, githubactions.Options{
		Repository: flags.Arg(0),
		Since:      since,
		Window:     window,
		MaxRuns:    *maxRuns,
	})
	if err != nil {
		fmt.Fprintf(stderr, "wasted-cycles github: %v\n", err)
		return 1
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "wasted-cycles github: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, githubactions.Plain(report))
	return 0
}

func resolveWindow(ytd bool, days int, now time.Time) (analyze.Window, time.Time) {
	switch {
	case ytd:
		return analyze.WindowYTD, analyze.WindowYTD.Since(now)
	case days == 7:
		return analyze.Window7d, analyze.Window7d.Since(now)
	case days == 30:
		return analyze.Window30d, analyze.Window30d.Since(now)
	default:
		// Custom --days keeps an exact lookback; the nearest chip is only a hint
		// until the user presses W / M / Y and snaps to a named window.
		return analyze.WindowFromDays(days, now), now.Add(-time.Duration(days) * 24 * time.Hour)
	}
}
