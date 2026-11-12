package analyze

import (
	"strings"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestStranded(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	w.node("n2", "", 4)
	// n1: 3 GPUs requested all window; 5 GPUs × 11 h = 55 GPU-hours stranded.
	w.pod("ml", "a", "n1", "", 3, always)
	// n2: fully requested.
	w.pod("ml", "b", "n2", "", 4, always)
	// DCGM sees one of n1's free GPUs, with no pod.
	w.s.Range["gpu_activity"] = append(w.s.Range["gpu_activity"], prom.Series{
		Labels: prom.Labels{"UUID": "GPU-free", "Hostname": "n1"}, Points: w.series(constant(0)),
	})
	r := w.run()
	got := findings(r, "GPU004")
	if len(got) != 1 || got[0].Target != "node/n1" || !near(got[0].WasteGPUHours, 55) || !near(got[0].WasteCost, 110) {
		t.Fatalf("stranded = %+v", got)
	}
	if got[0].Severity != High {
		t.Errorf("severity = %s", got[0].Severity)
	}
	ev := strings.Join(got[0].Evidence, " | ")
	if !strings.Contains(ev, "62.5%") || !strings.Contains(ev, "DCGM saw 1 GPUs on the node with no pod") {
		t.Errorf("evidence = %s", ev)
	}
}
