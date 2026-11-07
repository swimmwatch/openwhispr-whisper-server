package acceptance

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestActiveCancelRequiresCorrelatedRecovery(t *testing.T) {
	for _, fault := range []string{"none", "transient-health", "unavailable-health", "missing-dispatch", "changed-busy", "noncuda-busy", "transport-only", "same-generation", "identity", "readiness", "scratch", "speech", "changed-after-speech"} {
		t.Run(fault, func(t *testing.T) {
			count := 0
			health := func(generation, state string) *DockerPrivateHealth {
				return &DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: state, CUDAReady: true, Model: "base", ProcessGeneration: generation, ProtocolVersion: "1", ConfigurationFingerprint: "selected"}
			}
			w := activeCancelWork{
				model: "base", id: "active-unique", operation: 80 * time.Millisecond, cleanup: time.Second, provisioning: 500 * time.Millisecond,
				verify: func(context.Context) error {
					if fault == "identity" {
						return errors.New("unavailable")
					}
					return nil
				},
				observe: func(context.Context) (*DockerPrivateHealth, error) {
					count++
					if count == 1 {
						return health("old", "ready"), nil
					}
					if count == 2 {
						h := health("old", "busy")
						if fault == "changed-busy" {
							h.ProcessGeneration = "other"
						}
						if fault == "noncuda-busy" {
							h.CUDAReady = false
						}
						return h, nil
					}
					if (fault == "transient-health" && count == 3) || fault == "unavailable-health" {
						return nil, errors.New("temporarily unavailable")
					}
					if fault == "same-generation" {
						return health("old", "ready"), nil
					}
					if fault == "changed-after-speech" && count > 3 {
						return health("third", "ready"), nil
					}
					return health("new", "ready"), nil
				},
				logs: func(context.Context) ([]byte, error) {
					if fault == "missing-dispatch" {
						return []byte(`{"event":"queue_dispatched","request_id":"other"}`), nil
					}
					return []byte(`{"event":"queue_dispatched","request_id":"active-unique"}`), nil
				},
				released: func(context.Context) error {
					if fault == "scratch" {
						return errors.New("unavailable")
					}
					return nil
				},
				ready: func(context.Context) error {
					if fault == "readiness" {
						return errors.New("unavailable")
					}
					return nil
				},
				start: func(ctx context.Context, _ string) <-chan asyncPublicReply {
					ch := make(chan asyncPublicReply, 1)
					go func() {
						<-ctx.Done()
						if fault == "transport-only" {
							ch <- asyncPublicReply{err: errors.New("closed")}
						} else {
							ch <- asyncPublicReply{err: ctx.Err()}
						}
					}()
					return ch
				},
				speech: func(context.Context) (PublicReply, error) {
					if fault == "speech" {
						return PublicReply{Status: http.StatusOK, Text: "unidentified"}, nil
					}
					return PublicReply{Status: http.StatusOK, Text: "my fellow Americans ask what your country"}, nil
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := activeCancelCheck(ctx, w)
			if (err == nil) != (fault == "none" || fault == "transient-health") {
				t.Fatalf("incorrect verdict for %s: %v", fault, err)
			}
		})
	}
}
