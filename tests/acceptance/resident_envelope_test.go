package acceptance

import (
	"testing"
	"time"
)

// These controls exercise the acceptance decision, not the repository layout.
func TestResidentEnvelopeControls(t *testing.T) {
	point := func(api, backend, gpu uint64) residentResourcePoint {
		return residentResourcePoint{At: time.Now(), APIRSSBytes: api, BackendRSSBytes: backend, GPUBytes: gpu, APIFDs: 8}
	}
	idle := []residentResourcePoint{point(100, 1000, 500), point(102, 1002, 500), point(101, 1001, 500)}
	longest := []residentResourcePoint{point(140, 1200, 600), point(138, 1198, 600), point(139, 1199, 600)}
	envelope, err := calibrateResidentEnvelope(idle, longest)
	if err != nil {
		t.Fatal(err)
	}
	valid := []residentResourcePoint{point(140, 1200, 600), point(139, 1199, 600), point(140, 1200, 600)}
	if err := envelope.checkSeries(valid); err != nil {
		t.Fatalf("bounded latest-result retention rejected: %v", err)
	}
	for name, series := range map[string][]residentResourcePoint{
		"request copies":  {point(110, 1100, 550), point(120, 1120, 560), point(130, 1140, 570)},
		"model copy":      {point(140, 2200, 1100), point(140, 2200, 1100), point(140, 2200, 1100)},
		"API descriptors": {{At: time.Now(), APIRSSBytes: 140, BackendRSSBytes: 1200, GPUBytes: 600, APIFDs: 9}},
		"unlinked file":   {{At: time.Now(), APIRSSBytes: 140, BackendRSSBytes: 1200, GPUBytes: 600, APIFDs: 8, APIDeletedScratchFDs: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := envelope.checkSeries(series); err == nil {
				t.Fatal("retained resources were accepted")
			}
		})
	}
	if _, err := calibrateResidentEnvelope(nil, longest); err == nil {
		t.Fatal("absent idle calibration accepted")
	}
	if _, err := calibrateResidentEnvelope(idle, nil); err == nil {
		t.Fatal("absent longest calibration accepted")
	}
}

func TestResidentEnvelopeRejectsScratchAndUnknownObservations(t *testing.T) {
	for _, wire := range []string{`{}`, `{"deleted_scratch_fds":1,"deleted_scratch_bytes":4096}`, `{"deleted_scratch_fds":0}`} {
		if _, _, err := parseResidentDeletedScratch([]byte(wire)); err == nil {
			t.Fatalf("incomplete or retained scratch observation accepted: %s", wire)
		}
	}
	fds, bytes, err := parseResidentDeletedScratch([]byte(`{"deleted_scratch_fds":0,"deleted_scratch_bytes":0}`))
	if err != nil || fds != 0 || bytes != 0 {
		t.Fatal("actual clean observation rejected")
	}
}
