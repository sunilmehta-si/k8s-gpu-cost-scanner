package fleet

import (
	"math"
	"sort"
)

// Stats summarises a signal's values over the steps they were measured.
type Stats struct {
	Avg float64 `json:"avg"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
	// N is how many steps had a value.
	N int `json:"n"`
}

// Measured reports whether the signal had any value.
func (s Stats) Measured() bool { return s.N > 0 }

// Summarize computes the stats of the values, ignoring NaN. The 95th percentile uses the nearest-rank method, so
// it is always one of the values.
func Summarize(values []float64) Stats {
	vs := make([]float64, 0, len(values))
	for _, v := range values {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			vs = append(vs, v)
		}
	}
	if len(vs) == 0 {
		return Stats{}
	}
	sort.Float64s(vs)
	sum := 0.0
	for _, v := range vs {
		sum += v
	}
	rank := int(math.Ceil(0.95*float64(len(vs)))) - 1
	if rank < 0 {
		rank = 0
	}
	return Stats{Avg: sum / float64(len(vs)), P95: vs[rank], Max: vs[len(vs)-1], N: len(vs)}
}

// ShareAbove is the share of values above the threshold: for request counts, the share of steps the engine was
// busy at all.
func ShareAbove(values []float64, threshold float64) float64 {
	n, above := 0, 0
	for _, v := range values {
		if math.IsNaN(v) {
			continue
		}
		n++
		if v > threshold {
			above++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(above) / float64(n)
}
