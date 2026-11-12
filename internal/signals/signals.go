// Package signals is the catalogue of everything gpucost asks Prometheus for, and the PromQL for each.
//
// Each signal has variants for the metric names different versions export. The DCGM exporter renamed its
// fields and changed their units in 4.9.0 (DCGM_FI_PROF_GR_ENGINE_ACTIVE became
// DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, DCGM_FI_DEV_FB_USED in MiB became DCGM_FI_DEV_FB_USED_BYTES), and vLLM
// renamed vllm:gpu_cache_usage_perc to vllm:kv_cache_usage_perc. The first variant whose metrics the server has
// is used, and every variant returns the same unit, so the analysis never sees the difference.
package signals

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind says whether a signal is read as a range series over the window or as one value at its end.
type Kind int

const (
	// Range signals are sampled at every step of the window.
	Range Kind = iota
	// Instant signals are evaluated once, at the window's end, usually over the whole window.
	Instant
)

func (k Kind) String() string {
	if k == Range {
		return "range"
	}
	return "instant"
}

// Variant is one way to compute a signal, usable when the server has every metric in Needs.
type Variant struct {
	Needs []string
	// Query is PromQL with $WINDOW for the window's length and $STEP for the step, both in seconds.
	Query string
}

// Signal is one thing gpucost measures.
type Signal struct {
	Name     string
	Kind     Kind
	Source   string
	Unit     string
	Doc      string
	Variants []Variant
}

// Sources group signals by the exporter that provides them.
const (
	KubeStateMetrics = "kube-state-metrics"
	DCGM             = "dcgm-exporter"
	VLLM             = "vllm"
	SGLang           = "sglang"
	OpenCost         = "opencost"
)

const gpuResources = `resource=~"nvidia_com_gpu|nvidia_com_mig_.+"`

