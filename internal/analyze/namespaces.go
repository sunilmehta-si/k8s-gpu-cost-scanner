package analyze

import "sort"

// NamespaceCost is one namespace's GPU spend and waste: the showback a team can be sent.
type NamespaceCost struct {
	Namespace string  `json:"namespace"`
	Pods      int     `json:"pods"`
	GPUHours  float64 `json:"gpu_hours"`
	Cost      float64 `json:"cost"`
	Priced    bool    `json:"priced"`
	Activity  float64 `json:"activity"`
	Measured  bool    `json:"measured"`
	// WasteGPUHours and WasteCost are the namespace's pods' GPU-hours the findings counted as waste. Node-level
	// waste, GPUs no pod requested, belongs to no namespace and is not shared out.
	WasteGPUHours float64 `json:"waste_gpu_hours"`
	WasteCost     float64 `json:"waste_cost"`
}

func (a *analysis) namespaces() []NamespaceCost {
	byNS := map[string]*NamespaceCost{}
	measured := map[string]float64{}
	active := map[string]float64{}
	for _, p := range a.pods() {
		ns := byNS[p.Key.Namespace]
		if ns == nil {
			ns = &NamespaceCost{Namespace: p.Key.Namespace, Priced: true}
			byNS[p.Key.Namespace] = ns
		}
		eq, _ := a.f.GPUEquivalents(p)
		ns.Pods++
		ns.GPUHours += eq
		c, ok := a.cost(p.Node, eq)
		ns.Cost += c
		ns.Priced = ns.Priced && ok
		if p.Usage.Activity.Measured() {
			m := eq * p.Usage.Coverage
			measured[ns.Namespace] += m
			active[ns.Namespace] += m * p.Usage.Activity.Avg
		}
		if w := a.claimed[p.Key]; w > 0 {
			ns.WasteGPUHours += w
			wc, _ := a.cost(p.Node, w)
			ns.WasteCost += wc
		}
	}
	out := make([]NamespaceCost, 0, len(byNS))
	for name, ns := range byNS {
		if measured[name] > 0 {
			ns.Activity = active[name] / measured[name]
			ns.Measured = true
		}
		out = append(out, *ns)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GPUHours != out[j].GPUHours {
			return out[i].GPUHours > out[j].GPUHours
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}
