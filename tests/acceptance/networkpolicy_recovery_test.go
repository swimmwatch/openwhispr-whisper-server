package acceptance

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The parent must arrange an independent recovery invocation if the mutation
// process dies. A testing.Cleanup callback cannot run after process death.
type proveRecoveryRecord struct {
	Selection  pairingSelection  `json:"selection"`
	PolicyName string            `json:"policy_name"`
	PolicyUID  string            `json:"policy_uid"`
	Original   map[string]string `json:"original_selector"`
	Changed    map[string]string `json:"changed_selector"`
	Markers    proveMarkers      `json:"markers"`
}

func (r proveRecoveryRecord) validate() error {
	if r.Selection.validate() != nil || r.PolicyUID == "" {
		return errors.New("invalid recovery identity")
	}
	prefix, ordinal, ok := strings.Cut(r.Selection.BackendPod, "-backend-")
	n, err := strconv.Atoi(ordinal)
	if !ok || prefix == "" || err != nil || n < 0 || strconv.Itoa(n) != ordinal || r.PolicyName != prefix+"-pair-"+ordinal {
		return errors.New("invalid recovery policy")
	}
	if r.Original[proveOrdinalKey] != prefix+"-api-"+ordinal {
		return errors.New("invalid recovery original selector")
	}
	wrong := strings.TrimPrefix(r.Changed[proveOrdinalKey], prefix+"-api-")
	n, err = strconv.Atoi(wrong)
	if err != nil || n < 0 || strconv.Itoa(n) != wrong || r.Changed[proveOrdinalKey] != prefix+"-api-"+wrong || wrong == ordinal {
		return errors.New("invalid recovery changed selector")
	}
	expected := proveCopy(r.Original)
	expected[proveOrdinalKey] = r.Changed[proveOrdinalKey]
	if !reflect.DeepEqual(expected, r.Changed) {
		return errors.New("invalid recovery selector change")
	}
	for _, marker := range []struct{ token, annotation, prefix string }{
		{r.Markers.OwnerToken, r.Markers.OwnerAnnotation, "owner-"},
		{r.Markers.FenceToken, r.Markers.FenceAnnotation, "fence-"},
	} {
		raw, err := hex.DecodeString(marker.token)
		if err != nil || len(raw) != 16 || marker.token != hex.EncodeToString(raw) || marker.annotation != "acceptance.whisper-server.io/"+marker.prefix+marker.token {
			return errors.New("invalid recovery marker")
		}
	}
	if r.Markers.OwnerToken == r.Markers.FenceToken {
		return errors.New("recovery markers must be independent")
	}
	return nil
}

func provePrivateDir(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("absolute private recovery record path required")
	}
	st, err := os.Stat(filepath.Dir(path))
	if err != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return errors.New("private recovery directory required")
	}
	return nil
}

// Create exclusively and persist before mutation. Never overwrite a previous
// unresolved recovery record. The caller retains it even after clean recovery.
func proveWriteRecovery(path string, r proveRecoveryRecord) error {
	if provePrivateDir(path) != nil || r.validate() != nil {
		return errors.New("invalid private recovery record")
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > 65536 {
		return errors.New("cannot encode private recovery record")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private recovery record")
	}
	st, statErr := f.Stat()
	if statErr != nil || st.Mode().Perm() != 0600 {
		f.Close()
		return errors.New("private recovery record permissions invalid")
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.New("cannot persist private recovery record")
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot persist private recovery directory")
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot persist private recovery directory")
	}
	return nil
}
func proveReadRecovery(path string) (proveRecoveryRecord, error) {
	var r proveRecoveryRecord
	if provePrivateDir(path) != nil {
		return r, errors.New("invalid private recovery record")
	}
	f, err := os.Open(path)
	if err != nil {
		return r, errors.New("private recovery record unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return r, errors.New("private recovery record permissions invalid")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return r, errors.New("private recovery record unavailable")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.validate() != nil {
		return r, errors.New("invalid private recovery record")
	}
	return r, nil
}

// Invoke separately from a parent EXIT trap after the mutation process exits.
// Recovery leaves the private record in place and does not establish final
// network evidence. A killed parent needs an independent supervisor/operator.
func TestP08NetworkPolicyRecovery(t *testing.T) {
	if os.Getenv("P08_NETWORKPOLICY_RECOVERY") != "1" {
		t.Skip("requires parent-owned recovery invocation")
	}
	r, err := proveReadRecovery(os.Getenv("P08_NETWORKPOLICY_RECOVERY_RECORD"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	uid, err := r.Selection.command(ctx, r.Selection.Namespace, "get", "namespace", r.Selection.Namespace, "-o", "jsonpath={.metadata.uid}")
	if err != nil || string(uid) != r.Selection.NamespaceUID {
		t.Fatal("recovery namespace identity mismatch; manual recovery required")
	}
	store := proveKubeStore{r.Selection, r.PolicyName}
	p, err := store.read(ctx)
	if err != nil || p.Metadata.UID != r.PolicyUID || p.Metadata.Labels["app.kubernetes.io/instance"] != r.Selection.Release || p.Metadata.Annotations["meta.helm.sh/release-name"] != r.Selection.Release || p.Metadata.Annotations["meta.helm.sh/release-namespace"] != r.Selection.Namespace {
		t.Fatal("recovery policy identity unavailable; manual recovery required")
	}
	if err := proveRestore(ctx, store, r.PolicyUID, r.Original, r.Changed, r.Markers); err != nil {
		t.Fatal(err)
	}
}
