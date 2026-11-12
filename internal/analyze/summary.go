package analyze

// summary adds the fleet up.
func (a *analysis) summary() Summary {
	s := Summary{Findings: map[Severity]int{}}
	for _, n := range a.f.SortedNodes() {
		h := a.f.OfferedGPUHours(n)
		s.OfferedGPUHours += h
		if c, ok := a.cost(n.Name, h); ok {
			s.OfferedCost += c
		}
	}
	s.Nodes = len(a.f.Nodes)
	priced, active := 0.0, 0.0
	for _, p := range a.pods() {
		s.Pods++
		eq, _ := a.f.GPUEquivalents(p)
		s.RequestedGPUHours += eq
		if c, ok := a.cost(p.Node, eq); ok {
			s.Cost += c
			priced += eq
		}
		if p.Usage.Activity.Measured() {
			measured := eq * p.Usage.Coverage
			s.MeasuredGPUHours += measured
			active += measured * p.Usage.Activity.Avg
		}
	}
	if s.RequestedGPUHours > 0 {
		s.PricedShare = priced / s.RequestedGPUHours
	}
	if s.MeasuredGPUHours > 0 {
		s.Activity = active / s.MeasuredGPUHours
	}
	for _, fd := range a.findings {
		s.WasteGPUHours += fd.WasteGPUHours
		s.WasteCost += fd.WasteCost
		s.Findings[fd.Severity]++
	}
	return s
}
