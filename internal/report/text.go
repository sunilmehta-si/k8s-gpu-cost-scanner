package report

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
)

// TextOptions tune the terminal report.
type TextOptions struct {
	// Width is the line width evidence and advice wrap at. Zero means 100.
	Width int
	// MaxWorkloads caps the workloads table. Zero means 10.
	MaxWorkloads int
	// Verbose adds every finding's evidence, the advice findings and the PromQL used.
	Verbose bool
}

// Text writes the report for a terminal.
func Text(w io.Writer, r *analyze.Report, o TextOptions) error {
	if o.Width == 0 {
		o.Width = 100
	}
	if o.MaxWorkloads == 0 {
		o.MaxWorkloads = 10
	}
	b := &errWriter{w: w}
	s := r.Summary
	b.printf("gpucost · %s window ending %s · %s\n\n", windowText(r), r.Window.End.Format("2006-01-02 15:04 MST"), r.Source)

	b.printf("GPU fleet\n")
	b.printf("  %d nodes · %d GPU pods · %s GPU-hours offered · %s requested (%s)\n",
		s.Nodes, s.Pods, hours(s.OfferedGPUHours), hours(s.RequestedGPUHours), share(s.RequestedGPUHours, s.OfferedGPUHours))
	if s.MeasuredGPUHours > 0 {
		b.printf("  GPU activity %s, over the %s of requested GPU-hours DCGM measured\n", pct(s.Activity), share(s.MeasuredGPUHours, s.RequestedGPUHours))
	}
	if s.PricedShare > 0 {
		b.printf("  Cost %s for requested GPUs · %s for every allocatable GPU (%s of GPU-hours priced)\n",
			money(s.Cost, r.Currency), money(s.OfferedCost, r.Currency), pct(s.PricedShare))
	} else {
		b.printf("  No prices: costs are in GPU-hours. Pass --prices or --price, or run OpenCost.\n")
	}
	waste := hours(s.WasteGPUHours) + " GPU-hours"
	if s.PricedShare > 0 {
		waste = money(s.WasteCost, r.Currency) + " · " + waste
	}
	b.printf("  Recoverable waste %s · %s\n", waste, findingCounts(s.Findings))
	if s.IdleGPUHours > 0 {
		b.printf("  Idle time in serving pods %s GPU-hours (advice: only scaling to zero or sharing recovers it)\n", hours(s.IdleGPUHours))
	}

	b.printf("\nFindings\n")
	shown := 0
	for _, f := range r.Findings {
		if f.Severity == analyze.Info && !o.Verbose {
			continue
		}
		shown++
		b.printf("  %-6s %s %s · %s", strings.ToUpper(string(f.Severity)), f.Rule, f.Title, f.Target)
		switch {
		case f.WasteGPUHours > 0 && f.Priced:
			b.printf(" · %s GPU-hours · %s\n", hours(f.WasteGPUHours), money(f.WasteCost, r.Currency))
		case f.WasteGPUHours > 0:
			b.printf(" · %s GPU-hours\n", hours(f.WasteGPUHours))
		case f.IdleGPUHours > 0:
			b.printf(" · %s idle GPU-hours\n", hours(f.IdleGPUHours))
		default:
			b.printf("\n")
		}
		for _, e := range f.Evidence {
			for i, line := range wrap(e, o.Width-11) {
				lead := "         - "
				if i > 0 {
					lead = "           "
				}
				b.printf("%s%s\n", lead, line)
			}
		}
		for i, line := range wrap(f.Recommendation, o.Width-11) {
			lead := "         → "
			if i > 0 {
				lead = "           "
			}
			b.printf("%s%s\n", lead, line)
		}
	}
	if shown == 0 {
		b.printf("  None. Nothing held GPUs it did not use, by the thresholds in force.\n")
	}
	if hidden := len(r.Findings) - shown; hidden > 0 {
		b.printf("  (%d more findings of severity info; --verbose shows them)\n", hidden)
	}

	if len(r.Models) > 0 {
		b.printf("\nModels\n")
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  MODEL\tWORKLOAD\tREPLICAS\tGPU-HOURS\tCOST\tACTIVITY\tBUSY\tOUTPUT TOKENS\tPER 1M TOKENS\n")
		for _, m := range r.Models {
			fmt.Fprintf(tw, "  %s\t%s/%s\t%.1f (max %d)\t%s\t%s\t%s\t%s\t%s\t%s\n",
				truncate(m.Model, 40), m.Namespace, truncate(m.Workload, 36), m.Replicas, m.MaxReplicas, hours(m.GPUHours),
				costCell(m.Cost, m.Priced, r.Currency), activityCell(m.Activity, m.Measured), pct(m.BusyShare),
				count(m.GenerationTokens), perMillion(m, r.Currency))
		}
		tw.Flush()
	}

	if len(r.Namespaces) > 1 {
		b.printf("\nNamespaces\n")
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  NAMESPACE\tPODS\tGPU-HOURS\tCOST\tACTIVITY\tWASTE\n")
		for _, ns := range r.Namespaces {
			waste := hours(ns.WasteGPUHours) + " GPU-h"
			if ns.Priced && ns.WasteGPUHours > 0 {
				waste = money(ns.WasteCost, r.Currency) + " (" + waste + ")"
			}
			fmt.Fprintf(tw, "  %s\t%d\t%s\t%s\t%s\t%s\n", ns.Namespace, ns.Pods, hours(ns.GPUHours),
				costCell(ns.Cost, ns.Priced, r.Currency), activityCell(ns.Activity, ns.Measured), waste)
		}
		tw.Flush()
	}

	if len(r.Workloads) > 0 {
		b.printf("\nWorkloads by GPU-hours\n")
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  WORKLOAD\tPODS\tGPU-HOURS\tCOST\tACTIVITY\tWASTE GPU-HOURS\n")
		for i, wl := range r.Workloads {
			if i == o.MaxWorkloads {
				fmt.Fprintf(tw, "  … %d more\t\t\t\t\t\n", len(r.Workloads)-i)
				break
			}
			fmt.Fprintf(tw, "  %s/%s/%s\t%d\t%s\t%s\t%s\t%s\n", wl.Namespace, wl.Kind, truncate(wl.Name, 40), wl.Pods,
				hours(wl.GPUHours), costCell(wl.Cost, wl.Priced, r.Currency), activityCell(wl.Activity, wl.Measured), hours(wl.WasteGPUHours))
		}
		tw.Flush()
	}

	if len(r.BlindSpots) > 0 {
		b.printf("\nBlind spots: what gpucost could not see\n")
		for _, bs := range r.BlindSpots {
			b.printf("  %s %s", bs.Code, bs.Title)
			if bs.GPUHours > 0 {
				b.printf(" · %s GPU-hours (%s)", hours(bs.GPUHours), pct(bs.Share))
			}
			b.printf("\n")
			for _, line := range wrap(bs.Detail, o.Width-7) {
				b.printf("       %s\n", line)
			}
		}
	}

	if o.Verbose && len(r.Queries) > 0 {
		b.printf("\nQueries\n")
		for _, name := range sortedKeys(r.Queries) {
			b.printf("  %s\n    %s\n", name, r.Queries[name])
		}
	}
	return b.err
}

