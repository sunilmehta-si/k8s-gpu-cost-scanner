package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// fixture writes a small snapshot: one node of 2 GPUs and one pod holding one of them, idle, for 4 hours.
func fixture(t *testing.T) string {
	t.Helper()
	w, err := snapshot.PlanWindow(4*time.Hour, time.Hour, time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var pts []prom.Point
	for i := 0; i <= 4; i++ {
		pts = append(pts, prom.Point{T: w.Start.Add(time.Duration(i) * time.Hour).UnixMilli(), V: 1})
	}
	zero := make([]prom.Point, len(pts))
	for i := range pts {
		zero[i] = prom.Point{T: pts[i].T, V: 0}
	}
	two := make([]prom.Point, len(pts))
	for i := range pts {
		two[i] = prom.Point{T: pts[i].T, V: 2}
	}
	s := snapshot.New("gpucost test", "synthetic", w)
	s.Range["pod_gpu_requests"] = []prom.Series{{Labels: prom.Labels{"namespace": "lab", "pod": "nb-0", "node": "g1", "resource": "nvidia_com_gpu"}, Points: pts}}
	s.Range["node_gpu_allocatable"] = []prom.Series{{Labels: prom.Labels{"node": "g1", "resource": "nvidia_com_gpu"}, Points: two}}
	s.Range["gpu_activity"] = []prom.Series{{Labels: prom.Labels{"exported_namespace": "lab", "exported_pod": "nb-0", "UUID": "GPU-1", "modelName": "NVIDIA L4"}, Points: zero}}
	s.Present = []string{"DCGM_FI_PROF_GR_ENGINE_ACTIVE", "kube_node_status_allocatable", "kube_pod_container_resource_requests", "kube_pod_status_phase"}
	s.Queries["gpu_activity"] = "DCGM_FI_PROF_GR_ENGINE_ACTIVE"
	path := filepath.Join(t.TempDir(), "s.json.gz")
	if err := snapshot.Write(path, s); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestScanSnapshotTable(t *testing.T) {
	code, out, errOut := run("scan", "--snapshot", fixture(t), "--price", "l4=0.70")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"GPU001 GPUs held but never used · lab/Pod/nb-0 · 5.0 GPU-hours · $3.50", "GPU004 GPUs no pod requested · node/g1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestScanSnapshotJSONToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	code, _, errOut := run("scan", "--snapshot", fixture(t), "-o", "json", "--out", path, "--namespace", "lab")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var r map[string]any
	b, _ := readFile(path)
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r["schema"] != "gpucost.report/v1" {
		t.Errorf("schema = %v", r["schema"])
	}
}

func TestUsageErrors(t *testing.T) {
	snap := fixture(t)
	cases := [][]string{
		{"scan"},
		{"scan", "--snapshot", snap, "-o", "yaml"},
		{"scan", "--snapshot", snap, "--idle-activity", "5"},
		{"scan", "--snapshot", snap, "--price", "l4"},
		{"scan", "--snapshot", snap, "--prometheus", "http://p:9090"},
		{"scan", "--prometheus", "http://p:9090", "--end", "yesterday"},
		{"scan", "--snapshot", snap, "extra"},
		{"record", "--prometheus", "http://p:9090"},
		{"nope"},
		{},
	}
	for _, args := range cases {
		if code, _, _ := run(args...); code != exitUsage {
			t.Errorf("gpucost %v: exit %d, want %d", args, code, exitUsage)
		}
	}
}

func TestVersionAndHelp(t *testing.T) {
	if code, out, _ := run("version"); code != 0 || !strings.HasPrefix(out, "gpucost ") {
		t.Errorf("version: %d %q", code, out)
	}
	if code, out, _ := run("help"); code != 0 || !strings.Contains(out, "gpucost scan") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, _, _ := run("scan", "-h"); code != 0 {
		t.Errorf("scan -h: exit %d", code)
	}
}

func TestMissingSnapshotIsAnError(t *testing.T) {
	if code, _, errOut := run("scan", "--snapshot", "/nonexistent.json"); code != exitError || errOut == "" {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestRedactedSnapshotAnalysesTheSame(t *testing.T) {
	s, err := snapshot.Read(filepath.Join("..", "..", "testdata", "demo.snapshot.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Redact(s)
	path := filepath.Join(t.TempDir(), "redacted.json.gz")
	snapshot.Write(path, s)
	prices := filepath.Join("..", "..", "testdata", "demo.prices.csv")
	_, plain, _ := run("scan", "--snapshot", filepath.Join("..", "..", "testdata", "demo.snapshot.json.gz"), "--prices", prices, "-o", "json")
	_, redacted, _ := run("scan", "--snapshot", path, "--prices", prices, "-o", "json")
	pick := func(out string) (float64, int) {
		var r struct {
			Summary struct {
				WasteGPUHours float64 `json:"waste_gpu_hours"`
			} `json:"summary"`
			Findings []any `json:"findings"`
		}
		json.Unmarshal([]byte(out), &r)
		return r.Summary.WasteGPUHours, len(r.Findings)
	}
	h1, n1 := pick(plain)
	h2, n2 := pick(redacted)
	if h1 == 0 || h1 != h2 || n1 != n2 {
		t.Errorf("plain %.2f GPU-hours in %d findings, redacted %.2f in %d", h1, n1, h2, n2)
	}
	if strings.Contains(redacted, "legacy-mistral") || strings.Contains(redacted, "notebook-ana") {
		t.Error("names leaked through redaction")
	}
}
