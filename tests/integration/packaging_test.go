package integration

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	privateMarker       = "WHISPER_P05_PRIVATE_BUILD_MARKER"
	defaultSettingsPath = "internal/platform/config/defaults.json"
	yamlObjectScript    = "import json,sys,yaml; print(json.dumps(yaml.safe_load(open(sys.argv[1]))))"
)

func root(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func run(t *testing.T, input []byte, name string, args ...string) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root(t)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, stderr.Bytes())
	}

	return out
}

func docker(t *testing.T, args ...string) []byte {
	t.Helper()

	return run(t, nil, "docker", args...)
}

func pythonBinary() string {
	python := os.Getenv("PYTHON")
	if python == "" {
		python = "python3"
	}

	return python
}

func jsonObject(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}

	return value
}

func yamlObject(t *testing.T, path string) map[string]any {
	t.Helper()

	return jsonObject(t, run(t, nil, pythonBinary(), "-c", yamlObjectScript, path))
}

func defaultSettings(t *testing.T) map[string]any {
	t.Helper()

	return jsonObject(t, artifact(t, defaultSettingsPath))
}

func core(t *testing.T) map[string]any {
	t.Helper()

	return yamlObject(t, "docs/specs/gpu-whisper-server/core/contracts.yaml")
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()

	v, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", value)
	}

	return v
}

func artifact(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root(t), path))
	if err != nil {
		t.Fatalf("required packaging artifact %s: %v", path, err)
	}

	return data
}

func imageTests(t *testing.T) {
	t.Helper()

	if os.Getenv("P05_IMAGE_TESTS") != "1" {
		t.Skip("image execution requires P05_IMAGE_TESTS=1 after GPU-free builds")
	}
}

func TestBackendSecurityFixes(t *testing.T) {
	imageTests(t)

	args := []string{"run", "--rm", "--runtime=runc", "--entrypoint=/bin/sh", image("backend"), "-ec", `
while [ "$#" -gt 0 ]; do
  package=$1; minimum=$2; shift 2
  installed=$(dpkg-query -W -f='${Version}' "$package")
  dpkg --compare-versions "$installed" ge "$minimum" || {
    printf 'Unpatched runtime package: %s %s (minimum %s)\n' "$package" "$installed" "$minimum" >&2
    exit 1
  }
done`, "security-check", "openssl", "3.0.13-0ubuntu3.16", "libssl3t64", "3.0.13-0ubuntu3.16"}
	for _, name := range []string{
		"dirmngr", "gnupg", "gnupg-utils", "gnupg2", "gpg", "gpg-agent", "gpgconf", "gpgsm", "gpgv",
		"keyboxd",
	} {
		args = append(args, name, "2.4.4-2ubuntu17.4")
	}
	docker(t, args...)
}

func image(role string) string {
	if v := os.Getenv("P05_" + strings.ToUpper(role) + "_IMAGE"); v != "" {
		return v
	}

	return "whisper-" + role + ":local"
}

func inspect(t *testing.T, ref string) map[string]any {
	t.Helper()

	var values []map[string]any
	if err := json.Unmarshal(docker(t, "image", "inspect", ref), &values); err != nil || len(values) != 1 {
		t.Fatalf("inspect %s: %v", ref, err)
	}

	return values[0]
}

func copyFrom(t *testing.T, container, source, destination string) {
	t.Helper()

	docker(t, "cp", container+":"+source, destination)
}

// Resolve the literal ARG defaults used by this project's FROM instructions.
// Docker's real parser/build checks validate the full Dockerfile grammar.
func defaultBaseImages(file []byte) []string {
	arguments := make(map[string]string)
	expand := func(key string) string {
		return arguments[key]
	}

	var images []string
	for _, line := range strings.Split(string(file), "\n") {
		if declaration, ok := strings.CutPrefix(line, "ARG "); ok {
			if name, value, assigned := strings.Cut(declaration, "="); assigned {
				arguments[name] = os.Expand(value, expand)
			}
		}
		if declaration, ok := strings.CutPrefix(line, "FROM "); ok {
			fields := strings.Fields(declaration)
			if len(fields) > 0 {
				images = append(images, os.Expand(fields[0], expand))
			}
		}
	}

	return images
}

