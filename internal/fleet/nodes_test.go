package fleet

import (
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestDescribeNodes(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["pod_gpu_requests"] = []prom.Series{
		{Labels: prom.Labels{"namespace": "ml", "pod": "a", "node": "dcgm-node", "resource": "nvidia_com_gpu"}, Points: points(ts, 1)},
		{Labels: prom.Labels{"namespace": "ml", "pod": "b", "node": "gke-node", "resource": "nvidia_com_gpu"}, Points: points(ts, 1)},
		{Labels: prom.Labels{"namespace": "ml", "pod": "c", "node": "bare-node", "resource": "nvidia_com_mig_3g_40gb"}, Points: points(ts, 2, 2, 2, 2)},
	}
	s.Range["gpu_activity"] = []prom.Series{
		{Labels: prom.Labels{"UUID": "GPU-1", "Hostname": "dcgm-node", "modelName": "NVIDIA L4"}, Points: points(ts, 0.1)},
	}
	s.Instant["node_labels"] = []prom.Sample{
		{Labels: prom.Labels{"node": "dcgm-node", "label_nvidia_com_gpu_product": "Tesla-T4", "label_node_kubernetes_io_instance_type": "g2-standard-8"}},
		{Labels: prom.Labels{"node": "gke-node", "label_cloud_google_com_gke_accelerator": "nvidia-a100-80gb"}},
	}
	s.Instant["opencost_gpu_hourly"] = []prom.Sample{{Labels: prom.Labels{"node": "gke-node", "instance_type": "a2-ultragpu-1g"}, Value: 5}}
	f := Build(s)
	if n := f.Nodes["dcgm-node"]; n.Product != "l4" || n.InstanceType != "g2-standard-8" {
		t.Errorf("dcgm-node = %+v (the GPU's own name wins over the label)", n)
	}
	if n := f.Nodes["gke-node"]; n.Product != "a100-80gb" || n.InstanceType != "a2-ultragpu-1g" {
		t.Errorf("gke-node = %+v", n)
	}
	if n := f.Nodes["bare-node"]; n.Product != gpu.Unknown {
		t.Errorf("bare-node = %+v", n)
	}
	c := f.Pods[PodKey{"ml", "c"}]
	// Two 3g slices for an hour (four 15-minute steps) on an unknown product: 2 × 1 h × 3/7, share assumed.
	if eq, exact := f.GPUEquivalents(c); !close(eq, 2*1.0*3/7) || exact {
		t.Errorf("GPU equivalents = %v, exact %v", eq, exact)
	}
	f.Nodes["bare-node"].Product = "a30-24gb"
	if eq, exact := f.GPUEquivalents(c); !close(eq, 2*1.0*3/4) || !exact {
		t.Errorf("on an A30: %v, %v", eq, exact)
	}
}

func TestOfferedGPUHours(t *testing.T) {
	f := &Fleet{}
	n := &Node{Product: "a100-80gb", OfferedHours: map[string]float64{"nvidia_com_gpu": 10, "nvidia_com_mig_1g_10gb": 7}}
	if got := f.OfferedGPUHours(n); !close(got, 11) {
		t.Errorf("offered = %v, want 11", got)
	}
}
