package synth

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Node is a GPU node in the scenario.
type Node struct {
	Name, Model string
	GPUs        int
	MemoryBytes float64
}

// Workload is a set of pods in the scenario and what they do.
type Workload struct {
	Namespace, Kind, Name string
	Replicas              []Replica
	// Serving workloads run vLLM: the model, and how busy each replica is.
	Model string
	// Activity gives a replica's GPU activity at time t.
	Activity func(t time.Time, rng *rand.Rand) float64
	// Running gives a serving replica's running requests at time t; nil for workloads that serve nothing.
	Running func(t time.Time, rng *rand.Rand) float64
	// KV gives a serving replica's KV cache use at time t.
	KV func(t time.Time, rng *rand.Rand) float64
	// Awake gives whether the engine is awake at time t; nil when it never sleeps.
	Awake func(t time.Time) bool
	// TokensPerRequest is the output tokens one finished request generates.
	TokensPerRequest float64
}

// Replica is one pod of a workload: its node, GPU count, and when it ran.
type Replica struct {
	Node     string
	GPUs     int
	From, To time.Duration // offsets from the window's start; To 0 means to the end
}

// Scenario is a fleet with known answers.
type Scenario struct {
	Name      string
	End       time.Time
	Window    time.Duration
	Scrape    time.Duration
	Nodes     []Node
	Workloads []Workload
	// Prices are the illustrative per-GPU-hour prices the scenario is scanned with.
	Prices map[string]float64
	// Expect lists the findings the scenario was built to produce, with their GPU-hours.
	Expect []Expectation
}

// Expectation is a finding the scenario must produce.
type Expectation struct {
	Rule, Target string
	// GPUHours is the waste the finding should report (or idle hours for advice); Tolerance allows for where the
	// window's first and last steps fall.
	GPUHours, Tolerance float64
}

// Demo is the fleet the README and the end-to-end test use: three GPU nodes, one problem of each kind, and models
// that do their job. Times are fixed, so every run produces the same metrics.
func Demo() Scenario {
	end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h := time.Hour
	busy := func(lo, hi float64) func(time.Time, *rand.Rand) float64 {
		return func(t time.Time, rng *rand.Rand) float64 { return diurnal(t, lo, hi, rng) }
	}
	flat := func(v float64) func(time.Time, *rand.Rand) float64 {
		return func(_ time.Time, rng *rand.Rand) float64 { return math.Max(0, v+(rng.Float64()-0.5)*v*0.1) }
	}
	return Scenario{
		Name: "demo", End: end, Window: 24 * h, Scrape: time.Minute,
		Nodes: []Node{
			{"gpu-h100-a", "NVIDIA H100 80GB HBM3", 8, 80e9},
			{"gpu-h100-b", "NVIDIA H100 80GB HBM3", 8, 80e9},
			{"gpu-l4-a", "NVIDIA L4", 4, 24e9},
		},
		Workloads: []Workload{
			{
				// Three replicas of a 70B model; at the daily peak all three together fill 105% of one replica's
				// KV cache, so two at 80% would do: one replica, 4 GPUs, too many all day.
				Namespace: "ml", Kind: "Deployment", Name: "llama-70b", Model: "meta-llama/Llama-3.3-70B-Instruct",
				Replicas: []Replica{{"gpu-h100-a", 4, 0, 0}, {"gpu-h100-a", 4, 0, 0}, {"gpu-h100-b", 4, 0, 0}},
				Activity: busy(0.25, 0.7), Running: busy(2, 14), KV: busy(0.05, 0.35), TokensPerRequest: 350,
			},
			{
				// A small model with sparse traffic: busy in about one step in eight. Advice, not waste.
				Namespace: "ml", Kind: "Deployment", Name: "qwen-7b", Model: "Qwen/Qwen2.5-7B-Instruct",
				Replicas: []Replica{{"gpu-l4-a", 1, 0, 0}},
				Activity: func(t time.Time, rng *rand.Rand) float64 { return sparse(t, 0.6) },
				Running:  func(t time.Time, rng *rand.Rand) float64 { return sparse(t, 2) },
				KV:       func(t time.Time, rng *rand.Rand) float64 { return sparse(t, 0.1) }, TokensPerRequest: 220,
			},
			{
				// A model nobody calls any more.
				Namespace: "ml", Kind: "Deployment", Name: "legacy-mistral", Model: "mistralai/Mistral-7B-Instruct-v0.3",
				Replicas: []Replica{{"gpu-l4-a", 1, 0, 0}},
				Activity: flat(0.0), Running: flat(0), KV: flat(0), TokensPerRequest: 200,
			},
			{
				// An RL policy server asleep 70% of the day, holding its GPU.
				Namespace: "rl", Kind: "Deployment", Name: "policy-server", Model: "rl/policy-8b",
				Replicas: []Replica{{"gpu-l4-a", 1, 0, 0}},
				Activity: func(t time.Time, rng *rand.Rand) float64 {
					if awakeRL(t) {
						return 0.5
					}
					return 0
				},
				Running: func(t time.Time, rng *rand.Rand) float64 {
					if awakeRL(t) {
						return 3
					}
					return 0
				},
				KV: flat(0.2), Awake: awakeRL, TokensPerRequest: 500,
			},
			{
				// An embedding service doing real but light work on a whole L4. Advice: a smaller slice may do.
				Namespace: "search", Kind: "Deployment", Name: "embedder",
				Replicas: []Replica{{"gpu-l4-a", 1, 0, 0}},
				Activity: flat(0.12),
			},
			{
				// A notebook left running on an H100 all day.
				Namespace: "research", Kind: "Pod", Name: "notebook-ana",
				Replicas: []Replica{{"gpu-h100-b", 1, 0, 0}},
				Activity: flat(0.0),
			},
			{
				// A fine-tune that ran hard for six hours and finished: no finding.
				Namespace: "training", Kind: "Job", Name: "finetune-llama",
				Replicas: []Replica{{"gpu-h100-b", 2, 2 * h, 8 * h}},
				Activity: flat(0.92),
			},
		},
		Prices: map[string]float64{"h100-80gb": 4.00, "l4": 0.70},
		Expect: []Expectation{
			// One replica of 4 GPUs too many, all day: 4 × 24 h.
			{"GPU005", "ml/Deployment/llama-70b", 96, 0.5},
			{"GPU002", "ml/legacy-mistral", 24, 0.5},
			// Asleep 70% of the day on one GPU.
			{"GPU003", "rl/policy-server", 24 * 0.7, 0.5},
			{"GPU001", "research/notebook-ana", 24, 0.5},
			// gpu-h100-b: 8 GPUs × 24 h offered; 4 × 24 + 1 × 24 + 2 × 6 requested.
			{"GPU004", "node/gpu-h100-b", 8*24 - (4*24 + 24 + 2*6), 0.5},
			// qwen-7b runs a request in one step of eight: 7/8 of its 24 GPU-hours idle.
			{"GPU006", "ml/qwen-7b", 24 * 7 / 8, 1},
			{"GPU007", "search/embedder", 0, 0},
		},
	}
}

