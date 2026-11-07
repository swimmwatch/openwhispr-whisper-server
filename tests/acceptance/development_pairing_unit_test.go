package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestPairingRejectsPodReplacement(t *testing.T) {
	s := pairingSelection{Namespace: "target", OtherNamespace: "other", BackendPod: "backend", BackendPodUID: "backend-uid", PairedPod: "paired", PairedPodUID: "paired-uid", WrongPod: "wrong", WrongPodUID: "wrong-uid", OtherPod: "other", OtherPodUID: "other-uid", ControlPod: "control", ControlPodUID: "control-uid", BackendPort: 8080, ControlPort: 8081, ProbeContainer: "curl"}
	for _, replaced := range append([]string{""}, "backend", "paired", "wrong", "other", "control") {
		t.Run("replacement-"+replaced, func(t *testing.T) {
			executed := false
			command := func(_ context.Context, ns string, args ...string) ([]byte, error) {
				if args[0] == "exec" {
					executed = true
					return []byte("0|" + strconv.Itoa(http.StatusOK) + "|10.1.2.3|0.01"), nil
				}
				var p pairingPod
				for _, selected := range s.selectedPods() {
					if selected.namespace == ns && selected.name == args[2] {
						p.Metadata.UID = selected.uid
					}
				}
				if executed && args[2] == replaced {
					p.Metadata.UID = "replacement-uid"
				}
				p.Status.PodIP = "10.1.2.3"
				p.Status.Conditions = append(p.Status.Conditions, struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				}{"Ready", "True"})
				return json.Marshal(p)
			}
			err := s.probeWithCommand(context.Background(), command, s.Namespace, s.PairedPod, "10.1.2.3", s.BackendPort, "/health", true)
			if !executed || (err == nil) != (replaced == "") {
				t.Fatalf("replacement=%q executed=%t error=%v", replaced, executed, err)
			}
		})
	}
}

func TestPairingRejectsReplacementAfterIPObservation(t *testing.T) {
	s := pairingSelection{Namespace: "target", BackendPod: "backend", BackendPodUID: "original"}
	reads := 0
	command := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		reads++
		var p pairingPod
		p.Metadata.UID = "original"
		if reads == 2 {
			p.Metadata.UID = "replacement"
		}
		p.Status.PodIP = "10.1.2.3"
		p.Status.Conditions = append(p.Status.Conditions, struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		}{"Ready", "True"})
		return json.Marshal(p)
	}
	if _, err := s.podWithCommand(context.Background(), command, "target", "backend"); err == nil {
		t.Fatal("accepted replacement after collecting IP")
	}
}

func TestPairingProbeOutcomes(t *testing.T) {
	cases := []struct {
		name, result  string
		allowed, pass bool
	}{
		{"allowed", "0|" + strconv.Itoa(http.StatusOK) + "|10.1.2.3|0.012", true, true},
		{"denied timeout", "28|000||2.001", false, true},
		{"unexpected allowed", "0|" + strconv.Itoa(http.StatusOK) + "|10.1.2.3|0.012", false, false},
		{"refused", "7|000||0.001", false, false},
		{"dns failure", "6|000||0.001", false, false},
		{"connected but stalled", "28|000|10.1.2.3|3.000", false, false},
		{"immediate timeout", "28|000||0.001", false, false},
		{"HTTP failure", "0|" + strconv.Itoa(http.StatusServiceUnavailable) + "|10.1.2.3|0.012", true, false},
		{"missing probe tool", "127|||", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if (pairingResult([]byte(tc.result), tc.allowed) == nil) != tc.pass {
				t.Fatal("unexpected classification")
			}
		})
	}
}

func TestPairingIdentity(t *testing.T) {
	pod := func(role, ordinal string) pairingPod {
		var p pairingPod
		p.Metadata.Labels = map[string]string{"app.kubernetes.io/name": "whisper", "app.kubernetes.io/instance": "release", "app.kubernetes.io/component": role, "statefulset.kubernetes.io/pod-name": ordinal}
		return p
	}
	backend := pod("backend", "release-whisper-backend-0")
	paired := pod("api", "release-whisper-api-0")
	wrong := pod("api", "release-whisper-api-1")
	other := pod("api", "release-whisper-api-0")
	if err := pairingLabels(backend, paired, wrong, other, "release", "release-whisper-backend-0"); err != nil {
		t.Fatal(err)
	}
	if pairingLabels(backend, paired, paired, other, "release", "release-whisper-backend-0") == nil {
		t.Fatal("same ordinal accepted as wrong ordinal")
	}
	other.Metadata.Labels["app.kubernetes.io/instance"] = "different"
	if pairingLabels(backend, paired, wrong, other, "release", "release-whisper-backend-0") == nil {
		t.Fatal("different release accepted")
	}
}
