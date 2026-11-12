package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

// fixtureWithout returns the fixture snapshot with one signal removed.
func fixtureWithout(t *testing.T, signal string) string {
	t.Helper()
	s, err := snapshot.Read(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	delete(s.Range, signal)
	delete(s.Instant, signal)
	delete(s.Queries, signal)
	path := filepath.Join(t.TempDir(), "without.json")
	if err := snapshot.Write(path, s); err != nil {
		t.Fatal(err)
	}
	return path
}
