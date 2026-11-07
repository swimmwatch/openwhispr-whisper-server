package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// P08_DEVELOPMENT_PAIRING=1 and P08_PAIRING_TARGET select a private JSON file.
// The parent supplies ready, non-host-networked curl probe Pods, a listening
// API liveness control Pod, and the release's ready backend. This test creates nothing.
// Probe labels must emulate the matching API ordinal, a different API ordinal,
// and the matching ordinal in the separately selected namespace, respectively.
type pairingSelection struct {
	Kubeconfig        string `json:"kubeconfig"`
	Context           string `json:"context"`
	Namespace         string `json:"namespace"`
	NamespaceUID      string `json:"namespace_uid"`
	OtherNamespace    string `json:"other_namespace"`
	OtherNamespaceUID string `json:"other_namespace_uid"`
	Release           string `json:"release"`
	BackendPod        string `json:"backend_pod"`
	BackendPodUID     string `json:"backend_pod_uid"`
	PairedPod         string `json:"paired_pod"`
	PairedPodUID      string `json:"paired_pod_uid"`
	WrongPod          string `json:"wrong_pod"`
	WrongPodUID       string `json:"wrong_pod_uid"`
	OtherPod          string `json:"other_pod"`
	OtherPodUID       string `json:"other_pod_uid"`
	ControlPod        string `json:"control_pod"`
	ControlPodUID     string `json:"control_pod_uid"`
	ProbeContainer    string `json:"probe_container"`
	BackendPort       int    `json:"backend_port"`
	ControlPort       int    `json:"control_port"`
}

type pairingPod struct {
	Metadata struct {
		UID    string            `json:"uid"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		HostNetwork bool `json:"hostNetwork"`
	} `json:"spec"`
	Status struct {
		PodIP      string `json:"podIP"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

func (s pairingSelection) validate() error {
	if !filepath.IsAbs(s.Kubeconfig) || s.Context == "" || s.Namespace == "" || s.NamespaceUID == "" || s.OtherNamespace == "" || s.OtherNamespaceUID == "" || s.Namespace == s.OtherNamespace || s.NamespaceUID == s.OtherNamespaceUID || s.Release == "" || s.BackendPod == "" || s.BackendPodUID == "" || s.PairedPod == "" || s.PairedPodUID == "" || s.WrongPod == "" || s.WrongPodUID == "" || s.OtherPod == "" || s.OtherPodUID == "" || s.ControlPod == "" || s.ControlPodUID == "" || s.ProbeContainer == "" || s.BackendPort < 1 || s.BackendPort > 65535 || s.ControlPort < 1 || s.ControlPort > 65535 || s.PairedPod == s.WrongPod || s.BackendPod == s.ControlPod {
		return errors.New("complete distinct explicit pairing selection is required")
	}
	return nil
}

func (s pairingSelection) command(ctx context.Context, namespace string, args ...string) ([]byte, error) {
	prefix := []string{"--kubeconfig", s.Kubeconfig, "--context", s.Context, "--namespace", namespace, "--request-timeout=5s"}
	return runKubernetesCommand(ctx, "kubectl", 8*time.Second, 1<<20, nil, append(prefix, args...)...)
}

type pairingCommand func(context.Context, string, ...string) ([]byte, error)

type selectedPairingPod struct{ namespace, name, uid string }

func (s pairingSelection) selectedPods() []selectedPairingPod {
	return []selectedPairingPod{
		{s.Namespace, s.BackendPod, s.BackendPodUID},
		{s.Namespace, s.PairedPod, s.PairedPodUID},
		{s.Namespace, s.WrongPod, s.WrongPodUID},
		{s.OtherNamespace, s.OtherPod, s.OtherPodUID},
		{s.Namespace, s.ControlPod, s.ControlPodUID},
	}
}

func (s pairingSelection) readPod(ctx context.Context, command pairingCommand, selected selectedPairingPod) (pairingPod, error) {
	var p pairingPod
	data, err := command(ctx, selected.namespace, "get", "pod", selected.name, "-o", "json")
	if err != nil || json.Unmarshal(data, &p) != nil || selected.uid == "" || p.Metadata.UID != selected.uid || p.Spec.HostNetwork || net.ParseIP(p.Status.PodIP) == nil {
		return p, errors.New("pairing Pod identity or network mismatch")
	}
	for _, c := range p.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			return p, nil
		}
	}
	return p, errors.New("pairing Pod is not ready")
}

