package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
)

// Schema names the JSON report's format. It changes only when a field is removed or changes meaning; new fields
// can appear under the same schema.
const Schema = "gpucost.report/v1"

type jsonReport struct {
	Schema string `json:"schema"`
	*analyze.Report
}

// JSON writes the whole report as indented JSON, for scripts and dashboards.
func JSON(w io.Writer, r *analyze.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(jsonReport{Schema: Schema, Report: r})
}

// CSV writes one row per finding, for spreadsheets and tickets.
func CSV(w io.Writer, r *analyze.Report) error {
	cw := csv.NewWriter(w)
	header := []string{"rule", "slug", "severity", "title", "target", "namespace", "node", "pods",
		"waste_gpu_hours", "waste_cost", "currency", "priced", "idle_gpu_hours", "evidence", "recommendation"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, f := range r.Findings {
		row := []string{
			f.Rule, f.Slug, string(f.Severity), f.Title, f.Target, f.Namespace, f.Node, strings.Join(f.Pods, " "),
			num(f.WasteGPUHours), num(f.WasteCost), r.Currency, strconv.FormatBool(f.Priced), num(f.IdleGPUHours),
			strings.Join(f.Evidence, " | "), f.Recommendation,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func num(v float64) string { return fmt.Sprintf("%.4f", v) }
