# Pricing

gpucost reports GPU-hours always and money when it knows a price. It ships no price list: list prices differ by
cloud, region, commitment and contract, and a made-up price would make every dollar in the report wrong. A node
with no price is reported in GPU-hours and named as a blind spot (B002).

## Giving prices

From a file:

```bash
gpucost scan --prometheus http://prometheus:9090 --prices prices.csv
```

Or on the command line, repeatable, overriding the file:

```bash
gpucost scan --prometheus http://prometheus:9090 --price h100-80gb=4.10 --price l4=0.71 --price '*=1.00'
```

The file is CSV with a header naming a `gpu` column and a `price_per_gpu_hour` column; other columns, such as a note
on where a rate came from, are ignored, and lines starting with `#` are comments. See
[examples/prices.csv](../examples/prices.csv).

## Keys

| Key | Matches | Example |
|---|---|---|
| A GPU model, any spelling | that model, with that memory size | `NVIDIA H100 80GB HBM3`, `h100-80gb`, `nvidia-h100-80gb` |
| A family | every memory size with no price of its own | `a100` |
| `instance:` and a type | nodes of that instance type | `instance:a2-ultragpu-1g` |
| `*` | everything nothing else matched | `*` |

GPU names are normalized the same way whichever source reported them: the DCGM exporter's `NVIDIA A100-SXM4-80GB`,
GPU Feature Discovery's `NVIDIA-A100-SXM4-80GB` and GKE's `nvidia-a100-80gb` are all `a100-80gb`. Form factors
(SXM, PCIe) and memory types are ignored; memory size and NVL variants are kept.

## Which price a node gets

The most specific that applies:

1. your price for the node's instance type,
2. your price for its exact GPU model,
3. your price for the model's family,
4. OpenCost's `node_gpu_hourly_cost` for the node, when OpenCost runs in the cluster,
5. your `*` default.

The node's GPU model comes from the DCGM exporter's `modelName`, or from the `nvidia.com/gpu.product`,
`cloud.google.com/gke-accelerator` or `karpenter.k8s.aws/instance-gpu-name` node label when kube-state-metrics is
allowed to export it (`--metric-labels-allowlist=nodes=[nvidia.com/gpu.product,...]`). Its instance type comes from
`node.kubernetes.io/instance-type`, or from OpenCost.

## MIG slices

A MIG slice costs its share of the physical GPU's price: a `3g.40gb` slice of an H100 at 4.00 per hour costs
3/7 × 4.00 per hour. An A30 divides into 4 compute slices, the other MIG-capable GPUs into 7.

## Which price to use

Use what a GPU-hour actually costs you. With committed-use or reserved capacity, that is the committed rate: the
GPUs are paid for whether used or not, which is exactly why idle ones matter. With spot or preemptible capacity, use
the average rate you paid. For on-premises GPUs, an amortized hourly cost (hardware over its life, plus power,
cooling and space) makes the report comparable to cloud.

## Currency

Prices are in whatever currency you write them in; `--currency EUR` labels the report to match. USD is shown as
`$1,234.56`, other currencies as `1,234.56 EUR`.