// Catalog lists every signal. Names are stable: they are the keys of a snapshot.
var Catalog = []Signal{
	{
		Name: "pod_gpu_requests", Kind: Range, Source: KubeStateMetrics, Unit: "GPUs or MIG slices",
		Doc: "GPUs (or MIG slices) each running pod requests, by node and resource. Duplicate kube-state-metrics " +
			"replicas are collapsed before summing the pod's containers.",
		Variants: []Variant{{
			Needs: []string{"kube_pod_container_resource_requests", "kube_pod_status_phase"},
			Query: `sum by (namespace, pod, node, resource) (max by (namespace, pod, container, node, resource) (kube_pod_container_resource_requests{` + gpuResources + `})) * on (namespace, pod) group_left () (max by (namespace, pod) (kube_pod_status_phase{phase="Running"}) == 1)`,
		}},
	},
	{
		Name: "node_gpu_allocatable", Kind: Range, Source: KubeStateMetrics, Unit: "GPUs or MIG slices",
		Doc: "GPUs (or MIG slices) each node offers to pods.",
		Variants: []Variant{{
			Needs: []string{"kube_node_status_allocatable"},
			Query: `max by (node, resource) (kube_node_status_allocatable{` + gpuResources + `})`,
		}},
	},
	{
		Name: "pod_owner", Kind: Instant, Source: KubeStateMetrics,
		Doc: "The controller that owns each pod, seen at any time in the window.",
		Variants: []Variant{{
			Needs: []string{"kube_pod_owner"},
			Query: `max by (namespace, pod, owner_kind, owner_name) (last_over_time(kube_pod_owner{owner_is_controller="true"}[$WINDOW]))`,
		}},
	},
	{
		Name: "replicaset_owner", Kind: Instant, Source: KubeStateMetrics,
		Doc: "The Deployment (or other controller) that owns each ReplicaSet, to name a pod's workload.",
		Variants: []Variant{{
			Needs: []string{"kube_replicaset_owner"},
			Query: `max by (namespace, replicaset, owner_kind, owner_name) (last_over_time(kube_replicaset_owner[$WINDOW]))`,
		}},
	},
	{
		Name: "node_labels", Kind: Instant, Source: KubeStateMetrics,
		Doc: "Node labels kube-state-metrics is allowed to export: the GPU product and instance type when the " +
			"DCGM exporter does not name them, and GPU Feature Discovery's sharing labels, so a time-sliced GPU's " +
			"replicas count as shares of one GPU.",
		Variants: []Variant{{
			Needs: []string{"kube_node_labels"},
			Query: `max by (node, label_nvidia_com_gpu_product, label_cloud_google_com_gke_accelerator, label_node_kubernetes_io_instance_type, label_karpenter_k8s_aws_instance_gpu_name, label_nvidia_com_gpu_count, label_nvidia_com_gpu_replicas, label_nvidia_com_gpu_sharing_strategy) (last_over_time(kube_node_labels[$WINDOW]))`,
		}},
	},
	{
		Name: "gpu_activity", Kind: Range, Source: DCGM, Unit: "ratio 0-1",
		Doc: "How busy each GPU (or MIG instance) is. The graphics-engine activity profiling field when the " +
			"exporter has it; otherwise the coarser utilization field, which counts any kernel running as busy " +
			"and so reads higher.",
		Variants: []Variant{
			{Needs: []string{"DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO"}, Query: `DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO`},
			{Needs: []string{"DCGM_FI_PROF_GR_ENGINE_ACTIVE"}, Query: `DCGM_FI_PROF_GR_ENGINE_ACTIVE`},
			{Needs: []string{"DCGM_FI_DEV_GPU_UTIL_RATIO"}, Query: `DCGM_FI_DEV_GPU_UTIL_RATIO`},
			{Needs: []string{"DCGM_FI_DEV_GPU_UTIL"}, Query: `DCGM_FI_DEV_GPU_UTIL / 100`},
		},
	},
	{
		Name: "gpu_memory_used_bytes", Kind: Range, Source: DCGM, Unit: "bytes",
		Doc: "GPU memory in use. Note that vLLM and SGLang reserve most of the memory at start, so this shows the reservation, not the model's need.",
		Variants: []Variant{
			{Needs: []string{"DCGM_FI_DEV_FB_USED_BYTES"}, Query: `DCGM_FI_DEV_FB_USED_BYTES`},
			{Needs: []string{"DCGM_FI_DEV_FB_USED"}, Query: `DCGM_FI_DEV_FB_USED * 1048576`},
		},
	},
	{
		Name: "gpu_memory_total_bytes", Kind: Instant, Source: DCGM, Unit: "bytes",
		Doc: "Each GPU's memory capacity.",
		Variants: []Variant{
			{Needs: []string{"DCGM_FI_DEV_FB_CAPACITY_BYTES"}, Query: `max_over_time(DCGM_FI_DEV_FB_CAPACITY_BYTES[$WINDOW])`},
			{Needs: []string{"DCGM_FI_DEV_FB_USED", "DCGM_FI_DEV_FB_FREE"}, Query: `max_over_time((DCGM_FI_DEV_FB_USED + DCGM_FI_DEV_FB_FREE)[$WINDOW:$STEP]) * 1048576`},
		},
	},
	{
		Name: "gpu_power_watts", Kind: Instant, Source: DCGM, Unit: "watts",
		Doc: "Each GPU's average power draw over the window.",
		Variants: []Variant{
			{Needs: []string{"DCGM_FI_DEV_BOARD_POWER_WATTS"}, Query: `avg_over_time(DCGM_FI_DEV_BOARD_POWER_WATTS[$WINDOW])`},
			{Needs: []string{"DCGM_FI_DEV_POWER_USAGE"}, Query: `avg_over_time(DCGM_FI_DEV_POWER_USAGE[$WINDOW])`},
		},
	},
	{
		Name: "vllm_running", Kind: Range, Source: VLLM, Unit: "requests",
		Doc:      "Requests in vLLM's running batch.",
		Variants: []Variant{{Needs: []string{"vllm:num_requests_running"}, Query: `vllm:num_requests_running`}},
	},
	{
		Name: "vllm_waiting", Kind: Range, Source: VLLM, Unit: "requests",
		Doc:      "Requests waiting in vLLM's queue.",
		Variants: []Variant{{Needs: []string{"vllm:num_requests_waiting"}, Query: `vllm:num_requests_waiting`}},
	},
	{
		Name: "vllm_kv_usage", Kind: Range, Source: VLLM, Unit: "ratio 0-1",
		Doc: "Share of vLLM's KV cache in use.",
		Variants: []Variant{
			{Needs: []string{"vllm:kv_cache_usage_perc"}, Query: `vllm:kv_cache_usage_perc`},
			{Needs: []string{"vllm:gpu_cache_usage_perc"}, Query: `vllm:gpu_cache_usage_perc`},
		},
	},
	{
		Name: "vllm_awake", Kind: Range, Source: VLLM, Unit: "0 or 1",
		Doc:      "Whether the vLLM engine is awake (1) or in sleep mode (0).",
		Variants: []Variant{{Needs: []string{"vllm:engine_sleep_state"}, Query: `vllm:engine_sleep_state{sleep_state="awake"}`}},
	},
	{
		Name: "vllm_generation_tokens", Kind: Instant, Source: VLLM, Unit: "tokens",
		Doc:      "Output tokens vLLM generated in the window.",
		Variants: []Variant{{Needs: []string{"vllm:generation_tokens_total"}, Query: `increase(vllm:generation_tokens_total[$WINDOW])`}},
	},
	{
		Name: "vllm_prompt_tokens", Kind: Instant, Source: VLLM, Unit: "tokens",
		Doc:      "Prompt tokens vLLM processed in the window.",
		Variants: []Variant{{Needs: []string{"vllm:prompt_tokens_total"}, Query: `increase(vllm:prompt_tokens_total[$WINDOW])`}},
	},
	{
		Name: "vllm_requests", Kind: Instant, Source: VLLM, Unit: "requests",
		Doc:      "Requests vLLM finished in the window, for any reason.",
		Variants: []Variant{{Needs: []string{"vllm:request_success_total"}, Query: `sum without (finished_reason) (increase(vllm:request_success_total[$WINDOW]))`}},
	},
	{
		Name: "sglang_running", Kind: Range, Source: SGLang, Unit: "requests",
		Doc:      "Requests SGLang is running.",
		Variants: []Variant{{Needs: []string{"sglang:num_running_reqs"}, Query: `sglang:num_running_reqs`}},
	},
	{
		Name: "sglang_waiting", Kind: Range, Source: SGLang, Unit: "requests",
		Doc:      "Requests waiting in SGLang's queue.",
		Variants: []Variant{{Needs: []string{"sglang:num_queue_reqs"}, Query: `sglang:num_queue_reqs`}},
	},
	{
		Name: "sglang_kv_usage", Kind: Range, Source: SGLang, Unit: "ratio 0-1",
		Doc:      "Share of SGLang's token (KV) pool in use.",
		Variants: []Variant{{Needs: []string{"sglang:token_usage"}, Query: `sglang:token_usage`}},
	},
	{
		Name: "sglang_generation_tokens", Kind: Instant, Source: SGLang, Unit: "tokens",
		Doc:      "Output tokens SGLang generated in the window.",
		Variants: []Variant{{Needs: []string{"sglang:generation_tokens_total"}, Query: `sum without (is_streaming) (increase(sglang:generation_tokens_total[$WINDOW]))`}},
	},
	{
		Name: "sglang_prompt_tokens", Kind: Instant, Source: SGLang, Unit: "tokens",
		Doc:      "Prompt tokens SGLang processed in the window.",
		Variants: []Variant{{Needs: []string{"sglang:prompt_tokens_total"}, Query: `sum without (is_streaming) (increase(sglang:prompt_tokens_total[$WINDOW]))`}},
	},
	{
		Name: "sglang_requests", Kind: Instant, Source: SGLang, Unit: "requests",
		Doc:      "Requests SGLang finished in the window.",
		Variants: []Variant{{Needs: []string{"sglang:num_requests_total"}, Query: `sum without (is_streaming) (increase(sglang:num_requests_total[$WINDOW]))`}},
	},
	{
		Name: "opencost_gpu_hourly", Kind: Instant, Source: OpenCost, Unit: "currency per GPU-hour",
		Doc:      "OpenCost's hourly price of one GPU on each node, averaged over the window.",
		Variants: []Variant{{Needs: []string{"node_gpu_hourly_cost"}, Query: `avg by (node, instance_type) (avg_over_time(node_gpu_hourly_cost[$WINDOW]))`}},
	},
}

// ByName returns the signal with the given name.
func ByName(name string) (Signal, bool) {
	for _, s := range Catalog {
		if s.Name == name {
			return s, true
		}
	}
	return Signal{}, false
}

// Metrics lists every metric name any variant needs, sorted: the names to ask the server about.
func Metrics() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range Catalog {
		for _, v := range s.Variants {
			for _, m := range v.Needs {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// Choose returns the first variant whose metrics are all present, or false when none is.
func (s Signal) Choose(present map[string]bool) (Variant, bool) {
	for _, v := range s.Variants {
		ok := true
		for _, m := range v.Needs {
			if !present[m] {
				ok = false
				break
			}
		}
		if ok {
			return v, true
		}
	}
	return Variant{}, false
}

// Render fills a variant's query with the window and step.
func (v Variant) Render(window, step time.Duration) string {
	q := strings.ReplaceAll(v.Query, "$WINDOW", seconds(window))
	return strings.ReplaceAll(q, "$STEP", seconds(step))
}

func seconds(d time.Duration) string { return fmt.Sprintf("%ds", int64(d/time.Second)) }
