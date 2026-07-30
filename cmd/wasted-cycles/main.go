package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/zozo123/wasted-cycles/internal/analyze"
	"github.com/zozo123/wasted-cycles/internal/tui"
)

var version = "dev"

func main() {
	var (
		days    = flag.Int("days", 7, "number of recent days to analyze")
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

	var report analyze.Report
	var err error
	if *demo {
		report = analyze.DemoReport()
	} else {
		report, err = analyze.Run(analyze.Options{
			Since:    time.Now().Add(-time.Duration(*days) * 24 * time.Hour),
			MaxGap:   30 * time.Minute,
			MaxFiles: 600,
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
	if _, err := tea.NewProgram(tui.New(report, version), programOptions...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "wasted-cycles: interactive terminal unavailable, falling back to plain output")
		fmt.Println(tui.Plain(report))
	}
}