func TestImageContract(t *testing.T) {
	contract := core(t)
	runtime := object(t, contract["runtime"])
	for _, role := range []string{"api", "backend"} {
		t.Run(role, func(t *testing.T) {
			file := artifact(t, "containers/"+role+".Dockerfile")
			bases := defaultBaseImages(file)
			if len(bases) != 3 {
				t.Fatal("missing default build/runtime bases")
			}
			for _, base := range bases {
				if !regexp.MustCompile(`^[^\s]+@sha256:[a-f0-9]{64}$`).MatchString(base) {
					t.Fatal("default base identity is not pinned")
				}
			}
			imageTests(t)

			ref := image(role)
			meta := inspect(t, ref)
			if meta["Os"] != "linux" || meta["Architecture"] != "amd64" {
				t.Fatal("wrong image platform")
			}

			cfg := object(t, meta["Config"])
			if cfg["User"] != "10001:10001" {
				t.Fatal("runtime must use explicit non-root UID/GID")
			}

			labels := object(t, cfg["Labels"])
			if labels["org.opencontainers.image.source"] != "https://github.com/swimmwatch/openwhispr-whisper-server" ||
				labels["org.opencontainers.image.revision"] == "" {
				t.Fatal("missing OCI source identity")
			}
			for _, field := range []string{
				"title", "description", "authors", "vendor", "url", "documentation", "version",
				"licenses",
			} {
				value, ok := labels["org.opencontainers.image."+field].(string)
				if !ok || strings.TrimSpace(value) == "" {
					t.Fatalf("missing OCI metadata %s", field)
				}
			}
			if labels["org.opencontainers.image.title"] != "openwhispr-whisper-server-"+role {
				t.Fatal("OCI title does not identify the image role")
			}
			if created, ok := labels["org.opencontainers.image.created"].(string); ok {
				if _, err := time.Parse(time.RFC3339Nano, created); err != nil {
					t.Fatalf("invalid OCI creation timestamp: %v", err)
				}
			}

			ports := object(t, cfg["ExposedPorts"])
			if len(ports) != 1 || ports[map[string]string{
				"api":     "8080/tcp",
				"backend": "8081/tcp",
			}[role]] == nil {
				t.Fatal("image exposes unexpected listeners")
			}

			container := strings.TrimSpace(string(docker(t, "create", "--runtime=runc", "--entrypoint=/bin/true", ref)))
			t.Cleanup(func() {
				docker(t, "rm", "-f", container)
			})

			dir := t.TempDir()
			binary := filepath.Join(dir, "whisper-"+role)
			copyFrom(t, container, "/usr/local/bin/whisper-"+role, binary)
			info, err := buildinfo.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}

			goVersion := object(t, runtime["api"])["toolchain_baseline"].(string)
			if info.GoVersion != "go"+goVersion {
				t.Fatalf("actual Go compiler %s", info.GoVersion)
			}
			for _, setting := range info.Settings {
				if setting.Key == "CGO_ENABLED" && setting.Value != "0" {
					t.Fatal("Go binary unexpectedly requires CGO")
				}
			}

			var echo bool
			for _, module := range info.Deps {
				if module.Path == object(t, runtime["api"])["framework"] {
					echo = module.Version == object(t, runtime["api"])["framework_version"]
				}
			}
			if !echo {
				t.Fatal("actual Echo dependency does not match the contract")
			}
			copyFrom(t, container, "/usr/local/share/whisper", dir)
			sources, err := os.ReadFile(filepath.Join(dir, "whisper", "go-sources.txt"))
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(sources)), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 2 {
					t.Fatal("malformed source identity")
				}

				path := filepath.Clean(fields[1])
				if filepath.IsAbs(path) ||
					strings.HasPrefix(path, "..") ||
					!(path == "go.mod" ||
						path == "go.sum" ||
						strings.HasPrefix(path, "cmd/") ||
						strings.HasPrefix(path, "internal/")) {
					t.Fatal("source identity escapes the build inputs")
				}
				if digest := fmt.Sprintf("%x", sha256.Sum256(artifact(t, path))); digest != fields[0] {
					t.Fatalf("image was built from different source bytes: %s", path)
				}
			}

			identity, err := os.ReadFile(filepath.Join(dir, "whisper", "build.json"))
			if err != nil {
				t.Fatal(err)
			}

			var build map[string]any
			if err := json.Unmarshal(identity, &build); err != nil {
				t.Fatal(err)
			}
			if build["gpu_build"] != false || build["go_version"] != goVersion {
				t.Fatal("invalid build provenance")
			}
			for _, name := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md", "go"} {
				if _, err := os.Stat(filepath.Join(dir, "whisper", "licenses", name)); err != nil {
					t.Fatal(err)
				}
			}

			output := docker(
				t, "run", "--rm", "--runtime=runc", "--network=none",
				"--env=NVIDIA_VISIBLE_DEVICES=void", "--entrypoint=/bin/sh", ref, "-ec",
				"test \"$(id -u)\" = 10001; if test -e /dev/nvidia0; then exit 1; fi; ldd /usr/local/bin/whisper-"+role+" 2>&1 || true")
			if bytes.Contains(output, []byte("not found")) {
				t.Fatal("missing runtime library")
			}
			if role == "api" {
				archive, err := os.ReadFile(filepath.Join(dir, "whisper", "sources", "ffmpeg-n9.0.2.tar.gz"))
				if err != nil {
					t.Fatal(err)
				}
				if fmt.Sprintf("%x", sha256.Sum256(archive)) != build["ffmpeg_sha256"] {
					t.Fatal("FFmpeg corresponding source does not match the build")
				}

				version := docker(t, "run", "--rm", "--runtime=runc", "--network=none", "--entrypoint=ffmpeg", ref, "-version")
				if !bytes.Contains(version, []byte("ffmpeg version 9.0.2")) ||
					bytes.Contains(version, []byte("--enable-gpl")) ||
					bytes.Contains(version, []byte("--enable-nonfree")) {
					t.Fatal("incorrect FFmpeg version or license configuration")
				}
				docker(
					t, "run", "--rm", "--runtime=runc", "--network=none", "--entrypoint=/bin/sh", ref,
					"-ec",
					"if command -v whisper-server || command -v nvcc; then exit 1; fi; ! ldd /usr/local/bin/ffmpeg | grep 'not found'")
			} else {
				backend := object(t, runtime["backend"])
				if build["upstream_revision"] != backend["revision"] || build["server_blob"] != backend["server_blob"] {
					t.Fatal("native source identity changed")
				}

				var arches []string
				for _, v := range backend["cuda_architectures"].([]any) {
					arches = append(arches, v.(string))
				}
				if build["cuda_architectures"] != strings.Join(arches, ";") || build["ggml_native"] != false {
					t.Fatal("native CUDA build flags changed")
				}

				flags, err := os.ReadFile(filepath.Join(dir, "whisper", "native-build-flags.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(flags, []byte("GGML_NATIVE:BOOL=OFF")) ||
					!bytes.Contains(flags, []byte("="+strings.Join(arches, ";"))) {
					t.Fatal("actual CMake configuration does not match declared coverage")
				}

				cmake, err := os.ReadFile(filepath.Join(dir, "whisper", "cmake-version.txt"))
				if err != nil || !bytes.HasPrefix(cmake, []byte("cmake version "+build["cmake_version"].(string))) {
					t.Fatal("actual CMake version does not match build provenance")
				}

				libraries := filepath.Join(dir, "native-libs")
				copyFrom(t, container, "/usr/local/lib/whisper", libraries)
				for _, pair := range []struct {
					kind, flag string
				}{
					{"real", "--list-elf"}, {"virtual", "--list-ptx"},
				} {
					report := docker(
						t, "run", "--rm", "--runtime=runc", "--network=none",
						"--env=NVIDIA_VISIBLE_DEVICES=void",
						"--mount=type=bind,src="+libraries+",dst=/artifacts,readonly",
						"--entrypoint=/usr/local/cuda/bin/cuobjdump", build["cuda_builder"].(string),
						pair.flag, "/artifacts/libggml-cuda.so")
					for _, arch := range arches {
						parts := strings.Split(arch, "-")
						if parts[1] == pair.kind && !bytes.Contains(report, []byte("sm_"+parts[0])) {
							t.Fatalf("compiled CUDA artifact omits %s", arch)
						}
					}
				}
				// The host driver supplies libcuda.so.1 only when GPU access is granted.
				docker(
					t, "run", "--rm", "--runtime=runc", "--network=none",
					"--env=NVIDIA_VISIBLE_DEVICES=void", "--entrypoint=/bin/sh", ref, "-ec",
					"if command -v nvcc; then exit 1; fi; ldd /usr/local/bin/whisper-server | awk '/not found/ && $1 != \"libcuda.so.1\" {bad=1} END {exit bad}'; test -s /usr/local/share/whisper/licenses/whisper/LICENSE; test -s /usr/local/share/whisper/licenses/cuda/EULA.html")
			}
			scanLayers(t, ref)
		})
	}
}

