package models

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func TestDownloadFailures(t *testing.T) {
	data := modelBytes(51865)

	for _, scenario := range []struct {
		name        string
		response    []byte
		size        int64
		interrupted bool
		status      int
	}{
		{name: "success", response: data, size: int64(len(data)), status: 200},
		{name: "checksum", response: append(modelBytes(51865), 'x'), status: 200},
		{name: "short-size", response: data, size: int64(len(data) + 1), status: 200},
		{name: "long-size", response: data, size: int64(len(data) - 1), status: 200},
		{name: "overflow-safe-size", response: data, size: math.MaxInt64, status: 200},
		{name: "header", response: []byte("PRIVATE invalid header"), status: 200},
		{name: "interrupted", response: data[:50], interrupted: true, status: 200},
		{name: "status", status: 503},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				if scenario.interrupted {
					w.Header().Set("Content-Length", "1000")
				}
				w.WriteHeader(scenario.status)
				_, _ = w.Write(scenario.response)
			}))
			defer server.Close()

			opts := options(t, server.URL+"/PRIVATE", data)
			opts.Models.Catalog[0].Source.ExpectedBytes = scenario.size
			if scenario.name == "header" {
				opts.Models.Catalog[0].Source.SHA256 = checksum(scenario.response)
			}

			// An unrelated verified artifact must survive every outcome.
			other := writeCache(t, opts, []byte("prior corrupt target"))
			sentinel := filepath.Join(filepath.Dir(other), strings.Repeat("a", 64)+".bin")
			if err := os.WriteFile(sentinel, data, 0600); err != nil {
				t.Fatal(err)
			}

			artifact, err := Provision(context.Background(), opts, server.Client())
			if scenario.name == "success" {
				if err != nil || artifact.SHA256 != checksum(data) {
					t.Fatalf("download failed: %+v %v", artifact, err)
				}
				got, _ := os.ReadFile(other)
				if string(got) != string(data) {
					t.Fatal("downloaded bytes differ")
				}
				info, _ := os.Stat(other)
				if info.Mode().Perm() != 0600 {
					t.Fatalf("file mode: %v", info.Mode())
				}
				if count.Load() != 1 {
					t.Fatalf("successful download retried: %d", count.Load())
				}
			} else {
				requireFailure(t, err)
				got, _ := os.ReadFile(other)
				if string(got) != "prior corrupt target" {
					t.Fatal("failed download replaced target")
				}
				if count.Load() != int32(opts.Attempts) {
					t.Fatalf("attempt bound: %d", count.Load())
				}
			}

			got, _ := os.ReadFile(sentinel)
			if string(got) != string(data) {
				t.Fatal("unrelated cache removed")
			}
			requireNoPartials(t, opts)
		})
	}
}

func TestDownloadRetryRecovery(t *testing.T) {
	data := modelBytes(51865)

	var count atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count.Add(1) == 1 {
			_, _ = w.Write([]byte("interrupted"))
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	opts := options(t, server.URL, data)

	artifact, err := Provision(context.Background(), opts, server.Client())
	if err != nil || artifact.SHA256 != checksum(data) || count.Load() != 2 {
		t.Fatalf("retry recovery: %+v %v requests=%d", artifact, err, count.Load())
	}
	requireNoPartials(t, opts)
}

func TestDownloadRedirects(t *testing.T) {
	data := modelBytes(51865)
	var escaped atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped.Add(1)
		_, _ = w.Write(data)
	}))
	defer plain.Close()

	var count atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		switch r.URL.Path {
		case "/ok":
			http.Redirect(w, r, "/target", 302)
		case "/target":
			if r.Header.Get("Cookie") != "" {
				t.Error("cookies sent")
			}
			_, _ = w.Write(data)
		case "/downgrade":
			http.Redirect(w, r, plain.URL, 302)
		case "/credentials":
			http.Redirect(w, r, "https://PRIVATE:SECRET@"+r.Host+"/target", 302)
		case "/loop":
			http.Redirect(w, r, "/loop", 302)
		}
	}))
	defer server.Close()

	for _, path := range []string{"/ok", "/downgrade", "/credentials", "/loop"} {
		t.Run(path, func(t *testing.T) {
			count.Store(0)

			opts := options(t, server.URL+path, data)
			opts.Attempts = 1
			_, err := Provision(context.Background(), opts, server.Client())
			if path == "/ok" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireFailure(t, err)
			}
			if escaped.Load() != 0 || count.Load() > 10 {
				t.Fatalf("unsafe/unbounded redirects: %d %d", escaped.Load(), count.Load())
			}
			requireNoPartials(t, opts)
		})
	}
}

