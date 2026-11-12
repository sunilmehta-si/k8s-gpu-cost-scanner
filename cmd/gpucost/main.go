// Command gpucost finds the GPUs a Kubernetes inference cluster pays for and does not use, and what each model
// costs per hour and per million tokens. Run "gpucost help" for usage.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Main(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
