package analyze

import (
	"strings"
	"testing"
)

func TestExcessReplicas(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	// Four replicas of one Deployment, each 1 GPU; KV use per replica peaks at 0.3, so all four together peak at
	// 1.2 of one replica's cache: two replicas at 80% would have done. Two replicas too many for 11 hours.
	for _, name := range []string{"llm-a", "llm-b", "llm-c", "llm-d"} {
		w.pod("ml", name, "n1", "llm", 1, always)
		w.vllm("ml", name, "llama", constant(2), func(i int) float64 {
			if i == 5 {
				return 0.3
			}
			return 0.1
		}, 1000, 50)
	}
	r := w.run()
	got := findings(r, "GPU005")
	if len(got) != 1 {
		t.Fatalf("excess replicas = %+v", got)
	}
	f := got[0]
	if f.Target != "ml/Deployment/llm" || !near(f.WasteGPUHours, 22) || len(f.Pods) != 4 {
		t.Errorf("finding = %+v", f)
	}
	if !strings.Contains(f.Recommendation, "Cap the workload at 2 replicas") {
		t.Errorf("recommendation = %s", f.Recommendation)
	}
}

func TestQueueingWorkloadIsNotOverprovisioned(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	for _, name := range []string{"q-a", "q-b"} {
		w.pod("ml", name, "n1", "q", 1, always)
		w.vllm("ml", name, "m", constant(1), constant(0.05), 10, 5)
	}
	// One replica queued requests at some point.
	for i := range w.s.Range["vllm_waiting"] {
		w.s.Range["vllm_waiting"][i].Points[3].V = 4
	}
	if got := findings(w.run(), "GPU005"); len(got) != 0 {
		t.Errorf("flagged a workload that queued requests: %+v", got)
	}
}

func TestExcessReplicasNeedKV(t *testing.T) {
	w := newWorld(t)
	w.node("n1", "", 8)
	for _, name := range []string{"x-a", "x-b", "x-c"} {
		w.pod("ml", name, "n1", "x", 1, always)
		w.vllm("ml", name, "m", constant(1), nil, 10, 5)
	}
	if got := findings(w.run(), "GPU005"); len(got) != 0 {
		t.Errorf("judged replicas without KV cache data: %+v", got)
	}
}
