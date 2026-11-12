# Running gpucost

gpucost needs read access to one Prometheus-compatible server that has kube-state-metrics and the DCGM exporter
(and vLLM, SGLang or OpenCost if you run them). It needs no Kubernetes credentials and changes nothing. Run
`gpucost doctor` first: it says what the server has and what to fix.

## From a laptop

```bash
go install github.com/sunilmehta-si/k8s-gpu-cost-scanner/cmd/gpucost@latest
kubectl -n monitoring port-forward svc/prometheus-operated 9090   # in another terminal
gpucost doctor --prometheus http://localhost:9090
gpucost scan --prometheus http://localhost:9090 --prices prices.csv
```

Or download a release archive and check it came from this repository's release workflow:

```bash
gh attestation verify gpucost-v0.1.0-linux-amd64.tar.gz --repo sunilmehta-si/k8s-gpu-cost-scanner
```

## Against a hosted or multi-tenant Prometheus

| Server | How |
|---|---|
| Thanos, Cortex, Mimir | `--prometheus` the query frontend; `--header 'X-Scope-OrgID: tenant'` for a tenant |
| Grafana Cloud | the Prometheus endpoint, with a token in `GPUCOST_PROMETHEUS_TOKEN` or `--token-file` |
| Google Managed Service for Prometheus | the frontend proxy Google documents for PromQL clients, reached in the cluster or through port-forward |
| VictoriaMetrics | its Prometheus-compatible `/api/v1` endpoint |
| Amazon Managed Service for Prometheus | needs SigV4 signing, which gpucost does not do: put `aws-sigv4-proxy` in front of it |

A private CA is trusted with `--ca-file`; certificate checks are never turned off. A token file is read before every
request, so a rotated service account token keeps working.

## Inside the cluster, daily

[deploy/gpucost.yaml](../deploy/gpucost.yaml) runs the report as a CronJob. It mounts no service account token and
has no RBAC, since it never calls the Kubernetes API; its root filesystem is read-only, it drops every capability,
and its network policy reaches only Prometheus and DNS. Set the Prometheus URL, the network policy's namespace and
port, and your prices first, then:

```bash
kubectl apply -f deploy/gpucost.yaml
kubectl -n gpucost create job --from=cronjob/gpucost gpucost-now
kubectl -n gpucost logs -f job/gpucost-now
```

## In CI, with a budget

`--fail-over-cost`, `--fail-over-gpu-hours` and `--fail-on SEVERITY` exit 3 after printing the report when waste
passes the limit, apart from errors (1) and usage mistakes (2). `--step-summary` appends the Markdown report to a
file such as `$GITHUB_STEP_SUMMARY`. [examples/github-actions.yml](../examples/github-actions.yml) is a weekly job
that does both.

## Offline, and shared

`gpucost record` saves a window's raw metrics; `gpucost scan --snapshot` analyses it later, anywhere, with
different prices or thresholds, without Prometheus:

```bash
gpucost record --prometheus http://localhost:9090 --window 168h --step 15m -o week.json.gz
gpucost scan --snapshot week.json.gz --prices prices.csv -o markdown --out week.md
```

`--redact` replaces namespaces, pods, nodes, models and GPU IDs with pseudonyms, the same one wherever a name
appears, so a snapshot can be shared, with a vendor or in a bug report, and still gives the same findings.

## Choosing a window

A day finds idle GPUs and stranded nodes; a week catches models busy only on some days and workloads that scale
over the week. Over 30 days the step widens to stay under Prometheus' 11,000 points per series. Scans of long
windows ask Prometheus for more; four queries run at a time.

## Outputs

`-o table` (the default) for a terminal, `-o markdown` for a pull request, wiki or job summary, `-o json` for scripts
and dashboards (schema `gpucost.report/v1`), `-o csv` for one row per finding. `--out FILE` writes to a file.
