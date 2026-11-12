# Security policy

## Reporting a vulnerability

Please report it privately: use GitHub's **Report a vulnerability** button on this repository's Security tab, or
email sunilmehta695@gmail.com. Say what you found, how to reproduce it, and what it affects. You will get a reply
within three working days and a fix or a plan within fourteen. Please do not open a public issue first.

Fixes go into the latest release only.

## What gpucost touches

- **It reads, and only reads, one Prometheus-compatible server**, through `/api/v1/query`, `/api/v1/query_range` and
  `/api/v1/label/__name__/values`, all as POST so queries stay out of URLs and proxy logs. It never calls the
  Kubernetes API and needs no Kubernetes credentials; the in-cluster CronJob mounts no service account token.
- **Credentials.** A bearer token comes from `--token-file`, read before each request, or `GPUCOST_PROMETHEUS_TOKEN`.
  Neither is ever printed, logged or written into a snapshot or report. A password in the Prometheus URL is
  replaced by `redacted` wherever the URL is shown or stored.
- **TLS.** Certificates are always checked; `--ca-file` adds a CA, nothing turns verification off. TLS 1.2 is the
  minimum.
- **Snapshots and reports name your namespaces, pods, nodes and models.** Treat them like the metrics they came from.
  Snapshots are written readable by their owner only. `gpucost record --redact` replaces those names with
  pseudonyms keyed by a random secret that is not kept, for a snapshot you mean to share.
- **Untrusted input.** A snapshot file, a prices file and the server's responses are all parsed defensively:
  responses are capped at 512 MiB, the parsers are fuzz-tested, and a newer snapshot version is refused rather than
  misread.

## Supply chain

- The module has no dependencies outside the Go standard library.
- Every GitHub Action is pinned by commit SHA; Dependabot proposes updates 14 days after their release, security
  updates at once. Go is pinned in `go.mod`.
- Release binaries and the container image are built by the release workflow from the tagged commit, with
  provenance attestations: `gh attestation verify <archive> --repo sunilmehta-si/k8s-gpu-cost-scanner`.
- The image is distroless, runs as a non-root user, and holds only the binary.
