package audio

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
)

func TestDecoderRejectsInvalidOptions(t *testing.T) {
	for _, scenario := range []string{"command", "executable", "audio", "duration", "operation", "cleanup", "capacity"} {
		t.Run(scenario, func(t *testing.T) {
			opts := decoderOptions()
			switch scenario {
			case "command":
				opts.Command = nil
			case "executable":
				opts.Command[0] = ""
			case "audio":
				opts.AudioBytes = 0
			case "duration":
				opts.DecodedSeconds = 0
			case "operation":
				opts.OperationTimeout = 0
			case "cleanup":
				opts.CleanupTimeout = 0
			case "capacity":
				opts.Capacity = 0
			}

			_, err := New(opts)
			requireFailure(t, err, domain.InvalidInput)
		})
	}
}

func TestDecoderRejectsUnsafeOrChangedUpload(t *testing.T) {
	for _, scenario := range []string{"relative", "missing", "size-mismatch", "directory", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			input := ownedInput(t, []byte("audio"))
			want := domain.InvalidInput
			switch scenario {
			case "relative":
				input.UploadPath = "upload"
			case "missing":
				if err := os.Remove(input.UploadPath); err != nil {
					t.Fatal(err)
				}
				want = domain.UnsupportedAudio
			case "size-mismatch":
				input.Size++
			case "directory":
				input.UploadPath = t.TempDir()
			case "symlink":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(input.UploadPath, link); err != nil {
					t.Fatal(err)
				}
				input.UploadPath = link
				want = domain.UnsupportedAudio
			}

			decoder, err := New(decoderOptions())
			if err != nil {
				t.Fatal(err)
			}

			_, err = decoder.Normalize(context.Background(), input)
			requireFailure(t, err, want)
		})
	}
}

func TestDecoderOutputChild(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--decoder-output=") {
			mode = strings.TrimPrefix(arg, "--decoder-output=")
		}
	}
	if mode == "" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(2)
	}

	switch mode {
	case "empty":
		os.Exit(0)
	case "odd":
		_, _ = os.Stdout.Write([]byte{1})
	case "failed":
		_, _ = os.Stdout.Write([]byte{1, 2})
		os.Exit(3)
	case "pcm":
		_, _ = os.Stdout.Write([]byte{1, 2})
	default:
		os.Exit(4)
	}
	os.Exit(0)
}

func TestDecoderFailuresReleaseScratchAndCapacity(t *testing.T) {
	for _, scenario := range []string{"missing-command", "empty", "odd", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			opts := decoderOptions()
			opts.Capacity = 1
			opts.Command = []string{os.Args[0], "-test.run=^TestDecoderOutputChild$", "--", "--decoder-output=" + scenario}
			want := domain.UnsupportedAudio
			if scenario == "missing-command" {
				opts.Command = []string{filepath.Join(t.TempDir(), "missing-decoder")}
				want = domain.BackendUnavailable
			}

			decoder, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}

			input := ownedInput(t, []byte("PRIVATE audio"))
			for range 2 {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				result, err := decoder.Normalize(ctx, input)
				cancel()
				requireFailure(t, err, want)
				if result.Audio != nil {
					t.Fatal("failed decoder returned a partial normalized resource")
				}

				files, err := filepath.Glob(filepath.Join(filepath.Dir(input.UploadPath), "normalized-*"))
				if err != nil || len(files) != 0 {
					t.Fatal("failed decoder retained normalized scratch", err)
				}

				data, err := os.ReadFile(input.UploadPath)
				if err != nil || string(data) != "PRIVATE audio" {
					t.Fatal("failed decoder changed caller-owned upload", err)
				}
			}
		})
	}
}

func TestNormalizedCleanupFailureStillReleasesCapacity(t *testing.T) {
	opts := decoderOptions()
	opts.Capacity = 1
	opts.Command = []string{os.Args[0], "-test.run=^TestDecoderOutputChild$", "--", "--decoder-output=pcm"}
	decoder, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	input := ownedInput(t, []byte("audio"))
	first, err := decoder.Normalize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(filepath.Dir(input.UploadPath), "normalized-*"))
	if err != nil || len(files) != 1 {
		t.Fatal("normalized scratch missing", err)
	}
	if err := os.Remove(files[0]); err != nil {
		t.Fatal(err)
	}

	closeErr := first.Audio.Close()
	requireFailure(t, closeErr, domain.Internal)
	if err := first.Audio.Close(); err != closeErr {
		t.Fatal("repeated close did not preserve cleanup result", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	next, err := decoder.Normalize(ctx, input)
	if err != nil {
		t.Fatal("cleanup error retained the decoder capacity", err)
	}
	if err := next.Audio.Close(); err != nil {
		t.Fatal(err)
	}
	if err := next.Audio.Close(); err != nil {
		t.Fatal("successful close was not idempotent", err)
	}
}
