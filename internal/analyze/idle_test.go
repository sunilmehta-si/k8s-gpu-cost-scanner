package analyze

import (
	"math"
	"testing"
)

var nan = math.NaN()

func TestIdleGPU(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	// Idle for the whole window: 11 points × 1 h × 2 GPUs = 22 GPU-hours.
	w.pod("research", "notebook-0", "n1", "", 2, always)
	w.activity("research", "notebook-0", "GPU-1", constant(0.01))
	w.activity("research", "notebook-0", "GPU-2", constant(0.0))
	// Busy: not idle.
	w.pod("ml", "trainer-0", "n1", "", 1, always)
	w.activity("ml", "trainer-0", "GPU-3", constant(0.9))
	// Idle, but held for one hourly step, under the two-hour minimum set below: not judged.
	w.pod("ml", "blip", "n1", "", 1, func(i int) bool { return i == 0 })
	w.activity("ml", "blip", "GPU-4", constant(0))
	// Idle on most steps but with a burst at the 95th percentile: not idle.
	w.pod("ml", "bursty", "n1", "", 1, always)
	w.activity("ml", "bursty", "GPU-5", func(i int) float64 {
		if i == 10 {
			return 0.8
		}
		return 0
	})

	r := w.runWith(func(t *Thresholds) {
		t.MinHours = 2
		// Leave the node's unrequested GPUs out of this test's totals.
		t.MinStrandedHours = 1e9
	})
	got := findings(r, "GPU001")
	if len(got) != 1 {
		t.Fatalf("idle findings = %+v", got)
	}
	f := got[0]
	if f.Target != "research/Pod/notebook-0" || !near(f.WasteGPUHours, 22) || !near(f.WasteCost, 44) || !f.Priced {
		t.Errorf("finding = %+v", f)
	}
	if f.Severity != Medium {
		t.Errorf("severity = %s, want medium for 22 GPU-hours", f.Severity)
	}
	if !near(r.Summary.WasteGPUHours, 22) || r.Summary.Findings[Medium] != 1 {
		t.Errorf("summary = %+v", r.Summary)
	}
}

func TestIdleNeedsCoverage(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.pod("ml", "half-seen", "n1", "", 1, always)
	// DCGM measured 3 of 11 steps: too little to call the GPU idle.
	w.activity("ml", "half-seen", "GPU-1", func(i int) float64 {
		if i < 3 {
			return 0
		}
		return nan
	})
	if got := findings(w.run(), "GPU001"); len(got) != 0 {
		t.Errorf("judged on 3 of 11 steps: %+v", got)
	}
}

func TestIdleServingPodIsLeftToServingRules(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.pod("ml", "llm-0", "n1", "", 1, always)
	w.activity("ml", "llm-0", "GPU-1", constant(0.01))
	w.vllm("ml", "llm-0", "m", constant(1), nil, 500, 10)
	if got := findings(w.run(), "GPU001"); len(got) != 0 {
		t.Errorf("a pod that served requests was called idle: %+v", got)
	}
}

func TestSummary(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.pod("ml", "a", "n1", "", 2, always)
	w.activity("ml", "a", "GPU-1", constant(0.5))
	w.activity("ml", "a", "GPU-2", constant(0.3))
	s := w.run().Summary
	// 8 GPUs × 11 h offered; 2 × 11 requested at a flat price of 2.
	if !near(s.OfferedGPUHours, 88) || !near(s.RequestedGPUHours, 22) || !near(s.Cost, 44) || !near(s.OfferedCost, 176) {
		t.Errorf("summary = %+v", s)
	}
	if !near(s.Activity, 0.4) || !near(s.MeasuredGPUHours, 22) || s.PricedShare != 1 {
		t.Errorf("activity = %+v", s)
	}
}
