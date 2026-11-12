package gpu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MIG is a Multi-Instance GPU slice: a profile such as "1g.10gb" carved out of one physical GPU.
type MIG struct {
	// Profile is the profile name, for example "3g.40gb".
	Profile string
	// Compute is the number of compute slices the profile takes ("3g" is 3).
	Compute int
}

var migRE = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])(\d+)g\.(\d+)gb(?:\+me)?$`)

// ParseMIG reads a MIG profile from a profile name ("1g.10gb"), a Kubernetes resource name
// ("nvidia.com/mig-1g.10gb"), the sanitized form kube-state-metrics uses ("nvidia_com_mig_1g_10gb") or a GPU
// Feature Discovery product ("NVIDIA-A100-SXM4-40GB-MIG-1g.5gb").
func ParseMIG(s string) (MIG, bool) {
	s = strings.TrimSpace(s)
	// kube-state-metrics turns "nvidia.com/mig-1g.10gb" into "nvidia_com_mig_1g_10gb".
	if i := strings.Index(s, "mig_"); i >= 0 {
		rest := s[i+len("mig_"):]
		if j := strings.Index(rest, "g_"); j > 0 {
			s = rest[:j] + "g." + rest[j+2:]
		}
	}
	m := migRE.FindStringSubmatch(" " + s)
	if m == nil {
		return MIG{}, false
	}
	c, err := strconv.Atoi(m[1])
	if err != nil || c <= 0 {
		return MIG{}, false
	}
	return MIG{Profile: fmt.Sprintf("%sg.%sgb", m[1], m[2]), Compute: c}, true
}

// computeSlices is how many compute slices each MIG-capable GPU family divides into.
var computeSlices = map[Product]int{
	"a30": 4, "a100": 7, "h100": 7, "h100-nvl": 7, "h200": 7, "h200-nvl": 7, "h20": 7, "gh200": 7, "b200": 7, "gb200": 7,
	"rtx-pro-6000": 4,
}

// Share is the fraction of the physical GPU's compute the slice holds: 3g on an A100 is 3/7. It is the basis for
// pricing a slice. The second result is false when the product's slice count is unknown; 7 is assumed then.
func (m MIG) Share(p Product) (float64, bool) {
	n, ok := computeSlices[p.Family()]
	if !ok {
		n = 7
	}
	share := float64(m.Compute) / float64(n)
	if share > 1 {
		share = 1
	}
	return share, ok
}