func (s pairingSelection) podWithCommand(ctx context.Context, command pairingCommand, ns, name string) (pairingPod, error) {
	var selected selectedPairingPod
	matches := 0
	for _, candidate := range s.selectedPods() {
		if candidate.namespace == ns && candidate.name == name {
			selected = candidate
			matches++
		}
	}
	if matches != 1 {
		return pairingPod{}, errors.New("pairing Pod is not uniquely selected")
	}
	first, err := s.readPod(ctx, command, selected)
	if err != nil {
		return first, err
	}
	second, err := s.readPod(ctx, command, selected)
	if err != nil || first.Status.PodIP != second.Status.PodIP {
		return second, errors.New("pairing Pod changed after IP observation")
	}
	return second, nil
}

func (s pairingSelection) pod(ctx context.Context, ns, name string) (pairingPod, error) {
	return s.podWithCommand(ctx, s.command, ns, name)
}

func pairingLabels(backend, paired, wrong, other pairingPod, release, backendName string) error {
	b, p, w, o := backend.Metadata.Labels, paired.Metadata.Labels, wrong.Metadata.Labels, other.Metadata.Labels
	keys := []string{"app.kubernetes.io/name", "app.kubernetes.io/instance", "app.kubernetes.io/component", "statefulset.kubernetes.io/pod-name"}
	if b[keys[0]] == "" || b[keys[1]] != release || b[keys[2]] != "backend" || b[keys[3]] != backendName {
		return errors.New("backend release identity mismatch")
	}
	for _, labels := range []map[string]string{p, w, o} {
		if labels[keys[0]] != b[keys[0]] || labels[keys[1]] != release || labels[keys[2]] != "api" {
			return errors.New("probe release or role identity mismatch")
		}
	}
	prefix, ordinal, ok := strings.Cut(backendName, "-backend-")
	if !ok || prefix == "" || ordinal == "" {
		return errors.New("backend ordinal identity unavailable")
	}
	if n, err := strconv.Atoi(ordinal); err != nil || n < 0 || strconv.Itoa(n) != ordinal {
		return errors.New("invalid backend ordinal")
	}
	matched := prefix + "-api-" + ordinal
	if p[keys[3]] != matched || o[keys[3]] != matched {
		return errors.New("matching probe ordinal identity mismatch")
	}
	wrongOrdinal := strings.TrimPrefix(w[keys[3]], prefix+"-api-")
	n, err := strconv.Atoi(wrongOrdinal)
	if err != nil || n < 0 || strconv.Itoa(n) != wrongOrdinal || w[keys[3]] != prefix+"-api-"+wrongOrdinal || wrongOrdinal == ordinal {
		return errors.New("wrong probe must use another ordinal")
	}
	return nil
}

// Curl output includes exit status, HTTP status, remote IP and elapsed seconds.
// A timeout is accepted only before a TCP connection was established. DNS is
// never involved: destination addresses come from observed Pod status.
func pairingResult(data []byte, allowed bool) error {
	parts := strings.Split(strings.TrimSpace(string(data)), "|")
	if len(parts) != 4 {
		return errors.New("invalid pairing probe result")
	}
	seconds, err := strconv.ParseFloat(parts[3], 64)
	if err != nil {
		return errors.New("invalid pairing probe duration")
	}
	if allowed && parts[0] == "0" && parts[1] == strconv.Itoa(http.StatusOK) && net.ParseIP(parts[2]) != nil {
		return nil
	}
	if !allowed && parts[0] == "28" && parts[1] == "000" && parts[2] == "" && seconds >= 1.5 && seconds <= 4 {
		return nil
	}
	return errors.New("pairing probe did not show the expected connection outcome")
}

func (s pairingSelection) probe(ctx context.Context, namespace, pod, ip string, port int, path string, allowed bool) error {
	return s.probeWithCommand(ctx, s.command, namespace, pod, ip, port, path, allowed)
}

