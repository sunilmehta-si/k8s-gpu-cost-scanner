package analyze

import (
	"fmt"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
)

// stranded flags nodes whose GPUs were allocatable but requested by no pod. These GPU-hours belong to no pod, so
// they never overlap the pod rules. Every pod on the node counts, whatever namespaces the scan is limited to.
func (a *analysis) stranded() {
	requested := map[string]float64{}
	for _, p := range a.f.Pods {
		eq, _ := a.f.GPUEquivalents(p)
		requested[p.Node] += eq
	}
	unmapped := map[string][]*fleet.GPU{}
	for _, g := range a.f.GPUs {
		if g.Pod == nil && g.MIGInstance == "" {
			unmapped[g.Node] = append(unmapped[g.Node], g)
		}
	}
	window := a.f.Window.Hours()
	for _, n := range a.f.SortedNodes() {
		offered := a.f.OfferedGPUHours(n)
		left := offered - requested[n.Name]
		if offered <= 0 || left < a.t.MinStrandedHours {
			continue
		}
		ev := []string{
			fmt.Sprintf("offered %s (%s GPUs at most); pods requested %s, %s on average",
				hrs(offered), offeredText(n), hrs(requested[n.Name]), gpusAvg(requested[n.Name], window)),
			fmt.Sprintf("%s of the node's GPU time (%s) no pod requested", pct(left/offered), hrs(left)),
		}
		if gs := unmapped[n.Name]; len(gs) > 0 {
			busy := 0.0
			for _, g := range gs {
				busy += g.Activity.Avg
			}
			ev = append(ev, fmt.Sprintf("DCGM saw %d GPUs on the node with no pod, averaging %s activity", len(gs), pct(busy/float64(len(gs)))))
		}
		fd := Finding{
			Rule: "GPU004", Slug: "stranded-gpus", Title: "GPUs no pod requested",
			Target: "node/" + n.Name, Node: n.Name, Evidence: ev, WasteGPUHours: left,
			Recommendation: "Let the cluster autoscaler remove the node or shrink its node pool when GPUs sit unrequested, " +
				"or pack GPU pods onto fewer nodes: KAI Scheduler bin-packs GPUs by default; Volcano needs its binpack " +
				"plugin weighted for nvidia.com/gpu; the default scheduler needs MostAllocated scoring for it. If pods " +
				"are pending at the same time, the free GPUs are split across nodes in pieces too small for them.",
		}
		fd.WasteCost, fd.Priced = a.cost(n.Name, left)
		fd.Severity = a.severity(left)
		a.findings = append(a.findings, fd)
	}
}

func offeredText(n *fleet.Node) string {
	most := 0.0
	for _, v := range n.Offered {
		if v > most {
			most = v
		}
	}
	return fmt.Sprintf("%g", most)
}

func gpusAvg(hours, window float64) string {
	if window <= 0 {
		return "0 GPUs"
	}
	return fmt.Sprintf("%.1f GPUs", hours/window)
}
