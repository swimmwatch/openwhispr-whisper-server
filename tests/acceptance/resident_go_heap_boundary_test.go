package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reuse the existing Docker subprocess double, overriding only the consumed Go
// profile/log boundary. This is CPU evidence of harness discrimination, not CUDA.
func TestResidentGoHeapCLIHelper(t *testing.T) {
	if os.Getenv("P07_GO_HEAP_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	mode := os.Getenv("P07_GO_HEAP_MODE")
	if len(args) > 0 && args[0] == "logs" {
		expected := dockerAPIID
		if os.Getenv("P07_GO_HEAP_ROLE") == "backend" {
			expected = dockerBackendID
		}
		if args[len(args)-1] != expected {
			os.Exit(9)
		}
		if mode == "window-cuts" {
			var since, until time.Time
			for i := 1; i+1 < len(args); i++ {
				if args[i] == "--since" {
					since, _ = time.Parse(time.RFC3339Nano, args[i+1])
				}
				if args[i] == "--until" {
					until, _ = time.Parse(time.RFC3339Nano, args[i+1])
				}
			}
			if since.IsZero() || until.IsZero() {
				os.Exit(9)
			}
			pending := filepath.Join(os.Getenv("P07_DOCKER_HELPER_DIR"), "pending-gc-window")
			at := until.Add(-time.Nanosecond)
			cycle := 1
			if data, err := os.ReadFile(pending); err == nil {
				at, _ = time.Parse(time.RFC3339Nano, string(data))
				cycle = 2
			} else {
				_ = os.WriteFile(pending, []byte(until.Add(time.Nanosecond).Format(time.RFC3339Nano)), 0600)
			}
			if !at.Before(since) && !at.After(until) {
				fmt.Printf("%s gc %d @%ds 0%%: 0.084+1.8+0.021 ms clock, 2.0+0.41/8.9/1.0+0.50 ms cpu, 4->3->1 MB, 4 MB goal, 0 MB stacks, 1 MB globals, 24 P\n", at.UTC().Format(time.RFC3339Nano), cycle, cycle+100)
			}
			os.Exit(0)
		}
		if mode == "unbounded-history" {
			bounded := false
			for _, arg := range args {
				if arg == "--since" {
					bounded = true
				}
			}
			if !bounded {
				fmt.Print(strings.Repeat("private accumulated request log\n", dockerOutputLimit/20))
				os.Exit(0)
			}
		}
		if mode == "missing" {
			os.Exit(0)
		}
		at := time.Now().Add(-time.Second)
		cycles := 4
		if mode == "forced" {
			cycles++
		} // Four natural cycles must not hide one forced cycle.
		for i := 0; i < cycles; i++ {
			suffix := ""
			if mode == "forced" && i == 1 {
				suffix = " (forced)"
			}
			fmt.Printf("%s gc %d @%ds 0%%: 0.084+1.8+0.021 ms clock, 2.0+0.41/8.9/1.0+0.50 ms cpu, 4->3->1 MB, 4 MB goal, 0 MB stacks, 1 MB globals, 24 P%s\n", at.Add(time.Duration(i)*time.Millisecond).UTC().Format(time.RFC3339Nano), i+1, i+100, suffix)
		}
		if mode == "replaced-during-logs" {
			_ = os.WriteFile(filepath.Join(os.Getenv("P07_DOCKER_HELPER_DIR"), "go-replaced"), []byte("1"), 0600)
		}
		os.Exit(0)
	}
	if len(args) == 5 && args[0] == "exec" && args[2] == "sh" && args[3] == "-ec" {
		if mode == "tuned" {
			fmt.Print("gctrace=1,scavtrace=1\n50\n\n")
		} else {
			fmt.Print("gctrace=1,scavtrace=1\n\n\n")
		}
		os.Exit(0)
	}
	if len(args) > 0 && args[0] == "inspect" && mode == "replaced-during-logs" {
		if _, err := os.Stat(filepath.Join(os.Getenv("P07_DOCKER_HELPER_DIR"), "go-replaced")); err == nil {
			var states map[string]DockerInspection
			_ = json.Unmarshal([]byte(os.Getenv("P07_DOCKER_HELPER_INSPECTIONS")), &states)
			for _, state := range states {
				if state.ID == args[len(args)-1] {
					state.StartedAt = state.StartedAt.Add(time.Second)
					_ = json.NewEncoder(os.Stdout).Encode(state)
					os.Exit(0)
				}
			}
		}
	}
	TestDockerCLIHelper(t)
}

func TestResidentGoHeapReaderBindsRealRolesAndRejectsChangedProcesses(t *testing.T) {
	for _, role := range []string{"api", "backend"} {
		t.Run(role, func(t *testing.T) {
			target, dir := dockerFixture(t)
			t.Setenv("P07_GO_HEAP_HELPER", "1")
			t.Setenv("P07_GO_HEAP_ROLE", role)
			wrapper := filepath.Join(dir, "docker")
			if err := os.WriteFile(wrapper, dockerFixtureScript("TestResidentGoHeapCLIHelper"), 0700); err != nil {
				t.Fatal(err)
			}
			start := time.Now().Add(-time.Minute)
			apiImage, backendImage := "sha256:"+strings.Repeat("3", 64), "sha256:"+strings.Repeat("4", 64)
			states := map[string]DockerInspection{
				"api":     {ID: dockerAPIID, Image: apiImage, Running: true, PID: 123, StartedAt: start},
				"backend": {ID: dockerBackendID, Image: backendImage, Running: true, PID: 456, StartedAt: start},
			}
			setStates := func() { data, _ := json.Marshal(states); t.Setenv("P07_DOCKER_HELPER_INSPECTIONS", string(data)) }
			setStates()
			e := &acceptanceEnvironment{ctx: t.Context(), target: target, workdir: dir, selection: acceptanceSelection{APIID: apiImage, BackendID: backendImage}}
			if err := target.Up(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer target.Down(t.Context())
			before, err := target.Observe(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			policy, err := selectResidentGoHeapPolicy(t.Context(), e, role, before, start)
			if err != nil {
				t.Fatal(err)
			}
			if policy.Scope.Role != role || policy.Scope.ContainerID != states[role].ID || policy.Scope.ImageID != states[role].Image {
				t.Fatal("Go role acquired fabricated or cross-role process identity")
			}
			series, through, err := readResidentSupervisorHeap(t.Context(), e, *policy)
			if err != nil || series == nil || len(series.Records) != 4 {
				t.Fatal("real subprocess GC records were not consumed", err)
			}
			if _, err = freezeResidentSupervisorPostWork(*policy, series, start, through); err != nil {
				t.Fatal(err)
			}
			t.Setenv("P07_GO_HEAP_MODE", "unbounded-history")
			artifact := "supervisor-gc-window-control.json"
			if role == "api" {
				t.Setenv("P07_API_GC_TRACE", "1")
				recordResidentAPIGCObservation(t, e, "window-control")
				artifact = "api-gc-window-control.json"
			} else {
				recordResidentSupervisorGCObservation(t, e, "window-control")
			}
			data, err := os.ReadFile(filepath.Join(dir, artifact))
			var receipt struct {
				Window *residentGoHeapWindow `json:"bounded_window"`
			}
			if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.Window == nil || receipt.Window.Scope != policy.Scope || receipt.Window.Numeric.GC == nil || len(receipt.Window.Numeric.GC.Records) != 4 || !receipt.Window.ObservedThrough.Equal(through) {
				t.Fatal("supplemental hook reread accumulated history or lost verified numeric window identity")
			}
			if _, _, err := residentGoDiagnosticWindow(e, role); err == nil {
				t.Fatal("consumed diagnostic window was silently reused in another phase")
			}
			t.Run("bounded-cutoff", func(t *testing.T) {
				t.Setenv("P07_GO_HEAP_MODE", "window-cuts")
				first, cut, err := readResidentSupervisorHeapSince(t.Context(), e, *policy, start)
				if err != nil || first == nil || len(first.Records) != 1 || !first.Records[0].ObservedAt.Add(time.Nanosecond).Equal(cut) {
					t.Fatal("reader did not preserve its explicit pre-read cutoff", err)
				}
				second, nextCut, err := readResidentSupervisorHeapSince(t.Context(), e, *policy, cut)
				if err != nil || second == nil || len(second.Records) != 1 || second.Records[0].Cycle != 2 {
					t.Fatal("GC emitted after Docker snapshot was skipped by an observer wall-clock cursor", err)
				}
				merged, err := appendResidentGCWindow(first, second)
				if err != nil || len(merged.Records) != 2 || !residentGCCursor(nextCut, merged).Equal(nextCut) {
					t.Fatal("explicit completed window did not advance past request-only log history", err)
				}
				if _, _, err := readResidentSupervisorHeapSince(t.Context(), e, *policy, time.Now().Add(time.Minute)); err == nil {
					t.Fatal("a reversed Since/Until interval became a confirmed observation window")
				}
			})
			for _, fault := range []string{"image", "start", "native", "tuned", "replaced-during-logs", "forced", "missing"} {
				t.Run(fault, func(t *testing.T) {
					if _, _, err := readResidentSupervisorHeap(t.Context(), e, *policy); err != nil {
						t.Fatal(err)
					}
					original := states[role]
					changed := original
					switch fault {
					case "image":
						changed.Image = "unexpected-image"
					case "start":
						changed.StartedAt = start.Add(time.Second)
					}
					states[role] = changed
					setStates()
					defer func() { states[role] = original; setStates() }()
					t.Setenv("P07_GO_HEAP_MODE", fault)
					if fault == "image" || fault == "start" {
						if _, _, err := residentGoDiagnosticWindow(e, role); err == nil {
							t.Fatal("supplemental hook reused a stale changed-process window")
						}
					}
					p := *policy
					if fault == "native" {
						p.Scope.NativeGeneration = "unexpected-native"
					}
					s, end, err := readResidentSupervisorHeap(t.Context(), e, p)
					if err != nil {
						if _, _, cachedErr := residentGoDiagnosticWindow(e, role); cachedErr == nil {
							t.Fatal("failed read reused a previous successful numeric window")
						}
					}
					if err == nil {
						_, err = freezeResidentSupervisorPostWork(p, s, start, end)
					}
					if err == nil {
						t.Fatal("changed/missing/forced Go evidence became a frozen heap bound")
					}
				})
			}
		})
	}
}
