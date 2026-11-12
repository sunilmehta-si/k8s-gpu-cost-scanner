package analyze

import (
	"strings"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/price"
)

func TestModelCosts(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 4)
	w.pod("ml", "llama-a", "n1", "llama", 1, always)
	w.pod("ml", "llama-b", "n1", "llama", 1, func(i int) bool { return i < 5 })
	w.activity("ml", "llama-a", "GPU-1", constant(0.6))
	w.activity("ml", "llama-b", "GPU-2", constant(0.2))
	w.vllm("ml", "llama-a", "llama-8b", constant(4), constant(0.5), 1_000_000, 900)
	w.vllm("ml", "llama-b", "llama-8b", constant(1), constant(0.2), 100_000, 100)
	w.pod("ml", "trainer-0", "n1", "", 2, always)
	r := w.run()
	if len(r.Models) != 1 {
		t.Fatalf("models = %+v", r.Models)
	}
	m := r.Models[0]
	// 11 + 5 GPU-hours at 2 per hour.
	if m.Workload != "Deployment/llama" || !near(m.GPUHours, 16) || !near(m.Cost, 32) || m.MaxReplicas != 2 {
		t.Errorf("model = %+v", m)
	}
	// 32 for 1.1M output tokens.
	if !near(m.CostPerMillionTokens, 32/1.1) || !near(m.GPUHoursPerMillionTokens, 16/1.1) {
		t.Errorf("per million = %v, %v", m.CostPerMillionTokens, m.GPUHoursPerMillionTokens)
	}
	// Weighted by GPU-hours: (11 × 0.6 + 5 × 0.2) / 16.
	if !near(m.Activity, (11*0.6+5*0.2)/16) || !near(m.Replicas, 16.0/10) {
		t.Errorf("activity %v, replicas %v", m.Activity, m.Replicas)
	}
	if len(r.Workloads) != 2 || r.Workloads[0].Kind != "Pod" || !near(r.Workloads[0].GPUHours, 22) || r.Workloads[0].Measured {
		t.Errorf("workloads = %+v", r.Workloads)
	}
}

func TestBlindSpots(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 2)
	w.pod("ml", "a", "n1", "", 2, always)
	w.s.Note("vllm_running: skipped, the server has none of vllm:num_requests_running")
	w.s.Note("sglang_running: skipped, the server has none of sglang:num_running_reqs")
	f := fleet.Build(w.s)
	r := Run(f, w.s, price.Resolve(price.NewTable(), f, w.s), Options{Now: time.Now()})
	codes := map[string]BlindSpot{}
	var gaps []string
	for _, b := range r.BlindSpots {
		codes[b.Code] = b
		if b.Code == "B005" {
			gaps = append(gaps, b.Detail)
		}
	}
	if b := codes["B001"]; !near(b.GPUHours, 22) || b.Share != 1 {
		t.Errorf("unmeasured = %+v", b)
	}
	if b := codes["B002"]; !near(b.GPUHours, 22) || !strings.Contains(b.Detail, "n1") {
		t.Errorf("unpriced = %+v", b)
	}
	if _, ok := codes["B003"]; !ok {
		t.Error("unknown product not reported")
	}
	joined := strings.Join(gaps, "\n")
	if !strings.Contains(joined, "vllm_running") || strings.Contains(joined, "sglang_running") || !strings.Contains(joined, "GPU activity") {
		t.Errorf("collection gaps = %s", joined)
	}
}
