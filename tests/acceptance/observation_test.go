package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dockerObservationCLI(args []string, fail func()) {
	mode := os.Getenv("P07_DOCKER_HELPER_OBSERVE")
	if len(args) < 3 {
		fail()
	}
	switch args[2] {
	case "curl":
		if args[1] != dockerAPIID || args[len(args)-1] != "http://backend:8081/health" {
			fail()
		}
		if mode == "health-unavailable" {
			fail()
		}
		if mode == "missing-cuda" {
			fmt.Printf("{\"status\":\"ready\",\"model\":\"base\",\"protocol_version\":\"1\",\"configuration_fingerprint\":\"fingerprint\",\"process_generation\":\"generation\"}\n%d", http.StatusOK)
			return
		}
		if mode == "recovering" {
			fmt.Printf("{\"status\":\"recovering\",\"model\":\"base\",\"protocol_version\":\"1\",\"configuration_fingerprint\":\"fingerprint\",\"process_generation\":\"generation\",\"cuda_ready\":false}\n%d", http.StatusServiceUnavailable)
			return
		}
		fmt.Printf("{\"status\":\"ready\",\"model\":\"base\",\"protocol_version\":\"1\",\"configuration_fingerprint\":\"fingerprint\",\"process_generation\":\"generation\",\"cuda_ready\":true}\n%d", http.StatusOK)
	case "sh":
		role := args[len(args)-1]
		if role != "api" && role != "backend" {
			fail()
		}
		if role == "api" && args[1] != dockerAPIID || role == "backend" && args[1] != dockerBackendID {
			fail()
		}
		if role == "api" && mode == "api-unavailable" {
			fail()
		}
		if role == "api" && mode == "missing-fd" {
			fmt.Println(`{"rss_bytes":4096,"scratch_file_count":0,"scratch_bytes":0}`)
			return
		}
		if role == "backend" {
			fmt.Println(`{"rss_bytes":8192,"fd_count":6,"scratch_file_count":0,"scratch_bytes":0,"native_pid":42}`)
		} else {
			fmt.Println(`{"rss_bytes":4096,"fd_count":3,"scratch_file_count":0,"scratch_bytes":0,"native_pid":0}`)
		}
	default:
		fail()
	}
}

func TestLocalDockerTargetUsesOnlySelectedNamedDigests(t *testing.T) {
	target, dir := dockerFixtureWithImages(t, dockerFixtureCommandTimeout, true)
	ctx := context.Background()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	ids, err := target.Containers(ctx)
	if err != nil || ids.APIID != dockerAPIID || ids.BackendID != dockerBackendID {
		t.Fatalf("local selected images did not start in the owned project: %+v %v", ids, err)
	}
	if err := target.Down(ctx); err != nil {
		t.Fatal(err)
	}
	cache, err := os.ReadFile(filepath.Join(dir, "cache-model.bin"))
	if err != nil || string(cache) != "cached-model" {
		t.Fatal("local target teardown removed model cache")
	}
}

func TestLocalDockerTargetCannotEnterPublishedConstructorOrUseMutableRefs(t *testing.T) {
	root := t.TempDir()
	base := DockerOptions{Root: root, Project: "p07-inputs", ConfigFile: filepath.Join(root, "config.json"), PreparedDir: filepath.Join(root, "prepared"), APIImage: "whisper-p07-local-api@sha256:" + strings.Repeat("1", 64), BackendImage: "whisper-p07-local-backend@sha256:" + strings.Repeat("2", 64), GPUDevice: "0"}
	if _, err := NewLocalDockerTarget(base); err != nil {
		t.Fatal("selected named local digest rejected", err)
	}
	if _, err := NewDockerTarget(base); err == nil {
		t.Fatal("local images were accepted as a published candidate")
	}
	for name, image := range map[string]string{
		"bare-id":    "sha256:" + strings.Repeat("1", 64),
		"tag":        "whisper-p07-local-api:latest",
		"published":  "ghcr.io/swimmwatch/openwhispr-whisper-server-api@sha256:" + strings.Repeat("1", 64),
		"wrong-role": "whisper-p07-local-backend@sha256:" + strings.Repeat("1", 64),
		"uppercase":  "whisper-p07-local-api@sha256:" + strings.Repeat("A", 64),
	} {
		t.Run(name, func(t *testing.T) {
			opts := base
			opts.APIImage = image
			if _, err := NewLocalDockerTarget(opts); err == nil {
				t.Fatal("non-local or mutable selection accepted")
			}
		})
	}
}

