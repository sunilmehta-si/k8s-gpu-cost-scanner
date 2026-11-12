#!/usr/bin/env bash
# End-to-end test: backfill the synthetic fleet into a real Prometheus, record it with gpucost, and check the
# report against the committed one.
#
#   hack/e2e.sh            run the test
#   hack/e2e.sh --update   also rewrite testdata/demo.snapshot.json.gz from this run
#
# Needs Go and curl. Downloads Prometheus (version and checksums pinned below) into .e2e/.
set -euo pipefail

PROM_VERSION=3.15.1
# sha256 of each platform's tarball, from the release's sha256sums.txt.
prom_sha256() {
  case "$1" in
    linux-amd64) echo 6a726885328ad9efebbf36e876ce2fb6225468a71fd1e7ce3ba666d5f4d97a84 ;;
    linux-arm64) echo ceb6d21493c37ca0b4513b147642797c68cf39a0f50a444d50e1e80c74f21b26 ;;
    darwin-amd64) echo c89fcb30045d8c394c9d93e96cbbee3aaf2448a48326b417e811eb4e528edf88 ;;
    darwin-arm64) echo 0d4d3d9837d27e4fcd0bd16d115b1b8d6732a4bf3e69107f1190d9ef653ae471 ;;
  esac
}
PORT=${PORT:-19090}
END=2026-11-01T00:00:00Z

root=$(cd "$(dirname "$0")/.." && pwd)
work="$root/.e2e"
mkdir -p "$work"
cd "$root"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
platform="$os-$arch"
want=$(prom_sha256 "$platform")
[[ -n "$want" ]] || { echo "e2e: no pinned Prometheus checksum for $platform" >&2; exit 1; }

dir="$work/prometheus-$PROM_VERSION.$platform"
sha256() { (sha256sum "$1" 2>/dev/null || shasum -a 256 "$1") | cut -d' ' -f1; }
if [[ ! -x "$dir/prometheus" ]]; then
  tarball="$work/prometheus-$PROM_VERSION.$platform.tar.gz"
  if [[ ! -f "$tarball" || "$(sha256 "$tarball")" != "$want" ]]; then
    curl -fsSL --retry 3 -o "$tarball" \
      "https://github.com/prometheus/prometheus/releases/download/v$PROM_VERSION/prometheus-$PROM_VERSION.$platform.tar.gz"
  fi
  got=$(sha256 "$tarball")
  [[ "$got" == "$want" ]] || { echo "e2e: Prometheus checksum mismatch: got $got, want $want" >&2; exit 1; }
  tar -xzf "$tarball" -C "$work"
fi

echo "e2e: generating the synthetic fleet"
rm -rf -- "${work:?}/data"
go run ./hack/synthfleet > "$work/demo.om"
"$dir/promtool" tsdb create-blocks-from openmetrics "$work/demo.om" "$work/data" > /dev/null

echo "e2e: starting Prometheus $PROM_VERSION on 127.0.0.1:$PORT"
# The data is from a fixed date in the past: keep it, whatever today is.
"$dir/prometheus" --config.file=hack/prometheus.yml --storage.tsdb.path="$work/data" \
  --storage.tsdb.retention.time=100y --web.listen-address="127.0.0.1:$PORT" --log.level=warn &
prom=$!
trap 'kill $prom 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do
  curl -fs "http://127.0.0.1:$PORT/-/ready" > /dev/null 2>&1 && break
  sleep 1
done
curl -fs "http://127.0.0.1:$PORT/-/ready" > /dev/null || { echo "e2e: Prometheus did not become ready" >&2; exit 1; }

echo "e2e: recording the window with gpucost"
go run ./cmd/gpucost record --prometheus "http://127.0.0.1:$PORT" --end "$END" --window 24h --step 5m \
  -o "$work/demo.snapshot.json.gz" --verbose

if [[ "${1:-}" == "--update" ]]; then
  # An intended change to the queries or the scenario: commit this recording and the reports it gives.
  cp "$work/demo.snapshot.json.gz" testdata/demo.snapshot.json.gz
  go test -count=1 -run TestDemoGolden ./internal/cli -update-golden > /dev/null
  echo "e2e: updated testdata/demo.snapshot.json.gz and testdata/golden; review the diff"
fi

echo "e2e: comparing the report with the committed one"
GPUCOST_E2E_SNAPSHOT="$work/demo.snapshot.json.gz" go test -count=1 -run 'TestDemo' ./internal/analyze ./internal/cli
echo "e2e: passed"
