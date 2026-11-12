# gpucost on distroless: no shell, no package manager, a non-root user and CA certificates for HTTPS to Prometheus.
#
# The binary is built outside the image, the same way the release builds it (CGO off, trimmed paths), so the image
# and the release binaries are the same bytes:
#
#   make dist && docker buildx build --platform linux/amd64,linux/arm64 -t gpucost .
#
# The base is pinned by digest: gcr.io/distroless/static-debian12:nonroot, uploaded 2026-08-21.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG TARGETARCH
LABEL org.opencontainers.image.title="gpucost" \
      org.opencontainers.image.description="Finds the GPUs a Kubernetes inference cluster pays for and does not use" \
      org.opencontainers.image.source="https://github.com/sunilmehta-si/k8s-gpu-cost-scanner" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY dist/linux-${TARGETARCH}/gpucost /usr/local/bin/gpucost
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/gpucost"]
CMD ["--help"]
