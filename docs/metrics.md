# Metrics reference

<!-- Generated from internal/signals/signals.go by `go test ./internal/signals -update-docs`. Do not edit by hand. -->

Everything gpucost asks Prometheus for. For each signal the first variant whose metrics the server has is used; `gpucost doctor` says which. `$WINDOW` is the window and `$STEP` the step, in seconds. Range signals are sampled at every step of the window; instant signals are read once at its end.

## kube-state-metrics

Which pods held GPUs, for how long, on which node, for which workload. Required.

### `pod_gpu_requests` (range, GPUs or MIG slices)

GPUs (or MIG slices) each running pod requests, by node and resource. Duplicate kube-state-metrics replicas are collapsed before summing the pod's containers.

Needs `kube_pod_container_resource_requests`, `kube_pod_status_phase`:

```promql
sum by (namespace, pod, node, resource) (max by (namespace, pod, container, node, resource) (kube_pod_container_resource_requests{resource=~"nvidia_com_gpu|nvidia_com_mig_.+"})) * on (namespace, pod) group_left () (max by (namespace, pod) (kube_pod_status_phase{phase="Running"}) == 1)
```

### `node_gpu_allocatable` (range, GPUs or MIG slices)

GPUs (or MIG slices) each node offers to pods.

Needs `kube_node_status_allocatable`:

```promql
max by (node, resource) (kube_node_status_allocatable{resource=~"nvidia_com_gpu|nvidia_com_mig_.+"})
```

### `pod_owner` (instant)

The controller that owns each pod, seen at any time in the window.

Needs `kube_pod_owner`:

```promql
max by (namespace, pod, owner_kind, owner_name) (last_over_time(kube_pod_owner{owner_is_controller="true"}[$WINDOW]))
```

### `replicaset_owner` (instant)

The Deployment (or other controller) that owns each ReplicaSet, to name a pod's workload.

Needs `kube_replicaset_owner`:

```promql
max by (namespace, replicaset, owner_kind, owner_name) (last_over_time(kube_replicaset_owner[$WINDOW]))
```

### `node_labels` (instant)

Node labels kube-state-metrics is allowed to export: the GPU product and instance type when the DCGM exporter does not name them, and GPU Feature Discovery's sharing labels, so a time-sliced GPU's replicas count as shares of one GPU.

Needs `kube_node_labels`:

```promql
max by (node, label_nvidia_com_gpu_product, label_cloud_google_com_gke_accelerator, label_node_kubernetes_io_instance_type, label_karpenter_k8s_aws_instance_gpu_name, label_nvidia_com_gpu_count, label_nvidia_com_gpu_replicas, label_nvidia_com_gpu_sharing_strategy) (last_over_time(kube_node_labels[$WINDOW]))
```

## dcgm-exporter

How busy each GPU was, with NVIDIA's dcgm-exporter and its Kubernetes pod mapping on. Required to find idle GPUs.

### `gpu_activity` (range, ratio 0-1)

How busy each GPU (or MIG instance) is. The graphics-engine activity profiling field when the exporter has it; otherwise the coarser utilization field, which counts any kernel running as busy and so reads higher.

Variant 1 needs `DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO`:

```promql
DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO
```

Variant 2 needs `DCGM_FI_PROF_GR_ENGINE_ACTIVE`:

```promql
DCGM_FI_PROF_GR_ENGINE_ACTIVE
```

Variant 3 needs `DCGM_FI_DEV_GPU_UTIL_RATIO`:

```promql
DCGM_FI_DEV_GPU_UTIL_RATIO
```

Variant 4 needs `DCGM_FI_DEV_GPU_UTIL`:

```promql
DCGM_FI_DEV_GPU_UTIL / 100
```

### `gpu_memory_used_bytes` (range, bytes)

GPU memory in use. Note that vLLM and SGLang reserve most of the memory at start, so this shows the reservation, not the model's need.

Variant 1 needs `DCGM_FI_DEV_FB_USED_BYTES`:

```promql
DCGM_FI_DEV_FB_USED_BYTES
```

Variant 2 needs `DCGM_FI_DEV_FB_USED`:

```promql
DCGM_FI_DEV_FB_USED * 1048576
```

### `gpu_memory_total_bytes` (instant, bytes)

Each GPU's memory capacity.

Variant 1 needs `DCGM_FI_DEV_FB_CAPACITY_BYTES`:

