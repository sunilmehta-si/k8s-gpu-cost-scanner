package analyze

// ModelCost is what serving one model cost.
type ModelCost struct{}

// WorkloadCost is what one workload's GPUs cost.
type WorkloadCost struct{}

// BlindSpot is something gpucost could not measure, and how much it affects.
type BlindSpot struct{}

func (a *analysis) costs() ([]ModelCost, []WorkloadCost) { return []ModelCost{}, []WorkloadCost{} }

func (a *analysis) blindSpots() []BlindSpot { return []BlindSpot{} }
