package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBackendLossPrivacyRejectsUnknownWindows(t *testing.T) {
	for _, fault := range []string{"none", "leak", "too-large", "partial", "missing-anchor", "reordered", "reader", "identity"} {
		t.Run(fault, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "selected", Namespace: "selected", NamespaceUID: "ns", Release: "release", APIPod: "api-0", APIUID: "api-id", APIContainer: "api", BackendPod: "backend-0", BackendUID: "backend-id", BackendContainer: "backend"}
			expected := [2]p08ContainerIdentity{{ID: "api-container"}, {ID: "backend-container"}}
			reads := 0
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				command := strings.Join(args, " ")
				if strings.Contains(command, "get namespace") {
					return []byte("ns"), nil
				}
				if strings.Contains(command, "get pod") {
					role, uid, cid := "api", "api-id", "api-container"
					if strings.Contains(command, "backend-0") {
						role, uid, cid = "backend", "backend-id", "backend-container"
					}
					if fault == "identity" && reads >= 2 && role == "api" {
						cid = "changed"
					}
					body, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": uid, "labels": map[string]string{"app.kubernetes.io/instance": "release", "app.kubernetes.io/component": role}}, "status": map[string]any{"containerStatuses": []map[string]any{{"name": role, "containerID": cid, "restartCount": 0, "state": map[string]any{"running": map[string]any{}}}}}})
					return body, nil
				}
				if !strings.Contains(command, "--limit-bytes=1048577") {
					return nil, errors.New("missing log limit")
				}
				reads++
				if fault == "reader" {
					return nil, errors.New("private error")
				}
				data := "backend log\n"
				if strings.Contains(command, "logs api-0") {
					a := `{"event":"queue_dispatched","request_id":"active"}` + "\n"
					b := `{"event":"queue_admitted","request_id":"waiting"}` + "\n"
					data = a + b
					if fault == "missing-anchor" {
						data = a
					}
					if fault == "reordered" {
						data = b + a
					}
				}
				if fault == "leak" {
					data += "SECRET\n"
				}
				if fault == "too-large" {
					data += strings.Repeat("x", 1<<20) + "\n"
				}
				if fault == "partial" {
					data = strings.TrimSuffix(data, "\n")
				}
				return []byte(data), nil
			}
			err := p08BackendLossPrivacy(t.Context(), target, expected, "SECRET", "active", "waiting", false)
			if (err == nil) != (fault == "none") {
				t.Fatalf("incorrect privacy result: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatal("private content leaked")
			}
		})
	}
}

