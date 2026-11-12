package analyze

import (
	"fmt"
	"math"
	"sort"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
)

// overprovisioned flags serving workloads that ran more replicas than their busiest moment needed. At each step,
// the replicas needed are the KV cache in use across all replicas divided by KVTarget, rounded up; the workload's
// peak need is the most needed at any step. Replicas beyond that peak, at any time, were never needed. A workload
// that queued requests at any point is left alone: it may have needed every replica it had.
func (a *analysis) overprovisioned() {
	type group struct {
		name string
		ns   string
		pods []*fleet.Pod
	}
	groups := map[string]*group{}
	for _, p := range a.pods() {
		if p.Engine == nil || p.Workload.Kind == "Pod" {
			continue
		}
		key := p.Workload.String()
		g := groups[key]
		if g == nil {
			g = &group{name: key, ns: p.Workload.Namespace}
			groups[key] = g
		}
		g.pods = append(g.pods, p)
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)

	stepHours := a.f.Window.StepHours()
	for _, name := range names {
		g := groups[name]
		if len(g.pods) < 2 {
			continue
		}
		queued, kvKnown := false, true
		steps := map[int64]bool{}
		gpusPerPod, heldSteps := 0.0, 0.0
		for _, p := range g.pods {
			if p.Engine.Waiting.Max > 0 {
				queued = true
			}
			if !p.Engine.KV.Measured() || float64(p.Engine.KV.N) < 0.8*float64(len(p.Engine.Steps)) {
				kvKnown = false
			}
			for _, t := range p.Engine.Timeline() {
				steps[t] = true
			}
			eq, _ := a.f.GPUEquivalents(p)
			gpusPerPod += eq
			heldSteps += p.RunningHours
		}
		if queued || !kvKnown || heldSteps == 0 {
			continue
		}
		gpusPerPod /= heldSteps

		peakNeed, peakKV, maxReplicas := 1, 0.0, 0
		replicaSteps := map[int64]int{}
		for t := range steps {
			sum, n := 0.0, 0
			for _, p := range g.pods {
				if !p.Held(t) {
					continue
				}
				n++
				if l, ok := p.Engine.Steps[t]; ok && l.HasKV {
					sum += l.KV
				}
			}
			replicaSteps[t] = n
			if n > maxReplicas {
				maxReplicas = n
			}
			if sum > peakKV {
				peakKV = sum
			}
			if need := int(math.Ceil(sum/a.t.KVTarget - 1e-9)); need > peakNeed {
				peakNeed = need
			}
		}
		excess := 0.0
		for _, n := range replicaSteps {
			if n > peakNeed {
				excess += float64(n-peakNeed) * gpusPerPod * stepHours
			}
		}
		if excess < a.t.MinHours {
			continue
		}
		ev := []string{
			fmt.Sprintf("ran up to %d replicas of %s", maxReplicas, modelText(g.pods[0].Engine)),
			fmt.Sprintf("the KV cache in use across all replicas peaked at %s of one replica's cache", pct(peakKV)),
			fmt.Sprintf("at most %d replicas were needed to keep each under %s KV use, at the busiest step", peakNeed, pct(a.t.KVTarget)),
			"no request waited in a queue at any replica",
		}
		a.claimWorkload(name, g.ns, g.pods, Finding{
			Rule: "GPU005", Slug: "excess-replicas", Title: "More replicas than the peak load needed",
			Evidence: ev,
			Recommendation: fmt.Sprintf("Cap the workload at %d replicas, or let an autoscaler follow the load: scale on "+
				"KV cache use or requests in the system, which track an LLM server's capacity, not on CPU. Keep a "+
				"replica of headroom if the peak in this window was not the busiest you expect.", peakNeed),
		}, excess)
	}
}
