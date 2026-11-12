package analyze

import "fmt"

// lowDuty notes serving pods that had traffic but were busy for little of their time. Their idle hours are not
// counted as waste: getting them back means scaling to zero between bursts or sharing the GPU, which trades cold
// starts or isolation for cost, a choice the report should not make for you.
func (a *analysis) lowDuty() {
	for _, p := range a.pods() {
		e := p.Engine
		if e == nil || p.RunningHours < a.t.MinHours || e.BusyShare >= a.t.LowDuty || !e.Running.Measured() {
			continue
		}
		if a.unclaimed(p) <= 0 {
			continue
		}
		eq, _ := a.f.GPUEquivalents(p)
		idle := eq * (1 - e.BusyShare)
		ev := []string{
			fmt.Sprintf("held %s for %.1f hours of the window", gpusText(p), p.RunningHours),
			fmt.Sprintf("%s had a request running in %s of the steps, at most %g at once", engineName(e), pct(e.BusyShare), e.Running.Max),
		}
		if e.HasCounts {
			ev = append(ev, fmt.Sprintf("finished %.0f requests and generated %.0f tokens", e.Requests, e.GenerationTokens))
		}
		sev := Info
		if idle >= a.t.MediumHours {
			sev = Low
		}
		a.findings = append(a.findings, Finding{
			Rule: "GPU006", Slug: "low-duty", Title: "Serving pod busy for a small share of its time",
			Severity: sev, Target: p.Workload.String(), Namespace: p.Key.Namespace, Pods: []string{p.Key.Name}, Node: p.Node,
			Evidence: ev, IdleGPUHours: idle,
			Recommendation: "If the traffic comes in bursts, scale to zero between them (KServe, KEDA or Knative), " +
				"weighing the model's cold start against the idle hours. Otherwise share the GPU: run several small " +
				"models on one GPU, use MIG slices, or route this model's traffic to a pool that serves others.",
		})
	}
}

// oversized notes pods doing real work on a GPU much bigger than the work: activity and, for serving pods, KV use
// both peaked below the Oversized threshold. Advice only: the right size depends on the model's memory, which the
// metrics do not show, since vLLM and SGLang reserve most of the GPU's memory whatever the model needs.
func (a *analysis) oversized() {
	for _, p := range a.pods() {
		u := p.Usage
		if p.GPUs() == 0 || p.RunningHours < a.t.MinHours || !u.Activity.Measured() || u.Coverage < a.t.MinCoverage {
			continue
		}
		if a.claimed[p.Key] > 0 || u.Activity.P95 < a.t.IdleActivity || u.Activity.P95 >= a.t.Oversized {
			continue
		}
		if e := p.Engine; e != nil && (!e.KV.Measured() || e.KV.Max >= a.t.Oversized || e.BusyShare < a.t.LowDuty) {
			continue
		}
		ev := []string{
			fmt.Sprintf("GPU activity: average %s, 95th percentile %s over %.1f hours", pct(u.Activity.Avg), pct(u.Activity.P95), p.RunningHours),
		}
		if e := p.Engine; e != nil {
			ev = append(ev, fmt.Sprintf("%s KV cache use peaked at %s", engineName(e), pct(e.KV.Max)))
		}
		a.findings = append(a.findings, Finding{
			Rule: "GPU007", Slug: "oversized-gpu", Title: "Work much smaller than the GPU",
			Severity: Info, Target: p.Workload.String(), Namespace: p.Key.Namespace, Pods: []string{p.Key.Name}, Node: p.Node,
			Evidence: ev,
			Recommendation: "Try a MIG slice or a smaller GPU type for this workload, or share the GPU with another " +
				"workload. Check the model's weights fit first: GPU memory use does not show it, because inference " +
				"servers reserve most of the memory up front.",
		})
	}
}
