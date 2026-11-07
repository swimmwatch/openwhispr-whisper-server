package acceptance

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// This entry observes one caller-owned stage only. It never installs, uninstalls,
// initiates source attempts or inference, and does not certify offline behavior.
type kubernetesCacheEntry struct {
	Stage        string `json:"stage"`
	Kubeconfig   string `json:"kubeconfig"`
	Context      string `json:"context"`
	Namespace    string `json:"namespace"`
	NamespaceUID string `json:"namespace_uid"`
	Release      string `json:"release"`
	BackendPod   string `json:"backend_pod"`
	BackendUID   string `json:"backend_uid"`
	Container    string `json:"container"`
	Claim        string `json:"claim"`
	ClaimUID     string `json:"claim_uid"`
	ArtifactPath string `json:"artifact_path"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

func decodeKubernetesCacheEntry(r io.Reader) (kubernetesCacheEntry, error) {
	var e kubernetesCacheEntry
	data, err := io.ReadAll(io.LimitReader(r, 65537))
	if err != nil || len(data) > 65536 {
		return e, errors.New("private cache selection unavailable or too large")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	opening, err := keys.Token()
	if err != nil || opening != json.Delim('{') {
		return e, errors.New("invalid private cache selection")
	}
	seen := map[string]bool{}
	for keys.More() {
		token, err := keys.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return e, errors.New("invalid private cache selection")
		}
		seen[key] = true
		var value json.RawMessage
		if keys.Decode(&value) != nil {
			return e, errors.New("invalid private cache selection")
		}
	}
	if _, err := keys.Token(); err != nil {
		return e, errors.New("invalid private cache selection")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&e) != nil || decoder.Decode(new(any)) != io.EOF || (e.Stage != "before" && e.Stage != "retained" && e.Stage != "after") {
		return e, errors.New("invalid private cache selection")
	}
	s := e.selection()
	if e.Stage == "retained" {
		// No backend exists during uninstall; artifact expectation is still explicit.
		s.BackendPod, s.BackendUID, s.Container = "uninstalled", "uninstalled", "uninstalled"
	}
	if s.validate() != nil {
		return e, errors.New("incomplete private cache selection")
	}
	return e, nil
}

func (e kubernetesCacheEntry) selection() kubernetesCacheSelection {
	return kubernetesCacheSelection{Kubeconfig: e.Kubeconfig, Context: e.Context, Namespace: e.Namespace, NamespaceUID: e.NamespaceUID, Release: e.Release, BackendPod: e.BackendPod, BackendUID: e.BackendUID, Container: e.Container, Claim: e.Claim, ClaimUID: e.ClaimUID, ArtifactPath: e.ArtifactPath, SHA256: e.SHA256, Size: e.Size}
}

func TestP08CacheObservation(t *testing.T) {
	if os.Getenv("P08_CACHE_OBSERVATION") != "1" {
		t.Skip("requires parent-owned cache observation stage")
	}
	file := os.Getenv("P08_CACHE_SELECTION")
	if !filepath.IsAbs(file) {
		t.Fatal("absolute private cache selection path required")
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal("private cache selection unavailable")
	}
	defer f.Close()
	e, err := decodeKubernetesCacheEntry(f)
	if err != nil {
		t.Fatal(err)
	}
	s := e.selection()
	if e.Stage == "retained" {
		err = observeKubernetesRetainedClaim(t.Context(), s)
	} else {
		err = observeKubernetesCache(t.Context(), s)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Log("selected cache stage observed; offline reuse, public inference and P08 remain unproven")
}
