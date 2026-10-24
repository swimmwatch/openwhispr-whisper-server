package models

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Child copies of the test binary exercise OS locks, descriptor release on
// process death, and filesystem publication independently of Go mutexes.
func TestModelProcess(t *testing.T) {
	mode := os.Getenv("WHISPER_MODEL_TEST_PROCESS")
	if mode == "" {
		return
	}

	var opts Options
	if err := json.Unmarshal([]byte(os.Getenv("WHISPER_MODEL_TEST_OPTIONS")), &opts); err != nil {
		t.Fatal(err)
	}

	if mode == "lock" {
		dir := filepath.Join(opts.CacheDir, "locks")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(
			filepath.Join(dir, opts.Models.Catalog[0].Source.SHA256+".lock"),
			os.O_RDWR|os.O_CREATE,
			0600,
		)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
			t.Fatal(err)
		}
		fmt.Println("LOCKED")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}

	cert, err := hex.DecodeString(os.Getenv("WHISPER_MODEL_TEST_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(cert)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	if mode == "disk-limit" {
		// Only this child is constrained; the parent test runner is unaffected.
		var original unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &original); err != nil {
			t.Fatal(err)
		}
		defer func() {
			// The test binary writes coverage metadata after this helper returns.
			if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &original); err != nil {
				t.Error(err)
			}
		}()
		limited := original
		limited.Cur = 32
		if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &limited); err != nil {
			t.Fatal(err)
		}
	}

	_, err = Provision(context.Background(), opts, client)

	if mode == "disk-limit" {
		if err == nil {
			t.Fatal("write beyond file-size limit succeeded")
		}
		requireFailure(t, err)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

func modelProcess(t *testing.T, mode string, opts Options, server *httptest.Server) *exec.Cmd {
	t.Helper()

	data, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestModelProcess$", "-test.count=1")
	cmd.Env = append(
		os.Environ(),
		"WHISPER_MODEL_TEST_PROCESS="+mode,
		"WHISPER_MODEL_TEST_OPTIONS="+string(data),
		"WHISPER_MODEL_TEST_CERT="+hex.EncodeToString(server.Certificate().Raw),
	)

	return cmd
}

func holdProcessLock(t *testing.T, opts Options, server *httptest.Server) func() {
	t.Helper()

	cmd := modelProcess(t, "lock", opts, server)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "LOCKED\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child lock setup: %q %v", line, err)
	}

	released := false
	release := func() {
		if !released {
			released = true
			_ = input.Close()
			if err := cmd.Wait(); err != nil {
				t.Errorf("lock child: %v", err)
			}
		}
	}

	t.Cleanup(release)
	return release
}

func TestConcurrentPublication(t *testing.T) {
	data := modelBytes(51865)
	var requests atomic.Int32
	started := make(chan struct{}, 1)
	releaseStream := make(chan struct{})
	var releaseOnce sync.Once
	finishStream := func() {
		releaseOnce.Do(func() {
			close(releaseStream)
		})
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data[:48])
		w.(http.Flusher).Flush()
		started <- struct{}{}
		select {
		case <-releaseStream:
			_, _ = w.Write(data[48:])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer finishStream()

	opts := options(t, server.URL, data)
	opts.Timeout = 4 * time.Second
	releaseLock := holdProcessLock(t, opts, server)

	result := make(chan error, 1)
	go func() {
		_, err := Provision(context.Background(), opts, server.Client())
		result <- err
	}()

	// The held OS lock must prevent even a source request.
	select {
	case <-started:
		t.Fatal("download started before artifact lock released")
	case err := <-result:
		t.Fatalf("provision returned under held lock: %v", err)
	case <-time.After(80 * time.Millisecond):
	}

	releaseLock()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start after lock release")
	}

	final := cacheFile(opts, checksum(data))
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial exposed at final path: %v", err)
	}

	partials, _ := filepath.Glob(filepath.Join(opts.CacheDir, "artifacts", "*.partial-*"))
	if len(partials) != 1 {
		t.Fatalf("expected exactly one owned partial: %v", partials)
	}

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("partial exposed at final path: %v", err)
		}
		info, err := os.Stat(partials[0])
		if err == nil && info.Size() >= 48 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stream did not reach owned partial")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A second process must wait for publication and then reuse the cache.
	child := modelProcess(t, "provision", opts, server)
	var childOutput strings.Builder
	child.Stdout = &childOutput
	child.Stderr = &childOutput
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}

	childDone := make(chan error, 1)
	go func() {
		childDone <- child.Wait()
	}()

	select {
	case err := <-childDone:
		t.Fatalf("second process escaped lock: %v %s", err, childOutput.String())
	case <-time.After(80 * time.Millisecond):
	}
	if requests.Load() != 1 {
		t.Fatalf("concurrent writer made another request: %d", requests.Load())
	}

	finishStream()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-childDone; err != nil {
		t.Fatalf("second process: %v %s", err, childOutput.String())
	}

	got, err := os.ReadFile(final)
	if err != nil || checksum(got) != checksum(data) {
		t.Fatalf("published partial: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("cache recheck failed: %d", requests.Load())
	}
	requireNoPartials(t, opts)
}

