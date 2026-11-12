package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBudgetGate(t *testing.T) {
	snap := fixture(t)
	// The fixture wastes 5 GPU-hours on an idle pod and 5 on the node's unrequested GPU: 10 GPU-hours, $7.
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"--fail-over-cost", "100"}, 0},
		{[]string{"--fail-over-cost", "5"}, exitGate},
		{[]string{"--fail-over-gpu-hours", "9.5"}, exitGate},
		{[]string{"--fail-over-gpu-hours", "10"}, 0},
		{[]string{"--fail-on", "high"}, 0},
		{[]string{"--fail-on", "medium"}, exitGate},
		{[]string{"--fail-on", "critical"}, exitUsage},
	}
	for _, c := range cases {
		args := append([]string{"scan", "--snapshot", snap, "--price", "l4=0.70"}, c.args...)
		code, out, errOut := run(args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d\n%s", c.args, code, c.code, errOut)
		}
		if code == exitGate && (!strings.Contains(errOut, "budget gate") || !strings.Contains(out, "GPU001")) {
			t.Errorf("%v: the report must still print, and the reason too: %q", c.args, errOut)
		}
	}
}

func TestStepSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	os.WriteFile(path, []byte("earlier step\n"), 0o644)
	if code, _, errOut := run("scan", "--snapshot", fixture(t), "--step-summary", path); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "earlier step\n## GPU cost report") {
		t.Errorf("summary file = %q", b)
	}
}
