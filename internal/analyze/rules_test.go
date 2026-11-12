package analyze

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite docs/findings.md from the rule docs")

func TestRuleDocsFollowRuleOrder(t *testing.T) {
	// The documented order is the order rules run in.
	var names []string
	for _, r := range rules {
		full := runtime.FuncForPC(reflect.ValueOf(r).Pointer()).Name()
		names = append(names, full[strings.LastIndex(full, ".")+1:])
	}
	want := []string{"asleep", "noTraffic", "idle", "overprovisioned", "stranded", "lowDuty", "oversized"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("rules run in order %v", names)
	}
	ids := []string{"GPU003", "GPU002", "GPU001", "GPU005", "GPU004", "GPU006", "GPU007"}
	for i, r := range Rules {
		if r.Rule != ids[i] {
			t.Errorf("Rules[%d] = %s, want %s", i, r.Rule, ids[i])
		}
	}
}

func TestFindRule(t *testing.T) {
	if r, ok := FindRule("gpu001"); !ok || r.Slug != "idle-gpu" {
		t.Errorf("FindRule(gpu001) = %+v", r)
	}
	if r, ok := FindRule("Stranded-GPUs"); !ok || r.Rule != "GPU004" {
		t.Errorf("FindRule(Stranded-GPUs) = %+v", r)
	}
	if _, ok := FindRule("GPU999"); ok {
		t.Error("found a rule that does not exist")
	}
}

// TestFindingsDocIsCurrent fails when docs/findings.md differs from the rule docs; -update-docs rewrites it.
func TestFindingsDocIsCurrent(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteRulesMarkdown(&buf); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "docs", "findings.md")
	if *updateDocs {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/analyze -update-docs)", err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Error("docs/findings.md is out of date: run go test ./internal/analyze -update-docs")
	}
}
