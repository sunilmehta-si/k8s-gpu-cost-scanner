package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
)

// Markdown writes the report as GitHub-flavored Markdown, for a pull request comment, a wiki page or a CI job
// summary.
func Markdown(w io.Writer, r *analyze.Report) error {
	b := &errWriter{w: w}
	s := r.Summary
	b.printf("## GPU cost report\n\n")
	b.printf("%s window ending %s, from `%s`.\n\n", windowText(r), r.Window.End.Format("2006-01-02 15:04 MST"), cell(r.Source))

	b.printf("| | |\n|---|---|\n")
	b.printf("| GPU-hours offered / requested | %s / %s (%s) |\n", hours(s.OfferedGPUHours), hours(s.RequestedGPUHours), share(s.RequestedGPUHours, s.OfferedGPUHours))
	if s.MeasuredGPUHours > 0 {
		b.printf("| GPU activity | %s |\n", pct(s.Activity))
	}
	if s.PricedShare > 0 {
		b.printf("| Cost of requested GPUs | %s (%s priced) |\n", money(s.Cost, r.Currency), pct(s.PricedShare))
		b.printf("| **Recoverable waste** | **%s** · %s GPU-hours |\n", money(s.WasteCost, r.Currency), hours(s.WasteGPUHours))
	} else {
		b.printf("| **Recoverable waste** | **%s GPU-hours** (no prices) |\n", hours(s.WasteGPUHours))
	}
	if s.IdleGPUHours > 0 {
		b.printf("| Idle time in serving pods | %s GPU-hours (advice) |\n", hours(s.IdleGPUHours))
	}
	b.printf("| Findings | %s |\n\n", findingCounts(s.Findings))

	if len(r.Findings) > 0 {
		b.printf("### Findings\n\n")
		b.printf("| Severity | Rule | Finding | Target | Waste |\n|---|---|---|---|---|\n")
		for _, f := range r.Findings {
			b.printf("| %s | %s | %s | `%s` | %s |\n", f.Severity, f.Rule, cell(f.Title), cell(f.Target), wasteCell(f, r.Currency))
		}
		b.printf("\n")
		for _, f := range r.Findings {
			b.printf("<details><summary>%s %s: <code>%s</code></summary>\n\n", f.Rule, html(f.Title), html(f.Target))
			for _, e := range f.Evidence {
				b.printf("- %s\n", e)
			}
			b.printf("\n**Fix:** %s\n\n</details>\n\n", f.Recommendation)
		}
	}

	if len(r.Models) > 0 {
		b.printf("### Models\n\n")
		b.printf("| Model | Workload | Replicas | GPU-hours | Cost | Activity | Busy | Output tokens | Per 1M tokens |\n")
		b.printf("|---|---|---|---|---|---|---|---|---|\n")
		for _, m := range r.Models {
			b.printf("| `%s` | %s/%s | %.1f (max %d) | %s | %s | %s | %s | %s | %s |\n", cell(m.Model), cell(m.Namespace), cell(m.Workload),
				m.Replicas, m.MaxReplicas, hours(m.GPUHours), costCell(m.Cost, m.Priced, r.Currency),
				activityCell(m.Activity, m.Measured), pct(m.BusyShare), count(m.GenerationTokens), perMillion(m, r.Currency))
		}
		b.printf("\n")
	}

	if len(r.BlindSpots) > 0 {
		b.printf("### Blind spots\n\n")
		for _, bs := range r.BlindSpots {
			b.printf("- **%s %s**", bs.Code, bs.Title)
			if bs.GPUHours > 0 {
				b.printf(" (%s GPU-hours, %s)", hours(bs.GPUHours), pct(bs.Share))
			}
			b.printf(": %s\n", bs.Detail)
		}
		b.printf("\n")
	}
	b.printf("<sub>%s · thresholds: idle under %s activity at the 95th percentile, replicas sized to %s KV cache use</sub>\n",
		cell(r.Tool), pct(r.Thresholds.IdleActivity), pct(r.Thresholds.KVTarget))
	return b.err
}

func wasteCell(f analyze.Finding, currency string) string {
	switch {
	case f.WasteGPUHours > 0 && f.Priced:
		return fmt.Sprintf("%s (%s GPU-h)", money(f.WasteCost, currency), hours(f.WasteGPUHours))
	case f.WasteGPUHours > 0:
		return hours(f.WasteGPUHours) + " GPU-h"
	case f.IdleGPUHours > 0:
		return hours(f.IdleGPUHours) + " idle GPU-h"
	}
	return "advice"
}

// cell makes text safe inside a Markdown table cell.
func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ", "`", "'").Replace(s)
}

// html escapes text for the summary line of a details block.
func html(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
