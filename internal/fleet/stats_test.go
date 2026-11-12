package fleet

import (
	"math"
	"testing"
)

func TestSummarize(t *testing.T) {
	var vs []float64
	for i := 1; i <= 100; i++ {
		vs = append(vs, float64(i))
	}
	vs = append(vs, math.NaN(), math.Inf(1))
	s := Summarize(vs)
	if s.N != 100 || s.Avg != 50.5 || s.P95 != 95 || s.Max != 100 {
		t.Errorf("stats = %+v", s)
	}
	if one := Summarize([]float64{0.3}); one.P95 != 0.3 || one.Avg != 0.3 {
		t.Errorf("single value = %+v", one)
	}
	if empty := Summarize(nil); empty.Measured() {
		t.Errorf("empty = %+v", empty)
	}
}

func TestShareAbove(t *testing.T) {
	if got := ShareAbove([]float64{0, 0, 1, 3, math.NaN()}, 0); got != 0.5 {
		t.Errorf("ShareAbove = %v, want 0.5", got)
	}
	if got := ShareAbove(nil, 0); got != 0 {
		t.Errorf("ShareAbove(nil) = %v", got)
	}
}
