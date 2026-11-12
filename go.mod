module github.com/sunilmehta-si/k8s-gpu-cost-scanner

go 1.27

// Go 1.27.4 fixes GO-2026-6859 (CVE-2026-97054) in net/http. gpucost runs no HTTP server, so it is not exposed,
// but govulncheck flags the package; the fix is taken before the usual 14 days because it is a security release.
toolchain go1.27.4
