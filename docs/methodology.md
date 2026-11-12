# Methodology

How gpucost turns metrics into GPU-hours, costs and findings, and what it cannot see. Every number in a report
comes from the steps below; the PromQL for each signal is in the [metrics reference](metrics.md) and in every
report run with `--verbose`.

## The window

A scan covers one window, 24 hours by default (`--window`), ending now or at `--end`. The end is rounded down to
the step (`--step`, 5 minutes by default), so two scans of the same span line up. Range signals are sampled at every
step: a 24-hour window at 5 minutes has 289 points. If a long window would pass Prometheus' limit of 11,000 points
per series, the step is widened.

## GPU-hours

A pod's **GPU-hours** are the GPUs it requested, summed over every step it was running, times the step's length.
They come from kube-state-metrics: `kube_pod_container_resource_requests` for `nvidia.com/gpu` and
`nvidia.com/mig-*`, joined with `kube_pod_status_phase{phase="Running"}` so pending and finished pods count
nothing. Duplicate kube-state-metrics replicas are collapsed before a pod's containers are added up.

Summing per step, not averaging, matters: a pod that ran for one hour of a day counts one GPU-hour, not a day's
worth. The sum is a Riemann sum, so a pod's first and last steps can be off by up to one step each.

A **MIG slice** counts as its share of the physical GPU's compute: `3g.40gb` on an A100 or H100 is 3/7 of a GPU, on
an A30 a profile's slices are out of 4. When the GPU model is unknown, 7 is assumed and the report says so (blind
spot B004). These **GPU-equivalent hours** are the unit prices and waste are counted in.

A node's **offered GPU-hours** are its allocatable GPUs (`kube_node_status_allocatable`) summed the same way.

## Activity

GPU activity comes from NVIDIA's DCGM exporter. gpucost prefers the graphics-engine activity profiling field
(`DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO` from exporter 4.9, `DCGM_FI_PROF_GR_ENGINE_ACTIVE` before), the share of time
any kernel occupied the GPU's engine. When only the coarser utilization field is exported
(`DCGM_FI_DEV_GPU_UTIL_RATIO`, or `DCGM_FI_DEV_GPU_UTIL` in percent before 4.9), it is used and reads higher, since
it counts a GPU busy whenever one kernel runs.

Exporter 4.9.0 renamed its fields and changed their units (percent to ratio, MiB to bytes). gpucost queries
whichever names the server has and converts the old units, so reports read the same before and after an upgrade.

Each DCGM series is tied to a pod by the exporter's pod mapping. When Prometheus scrapes the exporter without
`honor_labels`, the workload's labels arrive as `exported_pod` and `exported_namespace`, and plain `pod` and
`namespace` name the exporter itself; gpucost reads the exported labels first, then `pod`, then the older
`pod_name`, and only counts a sample for a pod that requested GPUs, so the exporter's own pod is never taken for a
workload.

A pod's activity at a step is the average over its GPUs; its **activity stats** (average, 95th percentile by the
nearest-rank method, peak) cover only the steps it held the GPUs. **Coverage** is the share of those steps with a
value. A pod is judged idle only when at least half its GPU time was measured (`MinCoverage`).

## Serving engines

vLLM and SGLang export what they are doing: requests running and waiting, KV cache use, and counters of finished
requests and generated tokens. Their series are tied to pods by the scrape's target labels (`namespace` and `pod`
from a PodMonitor or ServiceMonitor, or the older `kubernetes_namespace` and `kubernetes_pod_name`).

- vLLM's data-parallel engines (the `engine` label) each run their own batch, so their running and waiting requests
  add up; their KV cache use is averaged.
- SGLang repeats the scheduler's numbers on every tensor-parallel rank, so its ranks are not added.
- A server scraped twice (two jobs, two Prometheus replicas) is collapsed.
- Token and request counts are `increase()` over the window, which handles counter resets.
- A pod's **busy share** is the share of the steps it held GPUs with at least one request running.
- vLLM's `vllm:engine_sleep_state{sleep_state="awake"}` gives the steps the engine slept.

## Waste, counted once

