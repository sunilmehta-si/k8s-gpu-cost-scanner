// Package snapshot holds the raw metrics of one window, as Prometheus returned them.
//
// A scan first collects a snapshot and then analyses it. Keeping the two apart means a snapshot can be saved
// (gpucost record), shared and analysed again later or elsewhere, and that every analysis test runs on a fixed
// input with no Prometheus at all.
package snapshot

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

// Version is the snapshot format version. A reader refuses a newer version instead of misreading it.
const Version = 1

// MaxPoints is the most points a range query may return per series. Prometheus refuses more than 11,000; the
// margin leaves room for a step that does not divide the window exactly.
const MaxPoints = 10000

// Window is the time span a snapshot covers and the step its range series are sampled at.
type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Step  Duration  `json:"step"`
}

// Length is the window's duration.
func (w Window) Length() time.Duration { return w.End.Sub(w.Start) }

// Hours is the window's length in hours.
func (w Window) Hours() float64 { return w.Length().Hours() }

// StepHours is the step in hours: what one point of a range series stands for.
func (w Window) StepHours() float64 { return time.Duration(w.Step).Hours() }

// Points is the number of points a range series sampled over the whole window has.
func (w Window) Points() int { return int(w.Length()/time.Duration(w.Step)) + 1 }

// PlanWindow returns the window of the given length ending at end, with the step widened if needed so a range
// query stays under MaxPoints. The end is rounded down to the step, so two scans of the same span line up.
func PlanWindow(length, step time.Duration, end time.Time) (Window, error) {
	if length <= 0 {
		return Window{}, errors.New("window must be positive")
	}
	if step <= 0 {
		return Window{}, errors.New("step must be positive")
	}
	if step > length {
		return Window{}, fmt.Errorf("step %s is longer than the window %s", step, length)
	}
	if min := length / MaxPoints; step < min {
		step = min.Round(time.Second)
		if step*MaxPoints < length {
			step += time.Second
		}
	}
	end = end.Truncate(step).UTC()
	return Window{Start: end.Add(-length), End: end, Step: Duration(step)}, nil
}

// Snapshot is the raw result of every query a scan ran over one window.
type Snapshot struct {
	Version int    `json:"version"`
	Tool    string `json:"tool"`
	// Source is where the metrics came from: a Prometheus URL without credentials, or "synthetic".
	Source string `json:"source"`
	Window Window `json:"window"`
	// Range holds range query results, by signal name.
	Range map[string][]prom.Series `json:"range"`
	// Instant holds instant query results, by signal name.
	Instant map[string][]prom.Sample `json:"instant"`
	// Present lists the metric names the server had over the window.
	Present []string `json:"metrics_present,omitempty"`
	// Queries holds the PromQL each signal was collected with, so a reader can rerun or audit it.
	Queries map[string]string `json:"queries,omitempty"`
	// Notes record what went wrong or was missing while collecting, for example a metric no target exports.
	Notes []string `json:"notes,omitempty"`
}

// Has reports whether the server had the metric over the window.
func (s *Snapshot) Has(metric string) bool {
	for _, m := range s.Present {
		if m == metric {
			return true
		}
	}
	return false
}

// New returns an empty snapshot of the window.
func New(tool, source string, w Window) *Snapshot {
	return &Snapshot{
		Version: Version,
		Tool:    tool,
		Source:  source,
		Window:  w,
		Range:   map[string][]prom.Series{},
		Instant: map[string][]prom.Sample{},
		Queries: map[string]string{},
	}
}

// Note records a collection problem once.
func (s *Snapshot) Note(format string, args ...any) {
	n := fmt.Sprintf(format, args...)
	for _, existing := range s.Notes {
		if existing == n {
			return
		}
	}
	s.Notes = append(s.Notes, n)
	sort.Strings(s.Notes)
}

// Write saves the snapshot as JSON, gzipped when the path ends in .gz.
func Write(path string, s *Snapshot) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	var w io.Writer = f
	if strings.HasSuffix(path, ".gz") {
		gz := gzip.NewWriter(f)
		defer func() {
			if cerr := gz.Close(); err == nil {
				err = cerr
			}
		}()
		w = gz
	}
	return Encode(w, s)
}

// Encode writes the snapshot as JSON.
func Encode(w io.Writer, s *Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(s)
}

// Read loads a snapshot written by Write, gunzipping when the path ends in .gz.
func Read(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		defer gz.Close()
		r = gz
	}
	s, err := Decode(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Decode reads a snapshot and checks it is one this version can analyse.
func Decode(r io.Reader) (*Snapshot, error) {
	var s Snapshot
	dec := json.NewDecoder(r)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	switch {
	case s.Version == 0:
		return nil, errors.New("not a gpucost snapshot: no version")
	case s.Version > Version:
		return nil, fmt.Errorf("snapshot version %d is newer than this gpucost reads (%d); upgrade gpucost", s.Version, Version)
	case !s.Window.End.After(s.Window.Start) || s.Window.Step <= 0:
		return nil, errors.New("snapshot window is empty or has no step")
	}
	if s.Range == nil {
		s.Range = map[string][]prom.Series{}
	}
	if s.Instant == nil {
		s.Instant = map[string][]prom.Sample{}
	}
	return &s, nil
}

// Duration is a time.Duration that reads and writes as a Go duration string such as "5m0s".
type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).String() }

// MarshalJSON writes the duration as a string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON reads a duration string.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}