```promql
max_over_time(DCGM_FI_DEV_FB_CAPACITY_BYTES[$WINDOW])
```

Variant 2 needs `DCGM_FI_DEV_FB_USED`, `DCGM_FI_DEV_FB_FREE`:

```promql
max_over_time((DCGM_FI_DEV_FB_USED + DCGM_FI_DEV_FB_FREE)[$WINDOW:$STEP]) * 1048576
```

### `gpu_power_watts` (instant, watts)

Each GPU's average power draw over the window.

Variant 1 needs `DCGM_FI_DEV_BOARD_POWER_WATTS`:

```promql
avg_over_time(DCGM_FI_DEV_BOARD_POWER_WATTS[$WINDOW])
```

Variant 2 needs `DCGM_FI_DEV_POWER_USAGE`:

```promql
avg_over_time(DCGM_FI_DEV_POWER_USAGE[$WINDOW])
```

## vllm

What each vLLM server did. Needed for model costs and the serving rules.

### `vllm_running` (range, requests)

Requests in vLLM's running batch.

Needs `vllm:num_requests_running`:

```promql
vllm:num_requests_running
```

### `vllm_waiting` (range, requests)

Requests waiting in vLLM's queue.

Needs `vllm:num_requests_waiting`:

```promql
vllm:num_requests_waiting
```

### `vllm_kv_usage` (range, ratio 0-1)

Share of vLLM's KV cache in use.

Variant 1 needs `vllm:kv_cache_usage_perc`:

```promql
vllm:kv_cache_usage_perc
```

Variant 2 needs `vllm:gpu_cache_usage_perc`:

```promql
vllm:gpu_cache_usage_perc
```

### `vllm_awake` (range, 0 or 1)

Whether the vLLM engine is awake (1) or in sleep mode (0).

Needs `vllm:engine_sleep_state`:

```promql
vllm:engine_sleep_state{sleep_state="awake"}
```

### `vllm_generation_tokens` (instant, tokens)

Output tokens vLLM generated in the window.

Needs `vllm:generation_tokens_total`:

```promql
increase(vllm:generation_tokens_total[$WINDOW])
```

### `vllm_prompt_tokens` (instant, tokens)

Prompt tokens vLLM processed in the window.

Needs `vllm:prompt_tokens_total`:

```promql
increase(vllm:prompt_tokens_total[$WINDOW])
```

### `vllm_requests` (instant, requests)

Requests vLLM finished in the window, for any reason.

Needs `vllm:request_success_total`:

```promql
sum without (finished_reason) (increase(vllm:request_success_total[$WINDOW]))
```

## sglang

What each SGLang server did. Needed for model costs and the serving rules.

### `sglang_running` (range, requests)

Requests SGLang is running.

Needs `sglang:num_running_reqs`:

```promql
sglang:num_running_reqs
```

### `sglang_waiting` (range, requests)

Requests waiting in SGLang's queue.

Needs `sglang:num_queue_reqs`:

```promql
sglang:num_queue_reqs
```

### `sglang_kv_usage` (range, ratio 0-1)

Share of SGLang's token (KV) pool in use.

Needs `sglang:token_usage`:

```promql
sglang:token_usage
```

### `sglang_generation_tokens` (instant, tokens)

Output tokens SGLang generated in the window.

Needs `sglang:generation_tokens_total`:

```promql
sum without (is_streaming) (increase(sglang:generation_tokens_total[$WINDOW]))
```

### `sglang_prompt_tokens` (instant, tokens)

Prompt tokens SGLang processed in the window.

Needs `sglang:prompt_tokens_total`:

```promql
sum without (is_streaming) (increase(sglang:prompt_tokens_total[$WINDOW]))
```

### `sglang_requests` (instant, requests)

Requests SGLang finished in the window.

Needs `sglang:num_requests_total`:

```promql
sum without (is_streaming) (increase(sglang:num_requests_total[$WINDOW]))
```

## opencost

GPU prices per node, when you run OpenCost. Optional: prices can come from a file instead.

### `opencost_gpu_hourly` (instant, currency per GPU-hour)

OpenCost's hourly price of one GPU on each node, averaged over the window.

Needs `node_gpu_hourly_cost`:

```promql
avg by (node, instance_type) (avg_over_time(node_gpu_hourly_cost[$WINDOW]))
```
