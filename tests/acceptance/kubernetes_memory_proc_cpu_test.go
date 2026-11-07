package acceptance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func p08MemoryProcFixtureCommand(t *testing.T, proc, mount string) *exec.Cmd {
	t.Helper()
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("Perl unavailable for CPU fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)
	return exec.CommandContext(ctx, "perl", "-e", p08MemoryNativeProcProbe, proc)
}
func p08MemoryProcFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	mount := filepath.Join(proc, "self", "mountinfo")
	if err := os.MkdirAll(filepath.Join(proc, "self", "ns"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proc, "1", "ns"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pid:[1]", filepath.Join(proc, "self", "ns", "pid")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pid:[1]", filepath.Join(proc, "1", "ns", "pid")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "1", "comm"), []byte("init\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mount, []byte("12 1 0:1 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "self", "uid_map"), []byte("0 0 4294967295\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return proc, mount
}
func p08MemoryProcWrite(t *testing.T, proc, pid, comm string) {
	t.Helper()
	dir := filepath.Join(proc, pid)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestP08MemoryProcSkipsOnlyProvenDisappearedPID(t *testing.T) {
	proc, mount := p08MemoryProcFixture(t)
	p08MemoryProcWrite(t, proc, "200", "unrelated\n")
	p08MemoryProcWrite(t, proc, "300", "whisper-server\n")
	fifoDir := filepath.Join(proc, "100")
	if err := os.Mkdir(fifoDir, 0700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(fifoDir, "comm")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(proc, "200")
	file := filepath.Join(dir, "comm")
	if !filepath.IsAbs(proc) || filepath.Clean(dir) != proc+string(os.PathSeparator)+"200" || filepath.Clean(file) != dir+string(os.PathSeparator)+"comm" {
		t.Fatal("fixture paths unavailable")
	}
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			done <- err
			return
		}
		defer f.Close()
		if err = os.Remove(file); err == nil {
			err = os.Remove(dir)
		}
		if err == nil {
			_, err = f.WriteString("init\n")
		}
		done <- err
	}()
	command := p08MemoryProcFixtureCommand(t, proc, mount)
	out, err := command.Output()
	if err != nil {
		fd, e := syscall.Open(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e == nil {
			_ = syscall.Close(fd)
		}
	}
	writeErr := <-done
	if err != nil || writeErr != nil || string(out) != "OK\n300\nEND\n" {
		t.Fatalf("proven vanished unrelated process rejected: failed=%t writer_failed=%t", err != nil, writeErr != nil)
	}
}

func TestP08MemoryProcRejectsNonRootRuntime(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires non-root test process")
	}
	out, err := p08MemoryProcFixtureCommand(t, "/proc", "").Output()
	if err != nil || string(out) != "ERROR:guard\n" {
		t.Fatal("non-root driver was accepted")
	}
}

func TestP08MemoryProcRejectsUnverifiedMountOrUserNamespace(t *testing.T) {
	for _, tc := range []struct{ name, mount, uid string }{
		{"hidepid mount", "12 1 0:1 / /proc rw,hidepid=0 - proc proc rw\n", "0 0 4294967295\n"},
		{"hidepid super", "12 1 0:1 / /proc rw - proc proc rw,hidepid=invisible\n", "0 0 4294967295\n"},
		{"subtree mask", "12 1 0:1 / /proc rw - proc proc rw\n13 12 0:2 / /proc/123/comm rw - tmpfs tmpfs rw\n", "0 0 4294967295\n"},
		{"duplicate", "12 1 0:1 / /proc rw - proc proc rw\n13 1 0:2 / /proc rw - proc proc rw\n", "0 0 4294967295\n"},
		{"non-host uid mapping", "12 1 0:1 / /proc rw - proc proc rw\n", "0 100000 65536\n"},
		{"malformed", "invalid\n", "0 0 4294967295\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc, mount := p08MemoryProcFixture(t)
			p08MemoryProcWrite(t, proc, "300", "whisper-server\n")
			if os.WriteFile(mount, []byte(tc.mount), 0600) != nil || os.WriteFile(filepath.Join(filepath.Dir(mount), "uid_map"), []byte(tc.uid), 0600) != nil {
				t.Fatal("fixture unavailable")
			}
			out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output()
			if err != nil || !strings.HasPrefix(string(out), "ERROR:") {
				t.Fatal("unverified mount or namespace accepted")
			}
		})
	}
}

func TestP08MemoryProcRejectsPIDNamespaceMismatch(t *testing.T) {
	proc, mount := p08MemoryProcFixture(t)
	path := filepath.Join(proc, "1", "ns", "pid")
	if !filepath.IsAbs(proc) || filepath.Clean(path) != proc+string(os.PathSeparator)+"1/ns/pid" {
		t.Fatal("fixture path unavailable")
	}
	if os.Remove(path) != nil || os.Symlink("pid:[2]", path) != nil {
		t.Fatal("fixture setup unavailable")
	}
	out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output()
	if err != nil || string(out) != "ERROR:guard\n" {
		t.Fatal("mismatched PID namespace accepted")
	}
}

func TestP08MemoryProcUnreadableAndIncompleteFailClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("fixture requires non-root process to establish EACCES")
	}
	for _, tc := range []struct {
		name, comm string
		deny       bool
	}{
		{name: "unreadable existing", comm: "private\n", deny: true}, {name: "partial", comm: "whisper-server"}, {name: "extra lines", comm: "name\nextra\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proc, mount := p08MemoryProcFixture(t)
			p08MemoryProcWrite(t, proc, "200", tc.comm)
			p08MemoryProcWrite(t, proc, "300", "whisper-server\n")
			if tc.deny {
				path := filepath.Join(proc, "200", "comm")
				if os.Chmod(path, 0000) != nil {
					t.Fatal("fixture chmod failed")
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
			}
			if out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output(); err != nil || !strings.HasPrefix(string(out), "ERROR:") {
				t.Fatal("inaccessible or incomplete comm accepted")
			}
		})
	}
}

