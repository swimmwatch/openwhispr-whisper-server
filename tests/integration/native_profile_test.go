package integration

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/adapter/audio"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
)

func TestAPIDecoderExcludesUnusedNativeParsers(t *testing.T) {
	imageTests(t)

	checks := map[string][]string{
		"-decoders": {"h264", "g2m", "pcx", "sunrast", "cdxl", "huffyuv", "ffvhuff", "hymt"},
		"-demuxers": {"imf"},
	}
	for command, excluded := range checks {
		t.Run(command, func(t *testing.T) {
			output := docker(t, "run", "--rm", "--runtime", "runc",
				"--env", "NVIDIA_VISIBLE_DEVICES=void", "--entrypoint", "ffmpeg", image("api"),
				"-hide_banner", command)
			nameIndex := 1
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) <= nameIndex {
					continue
				}
				for _, name := range excluded {
					if fields[nameIndex] == name {
						t.Errorf("excluded native component remains enabled: %s", name)
					}
				}
			}
		})
	}
	t.Run("native-parser-registry", func(t *testing.T) {
		container := strings.TrimSpace(string(docker(t, "create", "--entrypoint", "/bin/true", image("api"))))
		defer docker(t, "rm", "-f", container)

		libraries := t.TempDir()
		copyFrom(t, container, "/usr/local/lib/.", libraries)
		const probe = `import ctypes, sys
class Parser(ctypes.Structure):
    _fields_ = [("codec_ids", ctypes.c_int * 7)]
codec = ctypes.CDLL(sys.argv[1])
codec.av_parser_iterate.argtypes = [ctypes.POINTER(ctypes.c_void_p)]
codec.av_parser_iterate.restype = ctypes.POINTER(Parser)
codec.avcodec_get_name.argtypes = [ctypes.c_int]
codec.avcodec_get_name.restype = ctypes.c_char_p
state = ctypes.c_void_p()
names = set()
while True:
    parser = codec.av_parser_iterate(ctypes.byref(state))
    if not parser:
        break
    for identity in parser.contents.codec_ids:
        if identity:
            names.add(codec.avcodec_get_name(identity).decode())
assert {"aac", "mp3"} <= names, "Parser registry probe did not observe required audio parsers"
assert not {"h264", "dvd_subtitle"} & names, "Excluded video/subtitle parser is registered"
print("PASS: actual native parser registry preserves audio and excludes unused vulnerable components")
`
		run(t, nil, "env", "LD_LIBRARY_PATH="+libraries, pythonBinary(), "-c", probe,
			filepath.Join(libraries, "libavcodec.so"))
	})
}

func TestPackagedAudioNormalization(t *testing.T) {
	imageTests(t)

	container := strings.TrimSpace(string(docker(t, "create", "--runtime=runc", "--entrypoint=/bin/true", image("api"))))
	t.Cleanup(func() { docker(t, "rm", "-f", container) })
	directory := t.TempDir()
	decoderBinary := filepath.Join(directory, "ffmpeg")
	libraries := filepath.Join(directory, "lib")
	copyFrom(t, container, "/usr/local/bin/ffmpeg", decoderBinary)
	copyFrom(t, container, "/usr/local/lib/.", libraries)
	decoder, err := audio.New(audio.Options{
		Command:          []string{"env", "LD_LIBRARY_PATH=" + libraries, decoderBinary},
		AudioBytes:       8 << 20,
		DecodedSeconds:   2,
		OperationTimeout: 10 * time.Second,
		CleanupTimeout:   2 * time.Second,
		Capacity:         1,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, fixture := range []struct{ format, codec, video string }{
		{"wav", "pcm_s16le", ""},
		{"mp3", "libmp3lame", ""},
		{"mp4", "aac", ""},
		{"mpeg", "mp2", ""},
		{"mpga", "mp2", ""},
		{"m4a", "aac", ""},
		{"ogg", "libvorbis", ""},
		{"ogg", "libopus", ""},
		{"flac", "flac", ""},
		{"webm", "libopus", ""},
		{"mp4", "aac", "libx264"},
		{"mkv", "pcm_s16le", "huffyuv"},
	} {
		t.Run(strings.Join([]string{fixture.format, fixture.codec, fixture.video}, "-"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture."+fixture.format)
			args := []string{
				"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
				"sine=frequency=440:sample_rate=24000:duration=0.25",
			}
			if fixture.video != "" {
				args = append(args, "-f", "lavfi", "-i", "color=c=black:size=16x16:rate=4:duration=0.25",
					"-map", "0:a:0", "-map", "1:v:0", "-c:v", fixture.video, "-pix_fmt", "yuv422p")
			}
			args = append(args, "-threads", "1", "-c:a", fixture.codec)
			if fixture.format == "mpga" {
				args = append(args, "-f", "mp2")
			}
			run(t, nil, "ffmpeg", append(args, path)...)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.format == "mp4" && bytes.Index(data, []byte("moov")) < bytes.Index(data, []byte("mdat")) {
				t.Fatal("MP4 fixture does not exercise moov-at-end seeking")
			}
			result, err := decoder.Normalize(context.Background(), transcription.Input{
				UploadPath: path, Size: int64(len(data)), Model: "base", RequestID: "packaged-audio",
			})
			if err != nil {
				t.Fatal("packaged decoder rejected supported audio", err)
			}
			wave, readErr := io.ReadAll(result.Audio)
			closeErr := result.Audio.Close()
			if readErr != nil || closeErr != nil || len(wave) < 46 ||
				string(wave[:4]) != "RIFF" || string(wave[8:16]) != "WAVEfmt " ||
				binary.LittleEndian.Uint16(wave[22:]) != 1 ||
				binary.LittleEndian.Uint32(wave[24:]) != 16000 ||
				binary.LittleEndian.Uint16(wave[34:]) != 16 ||
				int64(binary.LittleEndian.Uint32(wave[40:])) != result.Size-44 ||
				int64(len(wave)) != result.Size || result.Duration <= 0 || result.Duration > 2*time.Second {
				t.Fatal("packaged decoder did not produce bounded mono 16 kHz PCM WAV")
			}
		})
	}
}

func TestBackendRuntimeOmitsNsightTooling(t *testing.T) {
	imageTests(t)

	output := docker(t, "run", "--rm", "--runtime", "runc", "--env", "NVIDIA_VISIBLE_DEVICES=void",
		"--entrypoint", "dpkg-query", image("backend"), "-W")
	if strings.Contains(strings.ToLower(string(output)), "nsight") {
		t.Fatal("CUDA runtime unexpectedly contains Nsight tooling")
	}
	docker(t, "run", "--rm", "--runtime", "runc", "--env", "NVIDIA_VISIBLE_DEVICES=void",
		"--entrypoint", "/bin/sh", image("backend"), "-c",
		"! command -v nsys && ! command -v ncu && ! command -v nv-nsight-cu-cli")
}
