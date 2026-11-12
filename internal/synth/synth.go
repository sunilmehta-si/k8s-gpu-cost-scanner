// Package synth generates a synthetic GPU fleet with known answers: the raw metrics a real cluster's exporters
// would produce, written as OpenMetrics so promtool can backfill them into a real Prometheus.
//
// The end-to-end test loads the fleet into Prometheus, runs gpucost against it, and checks each finding against
// the answer the scenario was built to have. Nothing here needs a GPU or a cluster.
package synth

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Series is one labelled series of samples.
type Series struct {
	Name    string
	Labels  map[string]string
	Samples []Sample
}

// Sample is one scraped value.
type Sample struct {
	T time.Time
	V float64
}

// Fleet is the generated metrics.
type Fleet struct {
	Start, End time.Time
	Scrape     time.Duration
	Series     []*Series
}

func (f *Fleet) add(name string, labels map[string]string) *Series {
	s := &Series{Name: name, Labels: labels}
	f.Series = append(f.Series, s)
	return s
}

// each calls fn for every scrape time in [from, to).
func (f *Fleet) each(from, to time.Time, fn func(t time.Time, i int)) {
	i := 0
	for t := from; t.Before(to); t = t.Add(f.Scrape) {
		fn(t, i)
		i++
	}
}

// WriteOpenMetrics writes the fleet as OpenMetrics text with explicit timestamps, for
// "promtool tsdb create-blocks-from openmetrics". Samples of one metric are written together, as the format
// requires, and the text ends with # EOF.
func (f *Fleet) WriteOpenMetrics(w io.Writer) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	byName := map[string][]*Series{}
	for _, s := range f.Series {
		byName[s.Name] = append(byName[s.Name], s)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, s := range byName[name] {
			lbl := labelText(s.Labels)
			for _, smp := range s.Samples {
				fmt.Fprintf(bw, "%s%s %s %s\n", name, lbl, strconv.FormatFloat(smp.V, 'g', -1, 64),
					strconv.FormatFloat(float64(smp.T.UnixMilli())/1000, 'f', -1, 64))
			}
		}
	}
	fmt.Fprintln(bw, "# EOF")
	return bw.Flush()
}

func labelText(l map[string]string) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		v := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(l[k])
		parts[i] = fmt.Sprintf(`%s="%s"`, k, v)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// diurnal is a daily load curve between low and high, peaking at 15:00 UTC, with a little seeded noise.
func diurnal(t time.Time, low, high float64, rng *rand.Rand) float64 {
	h := float64(t.Hour()) + float64(t.Minute())/60
	phase := math.Cos((h - 15) / 24 * 2 * math.Pi) // 1 at 15:00, -1 at 03:00
	v := low + (high-low)*(phase+1)/2
	v += (rng.Float64() - 0.5) * 0.04 * (high - low)
	return math.Max(low, math.Min(high, v))
}

func copyLabels(l map[string]string, extra ...string) map[string]string {
	out := make(map[string]string, len(l)+len(extra)/2)
	for k, v := range l {
		out[k] = v
	}
	for i := 0; i+1 < len(extra); i += 2 {
		out[extra[i]] = extra[i+1]
	}
	return out
}
