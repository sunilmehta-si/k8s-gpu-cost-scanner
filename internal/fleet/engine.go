package fleet

import (
	"sort"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// Engine kinds.
const (
	VLLM   = "vllm"
	SGLang = "sglang"
)

// Load is a serving engine's load at one step.
type Load struct {
	Running float64
	// KV is the share of the KV cache in use; HasKV is false when the engine did not report it at that step.
	KV    float64
	HasKV bool
	// Asleep is true when the engine reported being in sleep mode at that step.
	Asleep bool
}

// Engine is what a pod's inference server reported about its own work.
type Engine struct {
	Kind  string `json:"kind"`
	Model string `json:"model"`
	// Running and Waiting are requests in the batch and in the queue, summed over the pod's data-parallel engines.
	Running Stats `json:"running"`
	Waiting Stats `json:"waiting"`
	// BusyShare is the share of the steps the pod held GPUs with at least one request running.
	BusyShare float64 `json:"busy_share"`
	// KV is the share of the KV cache in use, averaged over the pod's engines.
	KV Stats `json:"kv"`
	// AwakeShare is the share of steps the engine was awake; nil when the engine does not report sleep mode.
	AwakeShare *float64 `json:"awake_share,omitempty"`
	// GenerationTokens, PromptTokens and Requests are counted over the window; HasCounts is false when the
	// engine's counters were missing.
	GenerationTokens float64 `json:"generation_tokens"`
	PromptTokens     float64 `json:"prompt_tokens"`
	Requests         float64 `json:"requests"`
	HasCounts        bool    `json:"has_counts"`
	// Steps is the load at each step the pod held GPUs, keyed by Unix milliseconds.
	Steps map[int64]Load `json:"-"`
}

// enginePod returns the pod a serving engine's series was scraped from. The engine exports no pod label of its
// own, so this is the scrape's target labels: namespace and pod from a PodMonitor or ServiceMonitor, or the
// kubernetes_* names older scrape configs use.
func enginePod(l prom.Labels) (PodKey, bool) {
	for _, pair := range [][2]string{{"namespace", "pod"}, {"kubernetes_namespace", "kubernetes_pod_name"}, {"k8s_namespace_name", "k8s_pod_name"}, {"namespace", "pod_name"}} {
		if ns, pod := l[pair[0]], l[pair[1]]; ns != "" && pod != "" {
			return PodKey{ns, pod}, true
		}
	}
	return PodKey{}, false
}

// combine merges an engine's series for one pod at one step. vLLM reports one series per data-parallel engine
// (the engine label), whose requests add up; SGLang repeats the scheduler's numbers on every tensor-parallel
// rank, so its series are not added. Duplicate scrapes of the same engine are collapsed either way.
func combine(kind string, series []prom.Series, sum bool) map[int64]float64 {
	// Most pods have one series: its points are the answer.
	if len(series) == 1 {
		out := make(map[int64]float64, len(series[0].Points))
		for _, pt := range series[0].Points {
			if !isNaN(pt.V) {
				out[pt.T] = pt.V
			}
		}
		return out
	}
	size := 0
	for _, s := range series {
		if len(s.Points) > size {
			size = len(s.Points)
		}
	}
	groups := map[string]map[int64]float64{}
	for _, s := range series {
		g := s.Labels["engine"]
		if kind == SGLang {
			g = ""
		}
		m := groups[g]
		if m == nil {
			m = make(map[int64]float64, size)
			groups[g] = m
		}
		for _, pt := range s.Points {
			if isNaN(pt.V) {
				continue
			}
			if cur, ok := m[pt.T]; !ok || pt.V > cur {
				m[pt.T] = pt.V
			}
		}
	}
	out := make(map[int64]float64, size)
	count := map[int64]int{}
	for _, m := range groups {
		for t, v := range m {
			if sum {
				out[t] += v
			} else {
				out[t] += v
				count[t]++
			}
		}
	}
	if !sum {
		for t := range out {
			out[t] /= float64(count[t])
		}
	}
	return out
}

type engineSignals struct {
	kind                                           string
	running, waiting, kv, awake                    string
	generationTokens, promptTokens, requestsFinish string
}

var engineSet = []engineSignals{
	{VLLM, "vllm_running", "vllm_waiting", "vllm_kv_usage", "vllm_awake", "vllm_generation_tokens", "vllm_prompt_tokens", "vllm_requests"},
	{SGLang, "sglang_running", "sglang_waiting", "sglang_kv_usage", "", "sglang_generation_tokens", "sglang_prompt_tokens", "sglang_requests"},
}

// attachEngines joins vLLM and SGLang metrics to the pods that serve them.
func (f *Fleet) attachEngines(s *snapshot.Snapshot) {
	for _, set := range engineSet {
		byPod := func(name string) map[PodKey][]prom.Series {
			out := map[PodKey][]prom.Series{}
			for _, series := range s.Range[name] {
				if key, ok := enginePod(series.Labels); ok {
					out[key] = append(out[key], series)
				}
			}
			return out
		}
		running, waiting, kv := byPod(set.running), byPod(set.waiting), byPod(set.kv)
		var awake map[PodKey][]prom.Series
		if set.awake != "" {
			awake = byPod(set.awake)
		}
		counts := func(name string) map[PodKey]float64 {
			out := map[PodKey]float64{}
			for _, smp := range s.Instant[name] {
				if key, ok := enginePod(smp.Labels); ok && !isNaN(smp.Value) {
					out[key] += smp.Value
				}
			}
			return out
		}
		gen, prompt, reqs := counts(set.generationTokens), counts(set.promptTokens), counts(set.requestsFinish)
		_, haveCounts := s.Instant[set.generationTokens]

		for key, rs := range running {
			pod := f.Pods[key]
			if pod == nil {
				continue
			}
			e := &Engine{Kind: set.kind, Steps: make(map[int64]Load, len(pod.held))}
			for _, series := range rs {
				if m := series.Labels["model_name"]; m != "" {
					e.Model = m
					break
				}
			}
			run := combine(set.kind, rs, true)
			wait := combine(set.kind, waiting[key], true)
			use := combine(set.kind, kv[key], false)
			var runV, waitV, kvV []float64
			for t := range pod.held {
				l := Load{}
				if v, ok := run[t]; ok {
					l.Running = v
					runV = append(runV, v)
				}
				if v, ok := wait[t]; ok {
					waitV = append(waitV, v)
				}
				if v, ok := use[t]; ok {
					l.KV, l.HasKV = v, true
					kvV = append(kvV, v)
				}
				if _, ok := run[t]; ok {
					e.Steps[t] = l
				}
			}
			e.Running, e.Waiting, e.KV = Summarize(runV), Summarize(waitV), Summarize(kvV)
			e.BusyShare = ShareAbove(runV, 0)
			if a := awake[key]; len(a) > 0 {
				aw := combine(set.kind, a, false)
				var awV []float64
				for t := range pod.held {
					if v, ok := aw[t]; ok {
						awV = append(awV, v)
					}
				}
				if len(awV) > 0 {
					share := ShareAbove(awV, 0.5)
					e.AwakeShare = &share
				}
				for t, v := range aw {
					if l, ok := e.Steps[t]; ok && v < 0.5 {
						l.Asleep = true
						e.Steps[t] = l
					}
				}
			}
			if haveCounts {
				e.HasCounts = true
				e.GenerationTokens, e.PromptTokens, e.Requests = gen[key], prompt[key], reqs[key]
			}
			pod.Engine = e
		}
	}
	serving := 0
	for _, p := range f.Pods {
		if p.Engine != nil {
			serving++
		}
	}
	if serving == 0 && len(f.Pods) > 0 && len(s.Range["vllm_running"])+len(s.Range["sglang_running"]) > 0 {
		f.note("vLLM or SGLang metrics were found, but none carry the namespace and pod of a GPU pod: scrape the " +
			"servers with a PodMonitor or ServiceMonitor so their series are labelled with their pod")
	}
}

// Timeline returns the engine's load at each step, in time order.
func (e *Engine) Timeline() []int64 {
	ts := make([]int64, 0, len(e.Steps))
	for t := range e.Steps {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	return ts
}
