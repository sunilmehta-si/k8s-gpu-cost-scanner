package analyze

import "fmt"

// idle flags pods whose GPUs did nothing: the 95th percentile of their activity stayed under IdleActivity for the
// time they held them. A serving pod that answered requests is left to the serving rules, which name the cause.
func (a *analysis) idle() {
	for _, p := range a.pods() {
		u := p.Usage
		if !u.Activity.Measured() || u.Coverage < a.t.MinCoverage || p.RunningHours < a.t.MinHours {
			continue
		}
		if u.Activity.P95 >= a.t.IdleActivity {
			continue
		}
		if e := p.Engine; e != nil && (e.Requests > 0 || e.GenerationTokens > 0 || e.Running.Max > 0) {
			continue
		}
		hours := a.unclaimed(p)
		if hours <= 0 {
			continue
		}
		ev := []string{
			fmt.Sprintf("held %s for %.1f hours of the window", gpusText(p), p.RunningHours),
			fmt.Sprintf("GPU activity: average %s, 95th percentile %s, peak %s (DCGM, %s of the time measured)",
				pct(u.Activity.Avg), pct(u.Activity.P95), pct(u.Activity.Max), pct(u.Coverage)),
		}
		if u.PowerWatts > 0 {
			ev = append(ev, fmt.Sprintf("average power draw %.0f W across its GPUs", u.PowerWatts))
		}
		if u.MemoryPeak > 0 {
			ev = append(ev, fmt.Sprintf("GPU memory peaked at %s of capacity: something was loaded, nothing ran on it", pct(u.MemoryPeak)))
		}
		a.claimPod(p, Finding{
			Rule: "GPU001", Slug: "idle-gpu", Title: "GPUs held but never used",
			Evidence: ev,
			Recommendation: "Find out why the pod holds GPUs it does not use (a finished job waiting to be cleaned up, " +
				"a notebook left running, a GPU request on a container that needs none) and release them: delete the " +
				"pod, set a TTL on finished Jobs, or cull idle notebooks.",
		}, hours)
	}
}

func gpusText(p interface{ GPUs() float64 }) string {
	n := p.GPUs()
	if n == 1 {
		return "1 GPU"
	}
	if n == 0 {
		return "MIG slices"
	}
	return fmt.Sprintf("%g GPUs", n)
}
