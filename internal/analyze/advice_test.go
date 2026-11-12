package analyze

import "testing"

func TestLowDuty(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 1)
	w.pod("ml", "rare-0", "n1", "", 1, always)
	// A request running at 2 of 11 steps.
	w.vllm("ml", "rare-0", "m", func(i int) float64 {
		if i == 2 || i == 7 {
			return 1
		}
		return 0
	}, nil, 300, 2)
	r := w.run()
	got := findings(r, "GPU006")
	if len(got) != 1 || got[0].WasteGPUHours != 0 || !near(got[0].IdleGPUHours, 9) || got[0].Severity != Low {
		t.Fatalf("low duty = %+v", got)
	}
	if r.Summary.WasteGPUHours != 0 || !near(r.Summary.IdleGPUHours, 9) {
		t.Errorf("summary = %+v: idle hours must stay out of waste", r.Summary)
	}
}

func TestOversized(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 2)
	w.pod("ml", "small-0", "n1", "", 1, always)
	w.activity("ml", "small-0", "GPU-1", constant(0.12))
	w.vllm("ml", "small-0", "tiny", constant(1), constant(0.05), 5000, 400)
	// Busy enough to be real work, near the threshold: not oversized.
	w.pod("ml", "fit-0", "n1", "", 1, always)
	w.activity("ml", "fit-0", "GPU-2", constant(0.6))
	r := w.run()
	got := findings(r, "GPU007")
	if len(got) != 1 || got[0].Target != "ml/small-0" || got[0].Severity != Info || got[0].WasteGPUHours != 0 {
		t.Fatalf("oversized = %+v", got)
	}
}
