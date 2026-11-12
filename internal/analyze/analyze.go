// Package analyze turns a fleet into a report: what the GPUs cost, which of them were wasted and why, and what
// gpucost could not see.
//
// Every wasted GPU-hour is counted once. Rules run in a fixed order, from the most specific cause to the most
// general, and a pod's hours claimed by one rule are not counted again by a later one.
package analyze

import (
	"fmt"
	"sort"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/price"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// Severity ranks a finding by the GPU-hours at stake, not by price, so it means the same with or without prices.
type Severity string

// Severities, from most to least.
const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
	Info   Severity = "info"
)

func (s Severity) rank() int {
	switch s {
	case High:
		return 0
	case Medium:
		return 1
	case Low:
		return 2
	}
	return 3
}

// Thresholds tune the rules. Defaults returns the values the documentation explains.
type Thresholds struct {
	// IdleActivity: a GPU whose 95th-percentile activity stays below this did nothing useful.
	IdleActivity float64 `json:"idle_activity"`
	// MinHours: a pod must hold its GPUs at least this long in the window to be judged at all.
	MinHours float64 `json:"min_hours"`
	// MinCoverage: the share of a pod's GPU time the DCGM exporter must have measured to judge its activity.
	MinCoverage float64 `json:"min_coverage"`
	// LowDuty: a serving pod busy for less than this share of its time is mostly idle.
	LowDuty float64 `json:"low_duty"`
	// KVTarget: the KV cache share one replica may fill at peak; replicas are sized to keep each below it.
	KVTarget float64 `json:"kv_target"`
	// Oversized: a pod whose GPU activity and KV use both peak below this could use a smaller GPU or a slice.
	Oversized float64 `json:"oversized"`
	// MinStrandedHours: a node's unrequested GPU-hours below this are not reported.
	MinStrandedHours float64 `json:"min_stranded_hours"`
	// HighHours and MediumHours set the severity of a finding by its wasted GPU-hours.
	HighHours   float64 `json:"high_hours"`
	MediumHours float64 `json:"medium_hours"`
}

// Defaults are the thresholds gpucost uses unless told otherwise.
func Defaults() Thresholds {
	return Thresholds{
		IdleActivity: 0.05, MinHours: 1, MinCoverage: 0.5, LowDuty: 0.25, KVTarget: 0.8, Oversized: 0.3,
		MinStrandedHours: 1, HighHours: 24, MediumHours: 4,
	}
}

// Finding is one problem, with the evidence for it and what it wastes.
type Finding struct {
	Rule     string   `json:"rule"`
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Severity Severity `json:"severity"`
	// Target is what the finding is about: a pod, a workload or a node.
	Target    string   `json:"target"`
	Namespace string   `json:"namespace,omitempty"`
	Pods      []string `json:"pods,omitempty"`
	Node      string   `json:"node,omitempty"`
	Evidence  []string `json:"evidence"`
	// WasteGPUHours are recoverable GPU-hours, counted once across all findings. Zero for findings that are
	// advice, such as a GPU that may be bigger than needed.
	WasteGPUHours float64 `json:"waste_gpu_hours"`
	// WasteCost is WasteGPUHours priced; Priced is false when part of it had no price.
	WasteCost      float64 `json:"waste_cost"`
	Priced         bool    `json:"priced"`
	Recommendation string  `json:"recommendation"`
}

// Report is the result of a scan.
type Report struct {
	Tool       string            `json:"tool"`
	Source     string            `json:"source"`
	Window     snapshot.Window   `json:"window"`
	Generated  time.Time         `json:"generated"`
	Currency   string            `json:"currency"`
	Thresholds Thresholds        `json:"thresholds"`
	Summary    Summary           `json:"summary"`
	Models     []ModelCost       `json:"models"`
	Workloads  []WorkloadCost    `json:"workloads"`
	Findings   []Finding         `json:"findings"`
	BlindSpots []BlindSpot       `json:"blind_spots"`
	Queries    map[string]string `json:"queries,omitempty"`
}

// Summary is the fleet in a few numbers.
type Summary struct {
	Nodes int `json:"nodes"`
	Pods  int `json:"pods"`
	// OfferedGPUHours are the GPU-hours nodes made allocatable; RequestedGPUHours those pods requested. Both
	// count MIG slices as their share of a GPU.
	OfferedGPUHours   float64 `json:"offered_gpu_hours"`
	RequestedGPUHours float64 `json:"requested_gpu_hours"`
	// MeasuredGPUHours are the requested GPU-hours the DCGM exporter measured activity for.
	MeasuredGPUHours float64 `json:"measured_gpu_hours"`
	// Activity is the measured GPUs' average activity, weighted by GPU-hours.
	Activity float64 `json:"activity"`
	// Cost is what the requested GPU-hours cost, over the priced share PricedShare of them.
	Cost        float64 `json:"cost"`
	PricedShare float64 `json:"priced_share"`
	// OfferedCost is what every allocatable GPU-hour cost, requested or not.
	OfferedCost float64 `json:"offered_cost"`
	// WasteGPUHours and WasteCost add up every finding's recoverable waste.
	WasteGPUHours float64 `json:"waste_gpu_hours"`
	WasteCost     float64 `json:"waste_cost"`
	// Findings counts findings by severity.
	Findings map[Severity]int `json:"findings"`
}