// sparse is busy (value v) for 3 minutes of every 24 since midnight, otherwise 0: one sample in eight, whether
// sampled every minute or at a 5-minute step.
func sparse(t time.Time, v float64) float64 {
	if (t.Hour()*60+t.Minute())%24 < 3 {
		return v
	}
	return 0
}

// awakeRL is awake from 08:00 to 15:12 UTC: 30% of the day.
func awakeRL(t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	return m >= 8*60 && m < 8*60+432
}

// Generate produces the scenario's metrics.
func (sc Scenario) Generate() *Fleet {
	start := sc.End.Add(-sc.Window)
	f := &Fleet{Start: start, End: sc.End, Scrape: sc.Scrape}
	// A little past the end, so the last step of a range query still finds a sample.
	stop := sc.End.Add(sc.Scrape)
	rng := rand.New(rand.NewPCG(1, 2))

	gpuIndex := map[string]int{}
	for _, n := range sc.Nodes {
		f.add("kube_node_status_allocatable", map[string]string{"node": n.Name, "resource": "nvidia_com_gpu", "unit": "integer"}).
			Samples = constant(f, start, stop, float64(n.GPUs))
		f.add("kube_node_labels", map[string]string{"node": n.Name}).Samples = constant(f, start, stop, 1)
	}
	nodeOf := map[string]Node{}
	for _, n := range sc.Nodes {
		nodeOf[n.Name] = n
	}

	for _, wl := range sc.Workloads {
		rs := wl.Name + "-7c9d8f"
		if wl.Kind == "Deployment" {
			f.add("kube_replicaset_owner", map[string]string{"namespace": wl.Namespace, "replicaset": rs, "owner_kind": "Deployment", "owner_name": wl.Name, "owner_is_controller": "true"}).
				Samples = constant(f, start, stop, 1)
		}
		for i, r := range wl.Replicas {
			pod := podName(wl, rs, i)
			from, to := start.Add(r.From), stop
			if r.To > 0 {
				to = start.Add(r.To)
			}
			pl := map[string]string{"namespace": wl.Namespace, "pod": pod}
			f.add("kube_pod_container_resource_requests", copyLabels(pl, "container", "main", "node", r.Node, "resource", "nvidia_com_gpu", "unit", "integer")).
				Samples = constant(f, from, to, float64(r.GPUs))
			f.add("kube_pod_status_phase", copyLabels(pl, "phase", "Running")).Samples = constant(f, from, to, 1)
			switch wl.Kind {
			case "Deployment":
				f.add("kube_pod_owner", copyLabels(pl, "owner_kind", "ReplicaSet", "owner_name", rs, "owner_is_controller", "true")).Samples = constant(f, from, to, 1)
			case "Job":
				f.add("kube_pod_owner", copyLabels(pl, "owner_kind", "Job", "owner_name", wl.Name, "owner_is_controller", "true")).Samples = constant(f, from, to, 1)
			}

			// One DCGM series per GPU, mapped to the pod. Prometheus scraped the exporter without honor_labels, so
			// the pod's labels arrive as exported_*, and namespace and pod name the exporter itself.
			n := nodeOf[r.Node]
			for g := 0; g < r.GPUs; g++ {
				idx := gpuIndex[r.Node]
				gpuIndex[r.Node]++
				gl := map[string]string{
					"gpu": fmt.Sprint(idx), "UUID": fmt.Sprintf("GPU-%s-%d", r.Node, idx), "modelName": n.Model,
					"Hostname": r.Node, "namespace": "gpu-operator", "pod": "nvidia-dcgm-exporter-" + r.Node,
					"exported_namespace": wl.Namespace, "exported_pod": pod, "exported_container": "main",
				}
				act := f.add("DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO", gl)
				mem := f.add("DCGM_FI_DEV_FB_USED_BYTES", gl)
				pw := f.add("DCGM_FI_DEV_BOARD_POWER_WATTS", gl)
				f.add("DCGM_FI_DEV_FB_CAPACITY_BYTES", gl).Samples = constant(f, from, to, n.MemoryBytes)
				f.each(from, to, func(t time.Time, _ int) {
					a := wl.Activity(t, rng)
					act.Samples = append(act.Samples, Sample{t, round(a)})
					used := 0.02
					if wl.Model != "" {
						used = 0.9 // inference servers reserve most of the memory up front
					}
					mem.Samples = append(mem.Samples, Sample{t, math.Round(n.MemoryBytes * used)})
					pw.Samples = append(pw.Samples, Sample{t, math.Round(60 + 600*a)})
				})
			}

			if wl.Model == "" {
				continue
			}
			vl := copyLabels(pl, "model_name", wl.Model, "engine", "0")
			run := f.add("vllm:num_requests_running", vl)
			wait := f.add("vllm:num_requests_waiting", vl)
			kv := f.add("vllm:kv_cache_usage_perc", vl)
			gen := f.add("vllm:generation_tokens_total", vl)
			prompt := f.add("vllm:prompt_tokens_total", vl)
			done := f.add("vllm:request_success_total", copyLabels(vl, "finished_reason", "stop"))
			var awake *Series
			if wl.Awake != nil {
				awake = f.add("vllm:engine_sleep_state", copyLabels(vl, "sleep_state", "awake"))
			}
			totalGen, totalPrompt, totalDone := 0.0, 0.0, 0.0
			f.each(from, to, func(t time.Time, _ int) {
				running := math.Round(wl.Running(t, rng))
				run.Samples = append(run.Samples, Sample{t, running})
				wait.Samples = append(wait.Samples, Sample{t, 0})
				kv.Samples = append(kv.Samples, Sample{t, round(wl.KV(t, rng))})
				// Each running request finishes in about 20 seconds.
				finished := running * sc.Scrape.Seconds() / 20
				totalDone += finished
				totalGen += finished * wl.TokensPerRequest
				totalPrompt += finished * wl.TokensPerRequest * 3
				gen.Samples = append(gen.Samples, Sample{t, math.Round(totalGen)})
				prompt.Samples = append(prompt.Samples, Sample{t, math.Round(totalPrompt)})
				done.Samples = append(done.Samples, Sample{t, math.Round(totalDone)})
				if awake != nil {
					v := 0.0
					if wl.Awake(t) {
						v = 1
					}
					awake.Samples = append(awake.Samples, Sample{t, v})
				}
			})
		}
	}
	// Node labels name the product too, as GPU Feature Discovery would.
	for _, s := range f.Series {
		if s.Name == "kube_node_labels" {
			s.Labels["label_nvidia_com_gpu_product"] = replaceSpaces(nodeOf[s.Labels["node"]].Model)
		}
	}
	return f
}

func podName(wl Workload, rs string, i int) string {
	switch wl.Kind {
	case "Deployment":
		return fmt.Sprintf("%s-%c%c%c%c%c", rs, 'a'+i, 'x', 'k', 'q'+i%3, 'z')
	case "Job":
		return fmt.Sprintf("%s-%d-bx7kq", wl.Name, i)
	}
	return wl.Name
}

func constant(f *Fleet, from, to time.Time, v float64) []Sample {
	var out []Sample
	f.each(from, to, func(t time.Time, _ int) { out = append(out, Sample{t, v}) })
	return out
}

func round(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func replaceSpaces(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r == ' ' {
			out[i] = '-'
		}
	}
	return string(out)
}
