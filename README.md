# k8s-gpu-cost-scanner

Find the GPUs a Kubernetes inference cluster pays for and does not use, and what each model costs per hour and per
million tokens.

`gpucost` reads the metrics your cluster already exports (kube-state-metrics, NVIDIA's DCGM exporter, vLLM or SGLang,
and optionally OpenCost) from Prometheus. It never talks to the Kubernetes API and never changes anything.

Work in progress.

## License

Apache 2.0. See [LICENSE](LICENSE).
