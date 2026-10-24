package backend

import (
	"fmt"
	"strings"
	"testing"
)

const deviceLine = "  Device 0: NVIDIA GeForce RTX 2060, compute capability 7.5, VMM: yes, VRAM: 6144 MiB\n"
const backendLine = "whisper_backend_init_gpu: using CUDA0 backend\n"
const allocationLine = "whisper_model_load:        CUDA0 total size =   147.95 MB\n"

func TestStartupEvidence(t *testing.T) {
	var facts nativeEvidence

	for _, line := range []string{deviceLine, backendLine, allocationLine} {
		for _, value := range []byte(line) {
			_, _ = facts.Write([]byte{value})
		}
	}

	got := facts.snapshot(0)
	if !got.ready || got.name != "NVIDIA GeForce RTX 2060" || got.capability != "7.5" {
		t.Fatalf("pinned source facts not recognized: %+v", got)
	}

	for _, invalid := range []string{
		deviceLine + backendLine,
		backendLine + allocationLine,
		deviceLine + "whisper_backend_init_gpu: using CPU backend\n" + allocationLine,
		deviceLine + backendLine + "whisper_model_load: CUDA0 total size = 0.00 MB\n",
		deviceLine + backendLine + "whisper_model_load: CUDA1 total size = 147.95 MB\n",
		deviceLine + "PRIVATE " + backendLine + allocationLine,
		deviceLine + backendLine + allocationLine + "whisper_backend_init_gpu: failed to initialize CUDA0 backend\n",
		deviceLine + strings.Repeat("X", 8192) + backendLine + allocationLine,
	} {
		var other nativeEvidence
		_, _ = other.Write([]byte(invalid))

		if other.snapshot(0).ready {
			t.Fatal("unsafe/incomplete native facts accepted")
		}
	}

	if facts.snapshot(1).ready {
		t.Fatal("evidence for another device accepted")
	}
}

func TestGPUIdentity(t *testing.T) {
	text := "GPU-11111111-1111-1111-1111-111111111111, NVIDIA GeForce RTX 2060, 7.5\n"
	fact := deviceFact{ready: true, name: "NVIDIA GeForce RTX 2060", capability: "7.5"}
	if _, err := matchGPU([]byte(text), fact); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []string{
		"",
		"PRIVATE driver failure",
		strings.ReplaceAll(text, "7.5", "8.6"),
		text + strings.ReplaceAll(text, "1111", "2222"),
		"GPU-invalid, NVIDIA GeForce RTX 2060, 7.5\n",
	} {
		if _, err := matchGPU([]byte(invalid), fact); err == nil {
			t.Fatal("ambiguous/missing/malformed GPU identity accepted", fmt.Sprintf("%q", invalid))
		}
	}
}
