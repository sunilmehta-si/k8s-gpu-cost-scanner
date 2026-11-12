package snapshot

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestPlanWindow(t *testing.T) {
	end := time.Date(2026, 11, 12, 10, 7, 30, 0, time.UTC)
	w, err := PlanWindow(24*time.Hour, 5*time.Minute, end)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 12, 10, 5, 0, 0, time.UTC); !w.End.Equal(want) {
		t.Errorf("end = %s, want %s (rounded down to the step)", w.End, want)
	}
	if w.Length() != 24*time.Hour || w.Points() != 289 || w.StepHours() != 5.0/60 {
		t.Errorf("window = %+v, points %d", w, w.Points())
	}
}

func TestPlanWindowWidensStep(t *testing.T) {
	w, err := PlanWindow(30*24*time.Hour, time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if w.Points() > MaxPoints+1 {
		t.Errorf("%d points over 30 days at step %s", w.Points(), w.Step)
	}
	if time.Duration(w.Step) <= time.Minute {
		t.Errorf("step %s was not widened", w.Step)
	}
}

func TestPlanWindowRejects(t *testing.T) {
	now := time.Now()
	for _, c := range []struct{ length, step time.Duration }{{0, time.Minute}, {time.Hour, 0}, {time.Minute, time.Hour}} {
		if _, err := PlanWindow(c.length, c.step, now); err == nil {
			t.Errorf("PlanWindow(%s, %s) accepted", c.length, c.step)
		}
	}
}

func sample() *Snapshot {
	w, _ := PlanWindow(time.Hour, time.Minute, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	s := New("gpucost test", "synthetic", w)
	s.Range["gpu_util"] = []prom.Series{{Labels: prom.Labels{"gpu": "0"}, Points: []prom.Point{{T: 1, V: 0.5}}}}
	s.Instant["tokens"] = []prom.Sample{{Labels: prom.Labels{"pod": "a"}, Value: 42}}
	s.Note("no %s", "SGLang")
	s.Note("no %s", "SGLang")
	return s
}

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"s.json", "s.json.gz"} {
		path := filepath.Join(t.TempDir(), name)
		if err := Write(path, sample()); err != nil {
			t.Fatal(err)
		}
		got, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.Range["gpu_util"][0].Points[0].V != 0.5 || got.Instant["tokens"][0].Value != 42 {
			t.Errorf("%s: got %+v", name, got)
		}
		if len(got.Notes) != 1 || time.Duration(got.Window.Step) != time.Minute {
			t.Errorf("%s: notes %v, step %s", name, got.Notes, got.Window.Step)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for name, in := range map[string]string{
		"no version": `{"window":{}}`,
		"newer":      `{"version":99,"window":{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","step":"1m"}}`,
		"empty":      `{"version":1,"window":{"start":"2026-01-01T00:00:00Z","end":"2026-01-01T00:00:00Z","step":"1m"}}`,
		"not json":   `nope`,
	} {
		if _, err := Decode(strings.NewReader(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDecodeFillsMaps(t *testing.T) {
	var buf bytes.Buffer
	s := sample()
	s.Range, s.Instant = nil, nil
	Encode(&buf, s)
	got, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Range == nil || got.Instant == nil {
		t.Error("maps left nil")
	}
}
