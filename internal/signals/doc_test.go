package signals

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite docs/metrics.md from the catalogue")

// TestMetricsDocIsCurrent fails when docs/metrics.md differs from the catalogue; -update-docs rewrites it.
func TestMetricsDocIsCurrent(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "docs", "metrics.md")
	if *updateDocs {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/signals -update-docs)", err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Error("docs/metrics.md is out of date: run go test ./internal/signals -update-docs")
	}
}

func TestEverySourceIsDocumented(t *testing.T) {
	for _, s := range Catalog {
		if sourceIntro[s.Source] == "" {
			t.Errorf("source %s has no introduction", s.Source)
		}
	}
}
