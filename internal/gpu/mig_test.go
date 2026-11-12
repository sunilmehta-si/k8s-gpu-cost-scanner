package gpu

import "testing"

func TestParseMIG(t *testing.T) {
	cases := map[string]string{
		"1g.10gb":                          "1g.10gb",
		"nvidia.com/mig-3g.40gb":           "3g.40gb",
		"nvidia_com_mig_1g_10gb":           "1g.10gb",
		"nvidia_com_mig_7g_80gb":           "7g.80gb",
		"NVIDIA-A100-SXM4-40GB-MIG-1g.5gb": "1g.5gb",
		"1g.10gb+me":                       "1g.10gb",
	}
	for in, want := range cases {
		m, ok := ParseMIG(in)
		if !ok || m.Profile != want {
			t.Errorf("ParseMIG(%q) = %+v, %v; want %s", in, m, ok, want)
		}
	}
	for _, in := range []string{"nvidia_com_gpu", "nvidia.com/gpu", "", "0g.5gb", "NVIDIA A100-SXM4-80GB"} {
		if m, ok := ParseMIG(in); ok {
			t.Errorf("ParseMIG(%q) = %+v, want no MIG", in, m)
		}
	}
}

func TestShare(t *testing.T) {
	m, _ := ParseMIG("3g.40gb")
	if s, ok := m.Share("a100-80gb"); !ok || s != 3.0/7 {
		t.Errorf("3g on a100 = %v, %v", s, ok)
	}
	m, _ = ParseMIG("2g.12gb")
	if s, ok := m.Share("a30-24gb"); !ok || s != 0.5 {
		t.Errorf("2g on a30 = %v, %v", s, ok)
	}
	m, _ = ParseMIG("1g.10gb")
	if s, ok := m.Share("mystery"); ok || s != 1.0/7 {
		t.Errorf("unknown product = %v, %v; want 1/7, false", s, ok)
	}
}
