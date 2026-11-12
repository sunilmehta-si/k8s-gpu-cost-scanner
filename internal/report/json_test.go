package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"testing"
)

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["schema"] != Schema {
		t.Errorf("schema = %v", got["schema"])
	}
	summary := got["summary"].(map[string]any)
	if summary["waste_gpu_hours"].(float64) != 168 {
		t.Errorf("summary = %v", summary)
	}
	if w := got["window"].(map[string]any); w["step"] != "5m0s" {
		t.Errorf("window = %v", w)
	}
	if n := len(got["findings"].([]any)); n != 3 {
		t.Errorf("%d findings", n)
	}
}

func TestCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := CSV(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0][0] != "rule" {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][0] != "GPU004" || rows[1][8] != "144.0000" || rows[1][9] != "576.0000" || rows[1][10] != "USD" {
		t.Errorf("first finding = %v", rows[1])
	}
	if rows[1][13] != "offered 192.0 GPU-hours | 75.0% of the node's GPU time no pod requested" {
		t.Errorf("evidence = %q", rows[1][13])
	}
}