func scanLayers(t *testing.T, ref string) {
	t.Helper()

	archive := filepath.Join(t.TempDir(), "image.tar")
	docker(t, "image", "save", "--output", archive, ref)
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	layers := map[string]bool{}
	outer := tar.NewReader(f)
	for {
		h, err := outer.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name != "manifest.json" {
			continue
		}

		var manifests []struct {
			Layers []string
		}
		if err := json.NewDecoder(outer).Decode(&manifests); err != nil {
			t.Fatal(err)
		}
		for _, manifest := range manifests {
			for _, layer := range manifest.Layers {
				layers[layer] = true
			}
		}
	}
	if len(layers) == 0 {
		t.Fatal("image archive contains no inspectable layers")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	outer = tar.NewReader(f)
	checked := 0
	markerBuffer := make([]byte, 64<<10+len(privateMarker))
	for {
		h, err := outer.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !layers[h.Name] {
			continue
		}
		checked++
		buffer := bufio.NewReader(outer)
		var reader io.Reader = buffer
		var compressed *gzip.Reader
		magic, err := buffer.Peek(2)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(magic, []byte{0x1f, 0x8b}) {
			compressed, err = gzip.NewReader(buffer)
			if err != nil {
				t.Fatal(err)
			}
			reader = compressed
		}

		inner := tar.NewReader(reader)
		for {
			entry, err := inner.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}

			name := "/" + strings.TrimPrefix(entry.Name, "./")
			for _, forbidden := range []string{".local/", "/.git/", "/go/pkg/mod/", "/src/whisper/", "/src/ffmpeg/"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("build/private input leaked into a layer: %s", name)
				}
			}
			if entry.Typeflag == tar.TypeReg {
				found, err := containsMarker(inner, markerBuffer)
				if err != nil {
					t.Fatal(err)
				}
				if found {
					t.Fatal("private marker leaked into image layer")
				}
			}
		}
		if compressed != nil {
			if err := compressed.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if checked != len(layers) {
		t.Fatal("not all saved image layers were inspected")
	}
}

func containsMarker(reader io.Reader, buffer []byte) (bool, error) {
	keep := 0
	for {
		n, err := reader.Read(buffer[keep:])
		total := keep + n
		if bytes.Contains(buffer[:total], []byte(privateMarker)) {
			return true, nil
		}
		keep = min(total, len(privateMarker)-1)
		copy(buffer[:keep], buffer[total-keep:total])
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}

func TestComposeConfiguration(t *testing.T) {
	compose := os.Getenv("P05_COMPOSE_FILE")
	if compose == "" {
		compose = "deploy/compose.yaml"
	}

	defaults := artifact(t, defaultSettingsPath)
	for _, registry := range []string{"local", "ghcr.io/swimmwatch", "docker.io/swimmwatch"} {
		for _, prepared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/prepared=%t", registry, prepared), func(t *testing.T) {
				dir := t.TempDir()
				settings := jsonObject(t, defaults)
				if prepared {
					models := object(t, settings["models"])
					models["catalog"] = []any{
						map[string]any{
							"id": "base",
							"source": map[string]any{
								"kind": "prepared",
								"path": "/prepared/model.bin",
							},
						},
					}
					models["downloadSet"] = []any{}
					api := object(t, settings["api"])
					api["port"] = float64(18080)
					api["backendUrl"] = "http://backend:18081"
					object(t, settings["backend"])["port"] = float64(18081)
				}

				grace := 45 * time.Second
				if registry == "local" && prepared {
					object(t, settings["timeouts"])["shutdownSeconds"] = float64(50)
					object(t, settings["timeouts"])["cleanupSeconds"] = float64(15)
					grace = 70 * time.Second
				}

				data, err := json.Marshal(settings)
				if err != nil {
					t.Fatal(err)
				}

				cfg := filepath.Join(dir, "settings.json")
				if err := os.WriteFile(cfg, data, 0600); err != nil {
					t.Fatal(err)
				}

				api, backend := "whisper-api:local", "whisper-backend:local"
				if registry != "local" {
					api, backend = registry+"/openwhispr-whisper-server-api:v0.1.0-rc.1",
						registry+"/openwhispr-whisper-server-backend:v0.1.0-rc.1"
				}

				env := filepath.Join(dir, "compose.env")
				apiPort := object(t, settings["api"])["port"].(float64)
				backendPort := object(t, settings["backend"])["port"].(float64)
				text := fmt.Sprintf(
					"WHISPER_API_IMAGE=%s\nWHISPER_BACKEND_IMAGE=%s\nWHISPER_CONFIG_FILE=%s\nWHISPER_PREPARED_DIR=%s\nWHISPER_GPU_DEVICE_ID=0\nWHISPER_STOP_GRACE=%s\nWHISPER_API_PORT=28080\nWHISPER_API_CONTAINER_PORT=%.0f\nWHISPER_BACKEND_CONTAINER_PORT=%.0f\n",
					api, backend, cfg, dir, grace, apiPort, backendPort)
				if err := os.WriteFile(env, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}

				rendered := jsonObject(t, docker(t, "compose", "--env-file", env, "-f", compose, "config", "--format=json"))
				services := object(t, rendered["services"])
				if len(services) != 2 {
					t.Fatal("unexpected standalone service")
				}
				for _, role := range []string{"api", "backend"} {
					s := object(t, services[role])
					want := map[string]string{
						"api":     api,
						"backend": backend,
					}[role]
					if s["image"] != want || s["read_only"] != true || s["user"] != "10001:10001" || s["privileged"] == true {
						t.Fatal("invalid image or isolation")
					}

					duration, err := time.ParseDuration(s["stop_grace_period"].(string))
					limits := object(t, settings["timeouts"])
					minimum := time.Duration(limits["shutdownSeconds"].(float64)+limits["cleanupSeconds"].(float64)+5) * time.Second
					if err != nil || duration != grace || duration < minimum {
						t.Fatal("incorrect stop budget")
					}
					if role == "api" {
						ports := s["ports"].([]any)
						p := object(t, ports[0])
						if len(ports) != 1 || p["host_ip"] != "127.0.0.1" || p["target"] != apiPort || p["published"] != "28080" {
							t.Fatal("public binding changed")
						}
						if s["deploy"] != nil {
							t.Fatal("API must not reserve a GPU")
						}
					} else {
						if s["ports"] != nil {
							t.Fatal("private backend port published")
						}

						resources := object(t, object(t, s["deploy"])["resources"])
						devices := object(t, resources["reservations"])["devices"].([]any)
						d := object(t, devices[0])
						ids := d["device_ids"].([]any)
						if len(devices) != 1 ||
							len(ids) != 1 ||
							ids[0] != "0" ||
							d["count"] != nil ||
							d["driver"] != "nvidia" ||
							!slices.ContainsFunc(d["capabilities"].([]any), func(v any) bool {
								return v == "gpu"
							}) {
							t.Fatal("backend GPU reservation changed")
						}
					}

					port, path := apiPort, "/health/ready"
					if role == "backend" {
						port, path = backendPort, "/health"
					}

					probe := object(t, s["healthcheck"])["test"].([]any)
					if !slices.Contains(probe, any(fmt.Sprintf("http://127.0.0.1:%.0f%s", port, path))) {
						t.Fatalf("%s healthcheck does not target the configured listener", role)
					}

					var configMount, scratch, cache, source bool
					for _, raw := range s["volumes"].([]any) {
						mount := object(t, raw)
						switch mount["target"] {
						case "/etc/whisper/config.json":
							configMount = mount["read_only"] == true && mount["source"] == cfg
						case "/tmp":
							scratch = mount["type"] == "volume" && mount["source"] == role+"-work"
						case "/models":
							cache = mount["type"] == "volume" && mount["source"] == "model-cache"
						case "/prepared":
							source = mount["read_only"] == true && mount["source"] == dir
						default:
							t.Fatal("unexpected writable mount")
						}
					}
					if !configMount || !scratch ||
						role == "backend" && (!cache || !source) ||
						role == "api" && (cache || source) {
						t.Fatal("storage ownership or config injection changed")
					}
				}
			})
		}
	}
}
