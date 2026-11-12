# Changelog

## [Unreleased]

## [0.1.0] - 2026-11-12

The first release: find the GPUs a Kubernetes inference cluster pays for and does not use, and what each model
costs per hour and per million tokens, from the metrics the cluster already exports.

### Added
- `gpucost scan`: collects one window (24 hours by default) from any Prometheus-compatible server, or reads a
  recorded snapshot, and reports the fleet, findings with evidence and fixes, cost per model and per million output
  tokens, cost and waste per workload and per namespace, and blind spots. Output as a terminal table, Markdown, JSON
  (schema `gpucost.report/v1`) or CSV.
- Seven rules, run from the most specific cause to the most general so each GPU-hour is counted as waste once:
  GPU003 engine asleep, GPU002 no traffic, GPU001 idle GPU, GPU005 excess replicas (sized on KV cache use), GPU004
  GPUs no pod requested, and the advice rules GPU006 low duty and GPU007 oversized GPU.
- Sources: kube-state-metrics (requests, owners, allocatable GPUs, node labels), the DCGM exporter before and after
  4.9.0's renaming (activity, memory, power; pod labels as `exported_pod`, `pod` or `pod_name`), vLLM (including the
  renamed KV cache gauge and sleep mode), SGLang, and OpenCost's GPU prices. MIG slices count as their share of a GPU,
  and so do time-sliced or MPS-shared replicas when GPU Feature Discovery's node labels are exported.
- Prices from a CSV file or `--price` flags by GPU model in any spelling, family, instance type or default, then
  OpenCost; gpucost ships no price list and reports unpriced GPUs in GPU-hours.
- `gpucost record` saves a window's raw metrics for offline analysis; `--redact` replaces names with pseudonyms for
  sharing. `gpucost doctor` checks what the cluster exports and says what to fix. `gpucost explain` documents each
  rule.
- Budget gates for CI (`--fail-over-cost`, `--fail-over-gpu-hours`, `--fail-on`, exit 3) and `--step-summary` for
  GitHub Actions job summaries.
- Bearer tokens (file or environment), extra headers for multi-tenant Thanos and Mimir, a custom CA.
- A read-only in-cluster CronJob (no service account token, no RBAC, network policy to Prometheus only) and a
  distroless image.
- A synthetic fleet with known answers, backfilled into Prometheus 3.15.1 and scanned in CI; golden reports in every
  format; fuzz tests; a 1,600-GPU benchmark.
