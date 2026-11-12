package snapshot

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

func TestRedact(t *testing.T) {
	s := sample()
	s.Source = "https://prometheus.corp.example"
	s.Range["pod_gpu_requests"] = []prom.Series{{Labels: prom.Labels{"namespace": "payments-ml", "pod": "fraud-model-0", "node": "gke-prod-a100-7", "resource": "nvidia_com_gpu"}}}
	s.Range["gpu_activity"] = []prom.Series{{Labels: prom.Labels{"exported_namespace": "payments-ml", "exported_pod": "fraud-model-0", "Hostname": "gke-prod-a100-7", "modelName": "NVIDIA A100-SXM4-80GB", "UUID": "GPU-1234"}}}
	s.Instant["pod_owner"] = []prom.Sample{{Labels: prom.Labels{"namespace": "payments-ml", "pod": "fraud-model-0", "owner_kind": "ReplicaSet", "owner_name": "fraud-model-6d5"}}}
	RedactWithKey(s, []byte("k"))

	var buf bytes.Buffer
	Encode(&buf, s)
	out := buf.String()
	for _, secret := range []string{"payments-ml", "fraud-model", "gke-prod", "GPU-1234", "corp.example"} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted snapshot still contains %q", secret)
		}
	}
	for _, kept := range []string{"nvidia_com_gpu", "NVIDIA A100-SXM4-80GB", "ReplicaSet"} {
		if !strings.Contains(out, kept) {
			t.Errorf("redaction removed %q, which the analysis needs", kept)
		}
	}
	req := s.Range["pod_gpu_requests"][0].Labels
	act := s.Range["gpu_activity"][0].Labels
	if req["pod"] != act["exported_pod"] || req["namespace"] != act["exported_namespace"] || req["node"] != act["Hostname"] {
		t.Errorf("one name got two pseudonyms: %v vs %v", req, act)
	}
	if !strings.HasPrefix(req["pod"], "pod-") || !strings.HasPrefix(req["node"], "node-") {
		t.Errorf("pseudonyms = %v", req)
	}
}

func TestRedactKeysDiffer(t *testing.T) {
	a, b := sample(), sample()
	a.Range["x"] = []prom.Series{{Labels: prom.Labels{"pod": "p"}}}
	b.Range["x"] = []prom.Series{{Labels: prom.Labels{"pod": "p"}}}
	Redact(a)
	Redact(b)
	if a.Range["x"][0].Labels["pod"] == b.Range["x"][0].Labels["pod"] {
		t.Error("two redactions gave the same pseudonym: the key is not random")
	}
}
