package acceptance

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDockerRSSProbeUsesPreciseResidentReport(t *testing.T) {
	for _, tc := range []struct {
		name, report string
		want         uint64
	}{
		{name: "precise resident total", report: "1000-2000 ---p 00000000 00:00 0 [rollup]\nRss: 8192 kB\nPss: 4096 kB\nAnonymous: 2048 kB\n", want: 8192 << 10},
		{name: "approximate status is not a precise total", report: "VmRSS: 8192 kB\n"},
		{name: "missing resident total", report: "Pss: 8192 kB\n"},
		{name: "wrong unit", report: "Rss: 8192 MB\n"},
		{name: "invalid resident total", report: "Rss: unknown kB\n"},
		{name: "overflow", report: "Rss: 18446744073709551615 kB\n"},
		{name: "duplicate resident totals", report: "Rss: 8192 kB\nRss: 1024 kB\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "sh", "-ec", dockerRSSProbe+"\nrss_bytes_from_report")
			command.Stdin = strings.NewReader(tc.report)
			output, err := command.Output()
			if tc.want == 0 {
				if err == nil {
					t.Fatal("unknown, approximate or invalid report became a precise RSS observation")
				}
				return
			}
			got, parseErr := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
			if err != nil || parseErr != nil || got != tc.want {
				t.Fatalf("precise resident report was not observed: got=%d, err=%v, parse=%v", got, err, parseErr)
			}
		})
	}
}

func TestResidentProcessRSSProducerUsesPreciseCounters(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	// Replace only the kernel-counter I/O boundary, not the producer or its
	// acceptance decision. Approximate status readers must not supply RSS/anon.
	readers := `
rss_bytes() { printf '8192\n'; }
anonymous_bytes() { printf '4096\n'; }
file_status_bytes() { printf '2048\n'; }
rss_json "$1"
`
	command := exec.CommandContext(ctx, "sh", "-ec", dockerRSSProbe+residentProcessRSSFunctions+readers, "owned-process", strconv.Itoa(os.Getpid()))
	output, err := command.Output()
	var point struct {
		RSS       uint64 `json:"rss_bytes"`
		Anonymous uint64 `json:"anonymous_bytes"`
		File      uint64 `json:"file_bytes"`
	}
	if err != nil || json.Unmarshal(output, &point) != nil || point.RSS != 8192 || point.Anonymous != 4096 || point.File != 2048 {
		t.Fatal("process RSS producer substituted approximate status counters for the precise observation boundary")
	}
}

func TestResidentProcessRSSProducerRejectsMissingCounter(t *testing.T) {
	for _, failed := range []string{"rss_bytes", "anonymous_bytes", "file_status_bytes"} {
		t.Run(failed, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			readers := "\nrss_bytes() { printf '8192\\n'; }\nanonymous_bytes() { printf '4096\\n'; }\nfile_status_bytes() { printf '2048\\n'; }\n" + failed + "() { return 1; }\nrss_json \"$1\"\n"
			command := exec.CommandContext(ctx, "sh", "-ec", dockerRSSProbe+residentProcessRSSFunctions+readers, "owned-process", strconv.Itoa(os.Getpid()))
			if _, err := command.Output(); err == nil {
				t.Fatal("failed counter was replaced by an approximate or zero observation")
			}
		})
	}
}

func TestResidentProcessRSSProbeReadsOwnedLiveProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	pid := strconv.Itoa(os.Getpid())
	command := exec.CommandContext(ctx, "sh", "-ec", residentProcessRSSProbe, "owned-process", pid, pid)
	output, err := command.Output()
	point, parseErr := parseResidentRSSBreakdown(output, time.Now())
	if err != nil || parseErr != nil || point.Supervisor.RSSBytes == 0 || point.Supervisor.AnonymousBytes == 0 || point.Native.RSSBytes == 0 || point.Native.AnonymousBytes == 0 || point.Supervisor.RSSBytes < point.Supervisor.AnonymousBytes || point.Native.RSSBytes < point.Native.AnonymousBytes {
		t.Fatal("real process did not produce complete precise RSS/anonymous observations")
	}
}

func TestDockerRSSProbeReadsOwnedLiveProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "sh", "-ec", dockerRSSProbe+"\nrss_bytes \"$1\"", "owned-process", strconv.Itoa(os.Getpid()))
	output, err := command.Output()
	value, parseErr := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil || parseErr != nil || value == 0 {
		t.Fatal("real owned process did not produce a known precise RSS observation")
	}
}
