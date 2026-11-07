package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// The nonce is never retained or emitted. The caller brackets this check with
// exact selected Pod/container/process identity reads.
func p08VerifyHTTPForward(ctx context.Context, client *PublicClient, readLogs func(context.Context) ([]byte, error)) error {
	if client == nil || readLogs == nil {
		return errors.New("forward correlation unavailable")
	}
	id, err := p08GCAnchorID()
	if err != nil || p08GCAnchor(ctx, p08ResourceTarget{}, client, "api", "", id) != nil {
		return errors.New("forward correlation unavailable")
	}
	data, err := readLogs(ctx)
	if err != nil || len(data) == 0 || len(data) >= 1<<20 || data[len(data)-1] != '\n' {
		return errors.New("forward correlation unavailable")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	matches := 0
	for scanner.Scan() {
		var record struct {
			Event     string `json:"event"`
			Component string `json:"component"`
			ID        string `json:"request_id"`
			Method    string `json:"method"`
			Route     string `json:"route"`
			Status    int    `json:"status"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.ID != id {
			continue
		}
		if record.Event != "request_completed" || record.Component != "api" || record.Method != http.MethodGet || record.Route != "/health/ready" || record.Status != http.StatusOK {
			return errors.New("forward correlation unavailable")
		}
		matches++
	}
	if scanner.Err() != nil || matches != 1 {
		return errors.New("forward correlation unavailable")
	}
	return nil
}
