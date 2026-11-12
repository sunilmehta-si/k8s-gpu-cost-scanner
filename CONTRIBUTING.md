# Contributing

Thank you for helping. Issues and pull requests are welcome; for a large change, open an issue first so we can
agree on the approach.

## Build and test

```bash
make test     # gofmt, go vet, and every test with the race detector, as CI runs them
make e2e      # the synthetic fleet in a real Prometheus (downloads Prometheus into .e2e/)
make vuln     # govulncheck
```

No cluster, GPU or Prometheus is needed for `make test`: the analysis runs on recorded snapshots.

## How the code is laid out

| Package | Does |
|---|---|
| `internal/prom` | the Prometheus HTTP API client |
| `internal/signals` | every query gpucost runs, with a variant per exporter version |
| `internal/collect` | runs the queries for a window into a snapshot |
| `internal/snapshot` | the snapshot format, redaction |
| `internal/fleet` | turns a snapshot into pods, GPUs, serving engines and nodes |
| `internal/price` | which price a node's GPU-hours get |
| `internal/analyze` | the rules, costs, namespaces and blind spots |
| `internal/report` | table, Markdown, JSON and CSV output |
| `internal/synth` | the synthetic demo fleet with known answers |
| `internal/cli` | the command line |

## Rules for changes

- **Every number has a test.** A change to a rule comes with a test on a hand-built snapshot in
  `internal/analyze`. A change to output updates the golden files: `make golden`, then read the diff.
- **References are generated.** `docs/findings.md` and `docs/metrics.md` come from the code; run `make docs` after
  changing a rule's documentation or a signal. A test fails when they drift.
- **Waste is counted once.** A new rule takes its place in the order in `internal/analyze/analyze.go` and claims
  GPU-hours through `claimPod` or `claimWorkload`, so no hour is counted twice.
- **No guesses.** gpucost never counts waste or a price it cannot see; it reports a blind spot instead.
- **A new exporter version** is a new variant in `internal/signals` converting to the same unit, plus a case in
  the synthetic fleet if its labels differ.
- **No new dependencies** without a strong reason; the standard library has served so far.
- Keep commits small, each building and passing tests, with a message that says what changed and why.

Pull requests are reviewed by [agentic-code-review-tool](https://github.com/sunilmehta-si/agentic-code-review-tool)
as well as by a person.

## Releasing

Update `CHANGELOG.md`, then push a `v*` tag on `main`; the release workflow builds, attests and publishes.
