package models

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestArtifactPathsRemainCanonicalAcrossDownloadAndCache(t *testing.T) {
	data := modelBytes(51865)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	opts := options(t, server.URL, data)
	// Preserve the accepted absolute-path spelling while requiring the same
	// canonical artifact path from download publication and later offline reuse.
	opts.CacheDir += "/../cache/"
	want := filepath.Join(opts.CacheDir, "artifacts", checksum(data)+".bin")
	for _, download := range []bool{true, false} {
		if !download {
			opts.Models.DownloadSet = nil
		}
		artifact, err := Provision(t.Context(), opts, server.Client())
		if err != nil || artifact.Path != want || artifact.SHA256 != checksum(data) {
			t.Fatalf("artifact path or identity changed: %+v %v", artifact, err)
		}
		got, err := os.ReadFile(artifact.Path)
		if err != nil || string(got) != string(data) {
			t.Fatal("returned artifact path did not identify the verified bytes", err)
		}
		requireNoPartials(t, opts)
	}
	if requests.Load() != 1 {
		t.Fatal("verified offline cache made another source request")
	}
}
