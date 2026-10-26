package acceptance

import "testing"

func TestGPUActivityUsesCorrelatedSMAndFramebufferColumns(t *testing.T) {
	reader := gpuActivityReader{pid: 123}
	reader.sample("# gpu pid type sm mem enc dec jpg ofa fb ccpm command")
	reader.sample("0 456 C 100 98 0 0 0 0 9000 0 unrelated")
	reader.sample("0 123 C 72 34 0 0 0 0 2048 0 whisper-server")
	if reader.activity.Samples != 1 || reader.activity.PeakSMPercent != 72 || reader.activity.PeakMemoryBytes != 2048<<20 {
		t.Fatalf("wrong correlated GPU utilization/allocation: %+v", reader.activity)
	}
}

func TestGPUActivityDoesNotConvertUnknownOrInvalidRowsToMeasurements(t *testing.T) {
	reader := gpuActivityReader{pid: 123}
	reader.sample("# gpu pid type sm mem fb command")
	for _, line := range []string{"0 123 C - 0 200 worker", "0 123 C 101 0 200 worker", "0 123 C 10 0 - worker", "0 123 C 10", "# Idx PID C/G % % MB name"} {
		reader.sample(line)
	}
	if reader.activity.Samples != 0 {
		t.Fatal("missing/invalid GPU observations became numeric evidence")
	}
}
