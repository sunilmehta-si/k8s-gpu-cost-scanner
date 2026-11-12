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

func TestTimeSlicedSlotsCountAsShares(t *testing.T) {
	s, ts := newSnap(t)
	// Two physical GPUs time-sliced into 4 replicas each: 8 allocatable slots.
	s.Range["node_gpu_allocatable"] = []prom.Series{{Labels: prom.Labels{"node": "shared", "resource": "nvidia_com_gpu"}, Points: points(ts, 8, 8, 8, 8, 8)}}
	s.Range["pod_gpu_requests"] = []prom.Series{{Labels: prom.Labels{"namespace": "ml", "pod": "a", "node": "shared", "resource": "nvidia_com_gpu"}, Points: points(ts, 1, 1, 1, 1, 1)}}
	s.Instant["node_labels"] = []prom.Sample{{Labels: prom.Labels{"node": "shared", "label_nvidia_com_gpu_sharing_strategy": "time-slicing", "label_nvidia_com_gpu_count": "2", "label_nvidia_com_gpu_replicas": "4"}}}
	f := Build(s)
	if n := f.Nodes["shared"]; n.SlotShare != 0.25 || !close(f.OfferedGPUHours(n), 2*1.25) {
		t.Errorf("node = %+v, offered %v", n, f.OfferedGPUHours(n))
	}
	// One slot for 1.25 hours is a quarter of a GPU for 1.25 hours.
	if eq, _ := f.GPUEquivalents(f.Pods[PodKey{"ml", "a"}]); !close(eq, 0.3125) {
		t.Errorf("GPU equivalents = %v", eq)
	}
	// Without the count, the replicas label alone gives the share; without a strategy, nothing is shared.
	if got := slotShare(map[string]string{"label_nvidia_com_gpu_sharing_strategy": "mps", "label_nvidia_com_gpu_replicas": "10"}, 0); got != 0.1 {
		t.Errorf("mps with 10 replicas = %v", got)
	}
	if got := slotShare(map[string]string{"label_nvidia_com_gpu_replicas": "4"}, 8); got != 1 {
		t.Errorf("no strategy = %v", got)
	}
}

func TestOfferedGPUHours(t *testing.T) {
	f := &Fleet{}
	n := &Node{Product: "a100-80gb", OfferedHours: map[string]float64{"nvidia_com_gpu": 10, "nvidia_com_mig_1g_10gb": 7}}
	if got := f.OfferedGPUHours(n); !close(got, 11) {
		t.Errorf("offered = %v, want 11", got)
	}
}
