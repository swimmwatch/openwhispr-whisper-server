package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestP08ResourceObservationRejectsOwnerChangesAndUnknownCounters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  string
		wantErr bool
	}{
		{"complete", "", false}, {"pod replaced", "pod", true}, {"container restarted", "restart", true}, {"native replaced", "process", true}, {"probe mismatched native", "native", true}, {"unknown RSS", "missing", true}, {"unknown descendant RSS", "descendant_unknown", true}, {"unknown scratch", "scratch_unknown", true}, {"deleted scratch measured", "deleted", false}, {"probe failure", "failure", true}, {"host PID", "hostpid", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := p08ResourceTarget{Kubeconfig: "/private/kubeconfig", Context: "private", Namespace: "test", NamespaceUID: "ns-uid", Release: "release", APIPod: "api-0", APIUID: "api-uid", APIContainer: "api", BackendPod: "backend-0", BackendUID: "backend-uid", BackendContainer: "backend"}
			podReads, processReads := 0, 0
			target.run = func(_ context.Context, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "get namespace") {
					return []byte("ns-uid"), nil
				}
				if strings.Contains(joined, "get pod") {
					podReads++
					role, uid, container := "api", "api-uid", "api"
					if strings.Contains(joined, "pod backend-0") {
						role, uid, container = "backend", "backend-uid", "backend"
					}
					if tc.change == "pod" && podReads > 2 {
						uid = "replacement"
					}
					restart := 0
					if tc.change == "restart" && podReads > 2 {
						restart = 1
					}
					host := tc.change == "hostpid"
					return []byte(fmt.Sprintf(`{"metadata":{"uid":%q,"labels":{"app.kubernetes.io/instance":"release","app.kubernetes.io/component":%q}},"spec":{"hostPID":%t},"status":{"containerStatuses":[{"name":%q,"containerID":"container-id","restartCount":%d,"state":{"running":{}}}]}}`, uid, role, host, container, restart)), nil
				}
				if strings.Contains(joined, "p08-identity") {
					processReads++
					if strings.HasSuffix(joined, "backend") {
						if tc.change == "process" && processReads > 2 {
							return []byte("111 42 999\n"), nil
						}
						return []byte("111 42 222\n"), nil
					}
					return []byte("111 0 0\n"), nil
				}
				if strings.Contains(joined, "p08-descendants") {
					if tc.change == "descendant_unknown" {
						return []byte("42:222:unknown\nOK\n"), nil
					}
					return []byte("43:333:8192\nOK\n"), nil
				}
				if strings.Contains(joined, "p08-scratch") {
					if tc.change == "scratch_unknown" {
						return []byte("bad"), nil
					}
					if tc.change == "deleted" {
						return []byte("D hash 1 2 100\nOK\n"), nil
					}
					return []byte("OK\n"), nil
				}
				if strings.Contains(joined, "p08-resource") {
					if tc.change == "failure" {
						return nil, errors.New("private command data")
					}
					native := 0
					if strings.HasSuffix(joined, "backend") {
						native = 42
						if tc.change == "native" {
							native = 43
						}
					}
					rss := `"rss_bytes":100,`
					if tc.change == "missing" {
						rss = ""
					}
					return []byte(fmt.Sprintf(`{%s"fd_count":4,"scratch_file_count":0,"scratch_bytes":0,"native_pid":%d,"ownership_lock_files":0}`, rss, native)), nil
				}
				return nil, errors.New("unexpected command")
			}
			got, err := target.observe(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected observation result: %v", err)
			}
			if err == nil {
				if got.API.RSSBytes == nil || *got.API.RSSBytes != 100 || got.Backend.FDCount == nil || *got.Backend.FDCount != 4 {
					t.Fatal("known measurements lost")
				}
				if got.NativeDescendantRSSBytes == nil || *got.NativeDescendantRSSBytes != 8192 || got.APIDeletedScratchFDs == nil || got.APIDeletedScratchBytes == nil {
					t.Fatal("extended observations unavailable")
				}
				if tc.change == "deleted" && (*got.APIDeletedScratchFDs != 1 || *got.APIDeletedScratchBytes != 100) {
					t.Fatal("deleted-open data was not reported")
				}
				data, _ := json.Marshal(got)
				if strings.Contains(string(data), "private") || strings.Contains(string(data), "container-id") || strings.Contains(string(data), "uid") {
					t.Fatal("private identity leaked")
				}
			}
		})
	}
}

func TestP08NativeDescendantProbeMeasuresPreciseAndRejectsUnknown(t *testing.T) {
	proc := t.TempDir()
	writeProc := func(pid, parent, start, report string) {
		t.Helper()
		dir := filepath.Join(proc, pid)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		stat := pid + " (test) S " + parent + strings.Repeat(" 0", 17) + " " + start + "\n"
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0600); err != nil {
			t.Fatal(err)
		}
		if report != "" {
			if err := os.WriteFile(filepath.Join(dir, "smaps_rollup"), []byte(report), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeProc("10001", "0", "100", "")
	writeProc("10002", "10001", "101", "Rss: 12 kB\n")
	writeProc("10003", "10002", "102", "Rss: 7 kB\n")
	run := func(start string) ([]byte, error) {
		return exec.CommandContext(t.Context(), "sh", "-eu", "-c", p08NativeDescendantProbe, "test", proc, "10001", start).Output()
	}
	data, err := run("100")
	total, parseErr := p08NativeDescendantRSS(data)
	if err != nil || parseErr != nil || total != 19*1024 {
		t.Fatalf("descendants were not measured precisely: %v %v", err, parseErr)
	}
	if _, err = run("999"); err == nil {
		t.Fatal("changed native lifetime was accepted")
	}
	if err = os.Remove(filepath.Join(proc, "10003", "smaps_rollup")); err != nil {
		t.Fatal(err)
	}
	if _, err = run("100"); err == nil {
		t.Fatal("unavailable precise descendant RSS was accepted")
	}
}

func TestP08ResourceObservationRequiresExplicitSelection(t *testing.T) {
	target := p08ResourceTarget{Kubeconfig: "relative", Context: "x", Namespace: "n", NamespaceUID: "n", Release: "r", APIPod: "a", APIUID: "a", APIContainer: "api", BackendPod: "b", BackendUID: "b", BackendContainer: "backend"}
	called := false
	target.run = func(context.Context, ...string) ([]byte, error) { called = true; return nil, nil }
	if _, err := target.observe(context.Background()); err == nil || called {
		t.Fatal("invalid selection reached Kubernetes")
	}
}
