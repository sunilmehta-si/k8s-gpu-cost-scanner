package prom

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// WithBearerToken sends a fixed bearer token, for example a Grafana Cloud or Google Managed Prometheus token.
func WithBearerToken(token string) Option {
	return func(c *Client) { c.token = func() (string, error) { return token, nil } }
}

// WithBearerTokenFile reads the bearer token from a file before every request, so a rotated token, such as a
// projected Kubernetes service account token, is picked up without a restart.
func WithBearerTokenFile(path string) Option {
	return func(c *Client) {
		c.token = func() (string, error) {
			b, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("bearer token file: %w", err)
			}
			t := strings.TrimSpace(string(b))
			if t == "" {
				return "", errors.New("bearer token file is empty")
			}
			return t, nil
		}
	}
}

// HTTPClient returns an HTTP client for Prometheus with the given timeout, trusting the CA bundle in caFile in
// addition to the system roots when caFile is set. It never turns certificate checks off.
func HTTPClient(timeout time.Duration, caFile string) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA file %s holds no PEM certificates", caFile)
		}
		tr.TLSClientConfig.RootCAs = pool
	}
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}