func (s pairingSelection) probeWithCommand(ctx context.Context, command pairingCommand, namespace, pod, ip string, port int, path string, allowed bool) error {
	if !((namespace == s.Namespace && (pod == s.PairedPod || pod == s.WrongPod)) || (namespace == s.OtherNamespace && pod == s.OtherPod)) {
		return errors.New("probe Pod is not selected")
	}
	destination := s.BackendPod
	if path == "/health/live" && port == s.ControlPort {
		destination = s.ControlPod
	} else if path != "/health" || port != s.BackendPort {
		return errors.New("probe destination is not selected")
	}
	check := func() error {
		for _, selected := range s.selectedPods() {
			observed, err := s.podWithCommand(ctx, command, selected.namespace, selected.name)
			if err != nil || (selected.namespace == s.Namespace && selected.name == destination && observed.Status.PodIP != ip) {
				return errors.New("pairing Pod identity or destination IP changed")
			}
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	url := "http://" + net.JoinHostPort(ip, strconv.Itoa(port)) + path
	const script = `result=$(curl --silent --noproxy '*' --output /dev/null --connect-timeout 2 --max-time 3 --write-out '%{http_code}|%{remote_ip}|%{time_total}' "$1"); code=$?; printf '%s|%s' "$code" "$result"`
	out, execErr := command(ctx, namespace, "exec", pod, "-c", s.ProbeContainer, "--", "sh", "-c", script, "pairing-probe", url)
	if err := check(); err != nil {
		return err
	}
	if execErr != nil {
		return execErr
	}
	return pairingResult(out, allowed)
}

func TestDevelopmentPrivatePairing(t *testing.T) {
	if os.Getenv("P08_DEVELOPMENT_PAIRING") != "1" {
		t.Skip("requires parent-owned Kubernetes pairing probes")
	}
	path := os.Getenv("P08_PAIRING_TARGET")
	if !filepath.IsAbs(path) {
		t.Fatal("absolute private pairing selection path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("private pairing selection unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		t.Fatal("private pairing selection unavailable or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var s pairingSelection
	if decoder.Decode(&s) != nil || decoder.Decode(new(any)) != io.EOF || s.validate() != nil {
		t.Fatal("invalid private pairing selection")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	for _, selected := range []struct{ ns, uid string }{{s.Namespace, s.NamespaceUID}, {s.OtherNamespace, s.OtherNamespaceUID}} {
		uid, err := s.command(ctx, selected.ns, "get", "namespace", selected.ns, "-o", "jsonpath={.metadata.uid}")
		if err != nil || string(uid) != selected.uid {
			t.Fatal("selected namespace identity mismatch")
		}
	}
	backend, e1 := s.pod(ctx, s.Namespace, s.BackendPod)
	paired, e2 := s.pod(ctx, s.Namespace, s.PairedPod)
	wrong, e3 := s.pod(ctx, s.Namespace, s.WrongPod)
	other, e4 := s.pod(ctx, s.OtherNamespace, s.OtherPod)
	control, e5 := s.pod(ctx, s.Namespace, s.ControlPod)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || pairingLabels(backend, paired, wrong, other, s.Release, s.BackendPod) != nil {
		t.Fatal("pairing Pod identities or readiness do not match selection")
	}
	for _, source := range []struct {
		ns, pod string
		allowed bool
	}{{s.Namespace, s.PairedPod, true}, {s.Namespace, s.WrongPod, false}, {s.OtherNamespace, s.OtherPod, false}, {s.Namespace, s.PairedPod, true}} {
		if err := s.probe(ctx, source.ns, source.pod, control.Status.PodIP, s.ControlPort, "/health/live", true); err != nil {
			t.Fatal("probe control endpoint is not reachable")
		}
		if err := s.probe(ctx, source.ns, source.pod, backend.Status.PodIP, s.BackendPort, "/health", source.allowed); err != nil {
			t.Fatal(fmt.Sprintf("pairing outcome failed (expected allowed=%t)", source.allowed))
		}
	}
}
