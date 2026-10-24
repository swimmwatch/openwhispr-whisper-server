package backend

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const maxGPUQueryBytes = 64 << 10

// These anchored diagnostics come from the pinned whisper.cpp/ggml CUDA source.
// Enumeration alone is insufficient: the selected backend and model allocation
// must both be positive. Arbitrary native output is never retained or emitted.
var devicePattern = regexp.MustCompile(`^  Device ([0-9]+): ([^\r\n]{1,256}), compute capability ([0-9]+\.[0-9]+), VMM: (?:yes|no), VRAM: [0-9]+ MiB$`)
var backendPattern = regexp.MustCompile(`^whisper_backend_init_gpu: using CUDA([0-9]+) backend$`)
var allocationPattern = regexp.MustCompile(`^whisper_model_load: +CUDA([0-9]+) total size = +([0-9]+(?:\.[0-9]+)?) MB$`)
var uuidPattern = regexp.MustCompile(`^GPU-[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

type deviceFact struct {
	ready            bool
	name, capability string
}
type nativeEvidence struct {
	mu                  sync.Mutex
	device              int
	line                [4096]byte
	used                int
	overflow            bool
	backend, allocation bool
	failed              bool
	name, capability    string
}

func (e *nativeEvidence) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			if !e.overflow {
				e.observe(string(e.line[:e.used]))
			}
			clear(e.line[:])
			e.used = 0
			e.overflow = false
		} else if e.used < len(e.line) && !e.overflow {
			e.line[e.used] = b
			e.used++
		} else {
			e.overflow = true
		}
	}

	return len(p), nil
}

func (e *nativeEvidence) observe(line string) {
	if line == "whisper_backend_init_gpu: no GPU found" ||
		line == "whisper_backend_init_gpu: failed to initialize CUDA"+strconv.Itoa(e.device)+" backend" {
		e.failed = true
	}

	if m := devicePattern.FindStringSubmatch(line); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n == e.device {
			e.name = m[2]
			e.capability = m[3]
		}
	}

	if m := backendPattern.FindStringSubmatch(line); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n == e.device {
			e.backend = true
		}
	}

	if m := allocationPattern.FindStringSubmatch(line); m != nil {
		n, err := strconv.Atoi(m[1])
		size, sizeErr := strconv.ParseFloat(m[2], 64)
		if err == nil && sizeErr == nil && n == e.device && size > 0 {
			e.allocation = true
		}
	}
}

func (e *nativeEvidence) snapshot(device int) deviceFact {
	e.mu.Lock()
	defer e.mu.Unlock()

	return deviceFact{
		ready:      device == e.device && !e.failed && e.backend && e.allocation && e.name != "",
		name:       e.name,
		capability: e.capability,
	}
}

func (e *nativeEvidence) discard() {
	e.mu.Lock()
	defer e.mu.Unlock()

	clear(e.line[:])
	e.used = 0
}

var errGPU = errors.New("GPU evidence unavailable")
var errNative = errors.New("native backend unavailable")

func matchGPU(output []byte, fact deviceFact) (string, error) {
	if len(output) > maxGPUQueryBytes || !fact.ready {
		return "", errGPU
	}

	reader := csv.NewReader(bytes.NewReader(output))
	reader.FieldsPerRecord = 3
	reader.TrimLeadingSpace = true

	identity := ""
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errGPU
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		if !uuidPattern.MatchString(row[0]) {
			return "", errGPU
		}
		if row[1] == fact.name && row[2] == fact.capability {
			if identity != "" {
				return "", errGPU
			}
			identity = row[0]
		}
	}

	if identity == "" {
		return "", errGPU
	}
	return identity, nil
}
