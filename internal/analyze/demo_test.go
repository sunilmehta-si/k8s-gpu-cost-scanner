package analyze

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/price"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/synth"
)

// demoSnapshot is the synthetic fleet as a real Prometheus returned it: the committed recording, or a fresh one
// from hack/e2e.sh when GPUCOST_E2E_SNAPSHOT names it.
func demoSnapshot(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	path := os.Getenv("GPUCOST_E2E_SNAPSHOT")
	if path == "" {
		path = filepath.Join("..", "..", "testdata", "demo.snapshot.json.gz")
	}
	s, err := snapshot.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestDemoMeetsExpectations checks every finding the synthetic fleet was built to produce, and that nothing else
// is counted as waste.
func TestDemoMeetsExpectations(t *testing.T) {
	sc := synth.Demo()
	s := demoSnapshot(t)
	f := fleet.Build(s)
	tb := price.NewTable()
	for k, v := range sc.Prices {
		tb.Set(k, v, "demo prices")
	}
	r := Run(f, s, price.Resolve(tb, f, s), Options{Now: time.Now()})

	found := map[string]Finding{}
	for _, fd := range r.Findings {
		found[fd.Rule+" "+fd.Target] = fd
	}
	for _, e := range sc.Expect {
		fd, ok := found[e.Rule+" "+e.Target]
		if !ok {
			t.Errorf("missing %s on %s", e.Rule, e.Target)
			continue
		}
		got := fd.WasteGPUHours
		if fd.WasteGPUHours == 0 {
			got = fd.IdleGPUHours
		}
		if math.Abs(got-e.GPUHours) > e.Tolerance {
			t.Errorf("%s on %s: %.2f GPU-hours, want %.2f ± %.2f", e.Rule, e.Target, got, e.GPUHours, e.Tolerance)
		}
		delete(found, e.Rule+" "+e.Target)
	}
	for key, fd := range found {
		t.Errorf("unexpected finding %s: %.2f GPU-hours", key, fd.WasteGPUHours)
	}
	// The fine-tune ran hard and finished: no finding names it.
	for _, fd := range r.Findings {
		if fd.Namespace == "training" {
			t.Errorf("the finished fine-tune was flagged: %+v", fd)
		}
	}
	if len(r.BlindSpots) != 0 {
		t.Errorf("the demo fleet exports everything, yet: %+v", r.BlindSpots)
	}
}
