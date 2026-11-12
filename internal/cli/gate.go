package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/report"
)

// exitGate is the exit code when a budget gate trips, apart from errors (1) and usage (2) so CI can tell them
// apart.
const exitGate = 3

// gate is the --fail-* flags: limits that turn a finished report into a failed job.
type gate struct {
	overCost     float64
	overGPUHours float64
	onSeverity   string
}

func (g gate) set() bool { return g.overCost > 0 || g.overGPUHours > 0 || g.onSeverity != "" }

func (g gate) validate() error {
	if g.overCost < 0 || g.overGPUHours < 0 {
		return usagef("--fail-over-cost and --fail-over-gpu-hours must not be negative")
	}
	switch g.onSeverity {
	case "", "high", "medium", "low":
		return nil
	}
	return usagef("--fail-on %q: want high, medium or low", g.onSeverity)
}

// check returns why the gate trips, or nil.
func (g gate) check(r *analyze.Report) error {
	var why []string
	s := r.Summary
	if g.overCost > 0 && s.WasteCost > g.overCost {
		why = append(why, fmt.Sprintf("recoverable waste %.2f %s is over the %.2f limit", s.WasteCost, r.Currency, g.overCost))
	}
	if g.overGPUHours > 0 && s.WasteGPUHours > g.overGPUHours {
		why = append(why, fmt.Sprintf("recoverable waste %.1f GPU-hours is over the %.1f limit", s.WasteGPUHours, g.overGPUHours))
	}
	if g.onSeverity != "" {
		order := map[analyze.Severity]int{analyze.High: 0, analyze.Medium: 1, analyze.Low: 2}
		limit := order[analyze.Severity(g.onSeverity)]
		n := 0
		for sev, c := range s.Findings {
			if rank, ok := order[sev]; ok && rank <= limit {
				n += c
			}
		}
		if n > 0 {
			why = append(why, fmt.Sprintf("%d findings of severity %s or above", n, g.onSeverity))
		}
	}
	if len(why) == 0 {
		return nil
	}
	return exitErr{fmt.Errorf("budget gate: %s", strings.Join(why, "; ")), exitGate}
}

// appendSummary appends the Markdown report to a file, such as GitHub Actions' $GITHUB_STEP_SUMMARY.
func appendSummary(path string, r *analyze.Report) (err error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return report.Markdown(f, r)
}
