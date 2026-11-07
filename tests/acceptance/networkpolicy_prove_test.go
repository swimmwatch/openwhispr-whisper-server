package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const proveOrdinalKey = "statefulset.kubernetes.io/pod-name"
const proveSelectorPath = "/spec/ingress/0/from/0/podSelector/matchLabels"

type provePolicy struct {
	Metadata struct {
		UID             string            `json:"uid"`
		ResourceVersion string            `json:"resourceVersion"`
		Labels          map[string]string `json:"labels"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		PodSelector struct {
			MatchLabels      map[string]string `json:"matchLabels"`
			MatchExpressions []json.RawMessage `json:"matchExpressions"`
		} `json:"podSelector"`
		Ingress []struct {
			From []struct {
				PodSelector *struct {
					MatchLabels      map[string]string `json:"matchLabels"`
					MatchExpressions []json.RawMessage `json:"matchExpressions"`
				} `json:"podSelector"`
				NamespaceSelector json.RawMessage `json:"namespaceSelector"`
				IPBlock           json.RawMessage `json:"ipBlock"`
			} `json:"from"`
		} `json:"ingress"`
	} `json:"spec"`
}

type proveOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}
type proveStore interface {
	read(context.Context) (provePolicy, error)
	patch(context.Context, []proveOperation) error
}
type proveKubeStore struct {
	selection pairingSelection
	name      string
}

func (k proveKubeStore) read(ctx context.Context) (provePolicy, error) {
	var p provePolicy
	data, err := k.selection.command(ctx, k.selection.Namespace, "get", "networkpolicy", k.name, "-o", "json")
	if err != nil || json.Unmarshal(data, &p) != nil {
		return p, errors.New("policy read failed")
	}
	return p, nil
}
func (k proveKubeStore) patch(ctx context.Context, ops []proveOperation) error {
	data, err := json.Marshal(ops)
	if err != nil {
		return err
	}
	_, err = k.selection.command(ctx, k.selection.Namespace, "patch", "networkpolicy", k.name, "--type=json", "-p", string(data))
	return err
}

func proveSelector(p provePolicy) (map[string]string, error) {
	if p.Metadata.UID == "" || p.Metadata.ResourceVersion == "" || len(p.Spec.Ingress) != 1 || len(p.Spec.Ingress[0].From) != 1 {
		return nil, errors.New("unexpected policy identity or ingress")
	}
	f := p.Spec.Ingress[0].From[0]
	if f.PodSelector == nil || len(f.PodSelector.MatchExpressions) != 0 || len(f.NamespaceSelector) != 0 || len(f.IPBlock) != 0 || len(f.PodSelector.MatchLabels) == 0 {
		return nil, errors.New("unexpected policy peer")
	}
	return f.PodSelector.MatchLabels, nil
}
func proveTests(p provePolicy) []proveOperation {
	return []proveOperation{{"test", "/metadata/uid", p.Metadata.UID}, {"test", "/metadata/resourceVersion", p.Metadata.ResourceVersion}}
}
func provePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
func proveCopy(m map[string]string) map[string]string {
	n := make(map[string]string, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}

type proveMarkers struct {
	OwnerAnnotation string `json:"owner_annotation"`
	OwnerToken      string `json:"owner_token"`
	FenceAnnotation string `json:"fence_annotation"`
	FenceToken      string `json:"fence_token"`
}

// The separate fence advances resourceVersion before inspection, invalidating a
// late mutation even if the original request timed out before server commit.
func proveRestore(ctx context.Context, store proveStore, uid string, original, changed map[string]string, markers proveMarkers) error {
	annotation, token := markers.FenceAnnotation, markers.FenceToken
	fenced := false
	first := true
	for ctx.Err() == nil {
		if !first {
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return errors.New("cleanup deadline exceeded; manual recovery required")
			case <-timer.C:
			}
		}
		first = false
		p, err := store.read(ctx)
		if err != nil {
			continue
		}
		if p.Metadata.UID != uid {
			return errors.New("policy UID changed; manual recovery required")
		}
		selector, err := proveSelector(p)
		if err != nil {
			return errors.New("policy changed; manual recovery required")
		}
		if !fenced {
			if value, exists := p.Metadata.Annotations[annotation]; exists {
				if value != token {
					return errors.New("fence annotation owned by another writer; manual recovery required")
				}
				// Our unique fence was committed despite an ambiguous response.
				fenced = true
				continue
			}
			ops := proveTests(p)
			if p.Metadata.Annotations == nil {
				ops = append(ops, proveOperation{"add", "/metadata/annotations", map[string]string{annotation: token}})
			} else {
				ops = append(ops, proveOperation{"add", "/metadata/annotations/" + provePointer(annotation), token})
			}
			if store.patch(ctx, ops) != nil {
				continue
			}
			fenced = true
			continue
		}
		if p.Metadata.Annotations[annotation] != token {
			return errors.New("fence disappeared; manual recovery required")
		}
		if !reflect.DeepEqual(selector, original) && !reflect.DeepEqual(selector, changed) {
			return errors.New("foreign selector; manual recovery required")
		}
		owner, owned := p.Metadata.Annotations[markers.OwnerAnnotation]
		if owned && owner != markers.OwnerToken {
			return errors.New("foreign ownership annotation; manual recovery required")
		}
		if reflect.DeepEqual(selector, changed) && !owned {
			return errors.New("changed selector lacks ownership marker; manual recovery required")
		}
		ops := proveTests(p)
		ops = append(ops, proveOperation{"test", proveSelectorPath, selector}, proveOperation{"test", "/metadata/annotations/" + provePointer(annotation), token})
		if owned {
			ops = append(ops, proveOperation{"test", "/metadata/annotations/" + provePointer(markers.OwnerAnnotation), markers.OwnerToken})
		}
		if reflect.DeepEqual(selector, changed) {
			ops = append(ops, proveOperation{"replace", proveSelectorPath, original})
		}
		if owned {
			ops = append(ops, proveOperation{Op: "remove", Path: "/metadata/annotations/" + provePointer(markers.OwnerAnnotation)})
		}
		ops = append(ops, proveOperation{Op: "remove", Path: "/metadata/annotations/" + provePointer(annotation)})
		if store.patch(ctx, ops) == nil {
			return nil
		}
	}
	return errors.New("cleanup deadline exceeded; manual recovery required")
}

func proveMutation(ctx context.Context, store proveStore, p provePolicy, original, changed map[string]string, markers proveMarkers) error {
	ops := proveTests(p)
	ops = append(ops, proveOperation{"test", proveSelectorPath, original})
	if p.Metadata.Annotations == nil {
		ops = append(ops, proveOperation{"add", "/metadata/annotations", map[string]string{markers.OwnerAnnotation: markers.OwnerToken}})
	} else {
		ops = append(ops, proveOperation{"add", "/metadata/annotations/" + provePointer(markers.OwnerAnnotation), markers.OwnerToken})
	}
	ops = append(ops, proveOperation{"replace", proveSelectorPath + "/" + provePointer(proveOrdinalKey), changed[proveOrdinalKey]})
	return store.patch(ctx, ops)
}

func proveProbe(ctx context.Context, s pairingSelection, ns, pod, controlIP, backendIP string, allowed bool) error {
	if s.probe(ctx, ns, pod, controlIP, s.ControlPort, "/health/live", true) != nil {
		return errors.New("control failed")
	}
	return s.probe(ctx, ns, pod, backendIP, s.BackendPort, "/health", allowed)
}

type proveProbeFunc func(context.Context, string, string, string, int, string, bool) error

func proveWidened(ctx context.Context, probe proveProbeFunc, ns, pod, control, backend string, controlPort, backendPort int) error {
	if probe(ctx, ns, pod, control, controlPort, "/health/live", true) != nil {
		return errors.New("mutation control failed")
	}
	if probe(ctx, ns, pod, backend, backendPort, "/health", true) != nil {
		return errors.New("wrong backend was not positively reachable under mutation")
	}
	if probe(ctx, ns, pod, control, controlPort, "/health/live", true) != nil {
		return errors.New("mutation control failed after negative check")
	}
	return nil
}

// This captures one real response so both assertions classify identical bytes.
// s.probe remains the owner of all control, baseline and final probes; it does
// not expose its response, so a second observation is necessary for PROVE.
func proveWidenedResponse(ctx context.Context, s pairingSelection, backendIP string) error {
	var data []byte
	command := func(ctx context.Context, namespace string, args ...string) ([]byte, error) {
		out, err := s.command(ctx, namespace, args...)
		if len(args) > 0 && args[0] == "exec" && err == nil {
			data = append([]byte(nil), out...)
		}
		return out, err
	}
	err := s.probeWithCommand(ctx, command, s.Namespace, s.WrongPod, backendIP, s.BackendPort, "/health", true)
	if err != nil || pairingResult(data, true) != nil || pairingResult(data, false) == nil {
		return errors.New("real wrong-backend response did not prove negative assertion detects widening")
	}
	return nil
}

// Explicit opt-in only: this mutates one selected release policy. No resources
// are created; the parent owns the selected probes and authorization.
func TestP08NetworkPolicyMutationProve(t *testing.T) {
	if os.Getenv("P08_NETWORKPOLICY_PROVE") != "1" {
		t.Skip("requires explicitly authorized test-release policy mutation")
	}
	path := os.Getenv("P08_PAIRING_TARGET")
	if !filepath.IsAbs(path) {
		t.Fatal("absolute private pairing selection required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("private pairing selection unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	var s pairingSelection
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err != nil || len(data) > 65536 || dec.Decode(&s) != nil || dec.Decode(new(any)) != io.EOF || s.validate() != nil {
		t.Fatal("invalid private pairing selection")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	for _, n := range []struct{ ns, uid string }{{s.Namespace, s.NamespaceUID}, {s.OtherNamespace, s.OtherNamespaceUID}} {
		uid, e := s.command(ctx, n.ns, "get", "namespace", n.ns, "-o", "jsonpath={.metadata.uid}")
		if e != nil || string(uid) != n.uid {
			t.Fatal("namespace identity mismatch")
		}
	}
	backend, e1 := s.pod(ctx, s.Namespace, s.BackendPod)
	paired, e2 := s.pod(ctx, s.Namespace, s.PairedPod)
	wrong, e3 := s.pod(ctx, s.Namespace, s.WrongPod)
	other, e4 := s.pod(ctx, s.OtherNamespace, s.OtherPod)
	control, e5 := s.pod(ctx, s.Namespace, s.ControlPod)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || pairingLabels(backend, paired, wrong, other, s.Release, s.BackendPod) != nil {
		t.Fatal("probe identity or readiness mismatch")
	}
	prefix, ordinal, _ := strings.Cut(s.BackendPod, "-backend-")
	store := proveKubeStore{s, prefix + "-pair-" + ordinal}
	p, err := store.read(ctx)
	if err != nil {
		t.Fatal("policy unavailable")
	}
	original, err := proveSelector(p)
	if err != nil || p.Metadata.Annotations["meta.helm.sh/release-name"] != s.Release || p.Metadata.Annotations["meta.helm.sh/release-namespace"] != s.Namespace || p.Metadata.Labels["app.kubernetes.io/instance"] != s.Release || len(p.Spec.PodSelector.MatchExpressions) != 0 || p.Spec.PodSelector.MatchLabels[proveOrdinalKey] != s.BackendPod || original[proveOrdinalKey] != paired.Metadata.Labels[proveOrdinalKey] {
		t.Fatal("policy release or selector identity mismatch")
	}
	for k, v := range original {
		if paired.Metadata.Labels[k] != v {
			t.Fatal("policy does not select paired probe")
		}
	}
	for k, v := range p.Spec.PodSelector.MatchLabels {
		if backend.Metadata.Labels[k] != v {
			t.Fatal("policy does not select backend")
		}
	}
	changed := proveCopy(original)
	changed[proveOrdinalKey] = wrong.Metadata.Labels[proveOrdinalKey]
	for k, v := range changed {
		if wrong.Metadata.Labels[k] != v {
			t.Fatal("mutated selector would not select wrong probe")
		}
	}
	for _, source := range []struct {
		ns, pod string
		allowed bool
	}{{s.Namespace, s.PairedPod, true}, {s.Namespace, s.WrongPod, false}} {
		if proveProbe(ctx, s, source.ns, source.pod, control.Status.PodIP, backend.Status.PodIP, source.allowed) != nil {
			t.Fatal("baseline probe failed")
		}
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal("cannot create ownership and fence tokens")
	}
	ownerToken, fenceToken := hex.EncodeToString(random[:16]), hex.EncodeToString(random[16:])
	markers := proveMarkers{OwnerAnnotation: "acceptance.whisper-server.io/owner-" + ownerToken, OwnerToken: ownerToken, FenceAnnotation: "acceptance.whisper-server.io/fence-" + fenceToken, FenceToken: fenceToken}
	if _, exists := p.Metadata.Annotations[markers.FenceAnnotation]; exists {
		t.Fatal("cleanup fence annotation already exists")
	}
	if _, exists := p.Metadata.Annotations[markers.OwnerAnnotation]; exists {
		t.Fatal("ownership annotation already exists")
	}
	record := proveRecoveryRecord{Selection: s, PolicyName: store.name, PolicyUID: p.Metadata.UID, Original: proveCopy(original), Changed: proveCopy(changed), Markers: markers}
	if err := proveWriteRecovery(os.Getenv("P08_NETWORKPOLICY_RECOVERY_RECORD"), record); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Minute)
		defer stop()
		if err := proveRestore(cleanup, store, p.Metadata.UID, original, changed, markers); err != nil {
			t.Error(err)
			return
		}
		for _, source := range []struct {
			ns, pod string
			allowed bool
		}{{s.Namespace, s.PairedPod, true}, {s.Namespace, s.WrongPod, false}, {s.OtherNamespace, s.OtherPod, false}} {
			if proveProbe(cleanup, s, source.ns, source.pod, control.Status.PodIP, backend.Status.PodIP, source.allowed) != nil {
				t.Error("post-restore pairing probe failed")
			}
		}
	})
	if proveMutation(ctx, store, p, original, changed, markers) != nil {
		t.Fatal("mutation result ambiguous; cleanup must fence and restore")
	}
	if err := proveWidened(ctx, s.probe, s.Namespace, s.WrongPod, control.Status.PodIP, backend.Status.PodIP, s.ControlPort, s.BackendPort); err != nil {
		t.Fatal(err)
	}
	if s.probe(ctx, s.Namespace, s.WrongPod, control.Status.PodIP, s.ControlPort, "/health/live", true) != nil || proveWidenedResponse(ctx, s, backend.Status.PodIP) != nil || s.probe(ctx, s.Namespace, s.WrongPod, control.Status.PodIP, s.ControlPort, "/health/live", true) != nil {
		t.Fatal("widening observation or control failed")
	}
}
