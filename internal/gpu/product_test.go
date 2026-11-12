package gpu

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]Product{
		// DCGM exporter modelName, as nvidia-smi prints it.
		"NVIDIA A100-SXM4-80GB":   "a100-80gb",
		"NVIDIA A100-SXM4-40GB":   "a100-40gb",
		"NVIDIA A100 80GB PCIe":   "a100-80gb",
		"NVIDIA H100 80GB HBM3":   "h100-80gb",
		"NVIDIA H100 PCIe":        "h100",
		"NVIDIA H100 NVL":         "h100-nvl",
		"NVIDIA H200":             "h200",
		"NVIDIA H200 NVL":         "h200-nvl",
		"NVIDIA B200":             "b200",
		"NVIDIA GB200":            "gb200",
		"NVIDIA GH200 480GB":      "gh200-480gb",
		"NVIDIA L4":               "l4",
		"NVIDIA L40S":             "l40s",
		"NVIDIA A10G":             "a10g",
		"NVIDIA A10":              "a10",
		"Tesla T4":                "t4",
		"Tesla V100-SXM2-16GB":    "v100-16gb",
		"NVIDIA RTX A6000":        "rtx-a6000",
		"NVIDIA GeForce RTX 4090": "rtx-4090",
		"NVIDIA RTX PRO 6000 Blackwell Server Edition": "rtx-pro-6000",
		// dcgm-exporter --replace-blanks-in-model-name, and GPU Feature Discovery's nvidia.com/gpu.product.
		"NVIDIA-A100-SXM4-80GB":             "a100-80gb",
		"NVIDIA-H100-80GB-HBM3":             "h100-80gb",
		"Tesla-T4":                          "t4",
		"NVIDIA-A100-SXM4-40GB-MIG-1g.5gb":  "a100-40gb",
		"NVIDIA-H100-80GB-HBM3-MIG-3g.40gb": "h100-80gb",
		// GKE's cloud.google.com/gke-accelerator.
		"nvidia-tesla-a100": "a100",
		"nvidia-a100-80gb":  "a100-80gb",
		"nvidia-h100-80gb":  "h100-80gb",
		"nvidia-l4":         "l4",
		"nvidia-tesla-t4":   "t4",
		// Nothing to go on.
		"":                   Unknown,
		"NVIDIA":             Unknown,
		"Some Accelerator X": "some-accelerator-x",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFamily(t *testing.T) {
	for in, want := range map[Product]Product{"a100-80gb": "a100", "l4": "l4", "h100-nvl": "h100-nvl", "gh200-480gb": "gh200", "rtx-pro-6000": "rtx-pro-6000"} {
		if got := in.Family(); got != want {
			t.Errorf("%s.Family() = %s, want %s", in, got, want)
		}
	}
}
