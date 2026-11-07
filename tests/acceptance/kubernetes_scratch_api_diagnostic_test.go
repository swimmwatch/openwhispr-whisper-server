package acceptance

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This observes the already-running, explicitly selected API Pod only. It is
// a diagnostic for the real scratch probe, not a GPU or P08 acceptance result.
func TestP08ScratchAPIDiagnostic(t *testing.T) {
	if os.Getenv("P08_SCRATCH_API_DIAGNOSTIC") != "1" {
		t.Skip("requires an explicitly selected test API Pod")
	}
	path, expectedUID := os.Getenv("P08_DEVELOPMENT_TARGET"), os.Getenv("P08_SCRATCH_API_UID")
	if !filepath.IsAbs(path) || expectedUID == "" {
		t.Fatal("absolute private target and selected API Pod UID required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("private target unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	closeErr := f.Close()
	var target developmentQueueTarget
	if err != nil || closeErr != nil || len(data) > 64<<10 || json.Unmarshal(data, &target) != nil || !filepath.IsAbs(target.Kubeconfig) || target.Context == "" || target.Namespace == "" || target.NamespaceUID == "" || target.Release == "" || target.APIPod == "" {
		t.Fatal("invalid private target")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	verify := func(ctx context.Context) error {
		ns, e := target.command(ctx, "get", "namespace", target.Namespace, "-o", "jsonpath={.metadata.uid}")
		if e != nil || string(ns) != target.NamespaceUID {
			return errResidentSupervisorHeapUnknown
		}
		uid, e := target.command(ctx, "get", "pod", target.APIPod, "-o", "jsonpath={.metadata.uid}")
		if e != nil || string(uid) != expectedUID {
			return errResidentSupervisorHeapUnknown
		}
		return nil
	}
	var snapshot p08ScratchSnapshot
	if err := waitP08ScratchRelease(ctx, func(ctx context.Context) error {
		if err := verify(ctx); err != nil {
			return err
		}
		output, err := target.command(ctx, "exec", target.APIPod, "-c", "api", "--", "sh", "-eu", "-c", p08ScratchProbe, "p08-scratch", "/tmp", "/proc", "1")
		if err != nil {
			return err
		}
		snapshot, err = parseP08Scratch(output)
		if err != nil {
			return err
		}
		return verify(ctx)
	}); err != nil {
		t.Fatal("scratch observation did not complete on the selected API Pod")
	}
	if !snapshot.valid || len(snapshot.deleted) != 0 {
		t.Fatal("scratch observation is unknown or includes deleted-open files")
	}
	t.Logf("selected API scratch observed: %d visible files, no deleted-open files; diagnostic only", len(snapshot.files))
}
