package fleet

import (
	"math"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// newSnap returns a snapshot of a one-hour window sampled every 15 minutes: points at 0, 15, 30, 45 and 60.
func newSnap(t *testing.T) (*snapshot.Snapshot, []int64) {
	t.Helper()
	w, err := snapshot.PlanWindow(time.Hour, 15*time.Minute, time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var ts []int64
	for i := 0; i < 5; i++ {
		ts = append(ts, w.Start.Add(time.Duration(i)*15*time.Minute).UnixMilli())
	}
	return snapshot.New("test", "synthetic", w), ts
}

func points(ts []int64, vs ...float64) []prom.Point {
	out := make([]prom.Point, 0, len(vs))
	for i, v := range vs {
		if !math.IsNaN(v) {
			out = append(out, prom.Point{T: ts[i], V: v})
		}
	}
	return out
}

var nan = math.NaN()

func TestBuildPodsAndNodes(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["pod_gpu_requests"] = []prom.Series{
		{Labels: prom.Labels{"namespace": "ml", "pod": "llama-0", "node": "n1", "resource": "nvidia_com_gpu"}, Points: points(ts, 2, 2, 2, 2, 2)},
		{Labels: prom.Labels{"namespace": "ml", "pod": "batch-1", "node": "n1", "resource": "nvidia_com_gpu"}, Points: points(ts, nan, nan, 1, 1, nan)},
		{Labels: prom.Labels{"namespace": "ml", "pod": "slice", "node": "n2", "resource": "nvidia_com_mig_1g_10gb"}, Points: points(ts, 1, 1, nan, nan, nan)},
		{Labels: prom.Labels{"namespace": "", "pod": "broken"}, Points: points(ts, 1)},
	}
	s.Range["node_gpu_allocatable"] = []prom.Series{
		{Labels: prom.Labels{"node": "n1", "resource": "nvidia_com_gpu"}, Points: points(ts, 8, 8, 8, 8, 8)},
	}
	f := Build(s)
	if len(f.Pods) != 3 {
		t.Fatalf("pods = %v", f.Pods)
	}
	llama := f.Pods[PodKey{"ml", "llama-0"}]
	// Five points of 15 minutes each, two GPUs: 2 × 5 × 0.25 = 2.5 GPU-hours.
	if got := llama.Allocations[0].Hours; got != 2.5 {
		t.Errorf("llama GPU-hours = %v, want 2.5", got)
	}
	if llama.GPUs() != 2 || llama.RunningHours != 1.25 || llama.Node != "n1" {
		t.Errorf("llama = %+v", llama)
	}
	batch := f.Pods[PodKey{"ml", "batch-1"}]
	if batch.Allocations[0].Hours != 0.5 || !batch.Held(ts[2]) || batch.Held(ts[0]) {
		t.Errorf("batch = %+v", batch)
	}
	if !batch.FirstSeen.Equal(time.UnixMilli(ts[2]).UTC()) || !batch.LastSeen.Equal(time.UnixMilli(ts[3]).UTC()) {
		t.Errorf("batch seen %s to %s", batch.FirstSeen, batch.LastSeen)
	}
	slice := f.Pods[PodKey{"ml", "slice"}]
	if slice.GPUs() != 0 || slice.Allocations[0].MIG == nil || slice.Allocations[0].MIG.Profile != "1g.10gb" {
		t.Errorf("slice = %+v", slice.Allocations)
	}
	n1 := f.Nodes["n1"]
	if n1.Offered["nvidia_com_gpu"] != 8 || n1.OfferedHours["nvidia_com_gpu"] != 10 {
		t.Errorf("n1 = %+v", n1)
	}
}

func TestBuildWithoutRequestsSaysSo(t *testing.T) {
	s, _ := newSnap(t)
	if f := Build(s); len(f.Notes) == 0 {
		t.Error("no note about missing GPU requests")
	}
}

func TestWorkloadsFollowReplicaSets(t *testing.T) {
	s, ts := newSnap(t)
	for _, pod := range []string{"api-7d9f-abc", "worker-0", "lonely"} {
		s.Range["pod_gpu_requests"] = append(s.Range["pod_gpu_requests"], prom.Series{
			Labels: prom.Labels{"namespace": "ml", "pod": pod, "resource": "nvidia_com_gpu"}, Points: points(ts, 1),
		})
	}
	s.Instant["pod_owner"] = []prom.Sample{
		{Labels: prom.Labels{"namespace": "ml", "pod": "api-7d9f-abc", "owner_kind": "ReplicaSet", "owner_name": "api-7d9f"}},
		{Labels: prom.Labels{"namespace": "ml", "pod": "worker-0", "owner_kind": "StatefulSet", "owner_name": "worker"}},
		{Labels: prom.Labels{"namespace": "ml", "pod": "lonely", "owner_kind": "<none>", "owner_name": "<none>"}},
	}
	s.Instant["replicaset_owner"] = []prom.Sample{
		{Labels: prom.Labels{"namespace": "ml", "replicaset": "api-7d9f", "owner_kind": "Deployment", "owner_name": "api"}},
	}
	f := Build(s)
	want := map[string]string{"api-7d9f-abc": "ml/Deployment/api", "worker-0": "ml/StatefulSet/worker", "lonely": "ml/Pod/lonely"}
	for pod, w := range want {
		if got := f.Pods[PodKey{"ml", pod}].Workload.String(); got != w {
			t.Errorf("%s workload = %s, want %s", pod, got, w)
		}
	}
}