func TestProvisionCancellation(t *testing.T) {
	data := modelBytes(51865)
	for _, parentEarlier := range []bool{false, true} {
		var count atomic.Int32
		finished := make(chan struct{}, 1)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			_, _ = w.Write(data[:48])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			finished <- struct{}{}
		}))

		opts := options(t, server.URL, data)
		opts.Timeout = 90 * time.Millisecond
		ctx := context.Background()
		cancel := func() {
		}
		if parentEarlier {
			opts.Timeout = time.Second
			ctx, cancel = context.WithTimeout(ctx, 90*time.Millisecond)
		}

		start := time.Now()
		_, err := Provision(ctx, opts, server.Client())

		cancel()
		requireFailure(t, err)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second || count.Load() != 1 {
			t.Fatalf("deadline lost: %v requests=%d", err, count.Load())
		}
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("HTTP stream survived return")
		}
		server.Close()
		requireNoPartials(t, opts)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var count atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(503)

		cancel()
	}))
	defer server.Close()

	opts := options(t, server.URL, data)

	_, err := Provision(ctx, opts, server.Client())
	if !errors.Is(err, context.Canceled) || count.Load() != 1 {
		t.Fatalf("retry cancellation: %v requests=%d", err, count.Load())
	}
	requireNoPartials(t, opts)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type trackedBody struct {
	io.Reader
	closed *atomic.Int32
}

func (b trackedBody) Close() error {
	b.closed.Add(1)
	return nil
}

type forbiddenJar struct{ calls atomic.Int32 }

func (j *forbiddenJar) SetCookies(_ *url.URL, _ []*http.Cookie) {
	j.calls.Add(1)
}

func (j *forbiddenJar) Cookies(_ *url.URL) []*http.Cookie {
	j.calls.Add(1)
	return []*http.Cookie{{Name: "private", Value: "PRIVATE"}}
}

func TestProvisionClientOwnership(t *testing.T) {
	data := modelBytes(51865)
	var closed, requests atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Header.Get("Cookie") != "" {
			t.Error("caller cookies sent")
		}
		return &http.Response{
			StatusCode: 200,
			Body:       trackedBody{strings.NewReader(string(data)), &closed},
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	jar := &forbiddenJar{}
	client := &http.Client{Transport: transport, Timeout: time.Second, Jar: jar}

	opts := options(t, "https://example.org/PRIVATE", data)

	_, err := Provision(context.Background(), opts, client)
	if err != nil || requests.Load() != 1 || closed.Load() != 1 {
		t.Fatalf("body ownership: %v %d %d", err, requests.Load(), closed.Load())
	}
	if client.Timeout != time.Second || client.CheckRedirect != nil || client.Jar != jar || jar.calls.Load() != 0 {
		t.Fatal("caller client mutated")
	}

	// Non-success responses must be closed too.
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 503,
			Body:       trackedBody{strings.NewReader("PRIVATE response"), &closed},
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})
	opts.CacheDir = filepath.Join(t.TempDir(), "cache")
	opts.Attempts = 1
	_, err = Provision(context.Background(), opts, client)
	requireFailure(t, err)
	if closed.Load() != 2 {
		t.Fatalf("failure body leaked: %d", closed.Load())
	}
}

