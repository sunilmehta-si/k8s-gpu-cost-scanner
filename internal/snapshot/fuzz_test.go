package snapshot

import (
	"bytes"
	"testing"
)

func FuzzDecode(f *testing.F) {
	var buf bytes.Buffer
	Encode(&buf, sample())
	f.Add(buf.Bytes())
	f.Add([]byte(`{"version":1,"window":{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","step":"1m"},"range":{"x":[{"labels":{},"points":[[1,"NaN"]]}]}}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Decode(bytes.NewReader(b))
		if err != nil {
			return
		}
		if s.Range == nil || s.Instant == nil || !s.Window.End.After(s.Window.Start) {
			t.Fatalf("decoded an invalid snapshot: %+v", s)
		}
		// What decodes must encode again.
		var out bytes.Buffer
		if err := Encode(&out, s); err != nil {
			t.Fatalf("re-encode: %v", err)
		}
	})
}