Findings come from seven rules, run from the most specific cause to the most general. When a rule counts a pod's
GPU-hours as waste, no later rule counts them again, so the findings add up to the total without double counting.
The [findings reference](findings.md) has each rule's exact test.

| Order | Rule | Waste |
|---|---|---|
| 1 | GPU003 engine asleep | the pod's GPU-hours × the share of time asleep, when its GPU was idle while asleep |
| 2 | GPU002 no traffic | all the pod's GPU-hours |
| 3 | GPU001 idle GPU | all the pod's GPU-hours not yet counted |
| 4 | GPU005 excess replicas | replicas beyond the peak need, at every step, less hours already counted |
| 5 | GPU004 stranded GPUs | a node's offered GPU-hours no pod requested |
| 6 | GPU006 low duty | none: idle GPU-hours are reported apart |
| 7 | GPU007 oversized | none: advice |

Stranded GPU-hours belong to no pod, so they never overlap the pod rules.

**Replica sizing (GPU005).** At each step, the replicas needed are the KV cache in use across all replicas divided
by the target share one replica may fill (`--kv-target`, 80%), rounded up, and at least one. The workload's peak
need is the most needed at any step; every replica beyond it, at any step, is waste. KV cache use is the capacity
measure because an LLM server runs out of KV cache before it runs out of compute. A workload that queued a request
at any replica, or lacks KV data on more than a fifth of its steps, is not judged.

**Severity** follows the wasted GPU-hours, not their price, so it means the same with or without prices: high
from 24 GPU-hours, medium from 4, low below that.

## Cost

A GPU-hour's price comes from you or from OpenCost; gpucost ships no price list, because list prices differ by
cloud, region, commitment and contract, and a made-up price would make every dollar in the report wrong. A node
takes the most specific price that applies: your price for its instance type, for its exact GPU model, for the
model's family (`a100` covers both memory sizes), OpenCost's `node_gpu_hourly_cost` for the node, then your `*`
default. A node with no price is reported in GPU-hours and named as a blind spot. See [pricing](pricing.md).

**Cost per million tokens** is a model's cost over the window divided by the output tokens it generated, times a
million: what a million tokens cost at the utilization actually seen, idle time included.

## Blind spots

A report lists what it could not see and how many GPU-hours that covers: GPU time without activity data (B001),
without a price (B002), nodes of unknown GPU model (B003), MIG slices of assumed size (B004), and gaps in
collection (B005). A blind spot can only hide waste. gpucost never counts a GPU-hour as waste on a guess.

## Limits

- **Memory is not used to size GPUs.** vLLM and SGLang reserve most of the GPU's memory at start whatever the
  model needs, so DCGM's memory use shows the reservation. GPU007 is advice for that reason.
- **Activity is not efficiency.** A GPU 100% active on a badly batched workload is busy, not efficient. gpucost
  finds GPUs doing nothing or little; it does not grade kernels.
- **The window is what it sees.** A model idle all of this window may be busy at month's end. Scan the windows that
  matter, and use `--min-hours` and the thresholds to match your workloads.
- **Time-sliced GPUs.** With time-slicing, several pods share one physical GPU and each requests one
  `nvidia.com/gpu` replica. gpucost counts each request as a GPU-hour, which overstates GPU-hours and cost on
  time-sliced nodes.
- **Dynamic Resource Allocation.** GPUs handed out through DRA ResourceClaims instead of `nvidia.com/gpu` requests
  are not counted yet: such pods request no `nvidia.com/gpu` and such nodes advertise none, so both are invisible
  to the request and allocatable signals, and a report on a DRA cluster understates its GPUs.

## How it is tested

- Each rule and each step above has unit tests on hand-built snapshots.
- A synthetic fleet (three nodes, seven workloads, one problem each) is backfilled into a real Prometheus 3.15.1
  with promtool and scanned by gpucost. Each finding is checked against the answer the scenario was built to have,
  and nothing else may count as waste (`hack/e2e.sh`, run in CI).
- The recording of that fleet is committed, so the same checks and the golden reports in every format run in
  `go test` without Prometheus.
- Fuzz tests cover the parsers.
