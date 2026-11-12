# gpucost: make test runs what CI runs, apart from the end-to-end test (make e2e).
GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/cli.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build test vuln e2e golden docs dist

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/gpucost ./cmd/gpucost

test:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }
	$(GO) vet ./...
	$(GO) test -race -count=1 ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

# Backfill the synthetic fleet into a real Prometheus and check the report (downloads Prometheus into .e2e/).
e2e:
	hack/e2e.sh

# Rewrite the golden reports and docs/findings.md after an intended change.
golden:
	$(GO) test ./internal/cli -run TestDemoGolden -update-golden
docs:
	$(GO) test ./internal/analyze -run TestFindingsDocIsCurrent -update-docs

# Release binaries for every platform, with checksums. Each build overwrites its own files in dist/.
dist:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		mkdir -p dist/$$os-$$arch && \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/$$os-$$arch/gpucost$$ext ./cmd/gpucost || exit 1; \
		tar -czf dist/gpucost-$(VERSION)-$$os-$$arch.tar.gz -C dist/$$os-$$arch . || exit 1; \
	done
	cd dist && (sha256sum gpucost-$(VERSION)-*.tar.gz 2>/dev/null || shasum -a 256 gpucost-$(VERSION)-*.tar.gz) > checksums.txt
