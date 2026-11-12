package cli

import (
	"strings"
	"testing"
)

func TestDoctor(t *testing.T) {
	code, out, _ := run("doctor", "--snapshot", fixture(t))
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{
		"✓ kube-state-metrics: GPU requests for 1 pods on 1 nodes",
		"! kube-state-metrics: no pod owners",
		"✓ DCGM exporter before 4.9: graphics-engine activity",
		"✓ DCGM exporter: activity for 1 of 1 GPU pods, 100.0% of GPU-hours",
		"- vLLM: not found",
		"- OpenCost: not found",
		"Ready: gpucost scan has what it needs.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorFailsWithoutDCGM(t *testing.T) {
	code, out, _ := run("doctor", "--snapshot", fixtureWithout(t, "gpu_activity"))
	if code != exitError || !strings.Contains(out, "✗ DCGM exporter: no GPU activity metrics") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}
