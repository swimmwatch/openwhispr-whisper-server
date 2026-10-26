package backend

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"golang.org/x/sys/unix"
)

func retiredReusePortConnection(t *testing.T, address string) {
	t.Helper()
	listener, err := nativeReusePortListener(address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTimeout("tcp4", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if _, err := client.Read(data[:]); !errors.Is(err, io.EOF) {
		t.Fatal("native-compatible active close was not observed")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

// cpp-httplib 0.20.0 in the pinned unchanged server chooses SO_REUSEPORT,
// not SO_REUSEADDR, on Linux. Match that external listener policy faithfully.
func nativeReusePortListener(address string) (net.Listener, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optionErr error
		err := raw.Control(func(fd uintptr) {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 0); err != nil {
				optionErr = err
				return
			}
			optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		})
		if err != nil {
			return err
		}
		return optionErr
	}}
	config.SetMultipathTCP(false)
	return config.Listen(context.Background(), "tcp4", address)
}

func serveReusePortNative(address string, handler http.Handler) error {
	listener, err := nativeReusePortListener(address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-closed:
		}
	}()
	err = server.Serve(listener)
	close(closed)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// A crashed native peer first sends FIN. Resetting our owned connection from
// CLOSE_WAIT must abort the peer's FIN_WAIT state, not assassinate TIME_WAIT.
// Thus this does not depend on the host's tcp_rfc1337 TIME_WAIT setting.
func TestNativeTransportCloseAfterPeerFINResetsWithoutTimeWait(t *testing.T) {
	opts, _, _ := supervisorOptions(t)
	listener, err := nativeReusePortListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	opts.NativePort = listener.Addr().(*net.TCPAddr).Port
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := s.transport.DialContext(ctx, "tcp4", s.nativeAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := peer.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	tcp, ok := peer.(*net.TCPConn)
	if !ok {
		t.Fatal("native fixture did not accept TCP")
	}
	if err := tcp.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if _, err := connection.Read(data[:]); !errors.Is(err, io.EOF) {
		t.Fatal("native peer FIN was not observed")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Read(data[:]); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatal("owned native close sent FIN instead of aborting the peer FIN_WAIT state")
	}
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	exclusive, err := net.Listen("tcp4", s.nativeAddress())
	if err != nil {
		t.Fatal("owned native close left an unrebindable endpoint")
	}
	exclusive.Close()
}

func TestRecoveryClosesNativeIdleHealthBeforeStop(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "crash"} {
		t.Run(mode, func(t *testing.T) {
			opts, root, _ := supervisorOptions(t)
			opts.CleanupTimeout = 2 * time.Second
			if err := os.WriteFile(filepath.Join(root, "mode"), []byte("reuse-port"), 0600); err != nil {
				t.Fatal(err)
			}
			s, runResult := runningSupervisorWithResult(t, opts)
			first, _ := s.Health(context.Background())
			old := starts(t, root)[0]
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
				defer cancel()
			}
			result := make(chan error, 1)
			request := requestFor(s, "block")
			go func() { _, err := s.Infer(ctx, request); result <- err }()
			await(t, func() bool { files, _ := filepath.Glob(filepath.Join(root, "active-*")); return len(files) == 1 })
			// Observe a health response while inference owns another connection. Its
			// response body closes normally, leaving a real idle native keepalive socket.
			s.mu.Lock()
			child := s.current
			s.mu.Unlock()
			if err := s.checkHealth(context.Background(), child); err != nil {
				t.Fatal("cannot seed native idle health connection", err)
			}
			if mode == "crash" {
				if err := syscall.Kill(child.cmd.Process.Pid, syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
			} else if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-result:
				kind := domain.DeadlineExceeded
				if mode == "crash" {
					kind = domain.BackendUnavailable
				}
				requireKind(t, err, kind)
			case <-time.After(opts.CleanupTimeout + time.Second):
				t.Fatal("interrupted native operation exceeded cleanup bounds")
			}
			deadline := time.NewTimer(4 * time.Second)
			defer deadline.Stop()
			for {
				h, _ := s.Health(context.Background())
				if h.Status == domain.BackendReady && h.ProcessGeneration != first.ProcessGeneration {
					break
				}
				select {
				case err := <-runResult:
					t.Fatalf("native-compatible recovery terminated after worker stop (address_in_use=%t): %v", errors.Is(err, syscall.EADDRINUSE), err)
				case <-deadline.C:
					t.Fatal("native-compatible recovery never became ready")
				case <-time.After(5 * time.Millisecond):
				}
			}
			pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(old), "start-"))
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("old native process was not reaped before recovery")
			}
			if _, err := s.Infer(context.Background(), requestFor(s, "")); err != nil {
				t.Fatal("fresh inference failed after native socket recovery", err)
			}
			if len(starts(t, root)) != 2 {
				t.Fatal("native recovery restarted or retried work unexpectedly")
			}
		})
	}
}

func TestNativePortGuardRejectsForeignSocketOwners(t *testing.T) {
	for _, mode := range []string{"ordinary-listener", "reuse-port-listener", "bound-not-listening", "mixed-timewait-bound"} {
		t.Run(mode, func(t *testing.T) {
			opts, root, _ := supervisorOptions(t)
			if err := os.WriteFile(filepath.Join(root, "mode"), []byte("reuse-port"), 0600); err != nil {
				t.Fatal(err)
			}
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.NativePort))
			if mode == "bound-not-listening" || mode == "mixed-timewait-bound" {
				if mode == "mixed-timewait-bound" {
					retiredReusePortConnection(t, address)
				}
				fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
					t.Fatal(err)
				}
				if err := unix.Bind(fd, &unix.SockaddrInet4{Port: opts.NativePort, Addr: [4]byte{127, 0, 0, 1}}); err != nil {
					t.Fatal(err)
				}
			} else {
				var listener net.Listener
				var err error
				if mode == "ordinary-listener" {
					listener, err = net.Listen("tcp4", address)
				} else {
					listener, err = nativeReusePortListener(address)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			}
			s, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = s.Run(ctx)
			if len(starts(t, root)) != 0 {
				t.Fatal("native model launched despite a foreign socket owner")
			}
			if !errors.Is(err, syscall.EADDRINUSE) {
				t.Fatal("foreign native-address owner was not rejected", err)
			}
		})
	}
}
