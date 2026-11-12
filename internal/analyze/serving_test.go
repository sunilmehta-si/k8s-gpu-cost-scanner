package analyze

import (
	"strings"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestNoTraffic(t *testing.T) {
	w := newWorld(t)
	// Exactly the two GPUs the pods request, so no node-level waste adds to the total.
	w.node("n1", "", 2)
	w.pod("ml", "forgotten-0", "n1", "forgotten", 1, always)
	w.activity("ml", "forgotten-0", "GPU-1", constant(0.0))
	w.vllm("ml", "forgotten-0", "old-model", constant(0), nil, 0, 0)
	w.pod("ml", "busy-0", "n1", "busy", 1, always)
	w.vllm("ml", "busy-0", "new-model", constant(3), nil, 9000, 120)
	r := w.run()
	got := findings(r, "GPU002")
	if len(got) != 1 || got[0].Target != "ml/Deployment/forgotten" || !near(got[0].WasteGPUHours, 11) {
		t.Fatalf("no-traffic = %+v", got)
	}
	// Claimed by the more specific rule, the same pod is not also called idle.
	if idle := findings(r, "GPU001"); len(idle) != 0 {
		t.Errorf("counted twice: %+v", idle)
	}
	if !near(r.Summary.WasteGPUHours, 11) {
		t.Errorf("total waste = %v, want 11", r.Summary.WasteGPUHours)
	}
}

func TestNoTrafficWithoutCounters(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.pod("ml", "quiet-0", "n1", "", 1, always)
	w.s.Range["vllm_running"] = append(w.s.Range["vllm_running"], prom.Series{
		Labels: prom.Labels{"namespace": "ml", "pod": "quiet-0"}, Points: w.series(constant(0)),
	})
	got := findings(w.run(), "GPU002")
	if len(got) != 1 || !strings.Contains(strings.Join(got[0].Evidence, " "), "counters were not collected") {
		t.Fatalf("no-traffic = %+v", got)
	}
}

func TestAsleep(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.pod("rl", "policy-0", "n1", "", 2, always)
	w.vllm("rl", "policy-0", "policy", constant(0), nil, 100, 2)
	// Awake for 3 of 11 steps.
	w.s.Range["vllm_awake"] = append(w.s.Range["vllm_awake"], prom.Series{
		Labels: prom.Labels{"namespace": "rl", "pod": "policy-0", "engine": "0"},
		Points: w.series(func(i int) float64 {
			if i < 3 {
				return 1
			}
			return 0
		}),
	})
	got := findings(w.run(), "GPU003")
	// 2 GPUs × 11 h × 8/11 asleep = 16 GPU-hours.
	if len(got) != 1 || !near(got[0].WasteGPUHours, 16) {
		t.Fatalf("asleep = %+v", got)
	}
}

func TestAsleepButGPUBusyIsNotFlagged(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.pod("rl", "shared-0", "n1", "", 1, always)
	w.activity("rl", "shared-0", "GPU-1", constant(0.7))
	w.vllm("rl", "shared-0", "policy", constant(0), nil, 100, 2)
	w.s.Range["vllm_awake"] = append(w.s.Range["vllm_awake"], prom.Series{
		Labels: prom.Labels{"namespace": "rl", "pod": "shared-0"}, Points: w.series(constant(0)),
	})
	if got := findings(w.run(), "GPU003"); len(got) != 0 {
		t.Errorf("flagged a sleeping engine whose GPU another engine kept busy: %+v", got)
	}
}
