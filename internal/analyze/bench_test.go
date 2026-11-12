package analyze

import (
	"fmt"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/price"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// largeFleet is 200 nodes of 8 GPUs, every GPU held by a one-GPU vLLM pod in Deployments of 8 replicas, over a
// day at a 5-minute step: 1,600 pods and 289 points per series, as a big inference cluster's scan would collect.
func largeFleet(b *testing.B) *snapshot.Snapshot {
	b.Helper()
	w, err := snapshot.PlanWindow(24*time.Hour, 5*time.Minute, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		b.Fatal(err)
	}
	s := snapshot.New("bench", "synthetic", w)
	n := w.Points()
	pts := func(f func(i int) float64) []prom.Point {
		out := make([]prom.Point, n)
		for i := range out {
			out[i] = prom.Point{T: w.Start.Add(time.Duration(i) * 5 * time.Minute).UnixMilli(), V: f(i)}
		}
		return out
	}
	for node := 0; node < 200; node++ {
		nn := fmt.Sprintf("node-%03d", node)
		s.Range["node_gpu_allocatable"] = append(s.Range["node_gpu_allocatable"], prom.Series{Labels: prom.Labels{"node": nn, "resource": "nvidia_com_gpu"}, Points: pts(func(int) float64 { return 8 })})
		for g := 0; g < 8; g++ {
			dep := fmt.Sprintf("model-%03d", node)
			pod := fmt.Sprintf("%s-%d", dep, g)
			s.Range["pod_gpu_requests"] = append(s.Range["pod_gpu_requests"], prom.Series{Labels: prom.Labels{"namespace": "ml", "pod": pod, "node": nn, "resource": "nvidia_com_gpu"}, Points: pts(func(int) float64 { return 1 })})
			s.Instant["pod_owner"] = append(s.Instant["pod_owner"], prom.Sample{Labels: prom.Labels{"namespace": "ml", "pod": pod, "owner_kind": "ReplicaSet", "owner_name": dep + "-rs"}})
			s.Range["gpu_activity"] = append(s.Range["gpu_activity"], prom.Series{Labels: prom.Labels{"exported_namespace": "ml", "exported_pod": pod, "UUID": pod, "Hostname": nn, "modelName": "NVIDIA H100 80GB HBM3"}, Points: pts(func(i int) float64 { return float64(i%10) / 10 })})
			el := prom.Labels{"namespace": "ml", "pod": pod, "model_name": dep, "engine": "0"}
			s.Range["vllm_running"] = append(s.Range["vllm_running"], prom.Series{Labels: el, Points: pts(func(i int) float64 { return float64(i % 4) })})
			s.Range["vllm_waiting"] = append(s.Range["vllm_waiting"], prom.Series{Labels: el, Points: pts(func(int) float64 { return 0 })})
			s.Range["vllm_kv_usage"] = append(s.Range["vllm_kv_usage"], prom.Series{Labels: el, Points: pts(func(i int) float64 { return 0.05 })})
			s.Instant["vllm_generation_tokens"] = append(s.Instant["vllm_generation_tokens"], prom.Sample{Labels: el, Value: 1e6})
		}
		s.Instant["replicaset_owner"] = append(s.Instant["replicaset_owner"], prom.Sample{Labels: prom.Labels{"namespace": "ml", "replicaset": fmt.Sprintf("model-%03d-rs", node), "owner_kind": "Deployment", "owner_name": fmt.Sprintf("model-%03d", node)}})
	}
	return s
}

func BenchmarkScanLargeFleet(b *testing.B) {
	s := largeFleet(b)
	tb := price.NewTable()
	tb.Set("*", 3, "bench")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := fleet.Build(s)
		r := Run(f, s, price.Resolve(tb, f, s), Options{Now: time.Now()})
		if len(r.Workloads) != 200 {
			b.Fatalf("%d workloads", len(r.Workloads))
		}
	}
}
