package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// A single selected container log stream must retain both unpredictable markers.
// This proves only an interval conditional on an ordered, unbroken CRI stream;
// it cannot independently prove a runtime never omitted an interior line.
type p08AnchoredWindow struct {
	Start, End time.Time
	residentSupervisorGCObservation
}

func p08AnchoredGC(data []byte, role, startID, endID string) (p08AnchoredWindow, error) {
	var zero p08AnchoredWindow
	if len(data) == 0 {
		return zero, p08GCFailure("window_parse", "empty_logs")
	}
	if len(data) >= p08GCLogLimit {
		return zero, p08GCFailure("window_parse", "logs_too_large")
	}
	if data[len(data)-1] != '\n' {
		return zero, p08GCFailure("window_parse", "truncated_logs")
	}
	if startID == "" || endID == "" || startID == endID {
		return zero, p08GCFailure("window_parse", "invalid_marker_ids")
	}
	if role != "api" && role != "backend" {
		return zero, p08GCFailure("window_parse", "invalid_role")
	}
	route := "/health/ready"
	if role == "backend" {
		route = "/health"
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	var numeric bytes.Buffer
	seenStart, seenEnd := false, false
	var last, start, end time.Time
	for scanner.Scan() {
		line := scanner.Text()
		prefix, body, ok := strings.Cut(line, " ")
		at, err := time.Parse(time.RFC3339Nano, prefix)
		if !ok || err != nil || (!last.IsZero() && at.Before(last)) {
			return zero, p08GCFailure("window_parse", "invalid_timestamp")
		}
		last = at
		var event struct {
			Event     string `json:"event"`
			Component string `json:"component"`
			ID        string `json:"request_id"`
			Method    string `json:"method"`
			Route     string `json:"route"`
			Status    int    `json:"status"`
		}
		if strings.HasPrefix(body, "{") && json.Unmarshal([]byte(body), &event) == nil && (event.ID == startID || event.ID == endID) {
			if event.Event != "request_completed" || event.Component != role || event.Method != http.MethodGet || event.Route != route || event.Status != http.StatusOK {
				return zero, p08GCFailure("window_parse", "invalid_marker")
			}
			if event.ID == startID {
				if seenStart || seenEnd {
					return zero, p08GCFailure("window_parse", "ambiguous_marker")
				}
				seenStart = true
				start = at
			} else {
				if !seenStart || seenEnd {
					return zero, p08GCFailure("window_parse", "ambiguous_marker")
				}
				seenEnd = true
				end = at
			}
			continue
		}
		if seenStart && !seenEnd && (strings.HasPrefix(body, "gc ") || strings.HasPrefix(body, "scav ")) {
			numeric.WriteString(line)
			numeric.WriteByte('\n')
		}
	}
	if scanner.Err() != nil {
		return zero, p08GCFailure("window_parse", "line_too_large")
	}
	if !seenStart || !seenEnd || end.Before(start) {
		return zero, p08GCFailure("window_parse", "missing_marker")
	}
	observed, err := observeResidentSupervisorGC(numeric.Bytes())
	if err != nil {
		return zero, p08GCFailure("window_parse", "invalid_gc")
	}
	// Reuse P07 ordering and duplicate-cycle checks; no absent series is turned into zero.
	if _, err = appendResidentGCWindow(nil, observed.GC); err != nil {
		return zero, p08GCFailure("window_parse", "invalid_gc")
	}
	return p08AnchoredWindow{Start: start, End: end, residentSupervisorGCObservation: observed}, nil
}

func p08GCAnchorID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "p08-gc-" + hex.EncodeToString(raw[:]), nil
}

func p08GCAnchor(ctx context.Context, target p08ResourceTarget, client *PublicClient, role, port, id string) error {
	if role == "api" {
		endpoint := strings.TrimSuffix(client.endpoint, "/v1/audio/transcriptions") + "/health/ready"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return errResidentSupervisorHeapUnknown
		}
		req.Header.Set("X-Request-ID", id)
		resp, err := client.client.Do(req)
		if err != nil {
			return errResidentSupervisorHeapUnknown
		}
		n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 4097))
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil || n > 4096 || resp.StatusCode != http.StatusOK || resp.Header.Get("X-Request-ID") != id {
			return errResidentSupervisorHeapUnknown
		}
		return nil
	}
	if role != "backend" {
		return errResidentSupervisorHeapUnknown
	}
	data, err := target.command(ctx, "exec", target.BackendPod, "-c", target.BackendContainer, "--", "curl", "--silent", "--show-error", "--noproxy", "*", "--connect-timeout", "1", "--max-time", "2", "--output", "/dev/null", "--write-out", "%{http_code}", "--header", "X-Request-ID: "+id, "http://127.0.0.1:"+port+"/health")
	if err != nil || string(data) != strconv.Itoa(http.StatusOK) {
		return errResidentSupervisorHeapUnknown
	}
	return nil
}

type p08AnchoredEvidence struct {
	API, Backend              p08AnchoredWindow
	Health                    residentHealthObservation
	BackendHealth             p08BackendHealthObservation
	Qualification, Limitation string
}

