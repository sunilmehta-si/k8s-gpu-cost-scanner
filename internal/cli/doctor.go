package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// check is one line of the doctor's report.
type check struct {
	status string // ok, warn, fail or skip
	text   string
	fix    string
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var conn connFlags
	conn.register(fs)
	snapPath := fs.String("snapshot", "", "check a recorded snapshot instead of querying Prometheus")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	var snap *snapshot.Snapshot
	var err error
	if *snapPath != "" {
		snap, err = snapshot.Read(*snapPath)
	} else {
		snap, err = conn.collectSnapshot(ctx, stderr, false)
	}
	if err != nil {
		return err
	}
	checks := diagnose(snap, fleet.Build(snap))
	fmt.Fprintf(stdout, "gpucost doctor · %s · %s window\n\n", snap.Source, snap.Window.Length())
	failed := 0
	for _, c := range checks {
		mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗", "skip": "-"}[c.status]
		fmt.Fprintf(stdout, "  %s %s\n", mark, c.text)
		if c.fix != "" {
			for _, line := range wrapText(c.fix, 92) {
				fmt.Fprintf(stdout, "      %s\n", line)
			}
		}
		if c.status == "fail" {
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(stdout, "\n%d required source missing or incomplete: gpucost scan would miss waste.\n", failed)
		return exitErr{fmt.Errorf("%d checks failed", failed), exitError}
	}
	fmt.Fprintf(stdout, "\nReady: gpucost scan has what it needs.\n")
	return nil
}

// diagnose says, source by source, what the snapshot has and what is missing, and how to fix it.
func diagnose(s *snapshot.Snapshot, f *fleet.Fleet) []check {
	var out []check
	add := func(status, text, fix string) { out = append(out, check{status, text, fix}) }

	// kube-state-metrics.
	switch {
	case len(f.Pods) > 0:
		add("ok", fmt.Sprintf("kube-state-metrics: GPU requests for %d pods on %d nodes", len(f.Pods), len(f.Nodes)), "")
	case s.Has("kube_pod_container_resource_requests"):
		add("warn", "kube-state-metrics: running, but no pod requested nvidia.com/gpu in the window",
			"If GPU pods ran, check their resource name: gpucost counts nvidia.com/gpu and nvidia.com/mig-* requests.")
	default:
		add("fail", "kube-state-metrics: no kube_pod_container_resource_requests",
			"Install kube-state-metrics (it comes with kube-prometheus-stack) and scrape it. Without it gpucost cannot tell which pods held GPUs.")
	}
	if len(f.Pods) > 0 {
		owned := 0
		for _, p := range f.Pods {
			if p.Workload.Kind != "Pod" {
				owned++
			}
		}
		if owned == 0 {
			add("warn", "kube-state-metrics: no pod owners, so findings name pods instead of Deployments",
				"Scrape kube_pod_owner and kube_replicaset_owner; both are on by default in kube-state-metrics.")
		} else {
			add("ok", fmt.Sprintf("kube-state-metrics: workload found for %d of %d GPU pods", owned, len(f.Pods)), "")
		}
	}
	if len(s.Range["node_gpu_allocatable"]) == 0 {
		add("warn", "kube-state-metrics: no allocatable GPUs per node, so GPUs no pod requested cannot be found",
			"Scrape kube_node_status_allocatable; it is on by default.")
	}

	// DCGM exporter.
	activity := s.Queries["gpu_activity"]
	hasActivity := len(s.Range["gpu_activity"]) > 0
	switch {
	case !hasActivity && activity == "":
		add("fail", "DCGM exporter: no GPU activity metrics",
			"Install NVIDIA's dcgm-exporter (the GPU Operator runs it) and scrape it. Without it no idle GPU can be found.")
	case strings.Contains(activity, "UTIL_RATIO") && strings.Contains(activity, "GR_ENGINE"):
		add("ok", "DCGM exporter 4.9 or later: graphics-engine activity (DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO)", "")
	case strings.Contains(activity, "GR_ENGINE"):
		add("ok", "DCGM exporter before 4.9: graphics-engine activity (DCGM_FI_PROF_GR_ENGINE_ACTIVE)", "")
	case activity == "":
		add("ok", "DCGM exporter: GPU activity found", "")
	default:
		add("warn", "DCGM exporter: only the coarse GPU utilization field, which counts any running kernel as busy",
			"Enable the profiling field DCGM_FI_PROF_GR_ENGINE_ACTIVE (DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO from 4.9) in the exporter's metrics list for a truer activity.")
	}
	if hasActivity && len(f.Pods) > 0 {
		total, measured := 0.0, 0.0
		pods := 0
		for _, p := range f.Pods {
			eq, _ := f.GPUEquivalents(p)
			total += eq
			measured += eq * p.Usage.Coverage
			if p.Usage.Activity.Measured() {
				pods++
			}
		}
		share := 0.0
		if total > 0 {
			share = measured / total
		}
		switch {
		case pods == 0:
			add("fail", "DCGM exporter: no GPU tied to a pod",
				"Turn on the exporter's Kubernetes pod mapping (--kubernetes, or kubernetes labels in the 4.9 Helm chart). If its pod labels arrive as exported_pod, gpucost reads them.")
		case share < 0.9:
			add("warn", fmt.Sprintf("DCGM exporter: activity for %d of %d GPU pods, %.1f%% of GPU-hours", pods, len(f.Pods), share*100),
				"Run the exporter on every GPU node; pods on nodes without it cannot be judged.")
		default:
			add("ok", fmt.Sprintf("DCGM exporter: activity for %d of %d GPU pods, %.1f%% of GPU-hours", pods, len(f.Pods), share*100), "")
		}
	}

	// Serving engines.
	serving := map[string]int{}
	for _, p := range f.Pods {
		if p.Engine != nil {
			serving[p.Engine.Kind]++
		}
	}
	for _, e := range []struct{ kind, name, metric string }{{fleet.VLLM, "vLLM", "vllm:num_requests_running"}, {fleet.SGLang, "SGLang", "sglang:num_running_reqs"}} {
		switch {
		case serving[e.kind] > 0:
			add("ok", fmt.Sprintf("%s: serving metrics for %d GPU pods", e.name, serving[e.kind]), "")
		case s.Has(e.metric):
			add("warn", fmt.Sprintf("%s: metrics found, but not labelled with the pod serving them", e.name),
				"Scrape the servers with a PodMonitor or ServiceMonitor so each series carries namespace and pod.")
		default:
			add("skip", fmt.Sprintf("%s: not found (fine if you do not run it; model costs and the serving rules need it)", e.name), "")
		}
	}

	// Products and prices.
	unknown := 0
	for _, n := range f.Nodes {
		if n.Product == gpu.Unknown {
			unknown++
		}
	}
	if unknown > 0 {
		add("warn", fmt.Sprintf("GPU model unknown on %d of %d nodes", unknown, len(f.Nodes)),
			"The DCGM exporter's modelName names it; so does the nvidia.com/gpu.product node label if kube-state-metrics exports it (--metric-labels-allowlist=nodes=[nvidia.com/gpu.product]).")
	}
	if s.Has("node_gpu_hourly_cost") {
		add("ok", "OpenCost: GPU prices per node", "")
	} else {
		add("skip", "OpenCost: not found; give prices with --prices or --price, or costs are in GPU-hours", "")
	}
	for _, n := range s.Notes {
		if strings.Contains(n, "query failed") || strings.Contains(n, "warning") {
			add("warn", n, "")
		}
	}
	return out
}

func wrapText(s string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
