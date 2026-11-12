package analyze

import (
	"math"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/price"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// world builds snapshots for rule tests: a 10-hour window sampled hourly (11 points), nodes of 8 GPUs.
type world struct {
	t  *testing.T
	s  *snapshot.Snapshot
	ts []int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w, err := snapshot.PlanWindow(10*time.Hour, time.Hour, time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var ts []int64
	for i := 0; i <= 10; i++ {
		ts = append(ts, w.Start.Add(time.Duration(i)*time.Hour).UnixMilli())
	}
	return &world{t: t, s: snapshot.New("gpucost test", "synthetic", w), ts: ts}
}

func (w *world) series(vals func(i int) float64) []prom.Point {
	var out []prom.Point
	for i, t := range w.ts {
		if v := vals(i); !math.IsNaN(v) {
			out = append(out, prom.Point{T: t, V: v})
		}
	}
	return out
}

func constant(v float64) func(int) float64 { return func(int) float64 { return v } }

// node adds a node offering n GPUs for the whole window.
func (w *world) node(name, model string, n float64) {
	w.s.Range["node_gpu_allocatable"] = append(w.s.Range["node_gpu_allocatable"], prom.Series{
		Labels: prom.Labels{"node": name, "resource": "nvidia_com_gpu"}, Points: w.series(constant(n)),
	})
	if model != "" {
		w.s.Instant["node_labels"] = append(w.s.Instant["node_labels"], prom.Sample{
			Labels: prom.Labels{"node": name, "label_nvidia_com_gpu_product": model}, Value: 1,
		})
	}
}

// pod adds a running pod holding gpus GPUs at every step where held(i) is true, owned by a Deployment.
func (w *world) pod(ns, name, node, deployment string, gpus float64, held func(int) bool) {
	w.s.Range["pod_gpu_requests"] = append(w.s.Range["pod_gpu_requests"], prom.Series{
		Labels: prom.Labels{"namespace": ns, "pod": name, "node": node, "resource": "nvidia_com_gpu"},
		Points: w.series(func(i int) float64 {
			if held(i) {
				return gpus
			}
			return math.NaN()
		}),
	})
	if deployment != "" {
		rs := deployment + "-5d8f7"
		w.s.Instant["pod_owner"] = append(w.s.Instant["pod_owner"], prom.Sample{Labels: prom.Labels{"namespace": ns, "pod": name, "owner_kind": "ReplicaSet", "owner_name": rs}})
		w.s.Instant["replicaset_owner"] = append(w.s.Instant["replicaset_owner"], prom.Sample{Labels: prom.Labels{"namespace": ns, "replicaset": rs, "owner_kind": "Deployment", "owner_name": deployment}})
	}
}

// activity adds a DCGM activity series for one of a pod's GPUs.
func (w *world) activity(ns, pod, uuid string, vals func(int) float64) {
	w.s.Range["gpu_activity"] = append(w.s.Range["gpu_activity"], prom.Series{
		Labels: prom.Labels{"exported_namespace": ns, "exported_pod": pod, "UUID": uuid, "modelName": "NVIDIA H100 80GB HBM3"},
		Points: w.series(vals),
	})
}

// vllm adds vLLM series for a pod.
func (w *world) vllm(ns, pod, model string, running, kv func(int) float64, generated, requests float64) {
	l := prom.Labels{"namespace": ns, "pod": pod, "model_name": model, "engine": "0"}
	w.s.Range["vllm_running"] = append(w.s.Range["vllm_running"], prom.Series{Labels: l, Points: w.series(running)})
	w.s.Range["vllm_waiting"] = append(w.s.Range["vllm_waiting"], prom.Series{Labels: l, Points: w.series(constant(0))})
	if kv != nil {
		w.s.Range["vllm_kv_usage"] = append(w.s.Range["vllm_kv_usage"], prom.Series{Labels: l, Points: w.series(kv)})
	}
	w.s.Instant["vllm_generation_tokens"] = append(w.s.Instant["vllm_generation_tokens"], prom.Sample{Labels: l, Value: generated})
	w.s.Instant["vllm_requests"] = append(w.s.Instant["vllm_requests"], prom.Sample{Labels: l, Value: requests})
}

func always(int) bool { return true }

// run analyses the world with a flat price of 2 per GPU-hour and the default thresholds.
func (w *world) run() *Report { return w.runWith(nil) }

// runWith analyses the world after letting tune adjust the default thresholds.
func (w *world) runWith(tune func(*Thresholds)) *Report {
	f := fleet.Build(w.s)
	tb := price.NewTable()
	tb.Set("*", 2, "test")
	th := Defaults()
	if tune != nil {
		tune(&th)
	}
	return Run(f, w.s, price.Resolve(tb, f, w.s), Options{Thresholds: th, Now: time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)})
}

func findings(r *Report, rule string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
