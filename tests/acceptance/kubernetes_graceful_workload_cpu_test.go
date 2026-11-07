package acceptance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestP08APIScratchCleanAllowsOnlyEmptyOwnerLock(t *testing.T) {
	path := sha256.Sum256([]byte("/tmp/whisper-api/.owner.lock"))
	empty := sha256.Sum256(nil)
	lock := fmt.Sprintf("F %x metadata %x", path, empty)
	for _, tc := range []struct {
		snapshot p08ScratchSnapshot
		want     bool
	}{
		{p08ScratchSnapshot{valid: true}, true},
		{p08ScratchSnapshot{valid: true, files: []string{lock}}, true},
		{p08ScratchSnapshot{valid: true, files: []string{"F other metadata " + fmt.Sprintf("%x", empty)}}, false},
		{p08ScratchSnapshot{valid: true, files: []string{fmt.Sprintf("F %x metadata nonempty", path)}}, false},
		{p08ScratchSnapshot{valid: true, deleted: []string{"deleted"}}, false},
		{p08ScratchSnapshot{}, false},
	} {
		if p08APIScratchClean(tc.snapshot) != tc.want {
			t.Fatal("incorrect API scratch clean result")
		}
	}
}

func TestP08BaselineRejectsChangedOrUnknownGeneration(t *testing.T) {
	for _, mode := range []string{"stable", "changed", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			observe := func(context.Context) (*DockerPrivateHealth, error) {
				calls++
				generation := "one"
				if mode == "changed" && calls%2 == 0 {
					generation = "two"
				}
				if mode == "unknown" {
					generation = ""
				}
				return &DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: "ready", CUDAReady: true, Model: "base", ProcessGeneration: generation, ProtocolVersion: "1", ConfigurationFingerprint: "fingerprint"}, nil
			}
			sample := func(context.Context) ([2]p08ContainerIdentity, [2]p08ScratchSnapshot, error) {
				return [2]p08ContainerIdentity{}, [2]p08ScratchSnapshot{{valid: true}, {valid: true}}, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, _, _, err := p08BracketBaseline(ctx, "base", observe, sample)
			if (err == nil) != (mode == "stable") {
				t.Fatal("generation baseline result incorrect")
			}
		})
	}
}

func TestP08PairScratchReleasePreservesNonemptyBackendIdentity(t *testing.T) {
	before := [2]p08ScratchSnapshot{{valid: true}, {valid: true, files: []string{"first", "second"}}}
	for _, tc := range []struct {
		name  string
		after [2]p08ScratchSnapshot
		want  bool
	}{
		{"unchanged", before, true},
		{"same count changed identity", [2]p08ScratchSnapshot{{valid: true}, {valid: true, files: []string{"first", "different"}}}, false},
		{"deleted open", [2]p08ScratchSnapshot{{valid: true, deleted: []string{"deleted"}}, before[1]}, false},
		{"unknown", [2]p08ScratchSnapshot{{}, before[1]}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (p08ScratchPairReleased(before, tc.after) == nil) != tc.want {
				t.Fatal("incorrect scratch release result")
			}
		})
	}
}

func TestRecoveryDiagnosticDoesNotExposePrivateErrors(t *testing.T) {
	for _, err := range []error{errors.New("private marker"), recoveryFailure("private marker")} {
		if strings.Contains(recoveryDiagnostic(err).Error(), "private marker") {
			t.Fatal("private error leaked")
		}
	}
}

