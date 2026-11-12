package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestMarkdown(t *testing.T) {
	r := sampleReport()
	r.Findings[1].Target = "research/note|book"
	var buf bytes.Buffer
	if err := Markdown(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"## GPU cost report",
		"| **Recoverable waste** | **$672.00** · 168.0 GPU-hours |",
		"| high | GPU004 | GPUs no pod requested | `node/gpu-b` | $576.00 (144.0 GPU-h) |",
		"| info | GPU007 | Work much smaller than the GPU | `ml/small-0` | advice |",
		"`research/note\\|book`",
		"<details><summary>GPU004 GPUs no pod requested: <code>node/gpu-b</code></summary>",
		"**Fix:** Let the cluster autoscaler remove the node.",
		"| `meta-llama/Llama-3.1-8B-Instruct` | ml/Deployment/llama | 2.0 (max 3) |",
		"- **B001 GPU time without activity data** (12.0 GPU-hours, 5.0%): Run the exporter.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown lacks %q\n%s", want, out)
		}
	}
}
