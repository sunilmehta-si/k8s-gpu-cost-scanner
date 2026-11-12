package analyze

import (
	"fmt"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
)

// noTraffic flags serving pods that held GPUs and answered nothing: no request finished and no token was generated
// in the window, or, when the engine's counters are missing, no request was ever running.
func (a *analysis) noTraffic() {
	for _, p := range a.pods() {
		e := p.Engine
		if e == nil || p.RunningHours < a.t.MinHours {
			continue
		}
		var silent bool
		var ev []string
		if e.HasCounts {
			silent = e.Requests == 0 && e.GenerationTokens == 0
			ev = append(ev, fmt.Sprintf("%s finished 0 requests and generated 0 tokens in the window", engineName(e)))
		} else {
			silent = e.Running.Measured() && e.Running.Max == 0
			ev = append(ev, fmt.Sprintf("%s never had a request running (its request and token counters were not collected)", engineName(e)))
		}
		if !silent {
			continue
		}
		hours := a.unclaimed(p)
		if hours <= 0 {
			continue
		}
		ev = append([]string{fmt.Sprintf("held %s for %.1f hours of the window", gpusText(p), p.RunningHours)}, ev...)
		if u := p.Usage; u.Activity.Measured() {
			ev = append(ev, fmt.Sprintf("GPU activity averaged %s", pct(u.Activity.Avg)))
		}
		a.claimPod(p, Finding{
			Rule: "GPU002", Slug: "no-traffic", Title: "Model served no traffic",
			Evidence: ev,
			Recommendation: "Scale the model to zero while nothing calls it (KServe minReplicas: 0, a KEDA ScaledObject " +
				"with minReplicaCount: 0, or Knative), or remove it if nothing should. Check first that clients are not " +
				"failing to reach it: a broken route also shows as no traffic.",
		}, hours)
	}
}

// asleep flags vLLM engines that spent much of the window in sleep mode while their pod kept the GPUs. Sleep mode
// frees GPU memory, but the GPU stays allocated to the pod, so nothing else can be scheduled on it.
func (a *analysis) asleep() {
	for _, p := range a.pods() {
		e := p.Engine
		if e == nil || e.AwakeShare == nil || p.RunningHours < a.t.MinHours {
			continue
		}
		asleep := 1 - *e.AwakeShare
		if asleep < 0.1 {
			continue
		}
		// Another engine in the same pod may use the GPU while this one sleeps; then the GPU is busy.
		if u := p.Usage; u.Activity.Measured() && u.Activity.Avg >= 2*a.t.IdleActivity {
			continue
		}
		eq, _ := a.f.GPUEquivalents(p)
		hours := eq * asleep
		ev := []string{
			fmt.Sprintf("held %s for %.1f hours of the window", gpusText(p), p.RunningHours),
			fmt.Sprintf("vLLM engine for %s was asleep %s of that time (vllm:engine_sleep_state)", modelText(e), pct(asleep)),
		}
		if u := p.Usage; u.Activity.Measured() {
			ev = append(ev, fmt.Sprintf("GPU activity averaged %s", pct(u.Activity.Avg)))
		}
		a.claimPod(p, Finding{
			Rule: "GPU003", Slug: "engine-asleep", Title: "Engine asleep while holding GPUs",
			Evidence: ev,
			Recommendation: "Sleep mode releases GPU memory, not the GPU. If the model sleeps this much, scale the pod " +
				"to zero and back instead, or run the models that take turns sleeping in one pod so they share the GPU.",
		}, hours)
	}
}

func engineName(e *fleet.Engine) string {
	name := "vLLM"
	if e.Kind == fleet.SGLang {
		name = "SGLang"
	}
	return name + " (" + modelText(e) + ")"
}

func modelText(e *fleet.Engine) string {
	if e.Model == "" {
		return "unnamed model"
	}
	return e.Model
}
