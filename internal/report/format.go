// Package report renders an analysis as a terminal table, JSON, Markdown or CSV.
package report

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// money formats an amount in the report's currency: $1,234.56 for USD, 1,234.56 EUR otherwise.
func money(v float64, currency string) string {
	n := grouped(v, 2)
	switch strings.ToUpper(currency) {
	case "USD", "":
		if strings.HasPrefix(n, "-") {
			return "-$" + n[1:]
		}
		return "$" + n
	default:
		return n + " " + strings.ToUpper(currency)
	}
}

// grouped formats a number with thousands separators and the given decimals.
func grouped(v float64, decimals int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "n/a"
	}
	s := fmt.Sprintf("%.*f", decimals, math.Abs(v))
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if v < 0 && strings.Trim(out, "0.,") != "" {
		out = "-" + out
	}
	return out
}

// hours formats GPU-hours.
func hours(v float64) string { return grouped(v, 1) }

// pct formats a 0-1 share as a percentage.
func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

// count formats a large count compactly: 950, 12.3K, 4.5M, 1.2B.
func count(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1e9:
		return fmt.Sprintf("%.1fB", v/1e9)
	case a >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case a >= 1e4:
		return fmt.Sprintf("%.1fK", v/1e3)
	default:
		return grouped(v, 0)
	}
}

// wrap breaks text into lines of at most width runes, on spaces.
func wrap(text string, width int) []string {
	var lines []string
	var cur strings.Builder
	for _, word := range strings.Fields(text) {
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+1+utf8.RuneCountInString(word) > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// truncate shortens s to at most n runes, marking the cut.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
