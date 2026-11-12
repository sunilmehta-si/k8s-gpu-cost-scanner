package signals

import (
	"strings"
	"testing"
	"time"
)

func TestNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Catalog {
		if seen[s.Name] {
			t.Errorf("duplicate signal %s", s.Name)
		}
		seen[s.Name] = true
		if len(s.Variants) == 0 || s.Doc == "" || s.Source == "" {
			t.Errorf("%s: needs variants, a doc and a source", s.Name)
		}
		for _, v := range s.Variants {
			if len(v.Needs) == 0 || v.Query == "" {
				t.Errorf("%s: a variant without needs or query", s.Name)
			}
		}
	}
}

func TestChooseFollowsVariantOrder(t *testing.T) {
	s, _ := ByName("gpu_activity")
	cases := []struct {
		present []string
		want    string
	}{
		{[]string{"DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO", "DCGM_FI_DEV_GPU_UTIL"}, "DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO"},
		{[]string{"DCGM_FI_PROF_GR_ENGINE_ACTIVE", "DCGM_FI_DEV_GPU_UTIL"}, "DCGM_FI_PROF_GR_ENGINE_ACTIVE"},
		{[]string{"DCGM_FI_DEV_GPU_UTIL"}, "DCGM_FI_DEV_GPU_UTIL / 100"},
	}
	for _, c := range cases {
		present := map[string]bool{}
		for _, m := range c.present {
			present[m] = true
		}
		v, ok := s.Choose(present)
		if !ok || v.Query != c.want {
			t.Errorf("present %v: chose %q", c.present, v.Query)
		}
	}
	if _, ok := s.Choose(map[string]bool{}); ok {
		t.Error("chose a variant with nothing present")
	}
}

func TestOldMemoryFieldIsConvertedToBytes(t *testing.T) {
	s, _ := ByName("gpu_memory_used_bytes")
	v, _ := s.Choose(map[string]bool{"DCGM_FI_DEV_FB_USED": true})
	if !strings.Contains(v.Query, "* 1048576") {
		t.Errorf("MiB field not converted: %s", v.Query)
	}
}

func TestRender(t *testing.T) {
	s, _ := ByName("gpu_memory_total_bytes")
	v, _ := s.Choose(map[string]bool{"DCGM_FI_DEV_FB_USED": true, "DCGM_FI_DEV_FB_FREE": true})
	got := v.Render(24*time.Hour, 5*time.Minute)
	if !strings.Contains(got, "[86400s:300s]") || strings.Contains(got, "$") {
		t.Errorf("rendered %s", got)
	}
}

func TestMetricsListsEveryNeed(t *testing.T) {
	m := Metrics()
	for _, want := range []string{"DCGM_FI_DEV_FB_USED_BYTES", "vllm:kv_cache_usage_perc", "vllm:gpu_cache_usage_perc", "kube_pod_status_phase", "node_gpu_hourly_cost"} {
		found := false
		for _, x := range m {
			found = found || x == want
		}
		if !found {
			t.Errorf("Metrics() is missing %s", want)
		}
	}
}
