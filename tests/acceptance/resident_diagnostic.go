package acceptance

import (
	"encoding/json"
	"errors"
	"time"
)

// Diagnostic collection retains the first failed acceptance decision. Later
// bounded observations are evidence, never a new calibration or a repaired pass.
type residentBlockDiagnostic struct {
	Points          []residentResourcePoint
	ViolatingBlocks []int
	firstFailure    error
}

func (d residentBlockDiagnostic) Err() error { return d.firstFailure }
func measureResidentDiagnosticBlocks(envelope residentEnvelope, measure func(int) (residentResourcePoint, error)) (residentBlockDiagnostic, error) {
	var diagnostic residentBlockDiagnostic
	for block := 0; block < 3; block++ {
		point, err := measure(block)
		if err != nil {
			return diagnostic, err
		}
		diagnostic.Points = append(diagnostic.Points, point)
		if err = envelope.checkSeries(diagnostic.Points); err != nil {
			diagnostic.ViolatingBlocks = append(diagnostic.ViolatingBlocks, block+1)
			if diagnostic.firstFailure == nil {
				diagnostic.firstFailure = err
			}
		}
	}
	return diagnostic, nil
}

// RSS and Anonymous are precise rollup counters. File remains the independently
// labeled approximate status/RssFile observation; it is not PSS or RSS-minus-anon.
type residentProcessRSS struct{ RSSBytes, AnonymousBytes, FileBytes uint64 }
type residentRSSBreakdown struct {
	At                 time.Time
	Supervisor, Native residentProcessRSS
}

func parseResidentRSSBreakdown(data []byte, at time.Time) (*residentRSSBreakdown, error) {
	type process struct {
		RSS       *uint64 `json:"rss_bytes"`
		Anonymous *uint64 `json:"anonymous_bytes"`
		File      *uint64 `json:"file_bytes"`
	}
	var wire struct{ Supervisor, Native *process }
	if json.Unmarshal(data, &wire) != nil || at.IsZero() {
		return nil, errors.New("resident process RSS breakdown unavailable")
	}
	for _, p := range []*process{wire.Supervisor, wire.Native} {
		if p == nil || p.RSS == nil || p.Anonymous == nil || p.File == nil {
			return nil, errors.New("resident process RSS counters unavailable")
		}
	}
	convert := func(p *process) residentProcessRSS { return residentProcessRSS{*p.RSS, *p.Anonymous, *p.File} }
	return &residentRSSBreakdown{at, convert(wire.Supervisor), convert(wire.Native)}, nil
}

// The fixed probe receives two already owned process IDs. It returns scalar
// counters only, never command lines, mappings, filenames or process contents.
const residentProcessRSSFunctions = `rss_json() {
 case "$1" in ''|*[!0-9]*) return 1;; esac
 rss=$(rss_bytes "$1")
 anon=$(anonymous_bytes "$1")
 file=$(file_status_bytes "$1")
 printf '{"rss_bytes":%s,"anonymous_bytes":%s,"file_bytes":%s}' "$rss" "$anon" "$file"
}
`

const residentProcessRSSProbe = "set -eu\n" + dockerRSSProbe + residentProcessRSSFunctions + `printf '{"supervisor":'
rss_json "$1"
printf ',"native":'
rss_json "$2"
printf '}\n'
`
