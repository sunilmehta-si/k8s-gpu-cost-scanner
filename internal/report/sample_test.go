package report

import (
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

func sampleReport() *analyze.Report {
	end := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	return &analyze.Report{
		Tool: "gpucost test", Source: "http://prometheus.monitoring:9090", Currency: "USD",
		Window:     snapshot.Window{Start: end.Add(-24 * time.Hour), End: end, Step: snapshot.Duration(5 * time.Minute)},
		Generated:  end,
		Thresholds: analyze.Defaults(),
		Summary: analyze.Summary{
			Nodes: 2, Pods: 3, OfferedGPUHours: 384, RequestedGPUHours: 240, MeasuredGPUHours: 240, Activity: 0.31,
			Cost: 960, PricedShare: 1, OfferedCost: 1536, WasteGPUHours: 168, WasteCost: 672, IdleGPUHours: 20,
			Findings: map[analyze.Severity]int{analyze.High: 2, analyze.Info: 1},
		},
		Findings: []analyze.Finding{
			{Rule: "GPU004", Slug: "stranded-gpus", Title: "GPUs no pod requested", Severity: analyze.High, Target: "node/gpu-b",
				Node: "gpu-b", Evidence: []string{"offered 192.0 GPU-hours", "75.0% of the node's GPU time no pod requested"},
				WasteGPUHours: 144, WasteCost: 576, Priced: true, Recommendation: "Let the cluster autoscaler remove the node."},
			{Rule: "GPU001", Slug: "idle-gpu", Title: "GPUs held but never used", Severity: analyze.High, Target: "research/notebook-0",
				Namespace: "research", Pods: []string{"notebook-0"}, Evidence: []string{"held 1 GPU for 24.0 hours"},
				WasteGPUHours: 24, WasteCost: 96, Priced: true, Recommendation: "Cull idle notebooks."},
			{Rule: "GPU007", Slug: "oversized-gpu", Title: "Work much smaller than the GPU", Severity: analyze.Info,
				Target: "ml/small-0", Evidence: []string{"GPU activity 12%"}, Recommendation: "Try a MIG slice."},
		},
		Models: []analyze.ModelCost{{
			Namespace: "ml", Workload: "Deployment/llama", Model: "meta-llama/Llama-3.1-8B-Instruct", Engine: "vllm",
			Replicas: 2, MaxReplicas: 3, GPUHours: 48, Cost: 192, Priced: true, Activity: 0.41, Measured: true, BusyShare: 0.88,
			GenerationTokens: 12_300_000, Requests: 41000, CostPerMillionTokens: 15.61, GPUHoursPerMillionTokens: 3.9,
		}},
		Workloads: []analyze.WorkloadCost{
			{Namespace: "ml", Kind: "Deployment", Name: "llama", Pods: 3, GPUHours: 48, Cost: 192, Priced: true, Activity: 0.41, Measured: true},
			{Namespace: "research", Kind: "Pod", Name: "notebook-0", Pods: 1, GPUHours: 24, Cost: 96, Priced: true, Measured: true, WasteGPUHours: 24},
		},
		BlindSpots: []analyze.BlindSpot{{Code: "B001", Title: "GPU time without activity data", Detail: "Run the exporter.", GPUHours: 12, Share: 0.05}},
		Queries:    map[string]string{"gpu_activity": "DCGM_FI_PROF_GR_ENGINE_ACTIVE"},
	}
}
