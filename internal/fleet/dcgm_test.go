package fleet

import (
	"strings"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestAttachDCGM(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["pod_gpu_requests"] = []prom.Series{
		{Labels: prom.Labels{"namespace": "ml", "pod": "llama-0", "node": "n1", "resource": "nvidia_com_gpu"}, Points: points(ts, 2, 2, 2, 2, nan)},
	}
	gpuLabels := func(uuid, idx string, extra prom.Labels) prom.Labels {
		l := prom.Labels{"UUID": uuid, "gpu": idx, "Hostname": "n1", "modelName": "NVIDIA H100 80GB HBM3",
			// The scrape's own target labels name the exporter pod; the workload pod is exported_*.
			"namespace": "gpu-operator", "pod": "nvidia-dcgm-exporter-x1"}
		for k, v := range extra {
			l[k] = v
		}
		return l
	}
	mappedTo := prom.Labels{"exported_namespace": "ml", "exported_pod": "llama-0"}
	s.Range["gpu_activity"] = []prom.Series{
		{Labels: gpuLabels("GPU-a", "0", mappedTo), Points: points(ts, 0.2, 0.4, 0.6, 0.8, 0.9)},
		{Labels: gpuLabels("GPU-b", "1", mappedTo), Points: points(ts, 0.0, 0.0, 0.2, nan, 0.9)},
		{Labels: gpuLabels("GPU-c", "2", nil), Points: points(ts, 0, 0, 0, 0, 0)},
	}
	s.Instant["gpu_memory_total_bytes"] = []prom.Sample{{Labels: prom.Labels{"UUID": "GPU-a"}, Value: 80e9}}
	s.Range["gpu_memory_used_bytes"] = []prom.Series{{Labels: prom.Labels{"UUID": "GPU-a"}, Points: points(ts, 40e9, 72e9)}}
	s.Instant["gpu_power_watts"] = []prom.Sample{{Labels: prom.Labels{"UUID": "GPU-a"}, Value: 300}, {Labels: prom.Labels{"UUID": "GPU-b"}, Value: 120}}

	f := Build(s)
	u := f.Pods[PodKey{"ml", "llama-0"}].Usage
	// Held at the first four steps; the last point (0.9) is after the pod stopped and does not count.
	// Step averages: 0.1, 0.2, 0.4, 0.8 (GPU-b has no value at step 4).
	if u.Activity.N != 4 || !close(u.Activity.Avg, 0.375) || u.Activity.Max != 0.8 {
		t.Errorf("activity = %+v", u.Activity)
	}
	if u.Coverage != 1 || u.GPUs != 2 || u.PowerWatts != 420 || !close(u.MemoryPeak, 0.9) {
		t.Errorf("usage = %+v", u)
	}
	if len(f.GPUs) != 3 {
		t.Fatalf("gpus = %d", len(f.GPUs))
	}
	var spare *GPU
	for _, g := range f.GPUs {
		if g.UUID == "GPU-c" {
			spare = g
		}
	}
	if spare == nil || spare.Pod != nil || spare.Node != "n1" || spare.Product != "h100-80gb" {
		t.Errorf("unassigned GPU = %+v", spare)
	}
}

func TestDCGMWithoutPodMappingSaysSo(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["pod_gpu_requests"] = []prom.Series{
		{Labels: prom.Labels{"namespace": "ml", "pod": "a", "resource": "nvidia_com_gpu"}, Points: points(ts, 1, 1)},
	}
	s.Range["gpu_activity"] = []prom.Series{
		{Labels: prom.Labels{"UUID": "GPU-a", "namespace": "gpu-operator", "pod": "nvidia-dcgm-exporter-x1"}, Points: points(ts, 0.5, 0.5)},
	}
	f := Build(s)
	if f.Pods[PodKey{"ml", "a"}].Usage.Activity.Measured() {
		t.Error("the exporter's own pod was taken for the workload")
	}
	if !strings.Contains(strings.Join(f.Notes, " "), "pod mapping") {
		t.Errorf("notes = %v", f.Notes)
	}
}

func TestOldPodLabelNames(t *testing.T) {
	if k, ok := podOf(prom.Labels{"pod_namespace": "ml", "pod_name": "x"}); !ok || k != (PodKey{"ml", "x"}) {
		t.Errorf("podOf = %v, %v", k, ok)
	}
}

func close(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