// Options configure an analysis.
type Options struct {
	Thresholds Thresholds
	Currency   string
	Now        time.Time
	// Namespaces, when set, limits pod-level findings and costs to these namespaces. Nodes are still judged on
	// every pod, since a node's GPUs are shared.
	Namespaces map[string]bool
}

type analysis struct {
	f      *fleet.Fleet
	s      *snapshot.Snapshot
	prices *price.Book
	t      Thresholds
	opts   Options
	// claimed holds, per pod, the GPU-hours a rule already counted as waste.
	claimed  map[fleet.PodKey]float64
	findings []Finding
}

// Run analyses the fleet built from the snapshot.
func Run(f *fleet.Fleet, s *snapshot.Snapshot, prices *price.Book, opts Options) *Report {
	if opts.Thresholds == (Thresholds{}) {
		opts.Thresholds = Defaults()
	}
	if opts.Currency == "" {
		opts.Currency = "USD"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	a := &analysis{f: f, s: s, prices: prices, t: opts.Thresholds, opts: opts, claimed: map[fleet.PodKey]float64{}}
	for _, rule := range rules {
		rule(a)
	}
	sort.SliceStable(a.findings, func(i, j int) bool {
		fi, fj := a.findings[i], a.findings[j]
		if fi.Severity != fj.Severity {
			return fi.Severity.rank() < fj.Severity.rank()
		}
		if fi.WasteGPUHours != fj.WasteGPUHours {
			return fi.WasteGPUHours > fj.WasteGPUHours
		}
		return fi.Target < fj.Target
	})
	r := &Report{
		Tool: s.Tool, Source: s.Source, Window: s.Window, Generated: opts.Now, Currency: opts.Currency,
		Thresholds: opts.Thresholds, Findings: a.findings, Queries: s.Queries,
	}
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	r.Summary = a.summary()
	r.Models, r.Workloads = a.costs()
	r.BlindSpots = a.blindSpots()
	return r
}

// rules run in this order, most specific cause first; see the package comment.
var rules = []func(*analysis){
	(*analysis).asleep,
	(*analysis).noTraffic,
	(*analysis).idle,
	(*analysis).overprovisioned,
	(*analysis).stranded,
}

// pods returns the pods in scope, in a stable order.
func (a *analysis) pods() []*fleet.Pod {
	var out []*fleet.Pod
	for _, p := range a.f.SortedPods() {
		if a.opts.Namespaces == nil || a.opts.Namespaces[p.Key.Namespace] {
			out = append(out, p)
		}
	}
	return out
}

// unclaimed is the pod's GPU-hours no earlier rule counted as waste.
func (a *analysis) unclaimed(p *fleet.Pod) float64 {
	eq, _ := a.f.GPUEquivalents(p)
	if left := eq - a.claimed[p.Key]; left > 0 {
		return left
	}
	return 0
}

// cost prices GPU-hours on a node. The second result is false when the node has no price.
func (a *analysis) cost(node string, hours float64) (float64, bool) {
	if a.prices == nil {
		return 0, false
	}
	p, ok := a.prices.For(node)
	if !ok {
		return 0, false
	}
	return hours * p.PerGPUHour, true
}

func (a *analysis) severity(hours float64) Severity {
	switch {
	case hours >= a.t.HighHours:
		return High
	case hours >= a.t.MediumHours:
		return Medium
	case hours > 0:
		return Low
	}
	return Info
}

// claimPod records a finding about one pod that wastes the given GPU-hours of it.
func (a *analysis) claimPod(p *fleet.Pod, fd Finding, hours float64) {
	if left := a.unclaimed(p); hours > left {
		hours = left
	}
	a.claimed[p.Key] += hours
	fd.Target = p.Key.String()
	fd.Namespace = p.Key.Namespace
	fd.Pods = []string{p.Key.Name}
	fd.Node = p.Node
	fd.WasteGPUHours = hours
	fd.WasteCost, fd.Priced = a.cost(p.Node, hours)
	if fd.Severity == "" {
		fd.Severity = a.severity(hours)
	}
	a.findings = append(a.findings, fd)
}

// claimWorkload records a finding about a workload that wastes the given GPU-hours, spread over its pods' hours
// no earlier rule claimed.
func (a *analysis) claimWorkload(w string, ns string, pods []*fleet.Pod, fd Finding, hours float64) {
	fd.Target, fd.Namespace = w, ns
	fd.Priced = true
	left := hours
	for _, p := range pods {
		fd.Pods = append(fd.Pods, p.Key.Name)
		take := a.unclaimed(p)
		if take > left {
			take = left
		}
		if take <= 0 {
			continue
		}
		left -= take
		a.claimed[p.Key] += take
		fd.WasteGPUHours += take
		c, ok := a.cost(p.Node, take)
		fd.WasteCost += c
		fd.Priced = fd.Priced && ok
	}
	if fd.Severity == "" {
		fd.Severity = a.severity(fd.WasteGPUHours)
	}
	a.findings = append(a.findings, fd)
}

func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

func hrs(v float64) string { return fmt.Sprintf("%.1f GPU-hours", v) }
