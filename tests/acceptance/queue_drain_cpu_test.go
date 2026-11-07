package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type delayedCancelTransport struct {
	started, finished, closed chan struct{}
	delay                     time.Duration
}

func (d *delayedCancelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	close(d.started)
	<-r.Context().Done()
	time.Sleep(d.delay)
	close(d.finished)
	return nil, r.Context().Err()
}
func (d *delayedCancelTransport) CloseIdleConnections() { close(d.closed) }

// Child intentionally fails its queue observation; the parent checks cleanup.
func TestQueueObservationFailureDrainsRequests(t *testing.T) {
	if mode := os.Getenv("QUEUE_DRAIN_CHILD"); mode != "" {
		delay, budget := 100*time.Millisecond, time.Second
		if mode == "timeout" {
			delay, budget = 2*time.Second, 30*time.Millisecond
		}
		var transports []*delayedCancelTransport
		observations := 0
		t.Run("expected_observation_failure", func(t *testing.T) {
			runQueueWorkload(t, queueWorkload{ctx: t.Context(), cleanupTimeout: budget, model: "base", long: []byte("sample"), short: []byte("sample"), client: func(t *testing.T, _ string) *PublicClient {
				tr := &delayedCancelTransport{started: make(chan struct{}), finished: make(chan struct{}), closed: make(chan struct{}), delay: delay}
				transports = append(transports, tr)
				c, e := NewPublicClient("http://127.0.0.1", time.Second, 1024)
				if e != nil {
					t.Fatal(e)
				}
				c.client.Transport = tr
				t.Cleanup(c.Close)
				return c
			}, logs: func(context.Context) ([]byte, error) {
				<-transports[observations].started
				observations++
				switch observations {
				case 1:
					return []byte(`{"event":"queue_dispatched","request_id":"fifo-active"}`), nil
				case 2:
					return []byte(`{"event":"queue_admitted","request_id":"fifo-second"}`), nil
				default:
					return nil, errors.New("unavailable")
				}
			}})
		})
		joined, closed := len(transports) == 3, len(transports) == 3
		for _, tr := range transports {
			select {
			case <-tr.finished:
			default:
				joined = false
			}
			select {
			case <-tr.closed:
			default:
				closed = false
			}
		}
		if joined {
			fmt.Println("DRAIN_JOINED")
		} else {
			fmt.Println("DRAIN_NOT_JOINED")
		}
		if closed {
			fmt.Println("CLIENT_CLOSED")
		} else {
			fmt.Println("CLIENT_NOT_CLOSED")
		}
		return
	}
	for _, tc := range []struct{ mode, marker string }{{"normal", "DRAIN_JOINED"}, {"timeout", "queue request cleanup exceeded its budget"}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestQueueObservationFailureDrainsRequests$", "-test.v")
			cmd.Env = append(os.Environ(), "QUEUE_DRAIN_CHILD="+tc.mode)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(out), tc.marker) || !strings.Contains(string(out), "CLIENT_CLOSED") {
				t.Fatalf("failed observation did not cancel, bound and close request owner: child error=%v output=%s", err, out)
			}
		})
	}
}
