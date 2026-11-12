package fleet

import (
	"strconv"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// describeNodes names each node's GPU product and instance type. The DCGM exporter's modelName is preferred, as
// it comes from the GPU itself; then the node labels GPU Feature Discovery, GKE and Karpenter set, when
// kube-state-metrics is allowed to export them; then OpenCost's instance type.
func (f *Fleet) describeNodes(s *snapshot.Snapshot) {
	node := func(name string) *Node {
		n := f.Nodes[name]
		if n == nil {
			n = &Node{Name: name, Offered: map[string]float64{}, OfferedHours: map[string]float64{}}
			f.Nodes[name] = n
		}
		return n
	}
	for _, p := range f.Pods {
		if p.Node != "" {
			node(p.Node)
		}
	}
	for _, g := range f.GPUs {
		if g.Node != "" && g.Product != gpu.Unknown && g.Product != "" {
			n := node(g.Node)
			if n.Product == "" {
				n.Product = g.Product
			}
		}
	}
	for _, smp := range s.Instant["node_labels"] {
		name := smp.Labels["node"]
		if name == "" {
			continue
		}
		n, ok := f.Nodes[name]
		if !ok {
			continue
		}
		if n.Product == "" {
			for _, l := range []string{"label_nvidia_com_gpu_product", "label_cloud_google_com_gke_accelerator", "label_karpenter_k8s_aws_instance_gpu_name"} {
				if v := smp.Labels[l]; v != "" {
					n.Product = gpu.Normalize(v)
					break
				}
			}
		}
		if n.InstanceType == "" {
			n.InstanceType = smp.Labels["label_node_kubernetes_io_instance_type"]
		}
		n.SlotShare = slotShare(smp.Labels, n.Offered["nvidia_com_gpu"])
	}
	for _, smp := range s.Instant["opencost_gpu_hourly"] {
		if n, ok := f.Nodes[smp.Labels["node"]]; ok && n.InstanceType == "" {
			n.InstanceType = smp.Labels["instance_type"]
		}
	}
	for _, n := range f.Nodes {
		if n.Product == "" {
			n.Product = gpu.Unknown
		}
		if n.SlotShare <= 0 {
			n.SlotShare = 1
		}
	}
}

// slotShare reads GPU Feature Discovery's labels: with time-slicing or MPS, each physical GPU is advertised as
// several nvidia.com/gpu replicas. The physical count over the allocatable slots is the share of a GPU one slot
// is; without the count, one over the replicas. Without a sharing strategy, a slot is a whole GPU.
func slotShare(l map[string]string, slots float64) float64 {
	switch l["label_nvidia_com_gpu_sharing_strategy"] {
	case "time-slicing", "mps":
	default:
		return 1
	}
	if count, err := strconv.ParseFloat(l["label_nvidia_com_gpu_count"], 64); err == nil && count > 0 && slots > count {
		return count / slots
	}
	if r, err := strconv.ParseFloat(l["label_nvidia_com_gpu_replicas"], 64); err == nil && r > 1 {
		return 1 / r
	}
	return 1
}

// Product is the GPU product of the node the pod ran on.
func (f *Fleet) Product(p *Pod) gpu.Product {
	if n, ok := f.Nodes[p.Node]; ok {
		return n.Product
	}
	return gpu.Unknown
}

// GPUEquivalents is the pod's GPU-hours with each MIG slice-hour counted as its share of a whole GPU, the unit
// prices and waste are counted in. The second result is false when a slice's share had to be assumed.
func (f *Fleet) GPUEquivalents(p *Pod) (float64, bool) {
	total, exact := 0.0, true
	for _, a := range p.Allocations {
		if a.MIG == nil {
			total += a.Hours * f.slotShare(p.Node)
			continue
		}
		share, ok := a.MIG.Share(f.Product(p))
		exact = exact && ok
		total += a.Hours * share
	}
	return total, exact
}

// OfferedGPUHours is the GPU-hours a node made allocatable, MIG slices counted as their share of a GPU.
func (f *Fleet) OfferedGPUHours(n *Node) float64 {
	total := 0.0
	for res, h := range n.OfferedHours {
		if m, ok := gpu.ParseMIG(res); ok {
			share, _ := m.Share(n.Product)
			total += h * share
			continue
		}
		total += h * f.slotShare(n.Name)
	}
	return total
}

func (f *Fleet) slotShare(node string) float64 {
	if n, ok := f.Nodes[node]; ok && n.SlotShare > 0 {
		return n.SlotShare
	}
	return 1
}