func TestBackendLossResultsAreExplicit(t *testing.T) {
	cases := []struct {
		name   string
		result asyncPublicReply
		pass   bool
	}{
		{"speech", asyncPublicReply{reply: PublicReply{Status: http.StatusOK, Text: "my fellow Americans ask what your country"}}, true},
		{"unavailable", asyncPublicReply{reply: PublicReply{Status: http.StatusServiceUnavailable, ErrorCode: "backend_unavailable"}}, true},
		{"not-ready", asyncPublicReply{reply: PublicReply{Status: http.StatusServiceUnavailable, ErrorCode: "not_ready"}}, true},
		{"timeout", asyncPublicReply{reply: PublicReply{Status: http.StatusRequestTimeout, ErrorCode: "operation_timeout"}}, true},
		{"lost", asyncPublicReply{err: &publicRequestError{io.EOF}}, true},
		{"unknown-error", asyncPublicReply{err: errors.New("unknown")}, false},
		{"conflicting", asyncPublicReply{reply: PublicReply{Status: http.StatusOK}, err: &publicRequestError{io.EOF}}, false},
		{"bad-success", asyncPublicReply{reply: PublicReply{Status: http.StatusOK, Text: "unknown"}}, false},
		{"wrong-status", asyncPublicReply{reply: PublicReply{Status: http.StatusOK, ErrorCode: "backend_unavailable"}}, false},
		{"conflicting-text", asyncPublicReply{reply: PublicReply{Status: http.StatusServiceUnavailable, ErrorCode: "backend_unavailable", Text: "speech"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, active := range []bool{true, false} {
				_, e := backendLossResult(tc.result, active)
				if (e == nil) != tc.pass {
					t.Fatalf("unexpected classification: %v", e)
				}
			}
		})
	}
}

func TestBackendLossDeleteIsBoundToBackendUID(t *testing.T) {
	for _, fault := range []string{"none", "uid", "container", "role", "namespace", "conflict"} {
		t.Run(fault, func(t *testing.T) {
			target := gracefulDeleteTarget{target: developmentQueueTarget{Kubeconfig: "/private/kubeconfig", Context: "selected", Namespace: "selected", NamespaceUID: "ns", Release: "release", APIPod: "api-0", BackendPod: "backend-0"}, apiUID: "api-id", backendUID: "backend-id", role: "backend", container: "backend", containerID: "container-id", imageID: "image-id"}
			deletes := 0
			target.run = func(_ context.Context, args []string, input []byte) ([]byte, error) {
				if args[7] == "delete" {
					deletes++
					if strings.Join(args[7:], " ") != "delete --raw /api/v1/namespaces/selected/pods/backend-0 -f -" {
						t.Fatal("wrong Pod deletion route")
					}
					var options struct {
						Preconditions struct {
							UID string `json:"uid"`
						} `json:"preconditions"`
					}
					if json.Unmarshal(input, &options) != nil || options.Preconditions.UID != "backend-id" {
						t.Fatal("missing backend UID precondition")
					}
					if fault == "conflict" {
						return nil, errors.New("conflict")
					}
					return []byte(`{}`), nil
				}
				if args[8] == "namespace" {
					if fault == "namespace" {
						return []byte("other"), nil
					}
					return []byte("ns"), nil
				}
				name := args[9]
				uid, role := "api-id", "api"
				if name == "backend-0" {
					uid, role = "backend-id", "backend"
				}
				if fault == "uid" && role == "backend" {
					uid = "other"
				}
				if fault == "role" && role == "backend" {
					role = "api"
				}
				cid := "container-id"
				if fault == "container" {
					cid = "other"
				}
				body, _ := json.Marshal(map[string]any{"metadata": map[string]any{"uid": uid, "labels": map[string]string{"app.kubernetes.io/instance": "release", "app.kubernetes.io/component": role}}, "status": map[string]any{"conditions": []map[string]string{{"type": "Ready", "status": "True"}}, "containerStatuses": []map[string]any{{"name": "backend", "ready": true, "containerID": cid, "imageID": "image-id"}}}})
				return body, nil
			}
			e := target.delete(context.Background())
			if (e == nil) != (fault == "none") {
				t.Fatalf("wrong verdict: %v", e)
			}
			if (deletes == 1) != (fault == "none" || fault == "conflict") {
				t.Fatal("invalid target reached deletion")
			}
		})
	}
}

func TestBackendLossQueueAndPrivacyBarriers(t *testing.T) {
	for _, fault := range []string{"none", "admission", "privacy", "recovery", "response"} {
		t.Run(fault, func(t *testing.T) {
			active, waiting := make(chan asyncPublicReply, 1), make(chan asyncPublicReply, 1)
			deleted := false
			n := 0
			health := func(state string) *DockerPrivateHealth {
				return &DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: state, CUDAReady: true, Model: "base", ProcessGeneration: "old", ProtocolVersion: "1", ConfigurationFingerprint: "x"}
			}
			w := gracefulWork{queue: queueWorkload{model: "base", logs: func(context.Context) ([]byte, error) {
				s := `{"event":"queue_dispatched","request_id":"a"}` + "\n"
				if fault != "admission" {
					s += `{"event":"queue_admitted","request_id":"w"}` + "\n"
				}
				return []byte(s), nil
			}}, activeID: "a", waitingID: "w", barrier: 20 * time.Millisecond, outcome: time.Second, recovery: time.Second,
				observe: func(context.Context) (*DockerPrivateHealth, error) {
					n++
					if n == 1 {
						return health("ready"), nil
					}
					return health("busy"), nil
				}, verify: func(context.Context) error { return nil },
				start: func(ctx context.Context, _ string, first bool) <-chan asyncPublicReply {
					ch := waiting
					if first {
						ch = active
					}
					go func() {
						<-ctx.Done()
						select {
						case ch <- asyncPublicReply{err: ctx.Err()}:
						default:
						}
					}()
					return ch
				},
				delete: func(context.Context) error {
					log := []byte("clean")
					if fault == "privacy" {
						log = []byte("secret-marker")
					}
					if checkResidentLogPrivacy(log, "secret-marker") != nil {
						return errors.New("privacy leak")
					}
					deleted = true
					r := asyncPublicReply{reply: PublicReply{Status: http.StatusServiceUnavailable, ErrorCode: "backend_unavailable"}}
					if fault == "response" {
						r.reply.Status = http.StatusOK
					}
					active <- r
					waiting <- r
					return nil
				}, classify: backendLossResult,
				recover: func(context.Context, *DockerPrivateHealth) error {
					if fault == "recovery" {
						return errors.New("unavailable")
					}
					return nil
				}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			e := gracefulWorkCheck(ctx, w)
			if (e == nil) != (fault == "none") {
				t.Fatalf("incorrect verdict: %v", e)
			}
			if (fault == "privacy" || fault == "admission") && deleted {
				t.Fatal("unsafe deletion")
			}
		})
	}
}
