package gpu

import "testing"

func FuzzNormalize(f *testing.F) {
	for _, s := range []string{"NVIDIA A100-SXM4-80GB", "Tesla T4", "nvidia-tesla-a100", "NVIDIA RTX PRO 6000 Blackwell Server Edition", "NVIDIA-A100-SXM4-40GB-MIG-1g.5gb", "", "rtx", "80gb", "nvl"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		p := Normalize(name)
		if p == "" {
			t.Fatalf("Normalize(%q) is empty", name)
		}
		// A key is its own key: prices set by key match what the scan normalizes.
		if again := Normalize(string(p)); again != p {
			t.Fatalf("Normalize(%q) = %q, but Normalize(%q) = %q", name, p, p, again)
		}
		if fam := p.Family(); fam == "" || len(fam) > len(p) {
			t.Fatalf("Family(%q) = %q", p, fam)
		}
	})
}

func FuzzParseMIG(f *testing.F) {
	for _, s := range []string{"1g.10gb", "nvidia_com_mig_3g_40gb", "NVIDIA-A100-SXM4-40GB-MIG-1g.5gb", "nvidia_com_gpu", "mig_g_", "0g.1gb"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		m, ok := ParseMIG(s)
		if !ok {
			return
		}
		if m.Compute <= 0 {
			t.Fatalf("ParseMIG(%q) = %+v with no compute", s, m)
		}
		again, ok := ParseMIG(m.Profile)
		if !ok || again != m {
			t.Fatalf("profile %q from %q does not parse back: %+v %v", m.Profile, s, again, ok)
		}
		if share, _ := m.Share("a100"); share <= 0 || share > 1 {
			t.Fatalf("share %v", share)
		}
	})
}
