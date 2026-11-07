package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

const (
	fixtureGeneration = "p05-cpu-fixture"
	fixtureText       = "CPU fixture result: \"quoted\"\n"
	fixturePrompt     = privateMarker + "-prompt"
	fixtureBodyLimit  = 1 << 20
)

type privateObservation struct {
	Calls, Valid, Cancelled int
	Active                  bool
	RequestID, Generation   string
	Fields                  map[string]string
	WAVBytes                int
}

type privateHTTPFixture struct {
	mu                 sync.Mutex
	fingerprint, model string
	maxTimeoutMS       int64
	next               string
	mismatch           bool
	seen               privateObservation
}

func newPrivateHTTPFixture(fingerprint, model string, timeout time.Duration) *privateHTTPFixture {
	return &privateHTTPFixture{
		fingerprint: fingerprint, model: model, maxTimeoutMS: timeout.Milliseconds(),
		seen: privateObservation{Generation: fixtureGeneration},
	}
}

func (f *privateHTTPFixture) observation() privateObservation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen // Fields is replaced, never modified, after publication.
}

func (f *privateHTTPFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		f.mu.Lock()
		fingerprint, generation := f.fingerprint, f.seen.Generation
		if f.mismatch {
			if fingerprint[0] == '0' {
				fingerprint = "1" + fingerprint[1:]
			} else {
				fingerprint = "0" + fingerprint[1:]
			}
		}
		f.mu.Unlock()
		// This is synthetic private-protocol readiness, NOT CUDA/model evidence.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ready", "protocol_version": "1", "configuration_fingerprint": fingerprint,
			"process_generation": generation, "model": f.model, "multilingual": true, "cuda_ready": true,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/fixture/observed":
		_ = json.NewEncoder(w).Encode(f.observation())
	case r.Method == http.MethodPost && (r.URL.Path == "/fixture/hold" || r.URL.Path == "/fixture/malformed"):
		f.mu.Lock()
		f.next = strings.TrimPrefix(r.URL.Path, "/fixture/")
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && (r.URL.Path == "/fixture/mismatch" || r.URL.Path == "/fixture/compatible"):
		f.mu.Lock()
		f.mismatch = r.URL.Path == "/fixture/mismatch"
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/inference":
		f.infer(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *privateHTTPFixture) infer(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen.Calls++
	generation := f.seen.Generation
	f.mu.Unlock()
	retire, cleaned := false, true
	defer func() {
		if retire {
			// Later cleanup defers run first. Only then publish synthetic recovery;
			// closing a transport alone is not the application's recovery contract.
			f.mu.Lock()
			f.seen.Active = false
			if cleaned {
				f.seen.Generation = fmt.Sprintf("%s-%d", fixtureGeneration, f.seen.Calls)
			}
			f.mu.Unlock()
		}
	}()
	milliseconds, err := strconv.ParseInt(r.Header.Get("X-Whisper-Timeout-Ms"), 10, 64)
	if err != nil || milliseconds <= 0 || milliseconds > f.maxTimeoutMS ||
		r.Header.Get("X-Whisper-Protocol") != "1" || r.Header.Get("X-Whisper-Configuration") != f.fingerprint ||
		r.Header.Get("X-Whisper-Generation") != generation || r.Header.Get("X-Request-ID") == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, fixtureBodyLimit)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer func() { cleaned = r.MultipartForm.RemoveAll() == nil }()
	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	wave, readErr := io.ReadAll(io.LimitReader(file, fixtureBodyLimit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || header.Filename != "audio.wav" ||
		len(wave) < 46 || len(wave) > fixtureBodyLimit || string(wave[:4]) != "RIFF" ||
		int(binary.LittleEndian.Uint32(wave[4:])) != len(wave)-8 || string(wave[8:16]) != "WAVEfmt " ||
		binary.LittleEndian.Uint32(wave[16:]) != 16 || binary.LittleEndian.Uint16(wave[20:]) != 1 ||
		binary.LittleEndian.Uint16(wave[22:]) != 1 || binary.LittleEndian.Uint32(wave[24:]) != 16000 ||
		binary.LittleEndian.Uint32(wave[28:]) != 32000 || binary.LittleEndian.Uint16(wave[32:]) != 2 ||
		binary.LittleEndian.Uint16(wave[34:]) != 16 || string(wave[36:40]) != "data" || len(wave)%2 != 0 ||
		int(binary.LittleEndian.Uint32(wave[40:])) != len(wave)-44 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	fields := make(map[string]string)
	for _, name := range []string{"language", "prompt", "temperature", "response_format"} {
		fields[name] = r.FormValue(name)
	}
	f.mu.Lock()
	f.seen.Valid++
	f.seen.RequestID, f.seen.Fields, f.seen.WAVBytes = r.Header.Get("X-Request-ID"), fields, len(wave)
	mode := f.next
	f.next = ""
	f.seen.Active = mode == "hold"
	retire = mode == "hold" || mode == "malformed"
	f.mu.Unlock()
	if mode == "malformed" {
		_ = json.NewEncoder(w).Encode(map[string]any{"text": false, "private": fixturePrompt})
		return
	}
	if mode == "hold" {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			f.mu.Lock()
			f.seen.Cancelled++
			f.mu.Unlock()
		case <-timer.C:
			w.WriteHeader(http.StatusGatewayTimeout)
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"text": fixtureText})
}

func TestPrivateHTTPBackendHelper(t *testing.T) {
	if os.Getenv("P05_NETWORK_HELPER") != "1" {
		return
	}
	settings, err := config.Load([]string{"--config", "/etc/whisper/config.json"}, []string{})
	if err != nil {
		t.Fatal("private fixture configuration failed")
	}
	fingerprint, err := config.CanonicalFingerprint(settings)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newPrivateHTTPFixture(fingerprint, settings.Models.Served, time.Duration(settings.Timeouts.OperationSeconds)*time.Second)
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr: ":" + os.Getenv("P05_NETWORK_HELPER_PORT"), Handler: fixture,
		ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	closed := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		closed <- err
	}()
	err = server.ListenAndServe()
	stop()
	shutdownErr := <-closed
	if !errors.Is(err, http.ErrServerClosed) || shutdownErr != nil {
		t.Fatal("private fixture server did not close cleanly")
	}
}

