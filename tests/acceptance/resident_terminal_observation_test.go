package acceptance

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func terminalResourceFixture(rss uint64) *residentResourcePoint {
	at := time.Now()
	return &residentResourcePoint{At: at, APIRSSBytes: rss, BackendRSSBytes: rss, GPUBytes: 500, APIFDs: 8, BackendProcessRSS: &residentRSSBreakdown{At: at, Supervisor: residentProcessRSS{RSSBytes: rss}, Native: residentProcessRSS{RSSBytes: rss}}}
}

func TestResidentTerminalObservationsKeepCalibrationUnknown(t *testing.T) {
	points := []residentTerminalPoint{{Phase: "first", Resources: terminalResourceFixture(100)}, {Phase: "second", Resources: terminalResourceFixture(200)}, {Phase: "third", Resources: terminalResourceFixture(300)}}
	if err := terminalResourceDecision(nil, points); err != nil {
		t.Fatal("actual observations without a calibrated memory verdict were rejected")
	}
	evidence := residentTerminalEvidence{TerminalPoints: points, Qualification: "not_established"}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var outcome struct {
		CalibratedEnvelope json.RawMessage         `json:"calibrated_envelope"`
		Qualification      string                  `json:"qualification"`
		Points             []residentTerminalPoint `json:"terminal_points"`
	}
	if err = json.Unmarshal(data, &outcome); err != nil {
		t.Fatal(err)
	}
	if string(outcome.CalibratedEnvelope) != "null" || outcome.Qualification != "not_established" || len(outcome.Points) != 3 || outcome.Points[2].Resources.APIRSSBytes != 300 {
		t.Fatal("terminal observations invented a calibrated pass or discarded actual resource values")
	}
}

func TestResidentTerminalKnownEnvelopeStillRejectsRetention(t *testing.T) {
	envelope := residentEnvelope{Maximum: residentResourceBounds{APIRSSBytes: 100, BackendRSSBytes: 100, GPUBytes: 500, APIFDs: 8}}
	if err := terminalResourceDecision(&envelope, []residentTerminalPoint{{Resources: terminalResourceFixture(100)}}); err != nil {
		t.Fatal("valid measured terminal resources rejected")
	}
	if err := terminalResourceDecision(&envelope, []residentTerminalPoint{{Resources: terminalResourceFixture(101)}}); err == nil {
		t.Fatal("known-envelope path silently became observation-only")
	}
}

func TestResidentTerminalFailedOutcomeKeepsUnknownResourcesAndActualStatus(t *testing.T) {
	result := lifecycleCallResult{reply: PublicReply{Status: http.StatusServiceUnavailable, Text: "private fixture content"}, err: errors.New("private fixture error")}
	point := terminalOutcomePoint("failed", 3, &result)
	if point.Resources != nil || point.HTTPStatus == nil || *point.HTTPStatus != http.StatusServiceUnavailable || point.TransportError == nil || !*point.TransportError {
		t.Fatal("failed request observations were replaced by expected clean resources or status")
	}
	unknown := terminalOutcomePoint("not-requested", 3, nil)
	if unknown.HTTPStatus != nil || unknown.TransportError != nil || unknown.Resources != nil {
		t.Fatal("unobserved HTTP/resource outcomes became expected zero or success")
	}
}

func TestResidentTerminalObservationUnknownAndDeletedStorageFail(t *testing.T) {
	for _, mode := range []string{"missing", "missing-process", "missing-time", "missing-GPU", "deleted-fd", "deleted-bytes"} {
		t.Run(mode, func(t *testing.T) {
			point := terminalResourceFixture(100)
			switch mode {
			case "missing":
				point = nil
			case "missing-process":
				point.BackendProcessRSS = nil
			case "missing-time":
				point.At = time.Time{}
			case "missing-GPU":
				point.GPUBytes = 0
			case "deleted-fd":
				point.APIDeletedScratchFDs = 1
			case "deleted-bytes":
				point.APIDeletedScratchBytes = 4096
			}
			if err := terminalResourceDecision(nil, []residentTerminalPoint{{Resources: point}}); err == nil {
				t.Fatal("unknown calibration masked missing actual observations or retained request storage")
			}
		})
	}
	if err := terminalResourceDecision(nil, nil); err == nil {
		t.Fatal("absent terminal observations became a clean result")
	}
}
