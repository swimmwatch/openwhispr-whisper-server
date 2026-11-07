package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestP08GCLogProcessHelper(t *testing.T) {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if !strings.HasPrefix(mode, "p08-child-") {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "private stderr")
	switch mode {
	case "p08-child-valid":
		_, _ = fmt.Fprintln(os.Stdout, `2026-01-01T00:00:01Z {"event":"request_completed","component":"api","request_id":"start","method":"GET","route":"/health/ready","status":200}`)
		line := "2026-01-01T00:00:02Z " + strings.Repeat("x", 256) + "\n"
		for i := 0; i < 4096; i++ {
			_, _ = fmt.Fprint(os.Stdout, line)
		}
		_, _ = fmt.Fprintln(os.Stdout, `2026-01-01T00:00:03Z {"event":"request_completed","component":"api","request_id":"end","method":"GET","route":"/health/ready","status":200}`)
	case "p08-child-overflow":
		for i := 0; i < 1024; i++ {
			if _, err := fmt.Fprint(os.Stdout, strings.Repeat("private", 2048)); err != nil {
				break
			}
		}
	case "p08-child-timeout":
		_, _ = fmt.Fprint(os.Stdout, "private output")
		time.Sleep(10 * time.Second)
	case "p08-child-fail":
		_, _ = fmt.Fprint(os.Stdout, "private output")
		os.Exit(3)
	}
	os.Exit(0)
}

func TestP08GCLogCommandBoundaries(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode, reason string
		timeout      time.Duration
	}{
		{"valid", "", 10 * time.Second},
		{"overflow", "logs_too_large", 10 * time.Second},
		{"timeout", "logs_timeout", 2 * time.Second},
		{"fail", "logs_unavailable", 10 * time.Second},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), tc.timeout)
			defer cancel()
			data, err := p08GCLogCommand(ctx, executable, "-test.run=^TestP08GCLogProcessHelper$", "--", "p08-child-"+tc.mode)
			if tc.reason != "" {
				var diagnostic *p08GCDiagnosticError
				if len(data) != 0 || !errors.Is(err, errResidentSupervisorHeapUnknown) || !errors.As(err, &diagnostic) || diagnostic.Stage != "window_logs" || diagnostic.Reason != tc.reason || strings.Contains(err.Error(), "private") {
					t.Fatalf("unsafe or incorrect command failure: %v", err)
				}
				return
			}
			if err != nil || len(data) <= 1<<20 || len(data) >= p08GCLogLimit {
				t.Fatalf("large bounded stream unavailable: %v", err)
			}
			if _, err := p08AnchoredGC(data, "api", "start", "end"); err != nil {
				t.Fatalf("complete stream rejected: %v", err)
			}
			if _, err := p08AnchoredGC(data, "api", "start", "missing"); !errors.Is(err, errResidentSupervisorHeapUnknown) {
				t.Fatal("missing end marker accepted")
			}
		})
	}
}
