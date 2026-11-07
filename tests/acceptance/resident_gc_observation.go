package acceptance

import (
	"bufio"
	"bytes"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var errSupervisorGCObservation = errors.New("supervisor GC trace observation is invalid or exceeds its read bound; raw logs withheld")

type residentByteInterval struct{ LowerInclusive, UpperExclusive uint64 }
type residentGCRecord struct {
	ObservedAt                                                                                       *time.Time
	Cycle                                                                                            uint64
	UptimeSeconds                                                                                    float64
	HeapStartWholeMiB, HeapEndWholeMiB, HeapGoalWholeMiB, StackWholeMiB, GlobalsWholeMiB, Processors uint64
	AfterGCLiveBytes                                                                                 residentByteInterval
	Forced                                                                                           bool
}
type residentScavengerRecord struct {
	ObservedAt                        *time.Time
	BackgroundWholeKiB, EagerWholeKiB uint64
	ReleasedNowBytes                  residentByteInterval
	UtilizationPercent                uint64
	Forced, ControllerReset           bool
}
type residentGCSeries struct{ Records []residentGCRecord }
type residentScavengerSeries struct{ Records []residentScavengerRecord }
type residentSupervisorGCObservation struct {
	GC        *residentGCSeries
	Scavenger *residentScavengerSeries
}

const residentTraceNumber = `[0-9]+(?:\.[0-9]+)?(?:e[+-]?[0-9]+)?`

var residentGCTracePattern = regexp.MustCompile(`^gc ([0-9]+) @(` + residentTraceNumber + `)s (` + residentTraceNumber + `)%(: )` +
	`(` + residentTraceNumber + `)\+(` + residentTraceNumber + `)\+(` + residentTraceNumber + `) ms clock, (` + residentTraceNumber + `)\+(` + residentTraceNumber + `)/(` + residentTraceNumber + `)/(` + residentTraceNumber + `)\+(` + residentTraceNumber + `) ms cpu, ([0-9]+)->([0-9]+)->([0-9]+) MB, ([0-9]+) MB goal, ([0-9]+) MB stacks, ([0-9]+) MB globals, ([0-9]+) P( \(forced\))?$`)
var residentScavengerTracePattern = regexp.MustCompile(`^scav ([0-9]+) KiB work \(bg\), ([0-9]+) KiB work \(eager\), ([0-9]+) KiB now, ([0-9]+)% util( \(forced\)| \[controller reset\])?$`)

func residentQuantizedBytes(whole, unit uint64) (residentByteInterval, error) {
	if unit == 0 || whole >= ^uint64(0)/unit {
		return residentByteInterval{}, errSupervisorGCObservation
	}
	return residentByteInterval{whole * unit, (whole + 1) * unit}, nil
}

// Parse only anchored numeric Go 1.27.3 gctrace/scavtrace records. Docker's
// timestamp, if present, is kept independently from the runtime uptime. No raw
// input line, response, payload, filename or unrecognized log field is retained.
// Absent GC/scavenger records remain nil, never a measured zero count/live heap.
func observeResidentSupervisorGC(logs []byte) (residentSupervisorGCObservation, error) {
	var result residentSupervisorGCObservation
	if len(logs) > dockerOutputLimit {
		return result, errSupervisorGCObservation
	}
	scanner := bufio.NewScanner(bytes.NewReader(logs))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		var at *time.Time
		if prefix, body, ok := strings.Cut(line, " "); ok && (strings.HasPrefix(body, "gc ") || strings.HasPrefix(body, "scav ")) {
			parsed, err := time.Parse(time.RFC3339Nano, prefix)
			if err != nil {
				return residentSupervisorGCObservation{}, errSupervisorGCObservation
			}
			at = &parsed
			line = body
		}
		if strings.HasPrefix(line, "gc ") {
			match := residentGCTracePattern.FindStringSubmatch(line)
			if match == nil {
				return residentSupervisorGCObservation{}, errSupervisorGCObservation
			}
			// All clock/CPU fields are numeric and finite even though only memory and
			// uptime facts are retained in this supplemental diagnostic.
			for _, index := range []int{2, 3, 5, 6, 7, 8, 9, 10, 11, 12} {
				value, err := strconv.ParseFloat(match[index], 64)
				if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
					return residentSupervisorGCObservation{}, errSupervisorGCObservation
				}
			}
			integers := make([]uint64, 8)
			for i, index := range []int{1, 13, 14, 15, 16, 17, 18, 19} {
				value, err := strconv.ParseUint(match[index], 10, 64)
				if err != nil {
					return residentSupervisorGCObservation{}, errSupervisorGCObservation
				}
				integers[i] = value
			}
			interval, err := residentQuantizedBytes(integers[3], 1<<20)
			if err != nil {
				return residentSupervisorGCObservation{}, err
			}
			uptime, _ := strconv.ParseFloat(match[2], 64)
			record := residentGCRecord{ObservedAt: at, Cycle: integers[0], UptimeSeconds: uptime, HeapStartWholeMiB: integers[1], HeapEndWholeMiB: integers[2], AfterGCLiveBytes: interval, HeapGoalWholeMiB: integers[4], StackWholeMiB: integers[5], GlobalsWholeMiB: integers[6], Processors: integers[7], Forced: match[20] != ""}
			if result.GC == nil {
				result.GC = &residentGCSeries{}
			}
			result.GC.Records = append(result.GC.Records, record)
		} else if strings.HasPrefix(line, "scav ") {
			match := residentScavengerTracePattern.FindStringSubmatch(line)
			if match == nil {
				return residentSupervisorGCObservation{}, errSupervisorGCObservation
			}
			var values [4]uint64
			for i := range values {
				v, err := strconv.ParseUint(match[i+1], 10, 64)
				if err != nil {
					return residentSupervisorGCObservation{}, errSupervisorGCObservation
				}
				values[i] = v
			}
			interval, err := residentQuantizedBytes(values[2], 1<<10)
			if err != nil {
				return residentSupervisorGCObservation{}, err
			}
			record := residentScavengerRecord{ObservedAt: at, BackgroundWholeKiB: values[0], EagerWholeKiB: values[1], ReleasedNowBytes: interval, UtilizationPercent: values[3], Forced: match[5] == " (forced)", ControllerReset: match[5] == " [controller reset]"}
			if result.Scavenger == nil {
				result.Scavenger = &residentScavengerSeries{}
			}
			result.Scavenger.Records = append(result.Scavenger.Records, record)
		}
	}
	if scanner.Err() != nil {
		return residentSupervisorGCObservation{}, errSupervisorGCObservation
	}
	return result, nil
}
