package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"golang.org/x/sys/unix"
)

func decoderOptions() Options {
	return Options{
		Command:          []string{"ffmpeg"},
		AudioBytes:       4 << 20,
		DecodedSeconds:   2,
		OperationTimeout: 5 * time.Second,
		CleanupTimeout:   time.Second,
		Capacity:         2,
	}
}

func encodedFixture(t *testing.T, format string, codec string, duration string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture."+format)
	args := []string{
		"-hide_banner",
		"-loglevel",
		"error",
		"-f",
		"lavfi",
		"-i",
		"sine=frequency=440:sample_rate=24000:duration=" + duration,
		"-c:a",
		codec,
		"-y",
		path,
	}
	if format == "mpga" {
		args = append(args[:len(args)-1], "-f", "mp2", path)
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("real FFmpeg fixture unavailable: %v %s", err, out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func ownedInput(t *testing.T, data []byte) transcription.Input {
	t.Helper()
	path := filepath.Join(t.TempDir(), "PRIVATE-name.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	return transcription.Input{
		UploadPath: path,
		Size:       int64(len(data)),
		Model:      "base",
		RequestID:  "request-1",
		Fields:     domain.InferenceFields{Language: "auto"},
	}
}

func TestAudioFamilies(t *testing.T) {
	d, err := New(decoderOptions())
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct{ format, codec string }{
		{"wav", "pcm_s16le"},
		{"mp3", "libmp3lame"},
		{"mp4", "aac"},
		{"mpeg", "mp2"},
		{"mpga", "mp2"},
		{"m4a", "aac"},
		{"ogg", "libvorbis"},
		{"ogg", "libopus"},
		{"flac", "flac"},
		{"webm", "libopus"},
	} {
		t.Run(test.format+"-"+test.codec, func(t *testing.T) {
			data := encodedFixture(t, test.format, test.codec, "0.25")
			if test.format == "mp4" && bytes.Index(data, []byte("moov")) < bytes.Index(data, []byte("mdat")) {
				t.Fatal("fixture does not exercise moov-at-end seeking")
			}

			input := ownedInput(t, data)
			result, err := d.Normalize(context.Background(), input)
			if err != nil {
				t.Fatal("advertised family did not normalize", err)
			}

			wave, readErr := io.ReadAll(result.Audio)
			closeErr := result.Audio.Close()
			if readErr != nil ||
				closeErr != nil ||
				len(wave) < 46 ||
				string(wave[:4]) != "RIFF" ||
				string(wave[8:16]) != "WAVEfmt " ||
				binary.LittleEndian.Uint16(wave[22:]) != 1 ||
				binary.LittleEndian.Uint32(wave[24:]) != 16000 ||
				binary.LittleEndian.Uint16(wave[34:]) != 16 ||
				int64(binary.LittleEndian.Uint32(wave[40:])) != result.Size-44 ||
				int64(len(wave)) != result.Size ||
				result.Duration <= 0 ||
				result.RequestID != input.RequestID ||
				result.Fields != input.Fields {
				t.Fatal("normalization changed format/fields/ownership")
			}

			files, _ := filepath.Glob(filepath.Join(filepath.Dir(input.UploadPath), "normalized-*"))
			if len(files) != 0 {
				t.Fatal("normalized output retained after Close")
			}
		})
	}
}

func requireFailure(t *testing.T, err error, kind domain.FailureKind) {
	t.Helper()
	var failure *domain.Failure
	if !errors.As(err, &failure) || failure.Kind != kind {
		t.Fatalf("failure=%v want=%s", err, kind)
	}
}

func TestAudioBounds(t *testing.T) {
	data := encodedFixture(t, "wav", "pcm_s16le", "0.25")
	opts := decoderOptions()
	opts.AudioBytes = int64(len(data))
	d, _ := New(opts)
	result, err := d.Normalize(context.Background(), ownedInput(t, data))
	if err != nil {
		t.Fatal("exact payload cap rejected", err)
	}

	_ = result.Audio.Close()
	_, err = d.Normalize(context.Background(), ownedInput(t, append(bytes.Clone(data), 0)))
	requireFailure(t, err, domain.InvalidInput)
	opts.DecodedSeconds = 1
	opts.AudioBytes = 4 << 20
	d, _ = New(opts)
	exact, err := d.Normalize(context.Background(), ownedInput(t, encodedFixture(t, "wav", "pcm_s16le", "1")))
	if err != nil {
		t.Fatal("exact decoded duration rejected", err)
	}
	if exact.Size != 32044 {
		t.Error("exact decoded sample count changed")
	}

	_ = exact.Audio.Close()
	input := ownedInput(t, encodedFixture(t, "wav", "pcm_s16le", "1.1"))
	_, err = d.Normalize(context.Background(), input)
	requireFailure(t, err, domain.AudioTooLong)
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(input.UploadPath), "normalized-*"))
	if len(files) != 0 {
		t.Fatal("overlong decode retained output")
	}

	_, err = d.Normalize(context.Background(), ownedInput(t, []byte("PRIVATE corrupt audio")))
	requireFailure(t, err, domain.UnsupportedAudio)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = d.Normalize(ctx, ownedInput(t, data))
	requireFailure(t, err, domain.DeadlineExceeded)
}

func TestDecoderSourceConfinement(t *testing.T) {
	var accesses atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accesses.Add(1)
		w.WriteHeader(404)
	}))
	defer server.Close()

	opts := decoderOptions()
	d, _ := New(opts)
	sentinel := filepath.Join(t.TempDir(), "private.ts")
	if err := os.WriteFile(sentinel, []byte("PRIVATE sentinel data"), 0600); err != nil {
		t.Fatal(err)
	}

	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err = unix.InotifyAddWatch(fd, sentinel, unix.IN_OPEN|unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{server.URL + "/private.ts", "file:" + sentinel} {
		playlists := []string{
			fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\n%s\n#EXT-X-ENDLIST\n", target),
			fmt.Sprintf(`<?xml version="1.0"?><MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT1S" minBufferTime="PT1S" profiles="urn:mpeg:dash:profile:isoff-on-demand:2011"><Period><AdaptationSet mimeType="audio/mp4"><Representation id="1" bandwidth="128000" codecs="mp4a.40.2"><BaseURL>%s</BaseURL><SegmentBase indexRange="0-10"><Initialization range="0-10"/></SegmentBase></Representation></AdaptationSet></Period></MPD>`, target),
		}
		for _, playlist := range playlists {
			_, err = d.Normalize(context.Background(), ownedInput(t, []byte(playlist)))
			requireFailure(t, err, domain.UnsupportedAudio)
		}
	}

	var events [4096]byte
	n, readErr := unix.Read(fd, events[:])
	if n > 0 || readErr != nil && readErr != unix.EAGAIN {
		t.Fatal("decoder opened a nested file source", n, readErr)
	}
	if accesses.Load() != 0 {
		t.Fatal("decoder contacted a nested network source")
	}
}

