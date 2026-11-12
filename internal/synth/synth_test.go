package synth

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDemoIsDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := Demo().Generate().WriteOpenMetrics(&a); err != nil {
		t.Fatal(err)
	}
	Demo().Generate().WriteOpenMetrics(&b)
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("two runs of the demo scenario differ")
	}
	out := a.String()
	if !strings.HasSuffix(out, "# EOF\n") {
		t.Error("no # EOF")
	}
	for _, want := range []string{
		`DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO{Hostname="gpu-h100-b",UUID=`,
		`exported_pod="notebook-ana"`,
		`vllm:engine_sleep_state{engine="0",model_name="rl/policy-8b"`,
		`kube_node_labels{label_nvidia_com_gpu_product="NVIDIA-L4",node="gpu-l4-a"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics lack %s", want)
		}
	}
}

func TestMetricFamiliesAreContiguous(t *testing.T) {
	var buf bytes.Buffer
	Demo().Generate().WriteOpenMetrics(&buf)
	seen := map[string]bool{}
	last := ""
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		name := line[:strings.IndexAny(line, "{ ")]
		if name != last {
			if seen[name] {
				t.Fatalf("metric %s appears in two places", name)
			}
			seen[name] = true
			last = name
		}
	}
}

func TestPodsStayWithinNodeCapacity(t *testing.T) {
	sc := Demo()
	used := map[string]int{}
	for _, wl := range sc.Workloads {
		for _, r := range wl.Replicas {
			used[r.Node] += r.GPUs
		}
	}
	for _, n := range sc.Nodes {
		if used[n.Name] > n.GPUs {
			t.Errorf("%s: %d GPUs requested of %d", n.Name, used[n.Name], n.GPUs)
		}
	}
}

func TestSparseIsOneInEight(t *testing.T) {
	sc := Demo()
	for _, step := range []time.Duration{time.Minute, 5 * time.Minute} {
		busy, n := 0, 0
		for ts := sc.End.Add(-sc.Window); ts.Before(sc.End); ts = ts.Add(step) {
			n++
			if sparse(ts, 1) > 0 {
				busy++
			}
		}
		if busy*8 != n {
			t.Errorf("step %s: busy %d of %d samples, want one in eight", step, busy, n)
		}
	}
}
