// Command synthfleet writes the demo scenario's metrics as OpenMetrics, for promtool to backfill into Prometheus.
//
//	go run ./hack/synthfleet > demo.om
//	promtool tsdb create-blocks-from openmetrics demo.om data/
package main

import (
	"fmt"
	"os"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/synth"
)

func main() {
	if err := synth.Demo().Generate().WriteOpenMetrics(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "synthfleet:", err)
		os.Exit(1)
	}
}