func TestDecoderChild(t *testing.T) {
	mode, pidRoot, binary, ffmpegStart := "", "", "", 0
	for _, arg := range os.Args {
		if arg == "--decoder-fixture" {
			mode = "block"
		}
		if strings.HasPrefix(arg, "--decoder-state=") {
			pidRoot = strings.TrimPrefix(arg, "--decoder-state=")
		}
		if strings.HasPrefix(arg, "--decoder-exec=") {
			binary = strings.TrimPrefix(arg, "--decoder-exec=")
			mode = "real"
		}
	}

	for index, arg := range os.Args {
		if arg == "-hide_banner" {
			ffmpegStart = index
			break
		}
	}
	if mode == "" {
		return
	}
	if pidRoot != "" {
		if err := os.WriteFile(filepath.Join(pidRoot, "start-"+strconv.Itoa(os.Getpid())), nil, 0600); err != nil {
			os.Exit(2)
		}
	}
	if mode == "real" {
		args := append([]string{binary, "-readrate", "1"}, os.Args[ffmpegStart:]...)
		if unix.Exec(binary, args, os.Environ()) != nil {
			os.Exit(3)
		}
	}
	fmt.Fprint(os.Stdout, strings.Repeat("x", 320))
	for {
		time.Sleep(time.Second)
	}
}

func TestDecoderCacheLifecycle(t *testing.T) {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}

	pidRoot := t.TempDir()
	opts := decoderOptions()
	opts.DecodedSeconds = 5
	opts.Command = []string{os.Args[0], "-test.run=^TestDecoderChild$", "--", "--decoder-state=" + pidRoot, "--decoder-exec=" + binary}
	d, _ := New(opts)
	input := ownedInput(t, encodedFixture(t, "mp4", "aac", "3"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		result, err := d.Normalize(ctx, input)
		if result.Audio != nil {
			_ = result.Audio.Close()
		}
		done <- err
	}()

	pid := 0
	cache := ""
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cache == "" {
		starts, _ := filepath.Glob(filepath.Join(pidRoot, "start-*"))
		if len(starts) > 0 {
			pid, _ = strconv.Atoi(strings.TrimPrefix(filepath.Base(starts[0]), "start-"))
		}
		if pid > 0 {
			fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
			for _, fd := range fds {
				path := fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name())
				target, _ := os.Readlink(path)
				if strings.HasPrefix(filepath.Base(target), "ffcache") {
					cache = strings.TrimSuffix(target, " (deleted)")
					info, err := os.Stat(path)
					if err != nil || info.Size() > input.Size {
						t.Error("FFmpeg cache exceeded the bounded uploaded bytes", err)
					}
					break
				}
			}
		}
		if cache == "" {
			time.Sleep(time.Millisecond)
		}
	}
	if cache == "" {
		t.Error("real FFmpeg cache descriptor was not observed")
	}
	cancel()
	select {
	case err := <-done:
		requireFailure(t, err, domain.DeadlineExceeded)
	case <-time.After(opts.CleanupTimeout + time.Second):
		t.Fatal("real cached decoder did not stop")
	}
	if pid > 0 {
		if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Error("cached decoder process group survived", err)
		}
	}
	if cache != "" {
		if _, err := os.Stat(cache); !os.IsNotExist(err) {
			t.Error("cached decoder left a temporary cache file", err)
		}
	}
}

