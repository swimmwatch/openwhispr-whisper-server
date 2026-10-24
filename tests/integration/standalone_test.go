package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateNetworkHelper(t *testing.T) {
	if os.Getenv("P05_NETWORK_HELPER") != "1" {
		return
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"starting"}`))
	})

	port := os.Getenv("P05_NETWORK_HELPER_PORT")
	if port == "" {
		port = "8081"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		t.Fatal(err)
	}
}

func TestStandalonePrivateNetwork(t *testing.T) {
	imageTests(t)

	for _, ports := range []struct {
		api, backend int
	}{
		{8080, 8081}, {18080, 18081},
	} {
		t.Run(fmt.Sprintf("api=%d/backend=%d", ports.api, ports.backend), func(t *testing.T) {
			suffix := fmt.Sprint(time.Now().UnixNano())
			project, outside := "whisper-p05-"+suffix, "whisper-p05-outside-"+suffix
			dir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			stack := yamlObject(t, "deploy/compose.yaml")
			services := object(t, stack["services"])
			backend := object(t, services["backend"])
			// Only the GPU-dependent process is replaced; topology/security/mounts come
			// from the canonical stack. CUDA requests are qualified separately in P07.
			delete(backend, "deploy")
			backend["entrypoint"] = []string{"/p05-network-helper"}
			backend["command"] = []string{"-test.run=^TestPrivateNetworkHelper$"}
			backend["environment"] = map[string]string{
				"P05_NETWORK_HELPER":      "1",
				"P05_NETWORK_HELPER_PORT": fmt.Sprint(ports.backend),
				"NVIDIA_VISIBLE_DEVICES":  "void",
			}
			backend["volumes"] = append(backend["volumes"].([]any), map[string]any{
				"type":      "bind",
				"source":    executable,
				"target":    "/p05-network-helper",
				"read_only": true,
			})
			for _, role := range []string{"api", "backend"} {
				s := object(t, services[role])
				s["runtime"] = "runc"
				s["pull_policy"] = "never"
			}

			data, err := json.Marshal(stack)
			if err != nil {
				t.Fatal(err)
			}

			fixture := filepath.Join(dir, "compose.json")
			if err := os.WriteFile(fixture, data, 0600); err != nil {
				t.Fatal(err)
			}

			env := filepath.Join(dir, "compose.env")
			settings := defaultSettings(t)
			api := object(t, settings["api"])
			api["port"] = ports.api
			api["backendUrl"] = fmt.Sprintf("http://backend:%d", ports.backend)
			object(t, settings["backend"])["port"] = ports.backend
			config, err := json.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}

			configFile := filepath.Join(dir, "config.json")
			if err := os.WriteFile(configFile, config, 0644); err != nil {
				t.Fatal(err)
			}

			contents := fmt.Sprintf(
				"WHISPER_API_IMAGE=%s\nWHISPER_BACKEND_IMAGE=%s\nWHISPER_CONFIG_FILE=%s\nWHISPER_PREPARED_DIR=%s\nWHISPER_API_PORT=0\nWHISPER_API_CONTAINER_PORT=%d\nWHISPER_BACKEND_CONTAINER_PORT=%d\n",
				image("api"), image("backend"), configFile, dir, ports.api, ports.backend)
			if err := os.WriteFile(env, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}

			compose := func(args ...string) []byte {
				return docker(t, append([]string{"compose", "-p", project, "--env-file", env, "-f", fixture}, args...)...)
			}
			t.Cleanup(func() {
				compose("down", "--volumes", "--timeout", "5")
			})
			compose("up", "-d")
			apiID := strings.TrimSpace(string(compose("ps", "-q", "api")))
			backendID := strings.TrimSpace(string(compose("ps", "-q", "backend")))
			deadline := time.Now().Add(10 * time.Second)
			for {
				probe := docker(
					t, "exec", backendID, "/bin/sh", "-c",
					fmt.Sprintf("curl -s --max-time 1 -o /dev/null -w '%%{http_code}' http://127.0.0.1:%d/health || true", ports.backend))
				if string(probe) == "503" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("private fixture did not start")
				}
				time.Sleep(50 * time.Millisecond)
			}

			response := docker(
				t, "exec", apiID, "curl", "-s", "--max-time", "2", "-o", "/dev/null",
				"-w", "%{http_code}", fmt.Sprintf("http://backend:%d/health", ports.backend))
			if string(response) != "503" {
				t.Fatal("API cannot reach paired private backend")
			}

			address := strings.TrimSpace(string(compose("port", "api", fmt.Sprint(ports.api))))
			client := &http.Client{Timeout: 2 * time.Second}
			reply, err := client.Get("http://" + address + "/health/live")
			if err != nil {
				t.Fatalf("published API port does not reach the configured listener: %v", err)
			}
			_ = reply.Body.Close()
			if reply.StatusCode != http.StatusOK {
				t.Fatalf("published API liveness returned %d", reply.StatusCode)
			}

			var state []map[string]any
			if err := json.Unmarshal(docker(t, "inspect", backendID), &state); err != nil {
				t.Fatal(err)
			}

			networks := object(t, object(t, state[0]["NetworkSettings"])["Networks"])
			if len(networks) != 1 {
				t.Fatal("backend must join only the dedicated network")
			}

			var backendAddress string
			for _, raw := range networks {
				backendAddress = object(t, raw)["IPAddress"].(string)
			}
			docker(t, "network", "create", outside)
			t.Cleanup(func() {
				docker(t, "network", "rm", outside)
			})

			blocked := docker(
				t, "run", "--rm", "--runtime=runc", "--network="+outside, "--read-only",
				"--entrypoint=/bin/sh", image("api"), "-c",
				fmt.Sprintf("curl -s --connect-timeout 1 --max-time 2 -o /dev/null -w '%%{http_code}' http://%s:%d/health || true", backendAddress, ports.backend))
			if string(blocked) != "000" {
				t.Fatal("private backend reachable from unrelated Docker network")
			}
			if ports := object(t, object(t, state[0]["HostConfig"])["PortBindings"]); len(ports) != 0 {
				t.Fatal("private backend publishes host ports")
			}
		})
	}
}

func TestStandaloneWiring(t *testing.T) {
	imageTests(t)

	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}

	settings := defaultSettings(t)
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
	timeouts := object(t, settings["timeouts"])
	for key := range timeouts {
		timeouts[key] = 1
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}

	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, data, 0644); err != nil {
		t.Fatal(err)
	}

	model := standaloneModel(t)
	if err := os.WriteFile(filepath.Join(dir, "model.bin"), model, 0644); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"api", "backend"} {
		t.Run(role, func(t *testing.T) {
			volume := fmt.Sprintf("whisper-p05-%s-%d", role, time.Now().UnixNano())
			docker(t, "volume", "create", volume)
			t.Cleanup(func() {
				docker(t, "volume", "rm", volume)
			})

			args := []string{
				"create", "--runtime=runc", "--network=none", "--read-only", "--cap-drop=ALL",
				"--security-opt=no-new-privileges", "--env=NVIDIA_VISIBLE_DEVICES=void",
				"--mount=type=volume,source=" + volume + ",target=/tmp",
				"--mount=type=bind,source=" + cfg + ",target=/etc/whisper/config.json,readonly",
			}
			if role == "api" {
				args = append(args, "--publish=127.0.0.1::8080")
			} else {
				args = append(args, "--mount=type=bind,source="+dir+",target=/prepared,readonly")
			}
			args = append(args, image(role), "--config", "/etc/whisper/config.json")
			container := strings.TrimSpace(string(docker(t, args...)))
			t.Cleanup(func() {
				docker(t, "rm", "-f", "-v", container)
			})
			docker(t, "start", container)
			if role == "api" {
				deadline := time.Now().Add(10 * time.Second)
				for {
					// Startup is observed through an actual HTTP request inside the container.
					probe := docker(
						t, "exec", container, "/bin/sh", "-c",
						"curl -s --max-time 1 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/health/live || true")
					if string(probe) == "200" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("API did not become live")
					}
					time.Sleep(50 * time.Millisecond)
				}

				code := docker(
					t, "exec", container, "curl", "-s", "--max-time", "2", "-o", "/dev/null",
					"-w", "%{http_code}", "http://127.0.0.1:8080/health/ready")
				if string(code) != "503" {
					t.Fatal("unavailable backend produced false readiness")
				}
				docker(
					t, "exec", container, "/bin/sh", "-ec",
					"test \"$(id -u)\" = 10001; test -w /tmp; ! touch /readonly-root-probe")
				var wav bytes.Buffer
				for _, value := range []any{
					[]byte("RIFF"), uint32(1636), []byte("WAVEfmt "), uint32(16), uint16(1), uint16(1),
					uint32(8000), uint32(16000), uint16(2), uint16(16), []byte("data"), uint32(1600),
				} {
					if err := binary.Write(&wav, binary.LittleEndian, value); err != nil {
						t.Fatal(err)
					}
				}
				wav.Write(make([]byte, 1600))
				pcm := run(
					t, wav.Bytes(), "docker", "exec", "-i", container, "ffmpeg", "-hide_banner",
					"-loglevel", "error", "-nostdin", "-protocol_whitelist", "pipe,cache",
					"-read_ahead_limit", "-1", "-i", "cache:pipe:0", "-ac", "1", "-ar", "16000",
					"-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
				if len(pcm) != 3200 {
					t.Fatalf("packaged decoder returned %d bytes, expected 16 kHz PCM", len(pcm))
				}
				docker(t, "stop", "--time=5", container)
			}

			exit := strings.TrimSpace(string(docker(t, "wait", container)))
			if role == "backend" && exit != "1" || role == "api" && exit != "0" {
				t.Fatalf("unexpected %s process exit %s", role, exit)
			}

			logs := docker(t, "logs", container)
			if bytes.Contains(logs, []byte(privateMarker)) ||
				role == "backend" && (!bytes.Contains(logs, []byte("backend_failed")) ||
					bytes.Contains(logs, []byte("backend_ready"))) {
				t.Fatal("unsafe or misleading startup diagnostics")
			}

			var state []map[string]any
			if err := json.Unmarshal(docker(t, "inspect", container), &state); err != nil {
				t.Fatal(err)
			}

			host := object(t, state[0]["HostConfig"])
			if host["ReadonlyRootfs"] != true || host["DeviceRequests"] != nil {
				t.Fatal("non-GPU startup was not isolated")
			}

			ports := object(t, host["PortBindings"])
			if role == "backend" && len(ports) != 0 || role == "api" && len(ports) != 1 {
				t.Fatal("private port exposed")
			}
		})
	}
}

func TestStandaloneCachedStartup(t *testing.T) {
	imageTests(t)

	model := standaloneModel(t)
	digest := fmt.Sprintf("%x", sha256.Sum256(model))
	settings := defaultSettings(t)
	models := object(t, settings["models"])
	models["catalog"] = []any{
		map[string]any{
			"id": "base",
			"source": map[string]any{
				"kind":   "download",
				"url":    "https://example.invalid/model.bin",
				"sha256": digest,
			},
		},
	}
	models["downloadSet"] = []any{}
	for key := range object(t, settings["timeouts"]) {
		object(t, settings["timeouts"])[key] = 1
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}

	config := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(config, data, 0644); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprint(time.Now().UnixNano())
	cache, work := "whisper-p05-cache-"+suffix, "whisper-p05-work-"+suffix
	for _, volume := range []string{cache, work} {
		docker(t, "volume", "create", volume)
		t.Cleanup(func() {
			docker(t, "volume", "rm", volume)
		})
	}
	// Populate the disk cache as the same non-root UID, using the image's volume ownership.
	run(
		t, model, "docker", "run", "--rm", "-i", "--runtime=runc", "--network=none", "--read-only",
		"--env=NVIDIA_VISIBLE_DEVICES=void", "--mount=type=volume,src="+cache+",dst=/models",
		"--entrypoint=/bin/sh", image("backend"), "-ec",
		"umask 077; mkdir -p /models/artifacts; cat > /models/artifacts/"+digest+".bin")
	container := strings.TrimSpace(string(docker(
		t, "create", "--runtime=runc", "--network=none", "--read-only", "--cap-drop=ALL",
		"--security-opt=no-new-privileges", "--env=NVIDIA_VISIBLE_DEVICES=void",
		"--mount=type=volume,src="+cache+",dst=/models", "--mount=type=volume,src="+work+",dst=/tmp",
		"--mount=type=bind,src="+config+",dst=/etc/whisper/config.json,readonly", image("backend"),
		"--config", "/etc/whisper/config.json")))
	removed := false
	t.Cleanup(func() {
		if !removed {
			docker(t, "rm", "-f", container)
		}
	})
	docker(t, "start", container)
	if exit := strings.TrimSpace(string(docker(t, "wait", container))); exit != "1" {
		t.Fatalf("GPU-free backend exited %s", exit)
	}

	logs := docker(t, "logs", container)
	if !bytes.Contains(logs, []byte("backend_failed")) ||
		bytes.Contains(logs, []byte("provisioning_failed")) ||
		bytes.Contains(logs, []byte("configuration_rejected")) ||
		bytes.Contains(logs, []byte("backend_ready")) {
		t.Fatalf("cache startup did not reach the unavailable-GPU boundary: %s", logs)
	}
	// Removing a service container must preserve the operator-owned model cache.
	docker(t, "rm", container)
	removed = true
	docker(
		t, "run", "--rm", "--runtime=runc", "--network=none", "--env=NVIDIA_VISIBLE_DEVICES=void",
		"--mount=type=volume,src="+cache+",dst=/models,readonly", "--entrypoint=/bin/sh",
		image("backend"), "-ec", "test -s /models/artifacts/"+digest+".bin")
}

func standaloneModel(t *testing.T) []byte {
	t.Helper()

	var model bytes.Buffer
	for _, value := range []int32{0x67676d6c, 51865, 1500, 512, 8, 6, 448, 512, 8, 6, 80, 1} {
		if err := binary.Write(&model, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}

	return model.Bytes()
}
