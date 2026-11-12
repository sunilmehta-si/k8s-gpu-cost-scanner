// Package collect runs every signal's query against Prometheus for one window and returns a snapshot.
package collect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/signals"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// Querier is what collect needs from a Prometheus client.
type Querier interface {
	Query(ctx context.Context, query string, t time.Time) ([]prom.Sample, []string, error)
	QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]prom.Series, []string, error)
	LabelValues(ctx context.Context, label string, matchers []string, start, end time.Time) ([]string, []string, error)
}

// Options tune a collection.
type Options struct {
	// Tool names the gpucost version, recorded in the snapshot.
	Tool string
	// Source is the server's address without credentials, recorded in the snapshot.
	Source string
	// Parallel is how many queries run at once. Zero means 4, gentle on a shared Prometheus.
	Parallel int
	// Progress, when set, is told about each signal as it finishes.
	Progress func(signal string, err error)
}

// Discover returns the metric names from the catalogue that the server had at any time in the window.
func Discover(ctx context.Context, q Querier, w snapshot.Window) ([]string, error) {
	wanted := signals.Metrics()
	matcher := fmt.Sprintf(`{__name__=~"%s"}`, strings.Join(wanted, "|"))
	names, _, err := q.LabelValues(ctx, "__name__", []string{matcher}, w.Start, w.End)
	if err == nil {
		return names, nil
	}
	// Some Prometheus-compatible servers do not filter label values by match[]. Fall back to asking at the end
	// of the window, which misses a metric that stopped before it.
	samples, _, qerr := q.Query(ctx, fmt.Sprintf(`group by (__name__) (%s)`, matcher), w.End)
	if qerr != nil {
		return nil, fmt.Errorf("discover metrics: %w (label values: %v)", qerr, err)
	}
	var out []string
	for _, s := range samples {
		if n := s.Labels["__name__"]; n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Collect discovers which metrics exist, picks each signal's variant and runs the queries. A signal whose metrics
// are missing, or whose query fails, is recorded as a note and the rest are still collected; only failing to
// reach the server at all is an error.
func Collect(ctx context.Context, q Querier, w snapshot.Window, opts Options) (*snapshot.Snapshot, error) {
	present, err := Discover(ctx, q, w)
	if err != nil {
		return nil, err
	}
	snap := snapshot.New(opts.Tool, opts.Source, w)
	snap.Present = present
	have := map[string]bool{}
	for _, m := range present {
		have[m] = true
	}

	type job struct {
		sig   signals.Signal
		query string
	}
	var jobs []job
	for _, sig := range signals.Catalog {
		v, ok := sig.Choose(have)
		if !ok {
			snap.Note("%s: skipped, the server has none of %s", sig.Name, needsList(sig))
			continue
		}
		jobs = append(jobs, job{sig, v.Render(w.Length(), time.Duration(w.Step))})
	}

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 4
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)
	failed := 0
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var warnings []string
			var err error
			var rs []prom.Series
			var is []prom.Sample
			if j.sig.Kind == signals.Range {
				rs, warnings, err = q.QueryRange(ctx, j.query, w.Start, w.End, time.Duration(w.Step))
			} else {
				is, warnings, err = q.Query(ctx, j.query, w.End)
			}
			mu.Lock()
			defer mu.Unlock()
			snap.Queries[j.sig.Name] = j.query
			for _, warn := range warnings {
				snap.Note("%s: server warning: %s", j.sig.Name, warn)
			}
			if err != nil {
				failed++
				snap.Note("%s: query failed: %v", j.sig.Name, err)
			} else if j.sig.Kind == signals.Range {
				snap.Range[j.sig.Name] = rs
			} else {
				snap.Instant[j.sig.Name] = is
			}
			if opts.Progress != nil {
				opts.Progress(j.sig.Name, err)
			}
		}(j)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(jobs) > 0 && failed == len(jobs) {
		return nil, errors.New("every query failed; see the notes with --verbose, or run gpucost doctor")
	}
	return snap, nil
}

func needsList(s signals.Signal) string {
	var names []string
	for _, v := range s.Variants {
		names = append(names, strings.Join(v.Needs, "+"))
	}
	return strings.Join(names, ", ")
}
