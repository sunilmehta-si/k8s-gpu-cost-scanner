// Package prom is a small client for the Prometheus HTTP API: instant and range queries, nothing else.
//
// It is written against the API every Prometheus-compatible server implements (Prometheus, Thanos, Mimir,
// VictoriaMetrics, Google Managed Prometheus), so it needs no client library.
package prom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Labels are the label names and values of one series.
type Labels map[string]string

// Key is a stable string for the label set, used to compare and sort series.
func (l Labels) Key() string {
	names := make([]string, 0, len(l))
	for n := range l {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteByte('{')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(n)
		b.WriteString(`="`)
		b.WriteString(l[n])
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// Sample is one value of an instant vector.
type Sample struct {
	Labels Labels  `json:"labels"`
	Value  float64 `json:"value"`
}

// Point is one value of a range series, at a Unix time in milliseconds.
type Point struct {
	T int64
	V float64
}

// MarshalJSON writes a point as [t, v], the compact form snapshots use.
func (p Point) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]any{p.T, jsonFloat(p.V)})
}

// UnmarshalJSON reads a point written by MarshalJSON.
func (p *Point) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &p.T); err != nil {
		return err
	}
	v, err := parseJSONFloat(raw[1])
	p.V = v
	return err
}

// Series is a range series: its labels and its points in time order.
type Series struct {
	Labels Labels  `json:"labels"`
	Points []Point `json:"points"`
}

