package snapshot

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
)

// identifying maps label names whose values name something in your cluster to the prefix their pseudonym gets.
// Labels that describe rather than name, such as the resource, the owner's kind, the phase or the GPU model, are
// kept, so a redacted snapshot analyses exactly like the original.
var identifying = map[string]string{
	"namespace": "ns", "exported_namespace": "ns", "pod_namespace": "ns", "kubernetes_namespace": "ns", "k8s_namespace_name": "ns",
	"pod": "pod", "exported_pod": "pod", "pod_name": "pod", "kubernetes_pod_name": "pod", "k8s_pod_name": "pod",
	"node": "node", "Hostname": "node", "hostname": "node", "kubernetes_node": "node",
	"replicaset": "rs", "owner_name": "owner", "container": "ctr", "exported_container": "ctr",
	"model_name": "model", "UUID": "gpu", "uuid": "gpu", "gpu_uuid": "gpu", "pci_bus_id": "pci",
	"instance": "inst", "job": "job", "provider_id": "pid", "uid": "uid",
}

// Redact replaces every name in the snapshot with a pseudonym, the same one wherever the name appears, so the
// redacted snapshot can be shared and still analyses to the same findings. The pseudonyms are keyed by a random
// secret that is not kept: they cannot be reversed or matched against another snapshot.
func Redact(s *Snapshot) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("snapshot: no randomness for redaction: " + err.Error())
	}
	RedactWithKey(s, key)
}

// RedactWithKey is Redact with a chosen key, for tests and for redacting several snapshots consistently.
func RedactWithKey(s *Snapshot, key []byte) {
	pseudo := func(prefix, v string) string {
		if v == "" {
			return v
		}
		m := hmac.New(sha256.New, key)
		m.Write([]byte(v))
		return prefix + "-" + hex.EncodeToString(m.Sum(nil))[:10]
	}
	relabel := func(l prom.Labels) {
		for k, v := range l {
			if prefix, ok := identifying[k]; ok {
				l[k] = pseudo(prefix, v)
			}
		}
	}
	for _, series := range s.Range {
		for _, x := range series {
			relabel(x.Labels)
		}
	}
	for _, samples := range s.Instant {
		for _, x := range samples {
			relabel(x.Labels)
		}
	}
	s.Source = "redacted"
	s.Note("names redacted: namespaces, pods, nodes, models and GPU IDs are pseudonyms")
}