func TestDecoderCancellationAndCredits(t *testing.T) {
	opts := decoderOptions()
	pidRoot := t.TempDir()
	opts.Command = []string{os.Args[0], "-test.run=^TestDecoderChild$", "--", "--decoder-fixture", "--decoder-state=" + pidRoot}
	opts.OperationTimeout = 100 * time.Millisecond
	d, _ := New(opts)
	input := ownedInput(t, []byte("fixture"))
	start := time.Now()
	_, err := d.Normalize(context.Background(), input)
	requireFailure(t, err, domain.DeadlineExceeded)
	if time.Since(start) > opts.OperationTimeout+opts.CleanupTimeout+time.Second {
		t.Fatal("hung decoder exceeded stop/reap budget")
	}

	starts, _ := filepath.Glob(filepath.Join(pidRoot, "start-*"))
	if len(starts) != 1 {
		t.Fatal("decoder fixture never started")
	}

	pid, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(starts[0]), "start-"))
	if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("decoder process group survived cancellation", err)
	}

	files, _ := filepath.Glob(filepath.Join(filepath.Dir(input.UploadPath), "normalized-*"))
	if len(files) != 0 {
		t.Fatal("cancelled decoder retained output")
	}

	opts = decoderOptions()
	opts.Capacity = 1
	d, _ = New(opts)
	input = ownedInput(t, encodedFixture(t, "wav", "pcm_s16le", "0.1"))
	first, err := d.Normalize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = d.Normalize(ctx, input)
	requireFailure(t, err, domain.DeadlineExceeded)
	_ = first.Audio.Close()
	next, err := d.Normalize(context.Background(), input)
	if err != nil {
		t.Fatal("normalized credit was not released", err)
	}

	_ = next.Audio.Close()
}

func TestDecoderSingleProcess(t *testing.T) {
	pidRoot := t.TempDir()
	opts := decoderOptions()
	opts.Command = []string{os.Args[0], "-test.run=^TestDecoderChild$", "--", "--decoder-fixture", "--decoder-state=" + pidRoot}
	d, _ := New(opts)
	input := ownedInput(t, []byte("fixture"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	finished := make(chan error, 2)
	go func() {
		_, err := d.Normalize(ctx, input)
		finished <- err
	}()

	deadline := time.Now().Add(time.Second)
	for {
		starts, _ := filepath.Glob(filepath.Join(pidRoot, "start-*"))
		if len(starts) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("decoder child did not start")
		}
		time.Sleep(time.Millisecond)
	}

	go func() {
		_, err := d.Normalize(ctx, input)
		finished <- err
	}()
	time.Sleep(50 * time.Millisecond)
	starts, _ := filepath.Glob(filepath.Join(pidRoot, "start-*"))
	if len(starts) != 1 {
		t.Error("concurrent decoders started")
	}
	cancel()
	for range 2 {
		select {
		case err := <-finished:
			requireFailure(t, err, domain.DeadlineExceeded)
		case <-time.After(opts.CleanupTimeout + time.Second):
			t.Fatal("decoder did not stop")
		}
	}
}

func TestAudioNoStreamAndReporting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	if out, err := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel",
		"error",
		"-f",
		"lavfi",
		"-i",
		"color=c=black:size=16x16:rate=1",
		"-t",
		"1",
		"-an",
		"-c:v",
		"mpeg4",
		path,
	).CombinedOutput(); err != nil {
		t.Fatalf("video-only fixture unavailable: %v %s", err, out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	d, _ := New(decoderOptions())
	_, err = d.Normalize(context.Background(), ownedInput(t, data))
	requireFailure(t, err, domain.UnsupportedAudio)
	report := filepath.Join(t.TempDir(), "private-report.log")
	data = encodedFixture(t, "wav", "pcm_s16le", "0.1")
	t.Setenv("FFREPORT", "file="+report)
	result, err := d.Normalize(context.Background(), ownedInput(t, data))
	if err != nil {
		t.Fatal(err)
	}

	_ = result.Audio.Close()
	if _, err := os.Stat(report); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("decoder retained an inherited content report", err)
	}
}
