package analyze

import (
	"sort"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
)

// ModelCost is what serving one model from one workload cost, and what it produced.
type ModelCost struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Model     string `json:"model"`
	Engine    string `json:"engine"`
	// Replicas is the average number of pods serving over the window; MaxReplicas the most at once.
	Replicas    float64 `json:"replicas"`
	MaxReplicas int     `json:"max_replicas"`
	GPUHours    float64 `json:"gpu_hours"`
	Cost        float64 `json:"cost"`
	Priced      bool    `json:"priced"`
	// Activity is the GPUs' average activity, weighted by measured GPU-hours; BusyShare the share of time a
	// request was running, weighted by GPU-hours.
	Activity         float64 `json:"activity"`
	BusyShare        float64 `json:"busy_share"`
	GenerationTokens float64 `json:"generation_tokens"`
	PromptTokens     float64 `json:"prompt_tokens"`
	Requests         float64 `json:"requests"`
	// CostPerMillionTokens is the cost of a million output tokens at the utilization seen; zero when the model
	// generated none or had no price. GPUHoursPerMillionTokens is the same in GPU-hours.
	CostPerMillionTokens     float64 `json:"cost_per_million_tokens"`
	GPUHoursPerMillionTokens float64 `json:"gpu_hours_per_million_tokens"`
	// WasteGPUHours are this model's GPU-hours the findings counted as waste.
	WasteGPUHours float64 `json:"waste_gpu_hours"`
}

// WorkloadCost is what one workload's GPUs cost, serving or not.
type WorkloadCost struct {
	Namespace     string  `json:"namespace"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	Pods          int     `json:"pods"`
	GPUHours      float64 `json:"gpu_hours"`
	Cost          float64 `json:"cost"`
	Priced        bool    `json:"priced"`
	Activity      float64 `json:"activity"`
	Measured      bool    `json:"measured"`
	WasteGPUHours float64 `json:"waste_gpu_hours"`
}

type acc struct {
	gpuHours, cost, measured, active, busyHours float64
	priced                                      bool
	pods                                        []*fleet.Pod
	waste                                       float64
}

func (a *analysis) add(x *acc, p *fleet.Pod) {
	eq, _ := a.f.GPUEquivalents(p)
	x.gpuHours += eq
	c, ok := a.cost(p.Node, eq)
	x.cost += c
	x.priced = x.priced && ok
	if p.Usage.Activity.Measured() {
		m := eq * p.Usage.Coverage
		x.measured += m
		x.active += m * p.Usage.Activity.Avg
	}
	if p.Engine != nil {
		x.busyHours += eq * p.Engine.BusyShare
	}
	x.waste += a.claimed[p.Key]
	x.pods = append(x.pods, p)
}

func (a *analysis) costs() ([]ModelCost, []WorkloadCost) {
	window := a.f.Window.Hours()
	models := map[[3]string]*acc{}
	workloads := map[fleet.Workload]*acc{}
	for _, p := range a.pods() {
		w := workloads[p.Workload]
		if w == nil {
			w = &acc{priced: true}
			workloads[p.Workload] = w
		}
		a.add(w, p)
		if p.Engine == nil {
			continue
		}
		key := [3]string{p.Workload.String(), p.Engine.Model, p.Engine.Kind}
		m := models[key]
		if m == nil {
			m = &acc{priced: true}
			models[key] = m
		}
		a.add(m, p)
	}

	var outM []ModelCost
	for key, m := range models {
		p0 := m.pods[0]
		mc := ModelCost{
			Namespace: p0.Workload.Namespace, Workload: p0.Workload.Kind + "/" + p0.Workload.Name,
			Model: key[1], Engine: key[2], GPUHours: m.gpuHours, Cost: m.cost, Priced: m.priced,
			WasteGPUHours: m.waste,
		}
		if mc.Model == "" {
			mc.Model = "(unnamed)"
		}
		steps := map[int64]int{}
		running := 0.0
		for _, p := range m.pods {
			running += p.RunningHours
			for _, t := range p.Engine.Timeline() {
				steps[t]++
			}
			mc.GenerationTokens += p.Engine.GenerationTokens
			mc.PromptTokens += p.Engine.PromptTokens
			mc.Requests += p.Engine.Requests
		}
		for _, n := range steps {
			if n > mc.MaxReplicas {
				mc.MaxReplicas = n
			}
		}
		if window > 0 {
			mc.Replicas = running / window
		}
		if m.measured > 0 {
			mc.Activity = m.active / m.measured
		}
		if m.gpuHours > 0 {
			mc.BusyShare = m.busyHours / m.gpuHours
		}
		if mc.GenerationTokens > 0 {
			mc.GPUHoursPerMillionTokens = m.gpuHours / mc.GenerationTokens * 1e6
			if m.priced {
				mc.CostPerMillionTokens = m.cost / mc.GenerationTokens * 1e6
			}
		}
		outM = append(outM, mc)
	}
	sort.Slice(outM, func(i, j int) bool {
		if outM[i].GPUHours != outM[j].GPUHours {
			return outM[i].GPUHours > outM[j].GPUHours
		}
		return outM[i].Workload+outM[i].Model < outM[j].Workload+outM[j].Model
	})

	var outW []WorkloadCost
	for wl, x := range workloads {
		wc := WorkloadCost{
			Namespace: wl.Namespace, Kind: wl.Kind, Name: wl.Name, Pods: len(x.pods), GPUHours: x.gpuHours,
			Cost: x.cost, Priced: x.priced, Measured: x.measured > 0, WasteGPUHours: x.waste,
		}
		if x.measured > 0 {
			wc.Activity = x.active / x.measured
		}
		outW = append(outW, wc)
	}
	sort.Slice(outW, func(i, j int) bool {
		if outW[i].GPUHours != outW[j].GPUHours {
			return outW[i].GPUHours > outW[j].GPUHours
		}
		return outW[i].Namespace+outW[i].Kind+outW[i].Name < outW[j].Namespace+outW[j].Kind+outW[j].Name
	})
	if outM == nil {
		outM = []ModelCost{}
	}
	if outW == nil {
		outW = []WorkloadCost{}
	}
	return outM, outW
}
