package price

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/gpu"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

func TestLoadCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.csv")
	os.WriteFile(path, []byte(`# Our committed-use rates, Nov 2026
gpu,price_per_gpu_hour,note
NVIDIA H100 80GB HBM3,4.10,3-year commitment
a100,2.20,
instance:g2-standard-8,0.71,L4 VM
*,1.00,anything else
`), 0o600)
	tb := NewTable()
	if err := tb.LoadCSV(path); err != nil {
		t.Fatal(err)
	}
	if tb.Len() != 4 || tb.products["h100-80gb"] != 4.10 || tb.instances["g2-standard-8"] != 0.71 || *tb.fallback != 1 {
		t.Errorf("table = %+v", tb)
	}
}

func TestLoadCSVErrors(t *testing.T) {
	for name, body := range map[string]string{
		"no header columns": "product,cost\nl4,1\n",
		"bad number":        "gpu,price_per_gpu_hour\nl4,cheap\n",
		"negative":          "gpu,price_per_gpu_hour\nl4,-1\n",
		"empty key":         "gpu,price_per_gpu_hour\n,1\n",
		"empty instance":    "gpu,price_per_gpu_hour\ninstance:,1\n",
	} {
		err := NewTable().readCSV(strings.NewReader(body), name)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseFlag(t *testing.T) {
	tb := NewTable()
	if err := tb.ParseFlag("h100-80gb=4.5"); err != nil || tb.products["h100-80gb"] != 4.5 {
		t.Errorf("err %v, table %+v", err, tb.products)
	}
	for _, bad := range []string{"h100", "h100=x", "=3"} {
		if err := NewTable().ParseFlag(bad); err == nil {
			t.Errorf("--price %q accepted", bad)
		}
	}
}

func TestResolveOrder(t *testing.T) {
	f := &fleet.Fleet{Nodes: map[string]*fleet.Node{
		"by-instance": {Name: "by-instance", Product: "l4", InstanceType: "g2-standard-8"},
		"by-product":  {Name: "by-product", Product: "h100-80gb"},
		"by-family":   {Name: "by-family", Product: "a100-80gb"},
		"by-opencost": {Name: "by-opencost", Product: "t4"},
		"by-default":  {Name: "by-default", Product: gpu.Unknown},
	}}
	s := snapshot.New("t", "synthetic", snapshot.Window{})
	s.Instant["opencost_gpu_hourly"] = []prom.Sample{
		{Labels: prom.Labels{"node": "by-opencost"}, Value: 0.35},
		{Labels: prom.Labels{"node": "by-product"}, Value: 9.99},
	}
	tb := NewTable()
	tb.Set("instance:g2-standard-8", 0.71, "prices.csv")
	tb.Set("l4", 0.5, "prices.csv")
	tb.Set("h100-80gb", 4.10, "prices.csv")
	tb.Set("a100", 2.2, "prices.csv")
	tb.Set("*", 1, "prices.csv")
	b := Resolve(tb, f, s)
	want := map[string]float64{"by-instance": 0.71, "by-product": 4.10, "by-family": 2.2, "by-opencost": 0.35, "by-default": 1}
	for node, v := range want {
		p, ok := b.For(node)
		if !ok || p.PerGPUHour != v {
			t.Errorf("%s = %+v, want %v", node, p, v)
		}
	}
	if p, _ := b.For("by-opencost"); !strings.Contains(p.Source, "OpenCost") {
		t.Errorf("source = %q", p.Source)
	}

	// With no table and no OpenCost price, a node stays unpriced rather than getting a made-up number.
	if _, ok := Resolve(NewTable(), f, snapshot.New("t", "synthetic", snapshot.Window{})).For("by-product"); ok {
		t.Error("a node was priced from nothing")
	}
}
