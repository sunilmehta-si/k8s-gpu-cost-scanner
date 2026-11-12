package collect

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/prom"
	"github.com/sunilmehta-si/k8s-gpu-cost-scanner/internal/snapshot"
)

type fake struct {
	mu        sync.Mutex
	names     []string
	namesErr  error
	queries   []string
	failQuery string
}

func (f *fake) LabelValues(ctx context.Context, label string, m []string, s, e time.Time) ([]string, []string, error) {
	return f.names, nil, f.namesErr
}

func (f *fake) Query(ctx context.Context, q string, t time.Time) ([]prom.Sample, []string, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	if strings.HasPrefix(q, "group by (__name__)") {
		return []prom.Sample{{Labels: prom.Labels{"__name__": "kube_pod_owner"}, Value: 1}}, nil, nil
	}
	if f.failQuery != "" && strings.Contains(q, f.failQuery) {
		return nil, nil, errors.New("boom")
	}
	return []prom.Sample{{Labels: prom.Labels{"pod": "p"}, Value: 7}}, []string{"partial"}, nil
}

func (f *fake) QueryRange(ctx context.Context, q string, s, e time.Time, step time.Duration) ([]prom.Series, []string, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	return []prom.Series{{Labels: prom.Labels{"pod": "p"}, Points: []prom.Point{{T: 1, V: 1}}}}, nil, nil
}

func window(t *testing.T) snapshot.Window {
	w, err := snapshot.PlanWindow(time.Hour, time.Minute, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestCollectPicksVariantsAndNotesGaps(t *testing.T) {
	f := &fake{names: []string{"DCGM_FI_DEV_GPU_UTIL", "kube_pod_container_resource_requests", "kube_pod_status_phase", "vllm:generation_tokens_total"}, failQuery: "vllm:generation_tokens_total"}
	snap, err := Collect(context.Background(), f, window(t), Options{Tool: "test", Source: "http://p"})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Queries["gpu_activity"]; got != "DCGM_FI_DEV_GPU_UTIL / 100" {
		t.Errorf("gpu_activity query = %q", got)
	}
	if len(snap.Range["pod_gpu_requests"]) != 1 {
		t.Errorf("pod_gpu_requests = %v", snap.Range["pod_gpu_requests"])
	}
	if _, ok := snap.Instant["vllm_generation_tokens"]; ok {
		t.Error("a failed query left a result")
	}
	notes := strings.Join(snap.Notes, "\n")
	for _, want := range []string{"vllm_generation_tokens: query failed", "sglang_running: skipped", "opencost_gpu_hourly: skipped"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if !snap.Has("DCGM_FI_DEV_GPU_UTIL") || snap.Has("DCGM_FI_DEV_FB_USED") {
		t.Errorf("present = %v", snap.Present)
	}
}

func TestDiscoverFallsBackToInstantQuery(t *testing.T) {
	f := &fake{namesErr: errors.New("match[] unsupported")}
	names, err := Discover(context.Background(), f, window(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "kube_pod_owner" {
		t.Errorf("names = %v", names)
	}
}

func TestCollectFailsWhenEveryQueryFails(t *testing.T) {
	f := &fake{names: []string{"kube_pod_owner"}, failQuery: "kube_pod_owner"}
	if _, err := Collect(context.Background(), f, window(t), Options{}); err == nil {
		t.Fatal("collected nothing without an error")
	}
}
