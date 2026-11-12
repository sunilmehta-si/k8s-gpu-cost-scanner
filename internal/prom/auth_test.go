package prom

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBearerTokenFileIsReadEachRequest(t *testing.T) {
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.Header.Get("X-Scope-OrgID") != "team-a" {
			t.Errorf("tenant header = %q", r.Header.Get("X-Scope-OrgID"))
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()
	path := filepath.Join(t.TempDir(), "token")
	os.WriteFile(path, []byte("one\n"), 0o600)
	c, err := New(ts.URL, WithBearerTokenFile(path), WithHeader("X-Scope-OrgID", "team-a"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := c.Query(ctx, "up", time.Now()); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("two"), 0o600)
	if _, _, err := c.Query(ctx, "up", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "Bearer one" || seen[1] != "Bearer two" {
		t.Errorf("Authorization headers = %q", seen)
	}
}

func TestEmptyTokenFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	os.WriteFile(path, []byte("  \n"), 0o600)
	c, _ := New("http://127.0.0.1:1", WithBearerTokenFile(path), WithRetries(0, 0))
	if _, _, err := c.Query(context.Background(), "up", time.Now()); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestBearerToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer abc" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()
	c, _ := New(ts.URL, WithBearerToken("abc"))
	if _, _, err := c.Query(context.Background(), "up", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPClientTrustsCAFile(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer ts.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	cert := ts.Certificate()
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600)

	// Without the CA the server's certificate is rejected.
	plain, _ := HTTPClient(5*time.Second, "")
	c, _ := New(ts.URL, WithHTTPClient(plain), WithRetries(0, 0))
	if _, _, err := c.Query(context.Background(), "up", time.Now()); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	trusting, err := HTTPClient(5*time.Second, ca)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = New(ts.URL, WithHTTPClient(trusting))
	if _, _, err := c.Query(context.Background(), "up", time.Now()); err != nil {
		t.Fatalf("trusted certificate rejected: %v", err)
	}
}

func TestHTTPClientRejectsNonPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(path, []byte("not a certificate"), 0o600)
	if _, err := HTTPClient(time.Second, path); err == nil {
		t.Fatal("non-PEM CA file accepted")
	}
}
