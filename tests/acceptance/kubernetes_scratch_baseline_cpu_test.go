package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stableP08LocalSnapshot(t *testing.T, dir string) p08ScratchSnapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got p08ScratchSnapshot
	err := waitP08ScratchRelease(ctx, func(ctx context.Context) error {
		var err error
		got, err = p08ScratchLocalSnapshot(ctx, dir, "/proc")
		return err
	})
	if err != nil {
		t.Fatal("scratch observation remained unknown", err)
	}
	return got
}

func TestP08ScratchIdentityTracksFilesAndDeletedOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() p08ScratchSnapshot {
		t.Helper()
		return stableP08LocalSnapshot(t, dir)
	}
	before := snapshot()
	if err := p08ScratchReleased(before, snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if p08ScratchReleased(before, snapshot()) == nil {
		t.Fatal("rewritten file was accepted")
	}
	before = snapshot()
	if err := os.WriteFile(path, []byte("def"), 0600); err != nil {
		t.Fatal(err)
	}
	if p08ScratchReleased(before, snapshot()) == nil {
		t.Fatal("same-size changed file was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if p08ScratchReleased(before, snapshot()) == nil {
		t.Fatal("new file with matching size was accepted")
	}
}

func TestP08ScratchUnknownProcFails(t *testing.T) {
	if _, err := p08ScratchLocalSnapshot(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing proc accepted")
	}
	if p08ScratchReleased(p08ScratchSnapshot{}, p08ScratchSnapshot{}) == nil {
		t.Fatal("unknown snapshots accepted")
	}
	if _, err := parseP08Scratch(nil); err == nil {
		t.Fatal("incomplete observation accepted")
	}
}

func TestP08ScratchRejectsNestedMount(t *testing.T) {
	root := t.TempDir()
	proc := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proc, "123", "fd"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "self"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "123", "task", "123"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "123", "task", "123", "children"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	stat := "123 (server) S 0" + strings.Repeat(" 0", 17) + " 12345\n"
	if err := os.WriteFile(filepath.Join(proc, "123", "stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", filepath.Join(proc, "123", "fd", "0")); err != nil {
		t.Fatal(err)
	}
	mountinfo := filepath.Join(proc, "self", "mountinfo")
	observe := func() error {
		return exec.CommandContext(t.Context(), "sh", "-eu", "-c", p08ScratchProbe, "p08-scratch", root, proc, "123").Run()
	}
	if err := os.WriteFile(mountinfo, []byte("10 1 0:1 / / rw - tmpfs tmpfs rw\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := observe(); err != nil {
		t.Fatalf("plain scratch observation failed: %v", err)
	}
	entry := fmt.Sprintf("11 10 0:2 / %s/nested rw - tmpfs tmpfs rw\n", root)
	if err := os.WriteFile(mountinfo, []byte(entry), 0600); err != nil {
		t.Fatal(err)
	}
	if err := observe(); err == nil {
		t.Fatal("scratch observation accepted an unexamined nested mount")
	}
}

func TestP08ScratchScopesProcessObservationToOwnedTree(t *testing.T) {
	for _, fault := range []string{"none", "unrelated-vanished", "selected-child-missing", "wrong-parent", "missing-task", "changed-owner", "oversized-children", "process-cycle"} {
		t.Run(fault, func(t *testing.T) {
			root, proc := t.TempDir(), t.TempDir()
			write := func(path, value string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			process := func(pid, parent int) {
				t.Helper()
				dir := filepath.Join(proc, fmt.Sprint(pid))
				write(filepath.Join(dir, "stat"), fmt.Sprintf("%d (fixture) S %d%s 12345\n", pid, parent, strings.Repeat(" 0", 17)))
				write(filepath.Join(dir, "task", fmt.Sprint(pid), "children"), "")
				if err := os.MkdirAll(filepath.Join(dir, "fd"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/dev/null", filepath.Join(dir, "fd", "0")); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(proc, "self", "mountinfo"), "10 1 0:1 / / rw - tmpfs tmpfs rw\n")
			process(100, 0)
			// A non-leader thread owns the child; the child owns a grandchild.
			write(filepath.Join(proc, "100", "task", "101", "children"), "102 ")
			if fault != "selected-child-missing" {
				parent := 100
				if fault == "wrong-parent" {
					parent = 0
				}
				process(102, parent)
				write(filepath.Join(proc, "102", "task", "102", "children"), "103 ")
				process(103, 102)
			}
			if fault == "unrelated-vanished" {
				if err := os.MkdirAll(filepath.Join(proc, "900"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(proc, "vanished"), filepath.Join(proc, "900", "stat")); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "missing-task" {
				path := filepath.Join(proc, "102", "task", "102", "children")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "oversized-children" {
				write(filepath.Join(proc, "100", "task", "101", "children"), strings.Repeat("1 ", 32769))
			}
			if fault == "process-cycle" {
				write(filepath.Join(proc, "103", "task", "103", "children"), "100 ")
			}
			if fault == "changed-owner" {
				realFind, err := exec.LookPath("find")
				if err != nil {
					t.Fatal("scratch fixture requires find")
				}
				bin := t.TempDir()
				write(filepath.Join(bin, "find"), "#!/bin/sh\nprintf '%s\\n' \"$ACCEPTANCE_CHANGED_STAT\" > \"$ACCEPTANCE_OWNER_STAT\"\nexec \"$ACCEPTANCE_REAL_FIND\" \"$@\"\n")
				if err := os.Chmod(filepath.Join(bin, "find"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("ACCEPTANCE_REAL_FIND", realFind)
				t.Setenv("ACCEPTANCE_OWNER_STAT", filepath.Join(proc, "100", "stat"))
				t.Setenv("ACCEPTANCE_CHANGED_STAT", "100 (fixture) S 0"+strings.Repeat(" 0", 17)+" 67890")
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			data, err := exec.CommandContext(ctx, "sh", "-eu", "-c", p08ScratchProbe, "p08-scratch", root, proc, "100").Output()
			if err == nil {
				_, err = parseP08Scratch(data)
			}
			valid := fault == "none" || fault == "unrelated-vanished"
			if (err == nil) != valid {
				t.Fatalf("selected process observation incorrect: valid=%t observed=%t", valid, err == nil)
			}
		})
	}
}

func TestP08ScratchReleasePolling(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := waitP08ScratchRelease(ctx, func(context.Context) error {
		calls++
		if calls < 2 {
			return errors.New("temporarily unknown")
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("retry did not converge: %v (%d)", err, calls)
	}
	expired, stop := context.WithCancel(context.Background())
	stop()
	if waitP08ScratchRelease(expired, func(context.Context) error { return nil }) == nil {
		t.Fatal("expired observation accepted")
	}
}

func TestP08ScratchIncludesOwnedChildDeletedFile(t *testing.T) {
	root := t.TempDir()
	before := stableP08LocalSnapshot(t, root)
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("ACCEPTANCE_SCRATCH_FILE", filepath.Join(root, "owned-child"))
	t.Setenv("ACCEPTANCE_SCRATCH_READY", ready)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	stopped := false
	t.Cleanup(func() {
		cancel()
		if !stopped {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("owned scratch child did not stop")
			}
		}
	})
	go func() {
		_, err := runKubernetesCommand(ctx, "sh", 10*time.Second, 1024, nil, "-eu", "-c", `exec 3>"$ACCEPTANCE_SCRATCH_FILE"; printf payload >&3; rm -- "$ACCEPTANCE_SCRATCH_FILE"; printf ready > "$ACCEPTANCE_SCRATCH_READY"; exec sleep 30`)
		done <- err
	}()
	deadline := time.After(2 * time.Second)
	for {
		if data, err := os.ReadFile(ready); err == nil && string(data) == "ready" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("owned child did not retain its deleted file")
		case <-time.After(5 * time.Millisecond):
		}
	}
	after := stableP08LocalSnapshot(t, root)
	if len(after.deleted) != 1 || p08ScratchReleased(before, after) == nil {
		t.Fatal("owned child's deleted-open scratch escaped observation")
	}
	cancel()
	select {
	case err := <-done:
		stopped = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal("scratch child cancellation was not preserved")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owned scratch child did not stop after observation")
	}
	if p08ScratchReleased(before, stableP08LocalSnapshot(t, root)) != nil {
		t.Fatal("terminated child's released scratch remained unavailable")
	}
}

func TestP08ScratchDeletedOpenFails(t *testing.T) {
	dir := t.TempDir()
	before := stableP08LocalSnapshot(t, dir)
	path := filepath.Join(dir, "open")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// The observer targets this process and its children in this CPU test.
	after := stableP08LocalSnapshot(t, dir)
	if p08ScratchReleased(before, after) == nil {
		t.Fatal("deleted-open scratch was accepted")
	}
	if p08ScratchReleased(after, after) == nil {
		t.Fatal("a pre-existing deleted-open scratch file was accepted")
	}
}
