package prom

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func server(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	c, err := New(ts.URL+"/prom", WithRetries(2, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestQueryVector(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/prom/api/v1/query" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("query"); got != `up{job="x"}` {
			t.Errorf("query = %q", got)
		}
		if got := r.PostForm.Get("time"); got != "1700000000.5" {
			t.Errorf("time = %q", got)
		}
		w.Write([]byte(`{"status":"success","warnings":["w1"],"data":{"resultType":"vector","result":[
			{"metric":{"pod":"b"},"value":[1700000000.5,"2"]},
			{"metric":{"pod":"a"},"value":[1700000000.5,"NaN"]},
			{"metric":{},"value":[1700000000.5,"+Inf"]}]}}`))
	})
	got, warnings, err := c.Query(context.Background(), `up{job="x"}`, time.UnixMilli(1700000000500))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0] != "w1" {
		t.Errorf("warnings = %v", warnings)
	}
	if len(got) != 3 {
		t.Fatalf("got %d samples", len(got))
	}
	// Sorted by label key: {pod="a"} < {pod="b"} < {}.
	if !math.IsNaN(got[0].Value) || got[1].Value != 2 || !math.IsInf(got[2].Value, 1) {
		t.Errorf("values = %v", got)
	}
	if got[1].Labels["pod"] != "b" || len(got[2].Labels) != 0 {
		t.Errorf("labels = %v", got)
	}
}

func TestQueryScalar(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"3.5"]}}`))
	})
	got, _, err := c.Query(context.Background(), "3.5", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != 3.5 || len(got[0].Labels) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestQueryRange(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/prom/api/v1/query_range" {
			t.Errorf("path = %s", r.URL.Path)
		}
		for k, want := range map[string]string{"start": "1700000000", "end": "1700000600", "step": "300"} {
			if got := r.PostForm.Get(k); got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"gpu":"1"},"values":[[1700000000,"0.5"],[1700000300,"0.25"]]},
			{"metric":{"gpu":"0"},"values":[[1700000000,"1"]]}]}}`))
	})
	got, _, err := c.QueryRange(context.Background(), "x", time.Unix(1700000000, 0), time.Unix(1700000600, 0), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Labels["gpu"] != "0" {
		t.Fatalf("got %v", got)
	}
	want := []Point{{T: 1700000000000, V: 0.5}, {T: 1700000300000, V: 0.25}}
	if len(got[1].Points) != 2 || got[1].Points[0] != want[0] || got[1].Points[1] != want[1] {
		t.Errorf("points = %v", got[1].Points)
	}
}

func TestQueryRangeArguments(t *testing.T) {
	c, _ := New("http://example.invalid")
	now := time.Now()
	if _, _, err := c.QueryRange(context.Background(), "x", now, now.Add(time.Hour), 0); err == nil {
		t.Error("zero step accepted")
	}
	if _, _, err := c.QueryRange(context.Background(), "x", now, now, time.Minute); err == nil {
		t.Error("empty range accepted")
	}
}

func TestAPIErrorIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error at char 3"}`))
	})
	_, _, err := c.Query(context.Background(), "up{", time.Now())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Type != "bad_data" || !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestTransientErrorIsRetried(t *testing.T) {
	var calls atomic.Int32
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"error","errorType":"unavailable","error":"busy"}`))
			return
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	})
	got, _, err := c.Query(context.Background(), "up", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || calls.Load() != 3 {
		t.Errorf("got %v after %d calls", got, calls.Load())
	}
}

func TestNonJSONErrorKeepsStatus(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden by proxy"))
	})
	_, _, err := c.Query(context.Background(), "up", time.Now())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != 403 || !strings.Contains(apiErr.Message, "forbidden by proxy") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewRejectsBadURLs(t *testing.T) {
	for _, u := range []string{"prometheus:9090", "ftp://x", "http://", ""} {
		if _, err := New(u); err == nil {
			t.Errorf("New(%q) accepted", u)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("https://user:secret@prom.example/x"); strings.Contains(got, "secret") {
		t.Errorf("Redact kept the password: %s", got)
	}
	c, _ := New("https://user:secret@prom.example")
	if strings.Contains(c.URL(), "secret") {
		t.Errorf("URL() kept the password: %s", c.URL())
	}
}

func TestPointJSONRoundTrip(t *testing.T) {
	in := []Point{{T: 1, V: 0.5}, {T: 2, V: math.NaN()}, {T: 3, V: math.Inf(-1)}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[[1,0.5],[2,"NaN"],[3,"-Inf"]]` {
		t.Errorf("json = %s", b)
	}
	var out []Point
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out[0] != in[0] || !math.IsNaN(out[1].V) || !math.IsInf(out[2].V, -1) {
		t.Errorf("round trip = %v", out)
	}
}

func TestLabelsKeyIsSorted(t *testing.T) {
	if got := (Labels{"b": "2", "a": "1"}).Key(); got != `{a="1",b="2"}` {
		t.Errorf("Key = %s", got)
	}
}

func TestLabelValues(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/prom/api/v1/label/__name__/values" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.PostForm["match[]"]; len(got) != 2 || got[0] != `{__name__=~"DCGM_.*"}` {
			t.Errorf("match[] = %v", got)
		}
		if r.PostForm.Get("start") == "" || r.PostForm.Get("end") == "" {
			t.Error("start or end missing")
		}
		w.Write([]byte(`{"status":"success","data":["vllm:num_requests_running","DCGM_FI_DEV_GPU_UTIL"]}`))
	})
	got, _, err := c.LabelValues(context.Background(), "__name__", []string{`{__name__=~"DCGM_.*"}`, `{__name__=~"vllm:.*"}`}, time.Unix(0, 0), time.Unix(3600, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "DCGM_FI_DEV_GPU_UTIL" {
		t.Errorf("got %v", got)
	}
}
