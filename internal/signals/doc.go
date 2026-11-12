package signals

import (
	"fmt"
	"io"
	"strings"
)

var sourceIntro = map[string]string{
	KubeStateMetrics: "Which pods held GPUs, for how long, on which node, for which workload. Required.",
	DCGM:             "How busy each GPU was, with NVIDIA's dcgm-exporter and its Kubernetes pod mapping on. Required to find idle GPUs.",
	VLLM:             "What each vLLM server did. Needed for model costs and the serving rules.",
	SGLang:           "What each SGLang server did. Needed for model costs and the serving rules.",
	OpenCost:         "GPU prices per node, when you run OpenCost. Optional: prices can come from a file instead.",
}

// WriteMarkdown writes the metrics reference, docs/metrics.md: every signal, the metrics each variant needs and
// the PromQL it runs.
func WriteMarkdown(w io.Writer) error {
	b := &strings.Builder{}
	b.WriteString("# Metrics reference\n\n")
	b.WriteString("<!-- Generated from internal/signals/signals.go by `go test ./internal/signals -update-docs`. Do not edit by hand. -->\n\n")
	b.WriteString("Everything gpucost asks Prometheus for. For each signal the first variant whose metrics the server has is ")
	b.WriteString("used; `gpucost doctor` says which. `$WINDOW` is the window and `$STEP` the step, in seconds. Range signals ")
	b.WriteString("are sampled at every step of the window; instant signals are read once at its end.\n")
	for _, src := range []string{KubeStateMetrics, DCGM, VLLM, SGLang, OpenCost} {
		fmt.Fprintf(b, "\n## %s\n\n%s\n", src, sourceIntro[src])
		for _, s := range Catalog {
			if s.Source != src {
				continue
			}
			unit := ""
			if s.Unit != "" {
				unit = ", " + s.Unit
			}
			fmt.Fprintf(b, "\n### `%s` (%s%s)\n\n%s\n", s.Name, s.Kind, unit, s.Doc)
			for i, v := range s.Variants {
				label := "Needs"
				if len(s.Variants) > 1 {
					label = fmt.Sprintf("Variant %d needs", i+1)
				}
				needs := make([]string, len(v.Needs))
				for j, n := range v.Needs {
					needs[j] = "`" + n + "`"
				}
				fmt.Fprintf(b, "\n%s %s:\n\n```promql\n%s\n```\n", label, strings.Join(needs, ", "), v.Query)
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