func windowText(r *analyze.Report) string {
	d := r.Window.Length()
	if d%(24*3600e9) == 0 {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d%3600e9 == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return d.String()
}

func share(part, whole float64) string {
	if whole <= 0 {
		return "n/a"
	}
	return pct(part / whole)
}

func findingCounts(m map[analyze.Severity]int) string {
	var parts []string
	total := 0
	for _, s := range []analyze.Severity{analyze.High, analyze.Medium, analyze.Low, analyze.Info} {
		if n := m[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
			total += n
		}
	}
	if total == 0 {
		return "no findings"
	}
	noun := "findings"
	if total == 1 {
		noun = "finding"
	}
	return fmt.Sprintf("%d %s (%s)", total, noun, strings.Join(parts, ", "))
}

func costCell(v float64, priced bool, currency string) string {
	if !priced {
		return "unpriced"
	}
	return money(v, currency)
}

func activityCell(v float64, measured bool) string {
	if !measured {
		return "n/a"
	}
	return pct(v)
}

func perMillion(m analyze.ModelCost, currency string) string {
	switch {
	case m.GenerationTokens <= 0:
		return "no output"
	case m.CostPerMillionTokens > 0:
		return money(m.CostPerMillionTokens, currency)
	default:
		return grouped(m.GPUHoursPerMillionTokens, 2) + " GPU-h"
	}
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}

func (e *errWriter) printf(format string, args ...any) { fmt.Fprintf(e, format, args...) }
