package report

import (
	"math"
	"strings"
	"testing"
)

func TestMoney(t *testing.T) {
	cases := map[string]string{
		money(1234567.891, "USD"): "$1,234,567.89",
		money(0.5, "usd"):         "$0.50",
		money(-12, "USD"):         "-$12.00",
		money(999.999, "EUR"):     "1,000.00 EUR",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestGroupedAndCount(t *testing.T) {
	for got, want := range map[string]string{
		grouped(100, 0): "100", grouped(1000, 0): "1,000", grouped(-1234.5, 1): "-1,234.5", grouped(math.NaN(), 1): "n/a",
		count(950): "950", count(12345): "12.3K", count(4.5e6): "4.5M", count(1.2e9): "1.2B",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestWrap(t *testing.T) {
	lines := wrap("the quick brown fox jumps over the lazy dog", 10)
	for _, l := range lines {
		if len(l) > 10 {
			t.Errorf("line %q is too long", l)
		}
	}
	if strings.Join(lines, " ") != "the quick brown fox jumps over the lazy dog" {
		t.Errorf("wrap lost words: %q", lines)
	}
	if got := truncate("abcdefgh", 5); got != "abcd…" {
		t.Errorf("truncate = %q", got)
	}
}
