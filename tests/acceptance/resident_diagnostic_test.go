package acceptance

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestResidentDiagnosticCompletesAllBlocksAndRetainsFirstFailure(t *testing.T) {
	bound := residentEnvelope{Maximum: residentResourceBounds{APIRSSBytes: 100, BackendRSSBytes: 100, GPUBytes: 100, APIFDs: 10}}
	calls := 0
	diagnostic, err := measureResidentDiagnosticBlocks(bound, func(block int) (residentResourcePoint, error) {
		calls++
		backend := uint64(101)
		if block > 0 {
			backend = 99
		}
		return residentResourcePoint{At: time.Now(), APIRSSBytes: 99, BackendRSSBytes: backend, GPUBytes: 99, APIFDs: 9}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(diagnostic.Points) != 3 {
		t.Fatal("first envelope violation discarded remaining bounded diagnostic blocks")
	}
	if !errors.Is(diagnostic.Err(), errResidentEnvelope) || len(diagnostic.ViolatingBlocks) != 3 || diagnostic.ViolatingBlocks[0] != 1 {
		t.Fatal("later observations erased the original failed acceptance decision")
	}
}

func TestResidentDiagnosticStopsWhenActualObservationIsUnavailable(t *testing.T) {
	unavailable := errors.New("fixture observation unavailable")
	calls := 0
	diagnostic, err := measureResidentDiagnosticBlocks(residentEnvelope{}, func(int) (residentResourcePoint, error) { calls++; return residentResourcePoint{}, unavailable })
	if !errors.Is(err, unavailable) || calls != 1 || len(diagnostic.Points) != 0 {
		t.Fatal("unknown resource observation became a measured point")
	}
}

func TestResidentProcessRSSProbeMeasuresActualOwnedProcessScalars(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	command := exec.CommandContext(t.Context(), "sh", "-eu", "-c", residentProcessRSSProbe, "p07-rss-probe", pid, pid)
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	breakdown, err := parseResidentRSSBreakdown(data, time.Now())
	if err != nil || breakdown.Supervisor.RSSBytes == 0 || breakdown.Native.RSSBytes == 0 {
		t.Fatal("actual process RSS observation unavailable")
	}
	for _, wire := range []string{`{}`, `{"supervisor":{},"native":{}}`, `{"supervisor":{"rss_bytes":1,"anonymous_bytes":0,"file_bytes":0},"native":{"rss_bytes":1,"anonymous_bytes":0}}`, `{"supervisor":{"rss_bytes":-1,"anonymous_bytes":0,"file_bytes":0},"native":{"rss_bytes":1,"anonymous_bytes":0,"file_bytes":0}}`} {
		if _, err := parseResidentRSSBreakdown([]byte(wire), time.Now()); err == nil {
			t.Fatal("unknown or invalid process scalar accepted as an actual measurement")
		}
	}
	if _, err := parseResidentRSSBreakdown([]byte(`{"supervisor":{"rss_bytes":1,"anonymous_bytes":0,"file_bytes":0},"native":{"rss_bytes":1,"anonymous_bytes":0,"file_bytes":0}}`), time.Now()); err != nil {
		t.Fatal("actually observed zero anonymous/file counters were rejected")
	}
}
