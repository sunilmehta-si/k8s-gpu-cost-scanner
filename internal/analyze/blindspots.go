package analyze

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
)

// BlindSpot is something gpucost could not see, and how much of the fleet it affects. A report with large blind
// spots understates the waste; it does not overstate it.
type BlindSpot struct {
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// GPUHours affected, and their share of all requested GPU-hours; zero for gaps that are not about hours.
	GPUHours float64 `json:"gpu_hours,omitempty"`
	Share    float64 `json:"share,omitempty"`
}

func (a *analysis) blindSpots() []BlindSpot {
	var out []BlindSpot
	total, unmeasured, unpriced, assumedMIG := 0.0, 0.0, 0.0, 0.0
	unpricedNodes := map[string]bool{}
	for _, p := range a.pods() {
		eq, exact := a.f.GPUEquivalents(p)
		total += eq
		unmeasured += eq * (1 - p.Usage.Coverage)
		if _, ok := a.cost(p.Node, eq); !ok {
			unpriced += eq
			unpricedNodes[p.Node] = true
		}
		if !exact {
			assumedMIG += eq
		}
	}
	share := func(h float64) float64 {
		if total == 0 {
			return 0
		}
		return h / total
	}
	if unmeasured > 0.005 {
		out = append(out, BlindSpot{
			Code: "B001", Title: "GPU time without activity data",
			Detail: "The DCGM exporter reported no activity for these GPU-hours, so no idle GPU among them can be " +
				"found. Run the exporter on every GPU node with its Kubernetes pod mapping on.",
			GPUHours: unmeasured, Share: share(unmeasured),
		})
	}
	if unpriced > 0.005 {
		names := keys(unpricedNodes)
		out = append(out, BlindSpot{
			Code: "B002", Title: "GPU time without a price",
			Detail: fmt.Sprintf("No price for nodes %s; their GPU-hours are left out of every cost. Pass "+
				"--prices or --price, or run OpenCost.", list(names, 5)),
			GPUHours: unpriced, Share: share(unpriced),
		})
	}
	var unknown []string
	for _, n := range a.f.SortedNodes() {
		if n.Product == gpu.Unknown {
			unknown = append(unknown, n.Name)
		}
	}
	if len(unknown) > 0 {
		out = append(out, BlindSpot{
			Code: "B003", Title: "Nodes whose GPU model is unknown",
			Detail: fmt.Sprintf("Nothing named the GPU on %s, so a price by GPU model cannot apply to them. The DCGM "+
				"exporter's modelName, or the nvidia.com/gpu.product node label exported by kube-state-metrics, "+
				"would.", list(unknown, 5)),
		})
	}
	if assumedMIG > 0.005 {
		out = append(out, BlindSpot{
			Code: "B004", Title: "MIG slices on a GPU of unknown slice count",
			Detail: "These slices were counted as sevenths of a GPU, the A100 and H100 layout; their cost may be off " +
				"if the GPU divides differently.",
			GPUHours: assumedMIG, Share: share(assumedMIG),
		})
	}
	notes := append(append([]string{}, a.f.Notes...), a.s.Notes...)
	for _, n := range notes {
		if strings.Contains(n, "skipped") && optionalSignal(n) {
			continue
		}
		out = append(out, BlindSpot{Code: "B005", Title: "Collection gap", Detail: n})
	}
	if out == nil {
		out = []BlindSpot{}
	}
	return out
}

// optionalSignal reports whether a skipped-signal note is about a source many clusters do not run, which is not
// worth a blind spot of its own: SGLang beside vLLM, OpenCost, sleep mode or power.
func optionalSignal(note string) bool {
	for _, p := range []string{"sglang_", "opencost_", "vllm_awake", "gpu_power_watts", "node_labels"} {
		if strings.HasPrefix(note, p) {
			return true
		}
	}
	return false
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k == "" {
			k = "(no node)"
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func list(xs []string, max int) string {
	if len(xs) <= max {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:max], ", ") + fmt.Sprintf(" and %d more", len(xs)-max)
}