func TestDockerObservationReturnsActualTypedFactsAndKnownZero(t *testing.T) {
	target, _ := dockerFixture(t)
	ctx := context.Background()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	defer target.Down(ctx)
	snapshot, err := target.Observe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.At.IsZero() || snapshot.PrivateHealth == nil || snapshot.PrivateHealth.HTTPStatus != http.StatusOK || !snapshot.PrivateHealth.CUDAReady || snapshot.PrivateHealth.ProcessGeneration != "generation" {
		t.Fatal("private health observation missing")
	}
	if snapshot.API.RSSBytes == nil || *snapshot.API.RSSBytes != 4096 || snapshot.Backend.RSSBytes == nil || *snapshot.Backend.RSSBytes != 8192 || snapshot.API.FDCount == nil || *snapshot.API.FDCount != 3 {
		t.Fatal("process resource observations missing")
	}
	if snapshot.API.ScratchFileCount == nil || *snapshot.API.ScratchFileCount != 0 || snapshot.API.ScratchBytes == nil || *snapshot.API.ScratchBytes != 0 {
		t.Fatal("observed empty scratch was confused with unavailable observation")
	}
	if snapshot.NativeHostPID == nil || *snapshot.NativeHostPID != 1234 || snapshot.NativeNamespacePID == nil || *snapshot.NativeNamespacePID != 42 {
		t.Fatal("native host/namespace process identities missing")
	}
}

func TestDockerObservationKeepsUnavailableFactsUnknown(t *testing.T) {
	for _, mode := range []string{"api-unavailable", "missing-fd", "health-unavailable", "missing-cuda", "missing-native"} {
		t.Run(mode, func(t *testing.T) {
			target, _ := dockerFixture(t)
			ctx := context.Background()
			if err := target.Up(ctx); err != nil {
				t.Fatal(err)
			}
			defer target.Down(ctx)
			t.Setenv("P07_DOCKER_HELPER_OBSERVE", mode)
			snapshot, err := target.Observe(ctx)
			if err == nil || strings.Contains(err.Error(), "PRIVATE_RUNTIME_DIAGNOSTIC") {
				t.Fatal("unavailable observation was accepted or leaked raw output", err)
			}
			switch mode {
			case "api-unavailable", "missing-fd":
				if snapshot.API.RSSBytes != nil || snapshot.API.FDCount != nil || snapshot.PrivateHealth == nil || snapshot.Backend.RSSBytes == nil {
					t.Fatal("missing API resource became zero or erased independent observations")
				}
			case "health-unavailable", "missing-cuda":
				if snapshot.PrivateHealth != nil || snapshot.API.RSSBytes == nil {
					t.Fatal("unknown health became a known false value or erased resource observation")
				}
			case "missing-native":
				if snapshot.NativeHostPID != nil || snapshot.NativeNamespacePID == nil {
					t.Fatal("missing native host PID became zero or erased independent namespace PID")
				}
			}
		})
	}
}

func TestDockerObservationPreservesActualUnreadyHealth(t *testing.T) {
	target, _ := dockerFixture(t)
	ctx := context.Background()
	if err := target.Up(ctx); err != nil {
		t.Fatal(err)
	}
	defer target.Down(ctx)
	t.Setenv("P07_DOCKER_HELPER_OBSERVE", "recovering")
	snapshot, err := target.Observe(ctx)
	if err != nil || snapshot.PrivateHealth == nil || snapshot.PrivateHealth.HTTPStatus != http.StatusServiceUnavailable || snapshot.PrivateHealth.Status != "recovering" || snapshot.PrivateHealth.CUDAReady {
		t.Fatalf("actual unready private health was hidden: %v", err)
	}
}
