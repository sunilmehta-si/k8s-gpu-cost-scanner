## GPU cost report

1d window ending 2026-11-01 00:00 UTC, from `http://prometheus`.

| | |
|---|---|
| GPU-hours offered / requested | 481.7 / 421.6 (87.5%) |
| GPU activity | 37.2% |
| Cost of requested GPUs | $1,368.43 (100.0% priced) |
| **Recoverable waste** | **$750.64** · 221.4 GPU-hours |
| Idle time in serving pods | 21.0 GPU-hours (advice) |
| Findings | 7 findings (4 high, 1 medium, 1 low, 1 info) |

### Findings

| Severity | Rule | Finding | Target | Waste |
|---|---|---|---|---|
| high | GPU005 | More replicas than the peak load needed | `ml/Deployment/llama-70b` | $385.33 (96.3 GPU-h) |
| high | GPU004 | GPUs no pod requested | `node/gpu-h100-b` | $240.33 (60.1 GPU-h) |
| high | GPU002 | Model served no traffic | `ml/Deployment/legacy-mistral` | $16.86 (24.1 GPU-h) |
| high | GPU001 | GPUs held but never used | `research/Pod/notebook-ana` | $96.33 (24.1 GPU-h) |
| medium | GPU003 | Engine asleep while holding GPUs | `rl/Deployment/policy-server` | $11.78 (16.8 GPU-h) |
| low | GPU006 | Serving pod busy for a small share of its time | `ml/Deployment/qwen-7b` | 21.0 idle GPU-h |
| info | GPU007 | Work much smaller than the GPU | `search/Deployment/embedder` | advice |

<details><summary>GPU005 More replicas than the peak load needed: <code>ml/Deployment/llama-70b</code></summary>

- ran up to 3 replicas of meta-llama/Llama-3.3-70B-Instruct
- the KV cache in use across all replicas peaked at 105.0% of one replica's cache
- at most 2 replicas were needed to keep each under 80.0% KV use, at the busiest step
- no request waited in a queue at any replica

**Fix:** Cap the workload at 2 replicas, or let an autoscaler follow the load: scale on KV cache use or requests in the system, which track an LLM server's capacity, not on CPU. Keep a replica of headroom if the peak in this window was not the busiest you expect.

</details>

<details><summary>GPU004 GPUs no pod requested: <code>node/gpu-h100-b</code></summary>

- offered 192.7 GPU-hours (8 GPUs at most); pods requested 132.6 GPU-hours, 5.5 GPUs on average
- 31.2% of the node's GPU time (60.1 GPU-hours) no pod requested

**Fix:** Let the cluster autoscaler remove the node or shrink its node pool when GPUs sit unrequested, or pack GPU pods onto fewer nodes: KAI Scheduler bin-packs GPUs by default; Volcano needs its binpack plugin weighted for nvidia.com/gpu; the default scheduler needs MostAllocated scoring for it. If pods are pending at the same time, the free GPUs are split across nodes in pieces too small for them.

</details>

<details><summary>GPU002 Model served no traffic: <code>ml/Deployment/legacy-mistral</code></summary>

- held 1 GPU for 24.1 hours of the window
- vLLM (mistralai/Mistral-7B-Instruct-v0.3) finished 0 requests and generated 0 tokens in the window
- GPU activity averaged 0.0%

**Fix:** Scale the model to zero while nothing calls it (KServe minReplicas: 0, a KEDA ScaledObject with minReplicaCount: 0, or Knative), or remove it if nothing should. Check first that clients are not failing to reach it: a broken route also shows as no traffic.

</details>

<details><summary>GPU001 GPUs held but never used: <code>research/Pod/notebook-ana</code></summary>

- held 1 GPU for 24.1 hours of the window
- GPU activity: average 0.0%, 95th percentile 0.0%, peak 0.0% (DCGM, 100.0% of the time measured)
- average power draw 60 W across its GPUs
- GPU memory peaked at 2.0% of capacity: something was loaded, nothing ran on it

**Fix:** Find out why the pod holds GPUs it does not use (a finished job waiting to be cleaned up, a notebook left running, a GPU request on a container that needs none) and release them: delete the pod, set a TTL on finished Jobs, or cull idle notebooks.

</details>

<details><summary>GPU003 Engine asleep while holding GPUs: <code>rl/Deployment/policy-server</code></summary>

- held 1 GPU for 24.1 hours of the window
- vLLM engine for rl/policy-8b was asleep 69.9% of that time (vllm:engine_sleep_state)
- GPU activity averaged 0.0% while the engine slept

**Fix:** Sleep mode releases GPU memory, not the GPU. If the model sleeps this much, scale the pod to zero and back instead, or run the models that take turns sleeping in one pod so they share the GPU.

</details>

<details><summary>GPU006 Serving pod busy for a small share of its time: <code>ml/Deployment/qwen-7b</code></summary>

- held 1 GPU for 24.1 hours of the window
- vLLM (Qwen/Qwen2.5-7B-Instruct) had a request running in 12.8% of the steps, at most 2 at once
- finished 1075 requests and generated 236444 tokens

**Fix:** If the traffic comes in bursts, scale to zero between them (KServe, KEDA or Knative), weighing the model's cold start against the idle hours. Otherwise share the GPU: run several small models on one GPU, use MIG slices, or route this model's traffic to a pool that serves others.

</details>

<details><summary>GPU007 Work much smaller than the GPU: <code>search/Deployment/embedder</code></summary>

- GPU activity: average 12.0%, 95th percentile 12.5% over 24.1 hours

**Fix:** Try a MIG slice or a smaller GPU type for this workload, or share the GPU with another workload. Check the model's weights fit first: GPU memory use does not show it, because inference servers reserve most of the memory up front.

</details>

### Models

| Model | Workload | Replicas | GPU-hours | Cost | Activity | Busy | Output tokens | Per 1M tokens |
|---|---|---|---|---|---|---|---|---|
| `meta-llama/Llama-3.3-70B-Instruct` | ml/Deployment/llama-70b | 3.0 (max 3) | 289.0 | $1,156.00 | 47.4% | 100.0% | 36.3M | $31.87 |
| `mistralai/Mistral-7B-Instruct-v0.3` | ml/Deployment/legacy-mistral | 1.0 (max 1) | 24.1 | $16.86 | 0.0% | 0.0% | 0 | no output |
| `rl/policy-8b` | rl/Deployment/policy-server | 1.0 (max 1) | 24.1 | $16.86 | 15.1% | 30.1% | 1.9M | $8.67 |
| `Qwen/Qwen2.5-7B-Instruct` | ml/Deployment/qwen-7b | 1.0 (max 1) | 24.1 | $16.86 | 7.7% | 12.8% | 236.4K | $71.30 |

### Namespaces

| Namespace | Pods | GPU-hours | Cost | Activity | Waste |
|---|---|---|---|---|---|
| ml | 5 | 337.2 | $1,189.72 | 41.2% | 120.4 GPU-h |
| research | 1 | 24.1 | $96.33 | 0.0% | 24.1 GPU-h |
| rl | 1 | 24.1 | $16.86 | 15.1% | 16.8 GPU-h |
| search | 1 | 24.1 | $16.86 | 12.0% | 0.0 GPU-h |
| training | 1 | 12.2 | $48.67 | 91.9% | 0.0 GPU-h |

<sub>gpucost VERSION · thresholds: idle under 5.0% activity at the 95th percentile, replicas sized to 80.0% KV cache use</sub>
