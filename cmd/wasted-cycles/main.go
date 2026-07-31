package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/zozo123/wasted-cycles/internal/analyze"
	"github.com/zozo123/wasted-cycles/internal/githubactions"
	"github.com/zozo123/wasted-cycles/internal/tui"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "github":
			return runGitHub(args[1:], stdout, stderr)
		case "help":
			if len(args) > 1 && args[1] == "github" {
				return runGitHub([]string{"--help"}, stdout, stderr)
			}
			printUsage(stdout)
			return 0
		}
	}
	return runLocal(args, stdout, stderr)
}

func runLocal(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("wasted-cycles", flag.ContinueOnError)
	flags.SetOutput(stderr)
	days := flags.Int("days", 7, "days of local trace history (1–365)")
	ytd := flags.Bool("ytd", false, "scan from January 1")
	demo := flags.Bool("demo", false, "use the built-in sample report")
	asJSON := flags.Bool("json", false, "print JSON")
	plain := flags.Bool("plain", false, "print text instead of the TUI")
	noAlt := flags.Bool("no-alt-screen", false, "keep the TUI in the current screen")
	showVer := flags.Bool("version", false, "print the version")
	flags.Usage = func() { printUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "wasted-cycles: unexpected argument %q\n\n", flags.Arg(0))
		printUsage(stderr)
		return 2
	}

	if *showVer {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if *days < 1 || *days > 365 {
		fmt.Fprintln(stderr, "wasted-cycles: --days must be between 1 and 365")
		return 2
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
			fmt.Fprintf(stderr, "wasted-cycles: %v\n", err)
			return 1
		}
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(stderr, "wasted-cycles: %v\n", err)
			return 1
		}
		return 0
	}

	outputFile, isFile := stdout.(*os.File)
	if *plain || !isFile || !term.IsTerminal(outputFile.Fd()) {
		fmt.Fprintln(stdout, tui.Plain(report))
		return 0
	}

	programOptions := []tea.ProgramOption{tea.WithOutput(stdout)}
	if !*noAlt {
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	model := tui.New(report, version, tui.Config{
		Window: window,
		Demo:   *demo,
	})
	if _, err := tea.NewProgram(model, programOptions...).Run(); err != nil {
		fmt.Fprintln(stderr, "wasted-cycles: TUI unavailable; printing text report")
		fmt.Fprintln(stdout, tui.Plain(report))
	}
	return 0
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "Wasted Cycles — find machine time blocking agent work")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  wasted-cycles [options]")
	fmt.Fprintln(output, "  wasted-cycles github [options] OWNER/REPO")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Local options:")
	fmt.Fprintln(output, "  --days N          history to scan (default 7, max 365)")
	fmt.Fprintln(output, "  --ytd             scan from January 1")
	fmt.Fprintln(output, "  --demo            use the built-in sample report")
	fmt.Fprintln(output, "  --json            print JSON")
	fmt.Fprintln(output, "  --plain           print text instead of the TUI")
	fmt.Fprintln(output, "  --no-alt-screen   keep the TUI in the current screen")
	fmt.Fprintln(output, "  --version         print the version")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Run `wasted-cycles help github` for repository analysis options.")
}

func runGitHub(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("wasted-cycles github", flag.ContinueOnError)
	flags.SetOutput(stderr)
	days := flags.Int("days", 7, "days of workflow history (1–365)")
	ytd := flags.Bool("ytd", false, "scan runs created since January 1")
	asJSON := flags.Bool("json", false, "print JSON")
	maxRuns := flags.Int("max-runs", 1000, "maximum workflow runs to fetch")
	flags.Usage = func() { printGitHubUsage(stderr) }
	normalized, err := normalizeGitHubArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "wasted-cycles github: %v\n", err)
		return 2
	}
	if err := flags.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		printGitHubUsage(stderr)
		return 2
	}
	if *days < 1 || *days > 365 {
		fmt.Fprintln(stderr, "wasted-cycles github: --days must be between 1 and 365")
		return 2
	}
	if *maxRuns < 1 || *maxRuns > 10000 {
		fmt.Fprintln(stderr, "wasted-cycles github: --max-runs must be between 1 and 10000")
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

func printGitHubUsage(output io.Writer) {
	fmt.Fprintln(output, "Analyze GitHub Actions workflow latency")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  wasted-cycles github [options] OWNER/REPO")
	fmt.Fprintln(output, "  wasted-cycles github [options] https://github.com/OWNER/REPO")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Options:")
	fmt.Fprintln(output, "  --days N       workflow history (default 7, max 365)")
	fmt.Fprintln(output, "  --ytd          scan runs created since January 1")
	fmt.Fprintln(output, "  --max-runs N   fetch cap (default 1000, max 10000)")
	fmt.Fprintln(output, "  --json         print JSON")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Public repositories need no login. Private repositories use `gh auth login`,")
	fmt.Fprintln(output, "GH_TOKEN, or GITHUB_TOKEN.")
}

// The standard flag package stops at the first positional argument. Accept the
// friendlier `github OWNER/REPO --days 30` form by moving known options before
// the one repository argument.
func normalizeGitHubArgs(args []string) ([]string, error) {
	var options, positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		name := strings.SplitN(arg, "=", 2)[0]
		switch name {
		case "--days", "-days", "--max-runs", "-max-runs":
			options = append(options, arg)
			if !strings.Contains(arg, "=") {
				if index+1 >= len(args) {
					return nil, fmt.Errorf("%s needs a value", arg)
				}
				index++
				options = append(options, args[index])
			}
		case "--ytd", "-ytd", "--json", "-json", "--help", "-help", "-h":
			options = append(options, arg)
		default:
			if strings.HasPrefix(arg, "-") {
				options = append(options, arg)
				continue
			}
			positional = append(positional, arg)
		}
	}
	return append(options, positional...), nil
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
