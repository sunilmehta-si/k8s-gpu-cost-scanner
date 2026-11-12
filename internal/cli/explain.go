package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
)

func runExplain(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "Rules, in the order they run (gpucost explain RULE for one):")
		fmt.Fprintln(stdout)
		for _, r := range analyze.Rules {
			counted := ""
			if !r.Counted {
				counted = " (advice, not counted as waste)"
			}
			fmt.Fprintf(stdout, "  %s  %-16s %s%s\n", r.Rule, r.Slug, r.Title, counted)
		}
		return nil
	}
	if len(args) > 1 {
		return usagef("explain takes one rule, got %d", len(args))
	}
	r, ok := analyze.FindRule(args[0])
	if !ok {
		return usagef("no rule %q; run gpucost explain to list them", args[0])
	}
	fmt.Fprintf(stdout, "%s %s: %s\n\n", r.Rule, r.Slug, r.Title)
	for _, part := range []struct{ head, body string }{{"What", r.What}, {"How it is found", r.How}, {"Fix", r.Fix}} {
		fmt.Fprintf(stdout, "%s\n", part.head)
		for _, line := range wrapText(part.body, 88) {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
		fmt.Fprintln(stdout)
	}
	if !r.Counted {
		fmt.Fprintln(stdout, strings.TrimSpace("Advice: its GPU-hours are reported as idle time, not as recoverable waste."))
	}
	return nil
}
