package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestP08SamplingOwnerBusyAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, status            string
		changed                 string
		samplingOK, completedOK bool
	}{
		{name: "ready", status: "ready", samplingOK: true, completedOK: true},
		{name: "busy", status: "busy", samplingOK: true},
		{name: "starting", status: "starting"}, {name: "recovering", status: "recovering"}, {name: "stopping", status: "stopping"}, {name: "failed", status: "failed"},
		{name: "uid changed", status: "busy", changed: "uid"}, {name: "generation changed", status: "busy", changed: "generation"},
		{name: "fingerprint changed", status: "busy", changed: "fingerprint"}, {name: "cuda absent", status: "busy", changed: "cuda"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "private", Namespace: "test", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "a", APIContainer: "api", BackendPod: "backend-0", BackendUID: "b", BackendContainer: "backend"}
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				cmd := strings.Join(args, " ")
				switch {
				case strings.Contains(cmd, "get namespace"):
					return []byte("ns"), nil
				case strings.Contains(cmd, "get pod"):
					role, uid := "api", "a"
					if strings.Contains(cmd, "pod backend-0") {
						role, uid = "backend", "b"
					}
					if tc.changed == "uid" {
						uid = "changed"
					}
					return []byte(fmt.Sprintf(`{"metadata":{"uid":%q,"labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":%q}},"status":{"containerStatuses":[{"name":%q,"containerID":%q,"restartCount":0,"state":{"running":{}}}]}}`, uid, role, role, role)), nil
				case strings.Contains(cmd, "p08-identity"):
					if strings.HasSuffix(cmd, "backend") {
						return []byte("20 42 30"), nil
					}
					return []byte("10 0 0"), nil
				case strings.Contains(cmd, "curl"):
					generation, fingerprint, cuda := "g", "f", true
					if tc.changed == "generation" {
						generation = "new"
					}
					if tc.changed == "fingerprint" {
						fingerprint = "new"
					}
					if tc.changed == "cuda" {
						cuda = false
					}
					return []byte(fmt.Sprintf(`{"status":%q,"model":"base","protocol_version":"1","configuration_fingerprint":%q,"process_generation":%q,"cuda_ready":%t}`+"\n%d", tc.status, fingerprint, generation, cuda, http.StatusOK)), nil
				default:
					return nil, errors.New("unexpected")
				}
			}
			expected := p08RepeatedIdentity{containers: [2]p08ContainerIdentity{{ID: "api"}, {ID: "backend"}}, apiStart: 10, backendStart: 20, nativePID: 42, nativeStart: 30, generation: "g", fingerprint: "f"}
			_, err := p08SamplingOwner(t.Context(), target, "base", "8081", expected)
			if (err == nil) != tc.samplingOK {
				t.Errorf("sampling result unexpected: %v", err)
			}
			_, err = p08RepeatedOwner(t.Context(), target, "base", "8081")
			if (err == nil) != tc.completedOK {
				t.Errorf("completed result unexpected: %v", err)
			}
		})
	}
}
