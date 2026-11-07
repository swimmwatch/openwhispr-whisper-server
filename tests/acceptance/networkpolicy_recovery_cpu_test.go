package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func proveRecoveryFixture() proveRecoveryRecord {
	s := pairingSelection{Kubeconfig: "/private/config", Context: "context", Namespace: "target", NamespaceUID: "namespace-uid", OtherNamespace: "other", OtherNamespaceUID: "other-uid", Release: "release", BackendPod: "release-backend-0", BackendPodUID: "backend-uid", PairedPod: "paired", PairedPodUID: "paired-uid", WrongPod: "wrong", WrongPodUID: "wrong-uid", OtherPod: "other-pod", OtherPodUID: "other-pod-uid", ControlPod: "control", ControlPodUID: "control-uid", ProbeContainer: "curl", BackendPort: 1234, ControlPort: 1235}
	original := map[string]string{"app": "api", proveOrdinalKey: "release-api-0"}
	changed := proveCopy(original)
	changed[proveOrdinalKey] = "release-api-1"
	owner, fence := strings.Repeat("ab", 16), strings.Repeat("cd", 16)
	markers := proveMarkers{OwnerAnnotation: "acceptance.whisper-server.io/owner-" + owner, OwnerToken: owner, FenceAnnotation: "acceptance.whisper-server.io/fence-" + fence, FenceToken: fence}
	return proveRecoveryRecord{Selection: s, PolicyName: "release-pair-0", PolicyUID: "uid", Original: original, Changed: changed, Markers: markers}
}
func privateRecoveryPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "record.json")
}

func TestNetworkPolicyRecoveryRecord(t *testing.T) {
	path := privateRecoveryPath(t)
	r := proveRecoveryFixture()
	if err := proveWriteRecovery(path, r); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("record permissions")
	}
	got, err := proveReadRecovery(path)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("record did not round trip")
	}
	if proveWriteRecovery(path, r) == nil {
		t.Fatal("existing record overwritten")
	}
	got, err = proveReadRecovery(path)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("previous record changed")
	}
	if os.Chmod(path, 0644) != nil {
		t.Fatal("chmod failed")
	}
	if _, err = proveReadRecovery(path); err == nil {
		t.Fatal("accepted public record")
	}
}
func TestNetworkPolicyRecoveryRejectsInvalidRecord(t *testing.T) {
	r := proveRecoveryFixture()
	r.Changed["app"] = "other"
	path := privateRecoveryPath(t)
	if proveWriteRecovery(path, r) == nil {
		t.Fatal("accepted unrelated selector change")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid recovery was persisted")
	}
	r = proveRecoveryFixture()
	r.Selection.Context = strings.Repeat("a", 65537)
	if proveWriteRecovery(path, r) == nil {
		t.Fatal("accepted a record that recovery cannot read")
	}
}
func TestNetworkPolicyRecoveryAfterProcessDeath(t *testing.T) {
	r := proveRecoveryFixture()
	f, _, _ := proveFixture()
	f.p.Spec.Ingress[0].From[0].PodSelector.MatchLabels = proveCopy(r.Original)
	baseline := f.p
	path := privateRecoveryPath(t)
	if err := proveWriteRecovery(path, r); err != nil {
		t.Fatal(err)
	}
	// No in-process cleanup: the next process reads the durable record.
	if err := proveMutation(context.Background(), f, baseline, r.Original, r.Changed, r.Markers); err != nil {
		t.Fatal(err)
	}
	saved, err := proveReadRecovery(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := proveRestore(ctx, f, saved.PolicyUID, saved.Original, saved.Changed, saved.Markers); err != nil {
		t.Fatal(err)
	}
	selector, _ := proveSelector(f.p)
	if !reflect.DeepEqual(selector, r.Original) {
		t.Fatal("recovery did not restore original selector")
	}
	if proveMutation(context.Background(), f, baseline, r.Original, r.Changed, r.Markers) == nil {
		t.Fatal("late mutation survived recovery fence")
	}
}
