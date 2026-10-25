package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func options(t *testing.T, url string, data []byte) Options {
	t.Helper()

	return Options{
		Models: config.Models{
			Catalog: []config.Model{{
				ID: "base",
				Source: config.Source{
					Kind:   "download",
					URL:    url,
					SHA256: checksum(data),
				},
			}},
			Served:      "base",
			DownloadSet: []string{"base"},
		},
		CacheDir: filepath.Join(t.TempDir(), "cache"),
		Attempts: 2,
		Timeout:  2 * time.Second,
	}
}

func cacheFile(opts Options, hash string) string {
	return filepath.Join(opts.CacheDir, "artifacts", hash+".bin")
}

func writeCache(t *testing.T, opts Options, data []byte) string {
	t.Helper()

	name := cacheFile(opts, opts.Models.Catalog[0].Source.SHA256)
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func requireFailure(t *testing.T, err error) {
	t.Helper()

	var failure *domain.Failure
	if !errors.As(err, &failure) || failure.Kind != domain.ProvisioningFailed || failure.Cause == nil {
		t.Fatalf("expected provisioning failure: %v", err)
	}

	for _, marker := range []string{"PRIVATE", "https://", ".bin", "/cache"} {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("private information in public error: %v", err)
		}
	}
}

func requireNoPartials(t *testing.T, opts Options) {
	t.Helper()

	partials, err := filepath.Glob(filepath.Join(opts.CacheDir, "artifacts", "*.partial-*"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("partial files leaked: %v %v", partials, err)
	}
}

func TestOfflineCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	data := modelBytes(51865)

	for _, selected := range []bool{true, false} {
		opts := options(t, server.URL, data)
		if !selected {
			opts.Models.DownloadSet = nil
		}
		name := writeCache(t, opts, data)

		artifact, err := Provision(context.Background(), opts, server.Client())
		if requests.Load() != 0 {
			t.Fatalf("valid cache contacted source: %d", requests.Load())
		}
		if err != nil || artifact.ID != "base" || artifact.Path != name || artifact.SHA256 != checksum(data) ||
			!artifact.Metadata.Multilingual {
			t.Fatalf("offline cache: %+v %v", artifact, err)
		}

		got, _ := os.ReadFile(name)
		if string(got) != string(data) {
			t.Fatal("valid cache modified")
		}
	}

	if requests.Load() != 0 {
		t.Fatalf("valid cache contacted source: %d", requests.Load())
	}

	opts := options(t, server.URL, data)
	opts.Models.DownloadSet = nil
	_, err := Provision(context.Background(), opts, server.Client())
	requireFailure(t, err)

	writeCache(t, opts, []byte("PRIVATE corrupt cache"))
	_, err = Provision(context.Background(), opts, server.Client())
	requireFailure(t, err)

	if requests.Load() != 0 {
		t.Fatal("unselected missing/corrupt artifact downloaded")
	}
}

func TestPreparedArtifact(t *testing.T) {
	data := modelBytes(51864)
	path := filepath.Join(t.TempDir(), "PRIVATE-model.bin")
	if err := os.WriteFile(path, data, 0444); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	for _, expected := range []string{"", checksum(data)} {
		opts := options(t, server.URL, data)
		opts.Models.DownloadSet = nil
		opts.Models.Catalog[0].Source = config.Source{Kind: "prepared", Path: path, SHA256: expected}

		artifact, err := Provision(context.Background(), opts, server.Client())
		if err != nil || artifact.Path != path || artifact.SHA256 != checksum(data) || artifact.Metadata.Multilingual {
			t.Fatalf("prepared: %+v %v", artifact, err)
		}
		if _, err := os.Stat(opts.CacheDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("prepared-only wrote cache: %v", err)
		}

		got, _ := os.ReadFile(path)
		info, _ := os.Stat(path)
		if string(got) != string(data) || info.Mode().Perm() != 0444 {
			t.Fatal("prepared file modified")
		}

		opts.Models.Catalog[0].Source.SHA256 = strings.Repeat("a", 64)
		_, err = Provision(context.Background(), opts, server.Client())
		requireFailure(t, err)
		opts.Models.Catalog[0].Source.Path += ".missing"
		_, err = Provision(context.Background(), opts, server.Client())
		requireFailure(t, err)
	}

	if requests.Load() != 0 {
		t.Fatal("prepared file caused download")
	}
}

func TestProvisionFailures(t *testing.T) {
	data := modelBytes(51865)

	for _, mutate := range []func(*Options){
		func(o *Options) {
			o.Attempts = 0
		},
		func(o *Options) {
			o.Timeout = 0
		},
		func(o *Options) {
			o.CacheDir = "relative"
		},
		func(o *Options) {
			o.Models.Served = "missing"
		},
		func(o *Options) {
			o.Models.DownloadSet = []string{"missing"}
		},
		func(o *Options) {
			o.Models.Catalog[0].Source.URL = "http://PRIVATE.invalid/model"
		},
	} {
		opts := options(t, "https://example.org/PRIVATE", data)
		mutate(&opts)
		_, err := Provision(context.Background(), opts, http.DefaultClient)
		if !errors.Is(err, config.ErrInvalid) {
			t.Fatalf("invalid options accepted: %v", err)
		}
		if _, err := os.Stat(opts.CacheDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid options touched cache: %v", err)
		}
	}
}
