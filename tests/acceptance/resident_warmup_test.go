package acceptance

import (
	"testing"
	"time"
)

func TestResidentWarmedCalibrationAllowsCalibratedOrdinaryShapeButRejectsAccumulation(t *testing.T) {
	point := func(api, backend, gpu uint64) residentResourcePoint {
		return residentResourcePoint{At: time.Now(), APIRSSBytes: api, BackendRSSBytes: backend, GPUBytes: gpu, APIFDs: 8}
	}
	cold := []residentResourcePoint{point(100, 1000, 500), point(102, 1002, 500), point(101, 1001, 500)}
	longest := []residentResourcePoint{point(140, 1200, 600), point(138, 1198, 600), point(139, 1199, 600)}
	completion := point(160, 1300, 600)
	warmed := []residentResourcePoint{point(158, 1298, 600), point(160, 1300, 600), point(159, 1299, 600)}
	envelope, err := calibrateWarmedResidentEnvelope(cold, longest, completion, warmed)
	if err != nil {
		t.Fatal(err)
	}
	if err = envelope.checkSeries([]residentResourcePoint{point(160, 1300, 600), point(159, 1299, 600), point(160, 1300, 600)}); err != nil {
		t.Fatal("independently calibrated ordinary-shape retention was rejected")
	}
	// This linear leak is inside the broad longest/warmed maximum, so checking
	// only an absolute high-water bound would incorrectly accept it.
	if err = envelope.checkSeries([]residentResourcePoint{point(110, 1100, 550), point(120, 1120, 560), point(130, 1140, 570)}); err == nil {
		t.Fatal("representative warmup hid accumulating request copies")
	}
	if err = envelope.checkSeries([]residentResourcePoint{point(160, 2300, 1100)}); err == nil {
		t.Fatal("representative warmup hid an extra model copy")
	}
	if err = envelope.checkSeries([]residentResourcePoint{point(163, 1303, 600)}); err == nil {
		t.Fatal("measured workload values retroactively expanded frozen calibration")
	}
	if _, err = calibrateWarmedResidentEnvelope(cold, longest, completion, nil); err == nil {
		t.Fatal("missing post-warmup observation became a calibrated envelope")
	}
}
