# Findings reference

<!-- Generated from internal/analyze/rules.go by `go test ./internal/analyze -update-docs`. Do not edit by hand. -->

Rules run in this order, from the most specific cause to the most general. A GPU-hour one rule counts as waste is not counted again by a later one, so the findings' waste adds up to the total without double counting.

| Rule | Finding | Counted as waste |
|---|---|---|
| [GPU003](#gpu003-engine-asleep) | Engine asleep while holding GPUs | yes |
| [GPU002](#gpu002-no-traffic) | Model served no traffic | yes |
| [GPU001](#gpu001-idle-gpu) | GPUs held but never used | yes |
| [GPU005](#gpu005-excess-replicas) | More replicas than the peak load needed | yes |
| [GPU004](#gpu004-stranded-gpus) | GPUs no pod requested | yes |
| [GPU006](#gpu006-low-duty) | Serving pod busy for a small share of its time | no, advice |
| [GPU007](#gpu007-oversized-gpu) | Work much smaller than the GPU | no, advice |

## GPU003 engine-asleep

A vLLM engine spent a tenth of its time or more in sleep mode while its pod kept its GPUs.

**How it is found.** Reads vllm:engine_sleep_state{sleep_state="awake"} at each step the pod held GPUs. Waste is the pod's GPU-hours times the share of time asleep. Skipped when the GPU stayed busy anyway (average activity at least twice the idle threshold), since another engine in the pod may be using it.

**Fix.** Sleep mode frees GPU memory, not the GPU. Scale the pod to zero and back instead, or run the models that take turns in one pod so they share the GPU.

## GPU002 no-traffic

A vLLM or SGLang pod held GPUs for at least --min-hours and finished no request and generated no token in the window.

**How it is found.** Reads increase() of the engine's request and generation-token counters over the window. When the counters are missing, a pod whose running-requests gauge never left zero counts instead.

**Fix.** Scale it to zero while nothing calls it (KServe minReplicas: 0, KEDA minReplicaCount: 0, Knative) or remove it. First check clients are not failing to reach it: a broken route also shows as no traffic.

## GPU001 idle-gpu

A pod held GPUs for at least --min-hours and their activity stayed under --idle-activity (5%) at the 95th percentile.

**How it is found.** Uses the DCGM exporter's graphics-engine activity (or its coarser utilization field) averaged across the pod's GPUs at each step it held them; at least half of that time must be measured. Serving pods that answered requests are left to the serving rules. Waste is all the pod's GPU-hours no earlier rule counted.

**Fix.** Release the GPUs: delete finished pods, set ttlSecondsAfterFinished on Jobs, cull idle notebooks, and remove GPU requests from containers that do not use them.

## GPU005 excess-replicas

A serving workload ran more replicas than its busiest step needed.

**How it is found.** At each step, the replicas needed are the KV cache in use across all replicas divided by --kv-target (80%), rounded up. Replicas beyond the window's peak need, at any time, are waste. Workloads that queued a request at any replica, or lack KV cache data, are skipped.

**Fix.** Cap the replicas at the peak need, or autoscale on KV cache use or requests in the system rather than CPU. Keep a replica of headroom if this window's peak is not your busiest.

## GPU004 stranded-gpus

A node offered GPUs that no pod requested.

**How it is found.** The node's allocatable GPU-hours (kube_node_status_allocatable) minus those its running pods requested, with MIG slices counted as their share of a GPU. These hours belong to no pod, so they never overlap the pod rules.

**Fix.** Let the cluster autoscaler remove the node or shrink its pool, or pack GPU pods onto fewer nodes: KAI Scheduler bin-packs GPUs by default; Volcano needs its binpack plugin weighted for nvidia.com/gpu; the default scheduler needs MostAllocated scoring for it. Pending pods at the same time mean the free GPUs are fragmented.

## GPU006 low-duty

A serving pod with traffic had a request running in fewer than --low-duty (25%) of its steps.

**How it is found.** Reports the idle GPU-hours (GPU-hours times the share of steps with nothing running) apart from waste, because only scaling to zero or sharing the GPU recovers them, at the price of cold starts or isolation.

**Fix.** Scale to zero between bursts if the model's cold start is acceptable; otherwise share the GPU: several small models per GPU, MIG slices, or one pool serving several models.

## GPU007 oversized-gpu

A pod did real work, but its GPU activity and, for serving pods, its KV cache use both peaked under 30%.

**How it is found.** Advice only. GPU memory is not used to size the GPU, since vLLM and SGLang reserve most of it whatever the model needs.

**Fix.** Try a MIG slice or a smaller GPU type, after checking the model's weights fit; or share the GPU with another workload.

## Severity

Severity follows the GPU-hours a finding wastes, not its price, so it means the same with or without prices: high from 24 GPU-hours, medium from 4, low below that. Advice findings are info, or low when their idle GPU-hours reach 4.
