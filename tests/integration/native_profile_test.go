package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIDecoderExcludesUnusedNativeParsers(t *testing.T) {
	imageTests(t)

	checks := map[string][]string{
		"-decoders": {"h264"},
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
						t.Fatalf("excluded video/subtitle decoder/parser remains enabled: %s", name)
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
