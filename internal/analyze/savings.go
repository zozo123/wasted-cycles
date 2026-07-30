package analyze

import (
	"fmt"
	"math"
	"time"
)

// Illustrative unit costs used only for the optional savings panel. They are
// deliberately mid-market and labelled as such — never presented as quotes.
const (
	illustrativeEngineerUSDPerHour = 100.0 // loaded cost of an idle engineer-hour
	illustrativeComputeUSDPerMin   = 0.008 // hosted Linux CI minute (near GH Actions 2-core)
)

// recoveryFraction is how much of a blocked bucket a well-run accelerator can
// typically claw back. Kept deliberately below vendor marketing claims.
var recoveryFraction = map[string]float64{
	"build_wait":      0.50,
	"test_wait":       0.40,
	"ci_wait":         0.50,
	"container_wait":  0.40,
	"dependency_wait": 0.50,
	"retry":           0.70,
}

// Savings is an optional, illustrative projection of what fixing accelerateable
// waits could be worth. It is never mixed into throughput or blocked_ns.
type Savings struct {
	Addressable       time.Duration   `json:"addressable_ns"`
	EngineerUSD       float64         `json:"engineer_usd"`
	ComputeUSD        float64         `json:"compute_usd"`
	AnnualEngineerUSD float64         `json:"annual_engineer_usd,omitempty"`
	AnnualComputeUSD  float64         `json:"annual_compute_usd,omitempty"`
	Assumptions       string          `json:"assumptions"`
	Disclaimer        string          `json:"disclaimer"`
	Options           []SavingsOption `json:"options"`
}

// SavingsOption is a named accelerator a reader might evaluate. Inclusion is
// informational, not an endorsement or affiliate relationship.
type SavingsOption struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Fit  string `json:"fit"`
}

func buildSavings(totals map[string]time.Duration, since time.Time) *Savings {
	var addressable time.Duration
	for id, fraction := range recoveryFraction {
		if totals[id] <= 0 || fraction <= 0 {
			continue
		}
		addressable += time.Duration(float64(totals[id]) * fraction)
	}
	if addressable < time.Minute {
		return nil
	}

	hours := addressable.Hours()
	minutes := addressable.Minutes()
	engineer := roundUSD(hours * illustrativeEngineerUSDPerHour)
	compute := roundUSD(minutes * illustrativeComputeUSDPerMin)

	out := &Savings{
		Addressable: addressable,
		EngineerUSD: engineer,
		ComputeUSD:  compute,
		Assumptions: fmt.Sprintf(
			"Applies conservative recovery to accelerateable waits only (build 50%%, tests 40%%, CI 50%%, containers 40%%, packages 50%%, retries 70%%). Money uses illustrative rates of $%.0f/engineer-hour and $%.3f/CI-minute.",
			illustrativeEngineerUSDPerHour, illustrativeComputeUSDPerMin,
		),
		Disclaimer: "Illustrative only — not a quote, invoice, ROI guarantee, or endorsement. Real savings depend on your stack, cache hit rate, runner mix, and pricing plan. Measure before and after; vendor claims vary.",
		Options: []SavingsOption{
			{
				Name: "Incredibuild Build Runner",
				URL:  "https://www.incredibuild.com/product/build-runner",
				Fit:  "Managed CI runners with persistent build cache for GitHub Actions / GitLab.",
			},
			{
				Name: "Blacksmith",
				URL:  "https://www.blacksmith.sh/",
				Fit:  "Drop-in GitHub Actions runners that trade faster hardware and co-located cache for lower billable minutes.",
			},
			{
				Name: "CircleCI",
				URL:  "https://circleci.com/",
				Fit:  "Parallelism and resource classes that shrink suite and pipeline wall-clock.",
			},
		},
	}

	if days := windowDays(since); days > 0 && days < 360 {
		scale := 365.0 / float64(days)
		out.AnnualEngineerUSD = roundUSD(engineer * scale)
		out.AnnualComputeUSD = roundUSD(compute * scale)
	}
	return out
}

func windowDays(since time.Time) int {
	if since.IsZero() {
		return 0
	}
	hours := time.Since(since).Hours()
	if hours < 24 {
		return 1
	}
	return int(math.Round(hours / 24))
}

func roundUSD(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value < 1 {
		return math.Round(value*100) / 100
	}
	return math.Round(value)
}
