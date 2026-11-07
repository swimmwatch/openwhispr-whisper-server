package acceptance

import (
	"errors"
	"net/http"
	"testing"
)

func residentGenerationStable(before, after DockerSnapshot) error {
	if err := lifecycleSnapshotFacts(before, "ready"); err != nil {
		return err
	}
	if err := lifecycleSnapshotFacts(after, "ready"); err != nil {
		return err
	}
	if *before.NativeHostPID != *after.NativeHostPID || *before.NativeNamespacePID != *after.NativeNamespacePID || before.PrivateHealth.ProcessGeneration != after.PrivateHealth.ProcessGeneration || before.PrivateHealth.Model != after.PrivateHealth.Model || before.PrivateHealth.ProtocolVersion != after.PrivateHealth.ProtocolVersion || before.PrivateHealth.ConfigurationFingerprint != after.PrivateHealth.ConfigurationFingerprint {
		return errors.New("successful requests did not preserve the resident model process")
	}
	return nil
}

func TestResidentContinuityRejectsReloadAndUnknown(t *testing.T) {
	host, namespace := 1000, 30
	health := DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: "ready", CUDAReady: true, Model: "base", ProcessGeneration: "first", ProtocolVersion: "1", ConfigurationFingerprint: "selected"}
	zero := uint64(0)
	resources := DockerResourceSnapshot{RSSBytes: &zero, FDCount: &zero, ScratchFileCount: &zero, ScratchBytes: &zero}
	before := DockerSnapshot{PrivateHealth: &health, NativeHostPID: &host, NativeNamespacePID: &namespace, API: resources, Backend: resources}
	if err := residentGenerationStable(before, before); err != nil {
		t.Fatal("unchanged healthy resident process rejected", err)
	}
	for _, mode := range []string{"generation", "host-pid", "missing-health", "missing-host", "no-cuda"} {
		t.Run(mode, func(t *testing.T) {
			after := before
			changed := health
			after.PrivateHealth = &changed
			switch mode {
			case "generation":
				changed.ProcessGeneration = "replacement"
			case "host-pid":
				next := host + 1
				after.NativeHostPID = &next
			case "missing-health":
				after.PrivateHealth = nil
			case "missing-host":
				after.NativeHostPID = nil
			case "no-cuda":
				changed.CUDAReady = false
			}
			if err := residentGenerationStable(before, after); err == nil {
				t.Fatal("resident reload or unavailable evidence was accepted")
			}
		})
	}
}
