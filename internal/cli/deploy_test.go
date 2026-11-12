package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDeployArgsAreScanFlags keeps deploy/gpucost.yaml in step with the CLI: every flag the CronJob passes must be
// one gpucost scan accepts.
func TestDeployArgsAreScanFlags(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "gpucost.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	known := scanFlagNames(t)
	args := regexp.MustCompile(`(?m)^\s+- (--?[a-z-]+)=`).FindAllSubmatch(b, -1)
	if len(args) < 3 {
		t.Fatalf("found only %d flags in the CronJob", len(args))
	}
	for _, m := range args {
		name := strings.TrimLeft(string(m[1]), "-")
		if !known[name] {
			t.Errorf("deploy/gpucost.yaml passes %s, which gpucost scan does not accept", m[1])
		}
	}
	for _, must := range []string{"automountServiceAccountToken: false", "readOnlyRootFilesystem: true", `drop: ["ALL"]`, "runAsNonRoot: true"} {
		if !bytes.Contains(b, []byte(must)) {
			t.Errorf("deploy/gpucost.yaml lost %q", must)
		}
	}
}

func scanFlagNames(t *testing.T) map[string]bool {
	t.Helper()
	code, _, errOut := run("scan", "-h")
	if code != 0 {
		t.Fatalf("scan -h: exit %d", code)
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  -([a-z-]+)`).FindAllStringSubmatch(errOut, -1) {
		names[m[1]] = true
	}
	return names
}

var _ = flag.ErrHelp
