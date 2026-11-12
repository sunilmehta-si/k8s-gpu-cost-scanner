package analyze

import "testing"

func TestNamespaces(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 4)
	w.pod("research", "nb-0", "n1", "", 1, always)
	w.activity("research", "nb-0", "GPU-1", constant(0))
	w.pod("ml", "train-0", "n1", "", 2, always)
	w.activity("ml", "train-0", "GPU-2", constant(0.8))
	w.activity("ml", "train-0", "GPU-3", constant(0.6))
	r := w.run()
	if len(r.Namespaces) != 2 {
		t.Fatalf("namespaces = %+v", r.Namespaces)
	}
	ml, research := r.Namespaces[0], r.Namespaces[1]
	if ml.Namespace != "ml" || !near(ml.GPUHours, 22) || !near(ml.Cost, 44) || !near(ml.Activity, 0.7) || ml.WasteGPUHours != 0 {
		t.Errorf("ml = %+v", ml)
	}
	// The idle notebook's 11 GPU-hours are its namespace's waste; the node's free GPU belongs to no namespace.
	if research.Namespace != "research" || !near(research.WasteGPUHours, 11) || !near(research.WasteCost, 22) || !research.Measured {
		t.Errorf("research = %+v", research)
	}
}
