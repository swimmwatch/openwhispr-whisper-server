package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

// This probe inspects PID 1, not the environment of a newly exec'd shell. It
// emits only a fixed token; even an empty explicit GC override is rejected.
const p08GCProfileProbe = `set -eu
tr '\000' '\n' < /proc/1/environ | awk '
BEGIN { n=0; bad=0 }
/^GODEBUG=/ { n++; if ($0 != "GODEBUG=gctrace=1,scavtrace=1") bad=1 }
/^GOGC=/ { bad=1 }
/^GOMEMLIMIT=/ { bad=1 }
END { if (n != 1 || bad) exit 1; print "verified" }'
`

type p08GCPartialEvidence struct {
	StartedAt     time.Time                       `json:"started_at"`
	FinishedAt    time.Time                       `json:"finished_at"`
	Health        residentHealthObservation       `json:"health"`
	API           residentSupervisorGCObservation `json:"api"`
	Backend       residentSupervisorGCObservation `json:"backend"`
	Qualification string                          `json:"qualification"`
	Limitation    string                          `json:"limitation"`
}

// This deliberately records diagnostics only. Kubernetes has no --until log
// selector; a bounded read cannot establish the completed, gap-free window
// needed by the shared post-work calibration and therefore cannot qualify it.
func observeP08GCPartial(ctx context.Context, target p08ResourceTarget, client *PublicClient, model, port string, owner func(context.Context) (p08RepeatedIdentity, error)) (p08GCPartialEvidence, error) {
	result := p08GCPartialEvidence{StartedAt: time.Now().UTC(), Qualification: "not_established", Limitation: "bounded numeric diagnostics only; no gap-free calibrated post-work windows, memory envelope or stability verdict"}
	if client == nil || model == "" || port == "" || owner == nil {
		return result, errResidentSupervisorHeapUnknown
	}
	initial, err := owner(ctx)
	if err != nil || initial.containers[0].ID == "" || initial.containers[1].ID == "" || initial.apiStart == 0 || initial.backendStart == 0 || initial.nativePID <= 1 || initial.nativeStart == 0 || initial.generation == "" || initial.fingerprint == "" {
		return result, errResidentSupervisorHeapUnknown
	}
	for _, pair := range [][2]string{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		b, e := target.command(ctx, "exec", pair[0], "-c", pair[1], "--", "sh", "-ec", p08GCProfileProbe)
		if e != nil || string(b) != "verified\n" {
			return result, errResidentSupervisorHeapUnknown
		}
	}
	if err = runResidentHealthObservation(ctx, client, &result.Health, residentHealthObservationBatch); err != nil {
		return result, err
	}
	result.FinishedAt = time.Now().UTC()
	for i, pair := range [][2]string{{target.APIPod, target.APIContainer}, {target.BackendPod, target.BackendContainer}} {
		b, e := target.command(ctx, "logs", pair[0], "-c", pair[1], "--timestamps=true", "--since-time="+result.StartedAt.Format(time.RFC3339Nano))
		if e != nil {
			return result, errResidentSupervisorHeapUnknown
		}
		numeric, e := observeResidentSupervisorGC(b)
		if e != nil {
			return result, errResidentSupervisorHeapUnknown
		}
		if numeric.GC != nil {
			for _, record := range numeric.GC.Records {
				if record.ObservedAt == nil || record.ObservedAt.Before(result.StartedAt) {
					return result, errResidentSupervisorHeapUnknown
				}
			}
		}
		if i == 0 {
			result.API = numeric
		} else {
			result.Backend = numeric
		}
	}
	after, err := owner(ctx)
	if err != nil || after != initial || ctx.Err() != nil {
		return result, errResidentSupervisorHeapUnknown
	}
	return result, nil
}

func TestP08GCDiagnostic(t *testing.T) {
	if os.Getenv("P08_GC_DIAGNOSTIC") != "1" {
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
	client, err := NewPublicClient(address, 5*time.Second, 1<<20)
	if err != nil {
		t.Fatal("public client unavailable")
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	owner := func(ctx context.Context) (p08RepeatedIdentity, error) {
		return p08RepeatedOwner(ctx, target, model, strconv.Itoa(settings.Backend.Port))
	}
	evidence, err := observeP08GCPartial(ctx, target, client, model, strconv.Itoa(settings.Backend.Port), owner)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("diagnostic encoding failed")
	}
	f, err := os.OpenFile(filepath.Join(dir, "p08-gc-diagnostic.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("diagnostic creation failed")
	}
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if errors.Join(writeErr, closeErr) != nil {
		t.Fatal("diagnostic write failed")
	}
	t.Log("partial numeric diagnostics recorded; calibrated memory evidence not established")
}
