// Package cli is gpucost's command line.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/collect"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/fleet"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/price"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/report"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usage = `gpucost finds the GPUs a Kubernetes inference cluster pays for and does not use,
and what each model costs per hour and per million tokens.

It reads kube-state-metrics, the NVIDIA DCGM exporter, vLLM or SGLang, and optionally
OpenCost from Prometheus. It never talks to the Kubernetes API and changes nothing.

Usage:
  gpucost scan    --prometheus URL [flags]   analyse a window and report waste and cost
  gpucost scan    --snapshot FILE  [flags]   analyse a recorded window again, offline
  gpucost record  --prometheus URL -o FILE   save a window's raw metrics for later
  gpucost doctor  --prometheus URL           check the cluster exports what gpucost needs
  gpucost explain [RULE]                     what a finding means and how to fix it
  gpucost version                            print the version

Run "gpucost COMMAND -h" for a command's flags.
`

// Main runs gpucost with the given arguments and returns the exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "scan":
		err = runScan(ctx, rest, stdout, stderr)
	case "record":
		err = runRecord(ctx, rest, stdout, stderr)
	case "doctor":
		err = runDoctor(ctx, rest, stdout, stderr)
	case "explain":
		err = runExplain(rest, stdout)
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, toolName())
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "gpucost: unknown command %q\n\n%s", cmd, usage)
		return exitUsage
	}
	var ue usageError
	var ee exitErr
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, flag.ErrHelp):
		return exitOK
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "gpucost %s: %v\n", cmd, err)
		return exitUsage
	case errors.As(err, &ee):
		fmt.Fprintf(stderr, "gpucost %s: %v\n", cmd, err)
		return ee.code
	default:
		fmt.Fprintf(stderr, "gpucost %s: %v\n", cmd, err)
		return exitError
	}
}

type usageError struct{ error }

func usagef(format string, args ...any) error { return usageError{fmt.Errorf(format, args...)} }

// exitErr ends the program with a specific exit code, for a CI gate.
type exitErr struct {
	error
	code int
}

// stringList is a repeatable flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// connFlags are the flags every command that talks to Prometheus shares.
type connFlags struct {
	url       string
	tokenFile string
	headers   stringList
	caFile    string
	timeout   time.Duration
	window    time.Duration
	step      time.Duration
	end       string
}

func (c *connFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.url, "prometheus", os.Getenv("GPUCOST_PROMETHEUS_URL"), "Prometheus-compatible server `URL` (or $GPUCOST_PROMETHEUS_URL)")
	fs.StringVar(&c.tokenFile, "token-file", "", "read a bearer token from `FILE` before each request")
	fs.Var(&c.headers, "header", "extra request header `Name: value`, repeatable (for example X-Scope-OrgID: team)")
	fs.StringVar(&c.caFile, "ca-file", "", "trust the CA certificates in `FILE` as well as the system's")
	fs.DurationVar(&c.timeout, "timeout", 2*time.Minute, "timeout for each query")
	fs.DurationVar(&c.window, "window", 24*time.Hour, "how far back to look")
	fs.DurationVar(&c.step, "step", 5*time.Minute, "resolution of range queries")
	fs.StringVar(&c.end, "end", "", "end of the window, RFC 3339 (default now)")
}

func (c *connFlags) client() (*prom.Client, error) {
	if c.url == "" {
		return nil, usagef("--prometheus is required (or set GPUCOST_PROMETHEUS_URL)")
	}
	hc, err := prom.HTTPClient(c.timeout, c.caFile)
	if err != nil {
		return nil, err
	}
	opts := []prom.Option{prom.WithHTTPClient(hc)}
	if token := os.Getenv("GPUCOST_PROMETHEUS_TOKEN"); token != "" && c.tokenFile == "" {
		opts = append(opts, prom.WithBearerToken(token))
	}
	if c.tokenFile != "" {
		opts = append(opts, prom.WithBearerTokenFile(c.tokenFile))
	}
	for _, h := range c.headers {
		name, value, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, usagef("--header %q: want Name: value", h)
		}
		opts = append(opts, prom.WithHeader(strings.TrimSpace(name), strings.TrimSpace(value)))
	}
	return prom.New(c.url, opts...)
}

func (c *connFlags) windowAt(now time.Time) (snapshot.Window, error) {
	end := now
	if c.end != "" {
		t, err := time.Parse(time.RFC3339, c.end)
		if err != nil {
			return snapshot.Window{}, usagef("--end %q: %v", c.end, err)
		}
		end = t
	}
	w, err := snapshot.PlanWindow(c.window, c.step, end)
	if err != nil {
		return w, usagef("%v", err)
	}
	return w, nil
}

// collectSnapshot runs the queries for the window, reporting progress on stderr when verbose.
func (c *connFlags) collectSnapshot(ctx context.Context, stderr io.Writer, verbose bool) (*snapshot.Snapshot, error) {
	client, err := c.client()
	if err != nil {
		return nil, err
	}
	w, err := c.windowAt(time.Now())
	if err != nil {
		return nil, err
	}
	opts := collect.Options{Tool: toolName(), Source: client.URL()}
	if verbose {
		opts.Progress = func(signal string, err error) {
			if err != nil {
				fmt.Fprintf(stderr, "  %-26s failed: %v\n", signal, err)
				return
			}
			fmt.Fprintf(stderr, "  %-26s ok\n", signal)
		}
		fmt.Fprintf(stderr, "collecting %s to %s from %s\n", w.Start.Format(time.RFC3339), w.End.Format(time.RFC3339), client.URL())
	}
	return collect.Collect(ctx, client, w, opts)
}

func runScan(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var conn connFlags
	conn.register(fs)
	snapPath := fs.String("snapshot", "", "analyse this recorded snapshot instead of querying Prometheus")
	saveTo := fs.String("save-snapshot", "", "also save the collected snapshot to `FILE` (.gz to compress)")
	output := fs.String("o", "table", "output format: table, json, markdown or csv")
	outFile := fs.String("out", "", "write the report to `FILE` instead of standard output")
	verbose := fs.Bool("verbose", false, "show advice findings, collection progress and the PromQL used")
	pricesFile := fs.String("prices", "", "GPU prices CSV `FILE` (columns gpu, price_per_gpu_hour)")
	var priceFlags, namespaces stringList
	fs.Var(&priceFlags, "price", "a GPU price `key=price`, repeatable, for example h100-80gb=4.10 or *=2.5")
	fs.Var(&namespaces, "namespace", "only report pods in this namespace, repeatable")
	currency := fs.String("currency", "USD", "currency the prices are in, for display")
	var g gate
	fs.Float64Var(&g.overCost, "fail-over-cost", 0, "exit 3 when recoverable waste costs more than this")
	fs.Float64Var(&g.overGPUHours, "fail-over-gpu-hours", 0, "exit 3 when recoverable waste is more GPU-hours than this")
	fs.StringVar(&g.onSeverity, "fail-on", "", "exit 3 when any finding is this severe or worse: high, medium or low")
	stepSummary := fs.String("step-summary", "", "also append the Markdown report to `FILE`, such as $GITHUB_STEP_SUMMARY")
	th := analyze.Defaults()
	fs.Float64Var(&th.IdleActivity, "idle-activity", th.IdleActivity, "a GPU is idle when its 95th-percentile activity is below this share")
	fs.Float64Var(&th.MinHours, "min-hours", th.MinHours, "judge only pods that held GPUs at least this many hours")
	fs.Float64Var(&th.KVTarget, "kv-target", th.KVTarget, "KV cache share one replica may fill at peak, for sizing replicas")
	fs.Float64Var(&th.LowDuty, "low-duty", th.LowDuty, "a serving pod is mostly idle when busy for less than this share of its time")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	if fs.NArg() > 0 {
		return usagef("unexpected argument %q", fs.Arg(0))
	}
	render, err := renderer(*output, *verbose)
	if err != nil {
		return err
	}
	if err := checkThresholds(th); err != nil {
		return err
	}
	if err := g.validate(); err != nil {
		return err
	}
	table := price.NewTable()
	if *pricesFile != "" {
		if err := table.LoadCSV(*pricesFile); err != nil {
			return usagef("--prices: %v", err)
		}
	}
	for _, p := range priceFlags {
		if err := table.ParseFlag(p); err != nil {
			return usageError{err}
		}
	}

	explicitURL := false
	fs.Visit(func(f *flag.Flag) { explicitURL = explicitURL || f.Name == "prometheus" })
	var snap *snapshot.Snapshot
	switch {
	case *snapPath != "" && explicitURL:
		return usagef("use either --snapshot or --prometheus, not both")
	case *snapPath != "":
		snap, err = snapshot.Read(*snapPath)
	default:
		snap, err = conn.collectSnapshot(ctx, stderr, *verbose)
	}
	if err != nil {
		return err
	}
	if *saveTo != "" {
		if err := snapshot.Write(*saveTo, snap); err != nil {
			return fmt.Errorf("save snapshot: %w", err)
		}
	}

	f := fleet.Build(snap)
	opts := analyze.Options{Thresholds: th, Currency: *currency}
	if len(namespaces) > 0 {
		opts.Namespaces = map[string]bool{}
		for _, ns := range namespaces {
			opts.Namespaces[ns] = true
		}
	}
	rep := analyze.Run(f, snap, price.Resolve(table, f, snap), opts)
	if err := writeTo(*outFile, stdout, func(w io.Writer) error { return render(w, rep) }); err != nil {
		return err
	}
	if *stepSummary != "" {
		if err := appendSummary(*stepSummary, rep); err != nil {
			return fmt.Errorf("step summary: %w", err)
		}
	}
	if g.set() {
		return g.check(rep)
	}
	return nil
}

func runRecord(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var conn connFlags
	conn.register(fs)
	out := fs.String("o", "", "write the snapshot to `FILE` (.gz to compress); required")
	verbose := fs.Bool("verbose", false, "show collection progress")
	redact := fs.Bool("redact", false, "replace namespace, pod, node, model and GPU names with pseudonyms, to share the snapshot")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	if *out == "" {
		return usagef("-o FILE is required")
	}
	snap, err := conn.collectSnapshot(ctx, stderr, *verbose)
	if err != nil {
		return err
	}
	if *redact {
		snapshot.Redact(snap)
	}
	if err := snapshot.Write(*out, snap); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "saved %s: %d range and %d instant signals over %s, %d notes\n",
		*out, len(snap.Range), len(snap.Instant), snap.Window.Length(), len(snap.Notes))
	return nil
}

func renderer(format string, verbose bool) (func(io.Writer, *analyze.Report) error, error) {
	switch format {
	case "table", "text":
		return func(w io.Writer, r *analyze.Report) error {
			return report.Text(w, r, report.TextOptions{Verbose: verbose})
		}, nil
	case "json":
		return report.JSON, nil
	case "markdown", "md":
		return report.Markdown, nil
	case "csv":
		return report.CSV, nil
	}
	return nil, usagef("-o %q: want table, json, markdown or csv", format)
}

func checkThresholds(t analyze.Thresholds) error {
	for name, v := range map[string]float64{"--idle-activity": t.IdleActivity, "--kv-target": t.KVTarget, "--low-duty": t.LowDuty} {
		if v <= 0 || v > 1 {
			return usagef("%s must be above 0 and at most 1, got %g", name, v)
		}
	}
	if t.MinHours < 0 {
		return usagef("--min-hours must not be negative")
	}
	return nil
}

func writeTo(path string, stdout io.Writer, write func(io.Writer) error) (err error) {
	if path == "" {
		return write(stdout)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return write(f)
}
