package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

// TestSampleCountNativeChild is a CPU-only HTTP stand-in, not the native server.
func TestSampleCountNativeChild(t *testing.T) {
	if os.Getenv("WHISPER_TEST_NATIVE") != "1" {
		return
	}
	var port string
	for i, arg := range os.Args {
		if arg == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	root := os.Getenv("WHISPER_TEST_STATE")
	fmt.Print(deviceLine, backendLine, allocationLine)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/inference", func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(filepath.Join(root, "submitted"), nil, 0600); err != nil {
			http.Error(w, "cannot record submission", http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || int64(len(body)) != r.ContentLength {
			http.Error(w, "invalid content length", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing audio", http.StatusBadRequest)
			return
		}
		defer file.Close()
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		data, err := io.ReadAll(file)
		if err != nil || len(data) < 44 || (len(data)-44)%2 != 0 {
			http.Error(w, "invalid audio", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(filepath.Join(root, "received.wav"), data, 0600); err != nil {
			http.Error(w, "cannot record audio", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"text":"fixture transcript"}`)
	})
	if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
		os.Exit(8)
	}
}

func sampleCountOptions(t *testing.T) (Options, string) {
	t.Helper()
	opts, root, _ := supervisorOptions(t)
	opts.NativeCommand = []string{os.Args[0], "-test.run=^TestSampleCountNativeChild$", "--"}
	return opts, root
}

func wavWithFrames(frames int) []byte {
	wave := make([]byte, 44+frames*2)
	copy(wave, wavFixture()[:44])
	binary.LittleEndian.PutUint32(wave[4:], uint32(len(wave)-8))
	binary.LittleEndian.PutUint32(wave[40:], uint32(frames*2))
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint16(wave[44+i*2:], uint16(i%32767+1))
	}
	return wave
}

func requireNativeWAV(t *testing.T, root string, original []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, "received.wav"))
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(original)
	if len(want) < 44+201*2 {
		want = append(want, make([]byte, 44+201*2-len(want))...)
		binary.LittleEndian.PutUint32(want[4:], uint32(len(want)-8))
		binary.LittleEndian.PutUint32(want[40:], uint32(len(want)-44))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("native WAV differs: got %d bytes (%d frames), want %d bytes (%d frames); original samples and zero padding must match", len(got), (len(got)-44)/2, len(want), (len(want)-44)/2)
	}
}

func requireNoShortWAVDispatch(t *testing.T, s *Supervisor, root, generation string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "submitted")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid or cancelled WAV reached native: %v", err)
	}
	health, err := s.Health(context.Background())
	if err != nil || health.Status != domain.BackendReady || health.ProcessGeneration != generation {
		t.Fatalf("pre-submission failure changed residency: %+v %v", health, err)
	}
	files, err := filepath.Glob(filepath.Join(s.opts.WorkRoot, "run-*", "request-*.wav"))
	if err != nil || len(files) != 0 {
		t.Fatalf("request cleanup: files=%v error=%v", files, err)
	}
}

func TestShortCanonicalWAVForwardedToFakeNative(t *testing.T) {
	opts, root := sampleCountOptions(t)
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())
	for _, frames := range []int{1, 199, 200, 201, 16000} {
		t.Run(strconv.Itoa(frames), func(t *testing.T) {
			wave := wavWithFrames(frames)
			original := bytes.Clone(wave)
			request := requestFor(s, "")
			request.Audio = bytes.NewReader(wave)
			request.Size = int64(len(wave))
			result, err := s.Infer(context.Background(), request)
			if err != nil || result.Text != "fixture transcript" {
				t.Fatalf("inference: result=%+v error=%v", result, err)
			}
			requireNativeWAV(t, root, original)
			if !bytes.Equal(wave, original) {
				t.Fatal("borrowed WAV was modified")
			}
			health, _ := s.Health(context.Background())
			if health.ProcessGeneration != first.ProcessGeneration {
				t.Fatal("successful inference restarted native")
			}
		})
	}
}

func TestShortCanonicalWAVInputBound(t *testing.T) {
	opts, root := sampleCountOptions(t)
	opts.MaxWAVBytes = 46 // The bound applies to caller input, not internal silence.
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())
	request := requestFor(s, "")
	request.Audio = bytes.NewReader(wavWithFrames(2))
	request.Size = 48
	_, err := s.Infer(context.Background(), request)
	requireKind(t, err, domain.InvalidInput)
	requireNoShortWAVDispatch(t, s, root, first.ProcessGeneration)

	wave := wavWithFrames(1)
	request.Audio = bytes.NewReader(wave)
	request.Size = int64(len(wave))
	if _, err := s.Infer(context.Background(), request); err != nil {
		t.Fatal("valid input at the configured bound rejected", err)
	}
	requireNativeWAV(t, root, wave)
}

func TestShortCanonicalWAVTruncatedBeforeSubmission(t *testing.T) {
	for _, frames := range []int{1, 199, 200} {
		t.Run(strconv.Itoa(frames), func(t *testing.T) {
			opts, root := sampleCountOptions(t)
			s := runningSupervisor(t, opts)
			first, _ := s.Health(context.Background())
			wave := wavWithFrames(frames)
			request := requestFor(s, "")
			request.Audio = bytes.NewReader(wave[:len(wave)-1])
			request.Size = int64(len(wave))
			_, err := s.Infer(context.Background(), request)
			requireKind(t, err, domain.InvalidInput)
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatal("truncated PCM cause lost", err)
			}
			requireNoShortWAVDispatch(t, s, root, first.ProcessGeneration)
		})
	}
}

func TestPrivateShortCanonicalWAVValidation(t *testing.T) {
	opts, root := sampleCountOptions(t)
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	for _, name := range []string{"truncated-header", "truncated-pcm", "excess-pcm", "odd-pcm", "empty", "riff-size", "data-size", "stereo", "float", "sample-rate", "max-size"} {
		t.Run(name, func(t *testing.T) {
			wave := wavWithFrames(200)
			switch name {
			case "truncated-header":
				wave = wave[:43]
			case "truncated-pcm":
				wave = wave[:len(wave)-2]
			case "excess-pcm":
				wave = append(wave, 0, 0)
			case "odd-pcm":
				wave = wave[:len(wave)-1]
				binary.LittleEndian.PutUint32(wave[4:], uint32(len(wave)-8))
				binary.LittleEndian.PutUint32(wave[40:], uint32(len(wave)-44))
			case "empty":
				wave = wavWithFrames(0)
			case "riff-size":
				binary.LittleEndian.PutUint32(wave[4:], 0)
			case "data-size":
				binary.LittleEndian.PutUint32(wave[40:], 0)
			case "stereo":
				binary.LittleEndian.PutUint16(wave[22:], 2)
			case "float":
				binary.LittleEndian.PutUint16(wave[20:], 3)
			case "sample-rate":
				binary.LittleEndian.PutUint32(wave[24:], 8000)
			case "max-size":
				wave = wavWithFrames(16001)
			}
			r := privateRequest(t, s, server.URL)
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			file, err := form.CreateFormFile("file", "audio.wav")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.Write(wave)
			_ = form.Close()
			_ = r.Body.Close()
			r.Body = io.NopCloser(&body)
			r.ContentLength = int64(body.Len())
			r.Header.Set("Content-Type", form.FormDataContentType())
			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			want := http.StatusBadRequest
			if name == "max-size" {
				want = http.StatusRequestEntityTooLarge
			}
			if readErr != nil || response.StatusCode != want {
				t.Fatalf("validation: status=%d body=%s error=%v", response.StatusCode, data, readErr)
			}
			requireNoShortWAVDispatch(t, s, root, first.ProcessGeneration)
		})
	}
}

type shortWAVReader struct {
	source io.Reader
	after  func() error
	reads  int
	closed bool
}

func (r *shortWAVReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads > 1 {
		if err := r.after(); err != nil {
			return 0, err
		}
	}
	return r.source.Read(p)
}

func (r *shortWAVReader) Close() error {
	r.closed = true
	return nil
}

func TestShortCanonicalWAVReadFailureBeforeSubmission(t *testing.T) {
	for _, name := range []string{"read-error", "cancel", "timeout"} {
		t.Run(name, func(t *testing.T) {
			opts, root := sampleCountOptions(t)
			s := runningSupervisor(t, opts)
			first, _ := s.Health(context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("audio source failure")
			reader := &shortWAVReader{source: bytes.NewReader(wavWithFrames(200))}
			reader.after = func() error { return cause }
			if name == "cancel" {
				reader.after = func() error { cancel(); return nil }
			}
			if name == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 50*time.Millisecond)
				defer stop()
				reader.after = func() error { <-ctx.Done(); return ctx.Err() }
			}
			request := requestFor(s, "")
			request.Audio = reader
			request.Size = 44 + 200*2
			_, err := s.Infer(ctx, request)
			if name == "read-error" {
				requireKind(t, err, domain.InvalidInput)
				if !errors.Is(err, cause) {
					t.Fatal("source error lost", err)
				}
			} else {
				requireKind(t, err, domain.DeadlineExceeded)
			}
			if reader.closed {
				t.Fatal("supervisor closed caller-owned stream")
			}
			requireNoShortWAVDispatch(t, s, root, first.ProcessGeneration)
			if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
				t.Fatal("pre-submission failure retained admission slot", err)
			}
		})
	}
}

func TestShortCanonicalWAVBorrowsOnlyDeclaredBytes(t *testing.T) {
	opts, root := sampleCountOptions(t)
	s := runningSupervisor(t, opts)
	wave := wavWithFrames(200)
	// A sized borrowed stream need not reach EOF; trailing bytes belong to its owner.
	reader, writer := io.Pipe()
	written := make(chan struct{})
	var writeErr error
	go func() {
		defer close(written)
		_, writeErr = writer.Write(wave)
	}()
	defer func() {
		_ = reader.Close()
		_ = writer.Close()
		<-written
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := requestFor(s, "")
	request.Audio = reader
	request.Size = int64(len(wave))
	result := make(chan error, 1)
	go func() {
		_, err := s.Infer(ctx, request)
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		_ = reader.CloseWithError(ctx.Err())
		<-result
		t.Fatal("supervisor read beyond declared WAV size")
	}
	<-written
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	requireNativeWAV(t, root, wave)

	// Already available excess bytes must likewise remain unread by Infer.
	trailing := []byte("caller-owned trailing bytes")
	source := bytes.NewReader(append(bytes.Clone(wave), trailing...))
	request.Audio = source
	if _, err := s.Infer(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(source)
	if err != nil || !bytes.Equal(remaining, trailing) {
		t.Fatalf("borrowed stream overread: remaining=%q error=%v", remaining, err)
	}
	requireNativeWAV(t, root, wave)
}

func TestShortCanonicalWAVValidationOrder(t *testing.T) {
	opts, root := sampleCountOptions(t)
	s := runningSupervisor(t, opts)
	first, _ := s.Health(context.Background())
	for _, name := range []string{"cancelled", "compatibility", "fields", "size"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &shortWAVReader{source: bytes.NewReader(wavWithFrames(200)), after: func() error { return nil }}
			request := requestFor(s, "")
			request.Audio, request.Size = reader, 44+200*2
			kind := domain.InvalidInput
			switch name {
			case "cancelled":
				cancel()
				request.Compatibility.ProcessGeneration = "stale"
				request.Fields.Language = "invalid-language"
				request.Size = opts.MaxWAVBytes + 1
				kind = domain.DeadlineExceeded
			case "compatibility":
				request.Compatibility.ProcessGeneration = "stale"
				request.Fields.Language = "invalid-language"
				request.Size = opts.MaxWAVBytes + 1
				kind = domain.NotReady
			case "fields":
				request.Fields.Language = "invalid-language"
				request.Size = opts.MaxWAVBytes + 1
			case "size":
				request.Size = opts.MaxWAVBytes + 1
			}
			_, err := s.Infer(ctx, request)
			requireKind(t, err, kind)
			if reader.reads != 0 || reader.closed {
				t.Fatal("rejected request consumed or closed borrowed audio")
			}
			requireNoShortWAVDispatch(t, s, root, first.ProcessGeneration)
		})
	}
}
