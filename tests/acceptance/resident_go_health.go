package acceptance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const residentHealthObservationMaxCalls = 4096
const residentHealthObservationBatch = 64

type residentHealthObservation struct {
	StartedAt, FinishedAt time.Time
	Deadline              time.Time
	Calls, Errors         int
}

// The existing public liveness handler only serializes {status:live}. It does
// not query a model or enqueue inference. Reuse the bounded public transport;
// never redirect, force GC, tune the runtime or create a fresh phase deadline.
func runResidentHealthObservation(ctx context.Context, client *PublicClient, facts *residentHealthObservation, calls int) (err error) {
	if facts.StartedAt.IsZero() {
		facts.StartedAt = time.Now()
	}
	defer func() { facts.FinishedAt = time.Now() }()
	deadline, ok := ctx.Deadline()
	if !ok || client == nil || calls < 0 || calls > residentHealthObservationMaxCalls-facts.Calls {
		return errResidentSupervisorHeapUnknown
	}
	if !facts.Deadline.IsZero() && !facts.Deadline.Equal(deadline) {
		return errResidentSupervisorHeapUnknown
	}
	facts.Deadline = deadline
	endpoint := strings.TrimSuffix(client.endpoint, "/v1/audio/transcriptions") + "/health/live"
	for i := 0; i < calls; i++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(errResidentSupervisorHeapUnknown, err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return errResidentSupervisorHeapUnknown
		}
		facts.Calls++
		response, err := client.client.Do(request)
		if err != nil {
			facts.Errors++
			return errors.Join(errResidentSupervisorHeapUnknown, &publicRequestError{err})
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		closeErr := response.Body.Close()
		var live struct {
			Status string `json:"status"`
		}
		if readErr != nil || closeErr != nil || len(body) > 4096 || response.StatusCode != http.StatusOK || decodePublicJSON(body, &live) != nil || live.Status != "live" {
			facts.Errors++
			return errResidentSupervisorHeapUnknown
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errResidentSupervisorHeapUnknown, err)
	}
	return nil
}