func observeP08AnchoredGC(ctx context.Context, target p08ResourceTarget, client *PublicClient, port string, owner func(context.Context) (p08RepeatedIdentity, error)) (p08AnchoredEvidence, error) {
	return observeP08AnchoredGCBatch(ctx, target, client, port, owner, residentHealthObservationBatch)
}

func observeP08AnchoredGCBatch(ctx context.Context, target p08ResourceTarget, client *PublicClient, port string, owner func(context.Context) (p08RepeatedIdentity, error), calls int) (p08AnchoredEvidence, error) {
	result := p08AnchoredEvidence{Qualification: "not_established", Limitation: "numeric diagnostic only; interval assumes one complete ordered CRI log stream, not calibrated heap or stability evidence"}
	if client == nil || port == "" || owner == nil || calls < 0 || calls > residentHealthObservationMaxCalls {
		return result, p08GCFailure("window_input", "invalid_input")
	}
	before, err := owner(ctx)
	if err != nil || before.containers[0].ID == "" || before.containers[1].ID == "" || before.apiStart == 0 || before.backendStart == 0 || before.nativePID <= 1 || before.nativeStart == 0 || before.generation == "" || before.fingerprint == "" {
		return result, p08GCFailure("window_owner_before", "identity_unavailable")
	}
	for _, pair := range [][2]string{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		b, e := target.command(ctx, "exec", pair[0], "-c", pair[1], "--", "sh", "-ec", p08GCProfileProbe)
		if e != nil || string(b) != "verified\n" {
			return result, p08GCFailure("window_profile", "profile_unverified")
		}
	}
	var ids [4]string
	for i := range ids {
		ids[i], err = p08GCAnchorID()
		if err != nil {
			return result, p08GCFailure("window_input", "invalid_input")
		}
	}
	if p08GCAnchor(ctx, target, client, "api", port, ids[0]) != nil || p08GCAnchor(ctx, target, client, "backend", port, ids[1]) != nil {
		return result, p08GCFailure("window_anchor_start", "request_failed")
	}
	apiCalls := (calls + 1) / 2
	if err = runResidentHealthObservation(ctx, client, &result.Health, apiCalls); err != nil {
		return result, p08GCFailure("window_health", "health_failed")
	}
	if err = runP08BackendHealthObservation(ctx, target, port, &result.BackendHealth, calls-apiCalls); err != nil {
		return result, p08GCFailure("window_health", "health_failed")
	}
	if p08GCAnchor(ctx, target, client, "backend", port, ids[2]) != nil || p08GCAnchor(ctx, target, client, "api", port, ids[3]) != nil {
		return result, p08GCFailure("window_anchor_end", "request_failed")
	}
	for i, pair := range [][2]string{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		data, e := p08GCLogs(ctx, target, pair[0], pair[1])
		if e != nil {
			return result, e
		}
		role, start, end := "api", ids[0], ids[3]
		if i == 1 {
			role, start, end = "backend", ids[1], ids[2]
		}
		gc, e := p08AnchoredGC(data, role, start, end)
		if e != nil {
			return result, e
		}
		if i == 0 {
			result.API = gc
		} else {
			result.Backend = gc
		}
	}
	after, err := owner(ctx)
	if err != nil || ctx.Err() != nil {
		return result, p08GCFailure("window_owner_after", "identity_unavailable")
	}
	if after != before {
		return result, p08GCFailure("window_owner_after", "identity_changed")
	}
	return result, nil
}

func TestP08GCAnchoredDiagnostic(t *testing.T) {
	if os.Getenv("P08_GC_ANCHORED") != "1" {
		t.Skip("requires an authorized parent-owned Kubernetes pair")
	}
	address, model, dir := os.Getenv("P08_DEVELOPMENT_API_URL"), os.Getenv("P08_DEVELOPMENT_MODEL"), os.Getenv("P08_RESOURCE_EVIDENCE_DIR")
	if !isLoopbackAPIRoot(address) || model == "" || !filepath.IsAbs(dir) {
		t.Fatal("explicit loopback API, model and private evidence directory required")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("existing mode 0700 private evidence directory required")
	}
	target, err := readP08ResourceTarget(os.Getenv("P08_RESOURCE_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load(nil, []string{})
	if err != nil {
		t.Fatal("workload configuration unavailable")
	}
	port := strconv.Itoa(settings.Backend.Port)
	client, err := NewPublicClient(address, 5*time.Second, 1<<20)
	if err != nil {
		t.Fatal("public client unavailable")
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	owner := func(ctx context.Context) (p08RepeatedIdentity, error) {
		return p08RepeatedOwner(ctx, target, model, port)
	}
	evidence, err := observeP08AnchoredGC(ctx, target, client, port, owner)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("diagnostic encoding failed")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-gc-anchored.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("diagnostic creation failed")
	}
	_, werr := f.Write(append(data, '\n'))
	cerr := f.Close()
	if errors.Join(werr, cerr) != nil {
		t.Fatal("diagnostic write failed")
	}
	t.Log("anchored partial GC diagnostics recorded; calibrated memory evidence not established")
}
