// Package fleet turns a snapshot's raw series into the GPU fleet it describes: which pods held which GPUs for how
// long, on which nodes, for which workloads, and how busy the GPUs and serving engines were.
package fleet

import (
	"fmt"
	"sort"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// PodKey names a pod.
type PodKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"pod"`
}

func (k PodKey) String() string { return k.Namespace + "/" + k.Name }

// Workload is the controller a pod belongs to, such as Deployment/vllm-llama.
type Workload struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

func (w Workload) String() string { return w.Namespace + "/" + w.Kind + "/" + w.Name }

// Allocation is one GPU resource a pod requested: whole GPUs, or slices of one MIG profile.
type Allocation struct {
	Resource string   `json:"resource"`
	MIG      *gpu.MIG `json:"mig,omitempty"`
	// Count is the most GPUs or slices the pod requested at once.
	Count float64 `json:"count"`
	// Hours is the GPU-hours or slice-hours: the count summed over every step the pod was running.
	Hours float64 `json:"hours"`
}

// Pod is a running pod that requested GPUs at some time in the window.
type Pod struct {
	Key         PodKey       `json:"key"`
	Node        string       `json:"node"`
	Workload    Workload     `json:"workload"`
	Allocations []Allocation `json:"allocations"`
	// RunningHours is how long the pod ran with its GPUs during the window.
	RunningHours float64   `json:"running_hours"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	// held marks the steps (by Unix milliseconds) the pod held GPUs, to line other series up against.
	held map[int64]float64
}

// GPUs is the most whole GPUs the pod requested; MIG slices are not counted here.
func (p *Pod) GPUs() float64 {
	n := 0.0
	for _, a := range p.Allocations {
		if a.MIG == nil {
			n += a.Count
		}
	}
	return n
}

// Held reports whether the pod held GPUs at time t (Unix milliseconds), and how many allocations' worth.
func (p *Pod) Held(t int64) bool { return p.held[t] > 0 }

// Node is a node that offered GPUs to pods at some time in the window.
type Node struct {
	Name string `json:"name"`
	// Offered is, per resource, the most GPUs or slices the node made allocatable.
	Offered map[string]float64 `json:"offered"`
	// OfferedHours is, per resource, the GPU-hours or slice-hours the node made allocatable.
	OfferedHours map[string]float64 `json:"offered_hours"`
}

// Fleet is everything the snapshot says about the cluster's GPUs.
type Fleet struct {
	Window snapshot.Window
	Pods   map[PodKey]*Pod
	Nodes  map[string]*Node
	// Notes say what the fleet could not be built from, for the report's blind spots.
	Notes []string
}

// Build reads the fleet out of a snapshot.
func Build(s *snapshot.Snapshot) *Fleet {
	f := &Fleet{Window: s.Window, Pods: map[PodKey]*Pod{}, Nodes: map[string]*Node{}}
	stepHours := s.Window.StepHours()

	for _, series := range s.Range["pod_gpu_requests"] {
		key := PodKey{series.Labels["namespace"], series.Labels["pod"]}
		if key.Namespace == "" || key.Name == "" {
			continue
		}
		p := f.Pods[key]
		if p == nil {
			p = &Pod{Key: key, held: map[int64]float64{}}
			f.Pods[key] = p
		}
		if n := series.Labels["node"]; n != "" {
			p.Node = n
		}
		res := series.Labels["resource"]
		a := Allocation{Resource: res}
		if m, ok := gpu.ParseMIG(res); ok {
			a.MIG = &m
		}
		for _, pt := range series.Points {
			if pt.V <= 0 || isNaN(pt.V) {
				continue
			}
			a.Hours += pt.V * stepHours
			if pt.V > a.Count {
				a.Count = pt.V
			}
			if _, seen := p.held[pt.T]; !seen {
				p.RunningHours += stepHours
			}
			p.held[pt.T] += pt.V
			ts := time.UnixMilli(pt.T).UTC()
			if p.FirstSeen.IsZero() || ts.Before(p.FirstSeen) {
				p.FirstSeen = ts
			}
			if ts.After(p.LastSeen) {
				p.LastSeen = ts
			}
		}
		if a.Hours > 0 {
			p.Allocations = append(p.Allocations, a)
		}
	}
	for key, p := range f.Pods {
		if len(p.Allocations) == 0 {
			delete(f.Pods, key)
			continue
		}
		sort.Slice(p.Allocations, func(i, j int) bool { return p.Allocations[i].Resource < p.Allocations[j].Resource })
	}
	if len(s.Range["pod_gpu_requests"]) == 0 {
		f.note("no GPU requests from kube-state-metrics: without them gpucost cannot tell which pods held GPUs")
	}

	for _, series := range s.Range["node_gpu_allocatable"] {
		name := series.Labels["node"]
		if name == "" {
			continue
		}
		n := f.Nodes[name]
		if n == nil {
			n = &Node{Name: name, Offered: map[string]float64{}, OfferedHours: map[string]float64{}}
			f.Nodes[name] = n
		}
		res := series.Labels["resource"]
		for _, pt := range series.Points {
			if pt.V <= 0 || isNaN(pt.V) {
				continue
			}
			n.OfferedHours[res] += pt.V * stepHours
			if pt.V > n.Offered[res] {
				n.Offered[res] = pt.V
			}
		}
	}

	f.resolveWorkloads(s)
	return f
}

// resolveWorkloads names each pod's controller, following a ReplicaSet up to its Deployment.
func (f *Fleet) resolveWorkloads(s *snapshot.Snapshot) {
	rsOwner := map[PodKey]prom.Labels{}
	for _, smp := range s.Instant["replicaset_owner"] {
		rsOwner[PodKey{smp.Labels["namespace"], smp.Labels["replicaset"]}] = smp.Labels
	}
	podOwner := map[PodKey]prom.Labels{}
	for _, smp := range s.Instant["pod_owner"] {
		podOwner[PodKey{smp.Labels["namespace"], smp.Labels["pod"]}] = smp.Labels
	}
	for key, p := range f.Pods {
		p.Workload = Workload{Namespace: key.Namespace, Kind: "Pod", Name: key.Name}
		o, ok := podOwner[key]
		if !ok || o["owner_kind"] == "" || o["owner_kind"] == "<none>" {
			continue
		}
		p.Workload.Kind, p.Workload.Name = o["owner_kind"], o["owner_name"]
		if o["owner_kind"] == "ReplicaSet" {
			if up, ok := rsOwner[PodKey{key.Namespace, o["owner_name"]}]; ok && up["owner_kind"] != "" && up["owner_kind"] != "<none>" {
				p.Workload.Kind, p.Workload.Name = up["owner_kind"], up["owner_name"]
			}
		}
	}
}

// SortedPods returns the pods in a stable order.
func (f *Fleet) SortedPods() []*Pod {
	out := make([]*Pod, 0, len(f.Pods))
	for _, p := range f.Pods {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.String() < out[j].Key.String() })
	return out
}

// SortedNodes returns the nodes in name order.
func (f *Fleet) SortedNodes() []*Node {
	out := make([]*Node, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (f *Fleet) note(format string, args ...any) {
	f.Notes = append(f.Notes, fmt.Sprintf(format, args...))
}

func isNaN(v float64) bool { return v != v }
