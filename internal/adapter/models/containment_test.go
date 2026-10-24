package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestArtifactContainment(t *testing.T) {
	data := modelBytes(51865)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	for _, scenario := range []string{
		"root-symlink",
		"artifacts-symlink",
		"locks-symlink",
		"artifact-symlink",
		"lock-symlink",
		"artifact-directory",
		"lock-directory",
		"artifact-fifo",
		"lock-fifo",
	} {
		t.Run(scenario, func(t *testing.T) {
			opts := options(t, server.URL, data)
			opts.Attempts = 1
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "PRIVATE-sentinel")
			if err := os.WriteFile(sentinel, data, 0600); err != nil {
				t.Fatal(err)
			}

			if err := os.MkdirAll(opts.CacheDir, 0700); err != nil {
				t.Fatal(err)
			}

			if scenario == "root-symlink" {
				if err := os.Remove(opts.CacheDir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, opts.CacheDir); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "artifacts-symlink" || scenario == "locks-symlink" {
				if err := os.Symlink(outside, filepath.Join(opts.CacheDir, strings.TrimSuffix(scenario, "-symlink"))); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, dir := range []string{"artifacts", "locks"} {
					if err := os.MkdirAll(filepath.Join(opts.CacheDir, dir), 0700); err != nil {
						t.Fatal(err)
					}
				}
				path := cacheFile(opts, checksum(data))
				if strings.HasPrefix(scenario, "lock-") {
					path = filepath.Join(opts.CacheDir, "locks", checksum(data)+".lock")
				}
				var err error
				switch {
				case strings.HasSuffix(scenario, "symlink"):
					err = os.Symlink(sentinel, path)
				case strings.HasSuffix(scenario, "directory"):
					err = os.Mkdir(path, 0700)
				case strings.HasSuffix(scenario, "fifo"):
					err = unix.Mkfifo(path, 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}

			start := time.Now()
			_, err := Provision(context.Background(), opts, server.Client())
			requireFailure(t, err)
			if time.Since(start) > time.Second {
				t.Fatal("unsafe file blocked provisioning")
			}

			got, _ := os.ReadFile(sentinel)
			if string(got) != string(data) {
				t.Fatal("escaped cache confinement")
			}
		})
	}

	if requests.Load() != 0 {
		t.Fatalf("unsafe cache triggered network: %d", requests.Load())
	}
}

func TestProvisionCancelledBeforeFilesystem(t *testing.T) {
	opts := options(t, "https://example.org/PRIVATE", modelBytes(51865))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Provision(ctx, opts, http.DefaultClient)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	if _, err := os.Stat(opts.CacheDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled operation created cache: %v", err)
	}

	_, err = Provision(context.Background(), opts, nil)
	if err == nil {
		t.Fatal("nil client accepted")
	}
}