// Client queries one Prometheus-compatible server.
type Client struct {
	base    *url.URL
	http    *http.Client
	headers http.Header
	token   func() (string, error)
	retries int
	backoff time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client, for timeouts and TLS.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithHeader adds a header to every request, for example X-Scope-OrgID for a multi-tenant Mimir or Thanos.
func WithHeader(name, value string) Option {
	return func(c *Client) { c.headers.Add(name, value) }
}

// WithRetries sets how many times a request is retried after a transient failure, and the first wait.
func WithRetries(n int, backoff time.Duration) Option {
	return func(c *Client) { c.retries, c.backoff = n, backoff }
}

// New returns a client for the server at rawURL, for example http://prometheus.monitoring:9090.
// A path prefix is kept, so a server behind a proxy at /prometheus works.
func New(rawURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("prometheus URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("prometheus URL %q: the scheme must be http or https", Redact(rawURL))
	}
	if u.Host == "" {
		return nil, fmt.Errorf("prometheus URL %q has no host", Redact(rawURL))
	}
	c := &Client{
		base:    u,
		http:    &http.Client{Timeout: 2 * time.Minute},
		headers: http.Header{},
		retries: 2,
		backoff: 500 * time.Millisecond,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// URL is the server's address with any credentials removed, safe to print and store.
func (c *Client) URL() string { return Redact(c.base.String()) }

// APIError is an error the server returned in the API's error format.
type APIError struct {
	HTTPStatus int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Type == "" {
		return fmt.Sprintf("prometheus: HTTP %d: %s", e.HTTPStatus, e.Message)
	}
	return fmt.Sprintf("prometheus: %s: %s", e.Type, e.Message)
}

// Temporary reports whether retrying the same request may succeed.
func (e *APIError) Temporary() bool {
	switch e.HTTPStatus {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return e.Type == "timeout"
}

type apiResponse struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
}

type apiData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

type apiVectorSample struct {
	Metric Labels             `json:"metric"`
	Value  [2]json.RawMessage `json:"value"`
}

type apiMatrixSeries struct {
	Metric Labels               `json:"metric"`
	Values [][2]json.RawMessage `json:"values"`
}

// Query evaluates an instant query at time t. It returns the samples and any warnings the server attached.
// A scalar result is returned as one sample with no labels.
func (c *Client) Query(ctx context.Context, query string, t time.Time) ([]Sample, []string, error) {
	form := url.Values{"query": {query}, "time": {formatTime(t)}}
	data, warnings, err := c.do(ctx, "/api/v1/query", form)
	if err != nil {
		return nil, warnings, err
	}
	switch data.ResultType {
	case "vector":
		var raw []apiVectorSample
		if err := json.Unmarshal(data.Result, &raw); err != nil {
			return nil, warnings, fmt.Errorf("prometheus: decode vector: %w", err)
		}
		out := make([]Sample, 0, len(raw))
		for _, s := range raw {
			v, err := parseJSONFloat(s.Value[1])
			if err != nil {
				return nil, warnings, fmt.Errorf("prometheus: sample value: %w", err)
			}
			out = append(out, Sample{Labels: nonNil(s.Metric), Value: v})
		}
		sortSamples(out)
		return out, warnings, nil
	case "scalar":
		var raw [2]json.RawMessage
		if err := json.Unmarshal(data.Result, &raw); err != nil {
			return nil, warnings, fmt.Errorf("prometheus: decode scalar: %w", err)
		}
		v, err := parseJSONFloat(raw[1])
		if err != nil {
			return nil, warnings, fmt.Errorf("prometheus: scalar value: %w", err)
		}
		return []Sample{{Labels: Labels{}, Value: v}}, warnings, nil
	default:
		return nil, warnings, fmt.Errorf("prometheus: instant query returned a %s, want a vector", data.ResultType)
	}
}

// QueryRange evaluates a range query from start to end at the given step.
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Series, []string, error) {
	if step <= 0 {
		return nil, nil, errors.New("prometheus: range query step must be positive")
	}
	if !end.After(start) {
		return nil, nil, errors.New("prometheus: range query end must be after its start")
	}
	form := url.Values{
		"query": {query},
		"start": {formatTime(start)},
		"end":   {formatTime(end)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	data, warnings, err := c.do(ctx, "/api/v1/query_range", form)
	if err != nil {
		return nil, warnings, err
	}
	if data.ResultType != "matrix" {
		return nil, warnings, fmt.Errorf("prometheus: range query returned a %s, want a matrix", data.ResultType)
	}
	var raw []apiMatrixSeries
	if err := json.Unmarshal(data.Result, &raw); err != nil {
		return nil, warnings, fmt.Errorf("prometheus: decode matrix: %w", err)
	}
	out := make([]Series, 0, len(raw))
	for _, s := range raw {
		pts := make([]Point, 0, len(s.Values))
		for _, pair := range s.Values {
			ts, err := parseJSONFloat(pair[0])
			if err != nil {
				return nil, warnings, fmt.Errorf("prometheus: point time: %w", err)
			}
			v, err := parseJSONFloat(pair[1])
			if err != nil {
				return nil, warnings, fmt.Errorf("prometheus: point value: %w", err)
			}
			pts = append(pts, Point{T: int64(math.Round(ts * 1000)), V: v})
		}
		out = append(out, Series{Labels: nonNil(s.Metric), Points: pts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Labels.Key() < out[j].Labels.Key() })
	return out, warnings, nil
}

func (c *Client) do(ctx context.Context, path string, form url.Values) (*apiData, []string, error) {
	var lastErr error
	wait := c.backoff
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
			wait *= 2
		}
		data, warnings, err := c.once(ctx, path, form)
		if err == nil {
			return data, warnings, nil
		}
		lastErr = err
		var apiErr *APIError
		if errors.As(err, &apiErr) && !apiErr.Temporary() {
			return nil, warnings, err
		}
		if ctx.Err() != nil {
			return nil, warnings, err
		}
	}
	return nil, nil, lastErr
}

func (c *Client) once(ctx context.Context, path string, form url.Values) (*apiData, []string, error) {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	// POST keeps long queries out of URLs, request logs and proxies' length limits.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.token != nil {
		tok, err := c.token()
		if err != nil {
			return nil, nil, fmt.Errorf("prometheus: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for name, values := range c.headers {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("prometheus: %w", scrubURLError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("prometheus: read response: %w", err)
	}
	var r apiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return nil, nil, &APIError{HTTPStatus: resp.StatusCode, Message: msg}
	}
	if r.Status != "success" {
		return nil, r.Warnings, &APIError{HTTPStatus: resp.StatusCode, Type: r.ErrorType, Message: r.Error}
	}
	var d apiData
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return nil, r.Warnings, fmt.Errorf("prometheus: decode data: %w", err)
	}
	return &d, r.Warnings, nil
}

// Redact removes a password or token from a URL so it can be printed or stored.
func Redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("redacted")
	return u.String()
}

func scrubURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s %s: %w", ue.Op, Redact(ue.URL), ue.Err)
	}
	return err
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', -1, 64)
}

func parseJSONFloat(raw json.RawMessage) (float64, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strconv.ParseFloat(s, 64)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, err
	}
	return f, nil
}

// jsonFloat writes NaN and infinities as strings, the way the Prometheus API does, since JSON has no such numbers.
type jsonFloat float64

func (f jsonFloat) MarshalJSON() ([]byte, error) {
	v := float64(f)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return json.Marshal(strconv.FormatFloat(v, 'f', -1, 64))
	}
	return json.Marshal(v)
}

func nonNil(l Labels) Labels {
	if l == nil {
		return Labels{}
	}
	return l
}

func sortSamples(s []Sample) {
	sort.Slice(s, func(i, j int) bool { return s[i].Labels.Key() < s[j].Labels.Key() })
}
