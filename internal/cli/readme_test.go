package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/analyze"
)

// TestReadmeExampleIsRealOutput checks every line of the README's example report is a line of the golden report
// for the demo fleet, so the README cannot show numbers gpucost does not produce.
func TestReadmeExampleIsRealOutput(t *testing.T) {
	readme := read(t, "README.md")
	golden := map[string]bool{}
	for _, l := range strings.Split(read(t, "testdata", "golden", "demo.txt"), "\n") {
		golden[l] = true
	}
	start := strings.Index(readme, "```text\n")
	end := strings.Index(readme[start+8:], "```")
	if start < 0 || end < 0 {
		t.Fatal("no example report in the README")
	}
	n := 0
	for _, l := range strings.Split(readme[start+8:start+8+end], "\n") {
		if l == "" || strings.HasPrefix(l, "$ ") {
			continue
		}
		n++
		if !golden[l] {
			t.Errorf("README example line is not in testdata/golden/demo.txt:\n%q", l)
		}
	}
	if n < 10 {
		t.Errorf("only %d example lines checked", n)
	}
}

// TestReadmeRulesTable checks the README names every rule with its title and says whether it counts as waste.
func TestReadmeRulesTable(t *testing.T) {
	readme := read(t, "README.md")
	for _, r := range analyze.Rules {
		row := regexp.MustCompile(`(?m)^\| ` + r.Rule + ` \| \*\*` + regexp.QuoteMeta(r.Title) + `\*\*.*\| (.+) \|$`).FindStringSubmatch(readme)
		if row == nil {
			t.Errorf("README rules table lacks %s %q", r.Rule, r.Title)
			continue
		}
		if counted := !strings.HasPrefix(row[1], "no"); counted != r.Counted {
			t.Errorf("README says %s counted=%v, the rule says %v", r.Rule, counted, r.Counted)
		}
	}
}

// TestReadmeDemoPrices checks the prices the README quotes are the ones the demo is scanned with.
func TestReadmeDemoPrices(t *testing.T) {
	readme := read(t, "README.md")
	prices := read(t, "testdata", "demo.prices.csv")
	for _, pair := range [][2]string{{"$4.00 per H100-hour", "h100-80gb,4.00"}, {"$0.70 per L4-hour", "l4,0.70"}} {
		if !strings.Contains(readme, pair[0]) || !strings.Contains(prices, pair[1]) {
			t.Errorf("README quotes %q; demo prices must say %q", pair[0], pair[1])
		}
	}
}

func read(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