func TestGracefulWorkRequiresBarriersAndBoundedResults(t *testing.T) {
	for _, fault := range []string{"none", "lost", "missing-admission", "waiting-dispatched", "duplicate-active-dispatch", "duplicate-waiting-admission", "reordered-anchors", "oversized-logs", "busy-generation", "target", "delete", "active-error", "waiting-success", "conflicting-response", "recovery"} {
		t.Run(fault, func(t *testing.T) {
			active := make(chan asyncPublicReply, 1)
			waiting := make(chan asyncPublicReply, 1)
			deleted := false
			health := func(state, generation string) *DockerPrivateHealth {
				return &DockerPrivateHealth{HTTPStatus: http.StatusOK, Status: state, CUDAReady: true, Model: "base", ProcessGeneration: generation, ProtocolVersion: "1", ConfigurationFingerprint: "fingerprint"}
			}
			observations := 0
			w := gracefulWork{queue: queueWorkload{model: "base", logs: func(context.Context) ([]byte, error) {
				s := `{"event":"queue_dispatched","request_id":"active"}` + "\n"
				if fault != "missing-admission" {
					s += `{"event":"queue_admitted","request_id":"waiting"}` + "\n"
				}
				if fault == "waiting-dispatched" {
					s += `{"event":"queue_dispatched","request_id":"waiting"}` + "\n"
				}
				if fault == "duplicate-active-dispatch" {
					s += `{"event":"queue_dispatched","request_id":"active"}` + "\n"
				}
				if fault == "duplicate-waiting-admission" {
					s += `{"event":"queue_admitted","request_id":"waiting"}` + "\n"
				}
				if fault == "reordered-anchors" {
					s = `{"event":"queue_admitted","request_id":"waiting"}` + "\n" + `{"event":"queue_dispatched","request_id":"active"}` + "\n"
				}
				if fault == "oversized-logs" {
					s += strings.Repeat("x", 1<<20) + "\n"
				}
				return []byte(s), nil
			}}, activeID: "active", waitingID: "waiting", barrier: 25 * time.Millisecond, outcome: time.Second, recovery: time.Second,
				observe: func(context.Context) (*DockerPrivateHealth, error) {
					observations++
					if observations == 1 {
						return health("ready", "same"), nil
					}
					if fault == "busy-generation" {
						return health("busy", "other"), nil
					}
					return health("busy", "same"), nil
				},
				verify: func(context.Context) error {
					if fault == "target" {
						return errors.New("unknown")
					}
					return nil
				},
				start: func(ctx context.Context, _ string, isActive bool) <-chan asyncPublicReply {
					ch := waiting
					if isActive {
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
					deleted = true
					if fault == "delete" {
						return errors.New("conflict")
					}
					a := asyncPublicReply{reply: PublicReply{Status: http.StatusOK, Text: "my fellow Americans ask what your country"}}
					q := asyncPublicReply{reply: PublicReply{Status: http.StatusServiceUnavailable, ErrorCode: "shutting_down"}}
					if fault == "lost" {
						a = asyncPublicReply{err: &publicRequestError{io.EOF}}
					}
					if fault == "active-error" {
						a = asyncPublicReply{err: errors.New("unknown")}
					}
					if fault == "waiting-success" {
						q = asyncPublicReply{reply: PublicReply{Status: http.StatusOK, Text: "success"}}
					}
					if fault == "conflicting-response" {
						a = asyncPublicReply{reply: PublicReply{Status: http.StatusOK}, err: &publicRequestError{io.EOF}}
					}
					active <- a
					waiting <- q
					return nil
				},
				recover: func(_ context.Context, before *DockerPrivateHealth) error {
					if fault == "recovery" {
						return recoveryFailure("scratch not released")
					}
					if before.ProcessGeneration != "same" {
						return errors.New("unknown")
					}
					return nil
				}}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := gracefulWorkCheck(ctx, w)
			if (err == nil) != (fault == "none" || fault == "lost") {
				t.Fatalf("incorrect result: %v", err)
			}
			if fault == "recovery" && !strings.Contains(err.Error(), "scratch not released") {
				t.Fatal("safe recovery category was lost")
			}
			if (fault == "missing-admission" || fault == "waiting-dispatched" || fault == "busy-generation" || fault == "target") && deleted {
				t.Fatal("deleted before valid barriers")
			}
		})
	}
}