func syntheticWAV(rate, channels int) []byte {
	data := make([]byte, 44+rate*channels/10*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], uint16(channels))
	binary.LittleEndian.PutUint32(data[24:], uint32(rate))
	binary.LittleEndian.PutUint32(data[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(data[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	return data
}

func fixtureMultipart(t *testing.T, ctx context.Context, url, filename string, fields [][2]string, audio []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("X-Request-ID", "p05-http-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	return request
}

func awaitFixture(t *testing.T, description string, observed func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if observed() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + description)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPackagedPublicHTTP(t *testing.T) {
	imageTests(t)
	settings := defaultSettings(t)
	object(t, object(t, settings["api"])["rateLimit"])["enabled"] = false
	pair := startPrivatePair(t, settings, "TestPrivateHTTPBackendHelper")
	base := "http://" + pair.address
	backend := fmt.Sprint(object(t, settings["api"])["backendUrl"])
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Transport: transport, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	readiness := func(status int) bool {
		reply, err := client.Get(base + "/health/ready")
		if err != nil {
			return false
		}
		_ = reply.Body.Close()
		return reply.StatusCode == status
	}
	awaitFixture(t, "public readiness against the CPU fixture", func() bool { return readiness(http.StatusOK) })
	control := func(t *testing.T, method, path string) []byte {
		t.Helper()
		return docker(t, "exec", pair.apiID, "curl", "--fail", "--silent", "--show-error", "--max-time", "2",
			"--request", method, backend+path)
	}
	observe := func(t *testing.T) privateObservation {
		t.Helper()
		var value privateObservation
		if err := json.Unmarshal(control(t, http.MethodGet, "/fixture/observed"), &value); err != nil {
			t.Fatal("invalid private fixture observation")
		}
		return value
	}
	request := func(t *testing.T, ctx context.Context, filename, format string, audio []byte) *http.Request {
		t.Helper()
		return fixtureMultipart(t, ctx, base+"/v1/audio/transcriptions", filename, [][2]string{
			{"model", "base"}, {"language", "en"}, {"prompt", fixturePrompt},
			{"temperature", "0.25"}, {"response_format", format},
		}, audio)
	}
	exchange := func(t *testing.T, req *http.Request, status int, mediaType string) []byte {
		t.Helper()
		reply, err := client.Do(req)
		if err != nil {
			t.Fatal("public request failed", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(reply.Body, fixtureBodyLimit+1))
		closeErr := reply.Body.Close()
		if readErr != nil || closeErr != nil || len(body) > fixtureBodyLimit || reply.StatusCode != status {
			t.Fatalf("public response contract failed: status=%d, expected=%d", reply.StatusCode, status)
		}
		if reply.Header.Get("X-Request-ID") != req.Header.Get("X-Request-ID") ||
			strings.Split(reply.Header.Get("Content-Type"), ";")[0] != mediaType {
			t.Fatal("public response type or request correlation was not preserved")
		}
		return body
	}
	clean := func(t *testing.T) {
		t.Helper()
		awaitFixture(t, "request scratch cleanup", func() bool {
			remaining := docker(t, "exec", pair.apiID, "sh", "-ec",
				`find /tmp/whisper-api -type f ! \( -path /tmp/whisper-api/.owner.lock -size 0c \) -print -quit`)
			return len(bytes.TrimSpace(remaining)) == 0
		})
	}
	wave := syntheticWAV(8000, 2)
	for _, format := range []struct{ extension, codec, response string }{
		{"wav", "", "json"}, {"mp4", "aac", "json"}, {"ogg", "libopus", "text"},
	} {
		t.Run(format.extension+"-"+format.response, func(t *testing.T) {
			t.Cleanup(func() { clean(t) })
			audio := wave
			if format.codec != "" {
				path := filepath.Join(t.TempDir(), "fixture."+format.extension)
				run(t, wave, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-threads", "1", "-c:a", format.codec, path)
				var err error
				audio, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			req := request(t, t.Context(), privateMarker+"."+format.extension, format.response, audio)
			mediaType := "application/json"
			if format.response == "text" {
				mediaType = "text/plain"
			}
			body := exchange(t, req, http.StatusOK, mediaType)
			text := string(body)
			if format.response == "json" {
				var response struct{ Text string }
				if json.Unmarshal(body, &response) != nil {
					t.Fatal("invalid public JSON response")
				}
				text = response.Text
			}
			seen := observe(t)
			if text != fixtureText || seen.RequestID != req.Header.Get("X-Request-ID") || seen.WAVBytes <= 44 ||
				seen.Fields["language"] != "en" || seen.Fields["prompt"] != fixturePrompt ||
				seen.Fields["temperature"] != "0.25" || seen.Fields["response_format"] != "json" {
				t.Fatal("public serialization or observed normalized private request failed")
			}
		})
	}
	t.Run("invalid-audio", func(t *testing.T) {
		before := observe(t).Calls
		t.Cleanup(func() { clean(t) })
		body := exchange(t, request(t, t.Context(), "invalid.wav", "json", []byte(privateMarker)), http.StatusBadRequest, "application/json")
		var failure struct{ Error struct{ Code string } }
		if json.Unmarshal(body, &failure) != nil || failure.Error.Code != "unsupported_audio" ||
			bytes.Contains(body, []byte(privateMarker)) || observe(t).Calls != before {
			t.Fatal("invalid audio failure contract or private inference isolation failed")
		}
	})
	t.Run("invalid-multipart", func(t *testing.T) {
		before := observe(t).Calls
		t.Cleanup(func() { clean(t) })
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/v1/audio/transcriptions", strings.NewReader(privateMarker))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		req.Header.Set("X-Request-ID", "p05-invalid-multipart")
		body := exchange(t, req, http.StatusBadRequest, "application/json")
		var failure struct{ Error struct{ Code string } }
		if json.Unmarshal(body, &failure) != nil || failure.Error.Code != "invalid_request" ||
			bytes.Contains(body, []byte(privateMarker)) || observe(t).Calls != before {
			t.Fatal("invalid multipart failure contract or private inference isolation failed")
		}
	})
	t.Run("malformed-private-reply", func(t *testing.T) {
		before := observe(t)
		t.Cleanup(func() { clean(t) })
		control(t, http.MethodPost, "/fixture/malformed")
		body := exchange(t, request(t, t.Context(), "malformed-reply.wav", "json", wave), http.StatusServiceUnavailable, "application/json")
		var failure struct{ Error struct{ Code string } }
		if json.Unmarshal(body, &failure) != nil || failure.Error.Code != "backend_unavailable" || bytes.Contains(body, []byte(privateMarker)) {
			t.Fatal("malformed private reply was not mapped to a safe public failure")
		}
		seen := observe(t)
		if seen.Calls != before.Calls+1 || seen.Cancelled != before.Cancelled || seen.Generation == before.Generation {
			t.Fatal("malformed private reply did not retire its synthetic operation")
		}
		awaitFixture(t, "readiness after malformed private reply recovery", func() bool { return readiness(http.StatusOK) })
	})
	t.Run("fingerprint-mismatch", func(t *testing.T) {
		before := observe(t)
		t.Cleanup(func() {
			control(t, http.MethodPost, "/fixture/compatible")
			awaitFixture(t, "restored private compatibility", func() bool { return readiness(http.StatusOK) })
			clean(t)
		})
		control(t, http.MethodPost, "/fixture/mismatch")
		awaitFixture(t, "readiness rejection of a mismatched fingerprint", func() bool { return readiness(http.StatusServiceUnavailable) })
		body := exchange(t, request(t, t.Context(), "mismatched.wav", "json", wave), http.StatusServiceUnavailable, "application/json")
		var failure struct{ Error struct{ Code string } }
		if json.Unmarshal(body, &failure) != nil || failure.Error.Code != "not_ready" || bytes.Contains(body, []byte(privateMarker)) {
			t.Fatal("private fingerprint mismatch did not fail closed")
		}
		if seen := observe(t); seen.Calls != before.Calls || seen.Cancelled != before.Cancelled {
			t.Fatal("mismatched private compatibility admitted inference")
		}
	})
	t.Run("active-cancellation", func(t *testing.T) {
		before := observe(t)
		t.Cleanup(func() { clean(t) })
		control(t, http.MethodPost, "/fixture/hold")
		ctx, cancel := context.WithCancel(t.Context())
		req := request(t, ctx, "cancel.wav", "json", wave)
		done := make(chan struct{})
		var requestErr error
		go func() {
			defer close(done)
			reply, err := client.Do(req)
			requestErr = err
			if reply != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(reply.Body, fixtureBodyLimit))
				_ = reply.Body.Close()
			}
		}()
		defer func() { cancel(); <-done }()
		awaitFixture(t, "observed active private inference", func() bool {
			seen := observe(t)
			return seen.Active && seen.RequestID == req.Header.Get("X-Request-ID")
		})
		cancel()
		<-done
		if !errors.Is(requestErr, context.Canceled) {
			t.Fatal("active public cancellation was not observed by the client")
		}
		awaitFixture(t, "private HTTP cancellation and synthetic generation retirement", func() bool {
			seen := observe(t)
			return seen.Cancelled == before.Cancelled+1 && !seen.Active && seen.Generation != before.Generation
		})
		clean(t)
		awaitFixture(t, "readiness after observed synthetic retirement", func() bool { return readiness(http.StatusOK) })
		followup := request(t, t.Context(), "after-cancel.wav", "json", wave)
		exchange(t, followup, http.StatusOK, "application/json")
		if seen := observe(t); seen.Calls != before.Calls+2 || seen.RequestID != followup.Header.Get("X-Request-ID") {
			t.Fatal("request capacity did not recover after cancellation")
		}
	})
	clean(t)
	if bytes.Contains(docker(t, "logs", pair.apiID), []byte(privateMarker)) {
		t.Fatal("production API logs retained private fixture data")
	}
}

func TestPrivateHTTPFixtureProtocolAndCancellation(t *testing.T) {
	fixture := newPrivateHTTPFixture(strings.Repeat("a", 64), "base", time.Second)
	server := httptest.NewServer(fixture)
	defer server.Close()
	client := server.Client()
	client.Timeout = 2 * time.Second
	request := func(ctx context.Context, channels int) *http.Request {
		req := fixtureMultipart(t, ctx, server.URL+"/inference", "audio.wav", [][2]string{
			{"language", "en"}, {"prompt", fixturePrompt}, {"temperature", "0.25"}, {"response_format", "json"},
		}, syntheticWAV(16000, channels))
		req.Header.Set("X-Whisper-Protocol", "1")
		req.Header.Set("X-Whisper-Configuration", fixture.fingerprint)
		req.Header.Set("X-Whisper-Generation", fixture.observation().Generation)
		req.Header.Set("X-Whisper-Timeout-Ms", "1000")
		return req
	}
	control := func(path string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		closeErr := reply.Body.Close()
		if closeErr != nil || reply.StatusCode != http.StatusNoContent {
			t.Fatal("private fixture control failed")
		}
	}
	for _, channels := range []int{2, 1} {
		before := fixture.observation()
		reply, err := client.Do(request(t.Context(), channels))
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, reply.Body)
		closeErr := reply.Body.Close()
		want, valid := http.StatusOK, before.Valid+1
		if channels == 2 {
			want, valid = http.StatusBadRequest, before.Valid
		}
		if readErr != nil || closeErr != nil || reply.StatusCode != want {
			t.Fatal("private fixture did not distinguish stereo from normalized mono")
		}
		if seen := fixture.observation(); seen.Calls != before.Calls+1 || seen.Valid != valid {
			t.Fatal("private fixture confused attempted and valid inference")
		}
	}
	for _, mode := range []string{"mismatch", "compatible"} {
		before := fixture.observation()
		control("/fixture/" + mode)
		reply, err := client.Get(server.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		var health struct {
			Fingerprint string `json:"configuration_fingerprint"`
			Generation  string `json:"process_generation"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(reply.Body, fixtureBodyLimit)).Decode(&health)
		closeErr := reply.Body.Close()
		if decodeErr != nil || closeErr != nil || reply.StatusCode != http.StatusOK ||
			(health.Fingerprint == fixture.fingerprint) != (mode == "compatible") || health.Generation != before.Generation {
			t.Fatal("private fixture compatibility control changed the wrong identity")
		}
	}
	beforeMalformed := fixture.observation()
	control("/fixture/malformed")
	reply, err := client.Do(request(t.Context(), 1))
	if err != nil {
		t.Fatal(err)
	}
	var malformed struct{ Text *bool }
	decodeErr := json.NewDecoder(io.LimitReader(reply.Body, fixtureBodyLimit)).Decode(&malformed)
	closeErr := reply.Body.Close()
	if decodeErr != nil || closeErr != nil || reply.StatusCode != http.StatusOK || malformed.Text == nil {
		t.Fatal("private fixture did not emit its deliberately malformed text value")
	}
	if seen := fixture.observation(); seen.Calls != beforeMalformed.Calls+1 || seen.Generation == beforeMalformed.Generation {
		t.Fatal("private fixture did not retire the malformed-response operation")
	}
	beforeCancellation := fixture.observation()
	control("/fixture/hold")
	ctx, cancel := context.WithCancel(t.Context())
	req := request(ctx, 1)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		reply, err := client.Do(req)
		if reply != nil {
			_ = reply.Body.Close()
		}
		done <- err
	}()
	defer func() { cancel(); <-done }()
	awaitFixture(t, "active CPU fixture request", func() bool { return fixture.observation().Active })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("private fixture client did not cancel")
	}
	awaitFixture(t, "CPU fixture cancellation and handler exit", func() bool {
		seen := fixture.observation()
		return seen.Cancelled == beforeCancellation.Cancelled+1 && !seen.Active && seen.Generation != beforeCancellation.Generation
	})
}