func TestProvisionDownloadSet(t *testing.T) {
	base, extra := modelBytes(51865), modelBytes(51864)
	var order []string
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/extra" {
			_, _ = w.Write(extra)
		} else {
			_, _ = w.Write(base)
		}
	}))
	defer server.Close()

	opts := options(t, server.URL+"/base", base)

	opts.Models.Catalog = append(
		opts.Models.Catalog,
		config.Model{
			ID:     "extra",
			Source: config.Source{Kind: "download", URL: server.URL + "/extra", SHA256: checksum(extra)},
		},
	)
	opts.Models.DownloadSet = []string{"extra", "base"}

	artifact, err := Provision(context.Background(), opts, server.Client())

	mu.Lock()
	defer mu.Unlock()
	if err != nil || artifact.ID != "base" || fmt.Sprint(order) != "[/extra /base]" {
		t.Fatalf("sequential preparation: %+v %v %v", artifact, err, order)
	}
	if _, err := os.Stat(cacheFile(opts, checksum(extra))); err != nil {
		t.Fatal("extra download missing", err)
	}
}

func TestProvisionRetryDelayCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: 503,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		})}

		opts := options(t, "https://example.org/PRIVATE", modelBytes(51865))
		opts.Timeout = time.Minute
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		result := make(chan error, 1)
		go func() {
			_, err := Provision(ctx, opts, client)
			result <- err
		}()

		synctest.Wait() // The worker is durably blocked in its retry timer.
		if requests.Load() != 1 {
			t.Fatalf("not waiting between attempts: %d", requests.Load())
		}

		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("retry delay ignored cancellation: %v", err)
		}
		if requests.Load() != 1 {
			t.Fatal("retry began after cancellation")
		}
		requireNoPartials(t, opts)
	})
}

func TestProvisionOwnedCollections(t *testing.T) {
	data, extra := modelBytes(51865), modelBytes(51864)

	opts := options(t, "https://example.org/base", data)

	opts.Models.Catalog = append(
		opts.Models.Catalog,
		config.Model{
			ID:     "extra",
			Source: config.Source{Kind: "download", URL: "https://example.org/extra", SHA256: checksum(extra)},
		},
	)
	opts.Models.DownloadSet = []string{"base", "extra"}

	started, release := make(chan struct{}), make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := data
		if r.URL.Path == "/base" {
			close(started)
			<-release
		} else {
			body = extra
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	result := make(chan error, 1)
	go func() {
		_, err := Provision(context.Background(), opts, client)
		result <- err
	}()

	<-started

	opts.Models.Catalog[1].Source.URL = "https://PRIVATE.invalid/changed"
	opts.Models.DownloadSet[1] = "missing"
	close(release)

	if err := <-result; err != nil {
		t.Fatal("retained caller collections", err)
	}
	if _, err := os.Stat(cacheFile(opts, checksum(extra))); err != nil {
		t.Fatal("original download selection lost", err)
	}
}

func TestProvisionTotalDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, extra := modelBytes(51865), modelBytes(51864)
		var requests atomic.Int32
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			select {
			case <-time.After(60 * time.Millisecond):
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			body := data
			if r.URL.Path == "/extra" {
				body = extra
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		})}

		opts := options(t, "https://example.org/base", data)
		opts.Timeout = 100 * time.Millisecond
		opts.Models.Catalog = append(
			opts.Models.Catalog,
			config.Model{
				ID:     "extra",
				Source: config.Source{Kind: "download", URL: "https://example.org/extra", SHA256: checksum(extra)},
			},
		)
		opts.Models.DownloadSet = []string{"base", "extra"}

		start := time.Now()
		_, err := Provision(context.Background(), opts, client)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != opts.Timeout || requests.Load() != 2 {
			t.Fatalf("timeout restarted per artifact: %v %v requests=%d", err, time.Since(start), requests.Load())
		}

		if _, err := os.Stat(cacheFile(opts, checksum(data))); err != nil {
			t.Fatal("completed first artifact lost", err)
		}

		if _, err := os.Stat(cacheFile(opts, checksum(extra))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("timed-out artifact published", err)
		}
		requireNoPartials(t, opts)
	})
}
