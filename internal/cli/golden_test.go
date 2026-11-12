package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/golden from this run")

// TestDemoGolden scans the synthetic fleet's recording and compares each output format with the committed one.
// Under hack/e2e.sh the recording is fresh from a real Prometheus, so this also checks that recording the same
// metrics again gives the same report.
func TestDemoGolden(t *testing.T) {
	snap := os.Getenv("GPUCOST_E2E_SNAPSHOT")
	if snap == "" {
		snap = filepath.Join("..", "..", "testdata", "demo.snapshot.json.gz")
	}
	prices := filepath.Join("..", "..", "testdata", "demo.prices.csv")
	for _, c := range []struct{ format, file string }{{"table", "demo.txt"}, {"markdown", "demo.md"}, {"json", "demo.json"}, {"csv", "demo.csv"}} {
		code, out, errOut := run("scan", "--snapshot", snap, "--prices", prices, "-o", c.format, "--verbose")
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", c.format, code, errOut)
		}
		got := normalize([]byte(out))
		path := filepath.Join("..", "..", "testdata", "golden", c.file)
		if *updateGolden {
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/cli -run TestDemoGolden -update-golden)", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s report differs from %s; if the change is intended, run go test ./internal/cli -run TestDemoGolden -update-golden", c.format, path)
		}
	}
}

var volatile = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`"generated": "[^"]*"`), `"generated": "GENERATED"`},
	{regexp.MustCompile(`http://127\.0\.0\.1:\d+`), `http://prometheus`},
	{regexp.MustCompile(`gpucost (dev|v[0-9][^ "<|]*)`), `gpucost VERSION`},
}

func normalize(b []byte) []byte {
	for _, v := range volatile {
		b = v.re.ReplaceAll(b, []byte(v.with))
	}
	return b
}
