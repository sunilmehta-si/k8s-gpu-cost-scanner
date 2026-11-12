package fleet

import (
	"sort"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// GPU is one physical GPU, or one MIG instance, the DCGM exporter reported.
type GPU struct {
	Node    string      `json:"node"`
	UUID    string      `json:"uuid"`
	Index   string      `json:"index"`
	Product gpu.Product `json:"product"`
	// MIGInstance is the MIG GPU instance ID when this is a slice.
	MIGInstance string `json:"mig_instance,omitempty"`
	// Pod is the pod the exporter mapped the GPU to, when it did and the pod requested GPUs.
	Pod      *PodKey `json:"pod,omitempty"`
	Activity Stats   `json:"activity"`
	// MemoryPeak is the most memory in use, as a share of capacity, when both were reported.
	MemoryPeak float64 `json:"memory_peak,omitempty"`
	PowerWatts float64 `json:"power_watts,omitempty"`
}

// Usage is how a pod used its GPUs, from the DCGM exporter.
type Usage struct {
	// Activity is the GPUs' activity averaged across the pod's GPUs at each step it held them.
	Activity Stats `json:"activity"`
	// Coverage is the share of the steps the pod held GPUs that had an activity value.
	Coverage float64 `json:"coverage"`
	// MemoryPeak is the highest memory use of any of the pod's GPUs, as a share of capacity.
	MemoryPeak float64 `json:"memory_peak,omitempty"`
	// PowerWatts is the pod's GPUs' summed average power draw.
	PowerWatts float64 `json:"power_watts,omitempty"`
	// GPUs is how many GPU series the exporter mapped to the pod.
	GPUs int `json:"gpus"`
	// Steps is the pod's activity at each step it held GPUs, keyed by Unix milliseconds.
	Steps map[int64]float64 `json:"-"`
}

// podOf returns the pod a DCGM sample is attributed to. When Prometheus scrapes the exporter without
// honor_labels, the exporter's own pod labels are renamed exported_pod and exported_namespace and plain pod and
// namespace name the exporter itself; 4.x before the rename used pod_name and pod_namespace. The caller still
// checks the pod requested GPUs, so the exporter's own pod is never mistaken for a workload.
func podOf(l prom.Labels) (PodKey, bool) {
	for _, pair := range [][2]string{{"exported_namespace", "exported_pod"}, {"namespace", "pod"}, {"pod_namespace", "pod_name"}} {
		if ns, pod := l[pair[0]], l[pair[1]]; ns != "" && pod != "" {
			return PodKey{ns, pod}, true
		}
	}
	return PodKey{}, false
}

// nodeOf returns the node a DCGM sample came from.
func nodeOf(l prom.Labels) string {
	for _, k := range []string{"Hostname", "hostname", "node", "kubernetes_node"} {
		if v := l[k]; v != "" {
			return v
		}
	}
	return ""
}

func uuidOf(l prom.Labels) string {
	for _, k := range []string{"UUID", "uuid", "gpu_uuid"} {
		if v := l[k]; v != "" {
			return v
		}
	}
	return ""
}

func gpuID(l prom.Labels) string {
	id := uuidOf(l)
	if id == "" {
		id = nodeOf(l) + "/" + l["gpu"]
	}
	if mig := l["GPU_I_ID"]; mig != "" {
		id += "/mig-" + mig
	}
	return id
}

// attachDCGM joins the DCGM exporter's per-GPU series to the pods holding the GPUs.
func (f *Fleet) attachDCGM(s *snapshot.Snapshot) {
	activity := s.Range["gpu_activity"]
	if len(activity) == 0 {
		if len(f.Pods) > 0 {
			f.note("no GPU activity from the DCGM exporter: idle GPUs cannot be found without it")
		}
		return
	}

	memTotal := map[string]float64{}
	for _, smp := range s.Instant["gpu_memory_total_bytes"] {
		memTotal[gpuID(smp.Labels)] = smp.Value
	}
	power := map[string]float64{}
	for _, smp := range s.Instant["gpu_power_watts"] {
		power[gpuID(smp.Labels)] = smp.Value
	}
	memPeak := map[string]float64{}
	for _, series := range s.Range["gpu_memory_used_bytes"] {
		id := gpuID(series.Labels)
		total := memTotal[id]
		if total <= 0 {
			continue
		}
		for _, pt := range series.Points {
			if r := pt.V / total; r > memPeak[id] {
				memPeak[id] = r
			}
		}
	}

	// Per pod, the activity of each of its GPUs at each step.
	perPod := map[PodKey]map[int64][]float64{}
	gpus := map[string]*GPU{}
	mapped := 0
	for _, series := range activity {
		id := gpuID(series.Labels)
		g := gpus[id]
		if g == nil {
			g = &GPU{
				Node:        nodeOf(series.Labels),
				UUID:        uuidOf(series.Labels),
				Index:       series.Labels["gpu"],
				Product:     gpu.Normalize(series.Labels["modelName"]),
				MIGInstance: series.Labels["GPU_I_ID"],
				MemoryPeak:  memPeak[id],
				PowerWatts:  power[id],
			}
			gpus[id] = g
		}
		key, ok := podOf(series.Labels)
		pod := f.Pods[key]
		if !ok || pod == nil {
			continue
		}
		mapped++
		k := key
		g.Pod = &k
		if g.Node == "" {
			g.Node = pod.Node
		}
		steps := perPod[key]
		if steps == nil {
			steps = make(map[int64][]float64, len(pod.held))
			perPod[key] = steps
		}
		for _, pt := range series.Points {
			if pod.Held(pt.T) {
				steps[pt.T] = append(steps[pt.T], pt.V)
			}
		}
	}

	// A GPU's own stats cover all of its series, whoever held it.
	byGPU := map[string][]float64{}
	for _, series := range activity {
		id := gpuID(series.Labels)
		if byGPU[id] == nil {
			byGPU[id] = make([]float64, 0, len(series.Points))
		}
		for _, pt := range series.Points {
			byGPU[id] = append(byGPU[id], pt.V)
		}
	}
	gpusOf := map[PodKey][]*GPU{}
	for id, g := range gpus {
		g.Activity = Summarize(byGPU[id])
		f.GPUs = append(f.GPUs, g)
		if g.Pod != nil {
			gpusOf[*g.Pod] = append(gpusOf[*g.Pod], g)
		}
	}
	sort.Slice(f.GPUs, func(i, j int) bool {
		if f.GPUs[i].Node != f.GPUs[j].Node {
			return f.GPUs[i].Node < f.GPUs[j].Node
		}
		return f.GPUs[i].UUID+f.GPUs[i].Index+f.GPUs[i].MIGInstance < f.GPUs[j].UUID+f.GPUs[j].Index+f.GPUs[j].MIGInstance
	})

	for key, pod := range f.Pods {
		steps := perPod[key]
		var avgs []float64
		perStep := make(map[int64]float64, len(steps))
		for t, vs := range steps {
			sum := 0.0
			for _, v := range vs {
				sum += v
			}
			perStep[t] = sum / float64(len(vs))
			avgs = append(avgs, perStep[t])
		}
		u := Usage{Activity: Summarize(avgs), Steps: perStep}
		if held := len(pod.held); held > 0 {
			u.Coverage = float64(len(steps)) / float64(held)
		}
		for _, g := range gpusOf[key] {
			u.GPUs++
			u.PowerWatts += g.PowerWatts
			if g.MemoryPeak > u.MemoryPeak {
				u.MemoryPeak = g.MemoryPeak
			}
		}
		pod.Usage = u
	}
	if mapped == 0 && len(f.Pods) > 0 {
		f.note("the DCGM exporter's series carry no pod that requested GPUs: turn on its Kubernetes pod mapping " +
			"(the --kubernetes flag, or kubernetes labels in the 4.9 Helm chart) so each GPU's activity can be tied to a pod")
	}
}
