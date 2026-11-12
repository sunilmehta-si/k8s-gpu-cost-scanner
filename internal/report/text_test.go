package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	var buf bytes.Buffer
	if err := Text(&buf, sampleReport(), TextOptions{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"gpucost · 1d window ending 2026-11-12 10:00 UTC · http://prometheus.monitoring:9090",
		"2 nodes · 3 GPU pods · 384.0 GPU-hours offered · 240.0 requested (62.5%)",
		"Recoverable waste $672.00 · 168.0 GPU-hours · 3 findings (2 high, 1 info)",
		"HIGH   GPU004 GPUs no pod requested · node/gpu-b · 144.0 GPU-hours · $576.00",
		"→ Let the cluster autoscaler remove the node.",
		"meta-llama/Llama-3.1-8B-Instruct",
		"12.3M",
		"$15.61",
		"(1 more findings of severity info; --verbose shows them)",
		"B001 GPU time without activity data · 12.0 GPU-hours (5.0%)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report lacks %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "GPU007") || strings.Contains(out, "Queries") {
		t.Error("info findings or queries shown without --verbose")
	}
	buf.Reset()
	Text(&buf, sampleReport(), TextOptions{Verbose: true})
	if !strings.Contains(buf.String(), "GPU007") || !strings.Contains(buf.String(), "DCGM_FI_PROF_GR_ENGINE_ACTIVE") {
		t.Error("--verbose hides info findings or queries")
	}
}

func TestTextWithoutPrices(t *testing.T) {
	r := sampleReport()
	r.Summary.PricedShare = 0
	var buf bytes.Buffer
	Text(&buf, r, TextOptions{})
	if !strings.Contains(buf.String(), "No prices: costs are in GPU-hours") || strings.Contains(buf.String(), "Recoverable waste $") {
		t.Errorf("unpriced report:\n%s", buf.String())
	}
}