func TestP08MemoryProcSelectedDisappearsBeforeRead(t *testing.T) {
	proc, mount := p08MemoryProcFixture(t)
	p08MemoryProcWrite(t, proc, "300", "whisper-server\n")
	fifoDir := filepath.Join(proc, "100")
	if os.Mkdir(fifoDir, 0700) != nil {
		t.Fatal("fixture unavailable")
	}
	fifo := filepath.Join(fifoDir, "comm")
	if syscall.Mkfifo(fifo, 0600) != nil {
		t.Fatal("fixture unavailable")
	}
	dir, file := filepath.Join(proc, "300"), filepath.Join(proc, "300", "comm")
	if !filepath.IsAbs(proc) || filepath.Clean(dir) != proc+string(os.PathSeparator)+"300" || filepath.Clean(file) != dir+string(os.PathSeparator)+"comm" {
		t.Fatal("fixture paths unavailable")
	}
	done := make(chan error, 1)
	go func() {
		f, e := os.OpenFile(fifo, os.O_WRONLY, 0)
		if e != nil {
			done <- e
			return
		}
		defer f.Close()
		if e = os.Remove(file); e == nil {
			e = os.Remove(dir)
		}
		if e == nil {
			_, e = f.WriteString("init\n")
		}
		done <- e
	}()
	out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output()
	if err != nil {
		fd, e := syscall.Open(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if e == nil {
			_ = syscall.Close(fd)
		}
	}
	writerErr := <-done
	if err != nil || writerErr != nil || string(out) != "OK\nEND\n" {
		t.Fatal("fixture did not observe disappearance")
	}
	a := fakeCUDAObserver("good")
	base := a.observer.run
	a.observer.run = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "perl -e") {
			return out, nil
		}
		return base(ctx, args...)
	}
	if _, err = a.identity(t.Context()); err == nil || err.Error() != "CUDA identity: native_missing" {
		t.Fatal("selected disappeared native was accepted")
	}
}

func TestP08MemoryProcMissingCommWithExistingPIDFailsClosed(t *testing.T) {
	proc, mount := p08MemoryProcFixture(t)
	if err := os.Mkdir(filepath.Join(proc, "200"), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output()
	if err != nil || string(out) != "ERROR:unconfirmed\n" {
		t.Fatal("existing PID with missing comm was treated as disappeared")
	}
}

func TestP08MemoryProcPIDReuseFailsIdentityFence(t *testing.T) {
	proc, mount := p08MemoryProcFixture(t)
	p08MemoryProcWrite(t, proc, "101", "whisper-server\n")
	out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output()
	if err != nil || string(out) != "OK\n101\nEND\n" {
		t.Fatal("fixture enumeration unavailable")
	}
	a := fakeCUDAObserver("pid-reuse")
	base := a.observer.run
	a.observer.run = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "perl -e") {
			return out, nil
		}
		return base(ctx, args...)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := observeCUDAActivity(ctx, a, nil, time.Millisecond, 100*time.Millisecond, 2); err == nil {
		t.Fatal("reused PID crossed the start-time identity fence")
	}
}

func TestP08MemoryProcDuplicateAndSelectedDisappearanceFailIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs []string
		want    string
	}{{"selected missing", nil, "native_missing"}, {"duplicate", []string{"101", "102"}, "native_ambiguous"}} {
		t.Run(tc.name, func(t *testing.T) {
			proc, mount := p08MemoryProcFixture(t)
			for _, pid := range tc.outputs {
				p08MemoryProcWrite(t, proc, pid, "whisper-server\n")
			}
			out, err := p08MemoryProcFixtureCommand(t, proc, mount).Output()
			if err != nil {
				t.Fatal("fixture enumeration failed")
			}
			a := fakeCUDAObserver("good")
			base := a.observer.run
			a.observer.run = func(ctx context.Context, args ...string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), "perl -e") {
					return out, nil
				}
				return base(ctx, args...)
			}
			_, err = a.identity(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal("missing or duplicate selected native accepted")
			}
		})
	}
}
