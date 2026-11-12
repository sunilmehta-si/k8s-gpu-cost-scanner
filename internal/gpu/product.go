// Package gpu names GPU products the same way whichever source reported them.
//
// The DCGM exporter reports "NVIDIA A100-SXM4-80GB", GPU Feature Discovery labels the node
// "NVIDIA-A100-SXM4-80GB", and GKE labels it "nvidia-a100-80gb". Prices are looked up by one short key for all
// three: "a100-80gb".
package gpu

import (
	"regexp"
	"strings"
	"unicode"
)

// Product is a GPU model's normalized key, such as "h100-80gb" or "l4".
type Product string

// Unknown is the product of a GPU no source named.
const Unknown Product = "unknown"

var (
	familyRE = regexp.MustCompile(`^(a|b|h|l|t|v|p|k|gb|gh)\d+[a-z]*$`)
	memoryRE = regexp.MustCompile(`^(\d+)gb$`)
	noise    = map[string]bool{
		"nvidia": true, "tesla": true, "geforce": true, "sxm": true, "sxm2": true, "sxm3": true, "sxm4": true,
		"sxm5": true, "pcie": true, "hbm2": true, "hbm2e": true, "hbm3": true, "hbm3e": true, "server": true,
		"edition": true, "blackwell": true, "hopper": true, "ada": true, "ampere": true, "gpu": true,
		"generation": true, "max": true, "q": true,
	}
)

// Normalize turns any GPU product name into its key. It ignores vendor words, form factors (SXM, PCIe) and memory
// types, keeps the memory size when the name has one, and keeps NVL variants apart. A MIG suffix is dropped:
// see ParseMIG for the slice.
func Normalize(name string) Product {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return Unknown
	}
	var family []string
	var mem string
	nvl := false
	// Split on any Unicode space as well as - _ /: a regexp's \s misses some, such as the vertical tab.
	tokens := strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' })
	// A MIG suffix ("...-MIG-1g.5gb") names a slice, not the product: drop it and what follows.
	for i, tok := range tokens {
		if tok == "mig" {
			tokens = tokens[:i]
			break
		}
	}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case tok == "" || noise[tok]:
		case memoryRE.MatchString(tok):
			if mem == "" {
				mem = tok
			}
		case tok == "nvl":
			nvl = true
		case tok == "rtx" && family == nil:
			family = []string{"rtx"}
			for j := i + 1; j < len(tokens); j++ {
				next := tokens[j]
				if noise[next] || memoryRE.MatchString(next) || next == "" {
					continue
				}
				family = append(family, next)
				i = j
				if next != "pro" {
					break
				}
			}
		case familyRE.MatchString(tok) && family == nil:
			family = []string{tok}
		}
	}
	if family == nil {
		var rest []string
		for _, tok := range tokens {
			if tok != "" && !noise[tok] {
				rest = append(rest, tok)
			}
		}
		if len(rest) == 0 {
			return Unknown
		}
		return Product(strings.Join(rest, "-"))
	}
	key := strings.Join(family, "-")
	if nvl {
		key += "-nvl"
	}
	if mem != "" {
		key += "-" + mem
	}
	return Product(key)
}

// Family is the product without its memory size: "a100-80gb" becomes "a100". A price set for the family covers
// every memory size that has no price of its own.
func (p Product) Family() Product {
	if i := strings.LastIndex(string(p), "-"); i > 0 && memoryRE.MatchString(string(p[i+1:])) {
		return p[:i]
	}
	return p
}
