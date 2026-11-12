package fleet

import (
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestAttachVLLM(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["pod_gpu_requests"] = []prom.Series{
		{Labels: prom.Labels{"namespace": "ml", "pod": "llama-0", "resource": "nvidia_com_gpu"}, Points: points(ts, 2, 2, 2, 2)},
	}
	pod := func(extra prom.Labels) prom.Labels {
		l := prom.Labels{"namespace": "ml", "pod": "llama-0", "model_name": "meta-llama/Llama-3.1-8B-Instruct"}
		for k, v := range extra {
			l[k] = v
		}
		return l
	}
	s.Range["vllm_running"] = []prom.Series{
		// Two data-parallel engines: their requests add up.
		{Labels: pod(prom.Labels{"engine": "0"}), Points: points(ts, 0, 2, 4, 0, 9)},
		{Labels: pod(prom.Labels{"engine": "1"}), Points: points(ts, 0, 1, 2, 0, 9)},
		// The same engine scraped twice is not counted twice.
		{Labels: pod(prom.Labels{"engine": "1", "job": "dup"}), Points: points(ts, 0, 1, 2, 0, 9)},
	}
	s.Range["vllm_kv_usage"] = []prom.Series{
		{Labels: pod(prom.Labels{"engine": "0"}), Points: points(ts, 0, 0.2, 0.6, 0)},
		{Labels: pod(prom.Labels{"engine": "1"}), Points: points(ts, 0, 0.4, 0.2, 0)},
	}
	s.Range["vllm_awake"] = []prom.Series{{Labels: pod(nil), Points: points(ts, 1, 1, 1, 0)}}
	s.Instant["vllm_generation_tokens"] = []prom.Sample{
		{Labels: pod(prom.Labels{"engine": "0"}), Value: 1000},
		{Labels: pod(prom.Labels{"engine": "1"}), Value: 500},
	}
	s.Instant["vllm_requests"] = []prom.Sample{{Labels: pod(nil), Value: 12}}

	f := Build(s)
	e := f.Pods[PodKey{"ml", "llama-0"}].Engine
	if e == nil {
		t.Fatal("no engine attached")
	}
	if e.Kind != VLLM || e.Model != "meta-llama/Llama-3.1-8B-Instruct" {
		t.Errorf("engine = %+v", e)
	}
	// Running per held step: 0, 3, 6, 0 (the fifth point is after the pod stopped).
	if e.Running.Max != 6 || e.Running.N != 4 || e.BusyShare != 0.5 {
		t.Errorf("running = %+v, busy %v", e.Running, e.BusyShare)
	}
	// KV averaged across engines: 0, 0.3, 0.4, 0.
	if !close(e.KV.Max, 0.4) {
		t.Errorf("kv = %+v", e.KV)
	}
	if e.AwakeShare == nil || *e.AwakeShare != 0.75 {
		t.Errorf("awake = %v", e.AwakeShare)
	}
	if !e.HasCounts || e.GenerationTokens != 1500 || e.Requests != 12 {
		t.Errorf("counts = %+v", e)
	}
	if got := e.Timeline(); len(got) != 4 || got[0] != ts[0] {
		t.Errorf("timeline = %v", got)
	}
}

func TestSGLangRanksAreNotAdded(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["pod_gpu_requests"] = []prom.Series{
		{Labels: prom.Labels{"kubernetes_namespace": "x", "namespace": "ml", "pod": "sgl-0", "resource": "nvidia_com_gpu"}, Points: points(ts, 4, 4)},
	}
	s.Range["sglang_running"] = []prom.Series{
		{Labels: prom.Labels{"kubernetes_namespace": "ml", "kubernetes_pod_name": "sgl-0", "tp_rank": "0"}, Points: points(ts, 5, 7)},
		{Labels: prom.Labels{"kubernetes_namespace": "ml", "kubernetes_pod_name": "sgl-0", "tp_rank": "1"}, Points: points(ts, 5, 7)},
	}
	f := Build(s)
	e := f.Pods[PodKey{"ml", "sgl-0"}].Engine
	if e == nil || e.Kind != SGLang || e.Running.Max != 7 {
		t.Fatalf("engine = %+v", e)
	}
	if e.HasCounts || e.AwakeShare != nil {
		t.Errorf("counts or sleep state invented: %+v", e)
	}
}

func TestEngineOnPodWithoutGPUsIsIgnored(t *testing.T) {
	s, ts := newSnap(t)
	s.Range["vllm_running"] = []prom.Series{{Labels: prom.Labels{"namespace": "ml", "pod": "cpu-only"}, Points: points(ts, 1)}}
	if f := Build(s); len(f.Pods) != 0 {
		t.Errorf("pods = %v", f.Pods)
	}
}