func TestProvisionLockCancellation(t *testing.T) {
	data := modelBytes(51865)
	var requests atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	opts := options(t, server.URL, data)
	opts.Timeout = 60 * time.Millisecond
	release := holdProcessLock(t, opts, server)

	_, err := Provision(context.Background(), opts, server.Client())
	requireFailure(t, err)
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 0 {
		t.Fatalf("lock wait not bounded: %v %d", err, requests.Load())
	}

	release()

	// Cancellation must close its descriptor without unlinking the stable lock.
	opts.Timeout = time.Second

	if _, err := Provision(context.Background(), opts, server.Client()); err != nil {
		t.Fatal("lock leaked after cancellation", err)
	}
	if _, err := os.Stat(filepath.Join(opts.CacheDir, "locks", checksum(data)+".lock")); err != nil {
		t.Fatal("stable lock removed", err)
	}
}

func TestPublicationCrashRecovery(t *testing.T) {
	data := modelBytes(51865)
	var requests atomic.Int32
	started := make(chan struct{}, 1)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write(data[:48])
			w.(http.Flusher).Flush()
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	opts := options(t, server.URL, data)

	child := modelProcess(t, "provision", opts, server)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal("crash fixture did not start")
	}

	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("child unexpectedly survived")
	}

	partials, _ := filepath.Glob(filepath.Join(opts.CacheDir, "artifacts", "*.partial-*"))
	if len(partials) != 1 {
		t.Fatalf("crash left no recoverable partial: %v", partials)
	}

	unrelated := filepath.Join(opts.CacheDir, "artifacts", strings.Repeat("a", 64)+".partial-unrelated")
	if err := os.WriteFile(unrelated, []byte("PRIVATE sentinel"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Provision(context.Background(), opts, server.Client()); err != nil {
		t.Fatal("recovery failed", err)
	}

	for _, partial := range partials {
		if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale partial survived: %v", err)
		}
	}

	got, _ := os.ReadFile(unrelated)
	if string(got) != "PRIVATE sentinel" {
		t.Fatal("recovery removed unrelated partial")
	}

	got, _ = os.ReadFile(cacheFile(opts, checksum(data)))
	if checksum(got) != checksum(data) {
		t.Fatal("recovery published invalid bytes")
	}
}

func TestDownloadWriteFailure(t *testing.T) {
	data := modelBytes(51865)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()

	opts := options(t, server.URL, data)
	opts.Attempts = 1

	cmd := modelProcess(t, "disk-limit", opts, server)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write-failure child: %v %s", err, output)
	}

	if _, err := os.Stat(cacheFile(opts, checksum(data))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed write published: %v", err)
	}
	requireNoPartials(t, opts)
}
