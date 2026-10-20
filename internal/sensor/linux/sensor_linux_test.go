//go:build linux

package linux

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

func TestNewRejectsZeroScope(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatal("New accepted a zero scope ID")
	}
}

func TestFileAndConnectObserver(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirfd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(dirfd) })
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
			close(accepted)
		}
	}()

	var scopeBytes [8]byte
	if _, err := rand.Read(scopeBytes[:]); err != nil {
		t.Fatal(err)
	}
	scopeID := binary.LittleEndian.Uint64(scopeBytes[:])
	if scopeID == 0 {
		scopeID = 1
	}
	sensor, err := New(scopeID)
	if err != nil {
		t.Fatalf("load and attach operation observer: %v", err)
	}
	defer sensor.Close()
	if err := sensor.Barrier(5 * time.Second); err != nil {
		t.Fatalf("sensor barrier: %v", err)
	}
	if err := sensor.TrackProcess(uint32(os.Getpid())); err != nil {
		t.Fatalf("track observer test process: %v", err)
	}
	if _, err := sensor.DropCounts(); err != nil {
		t.Fatalf("read BPF drop counters: %v", err)
	}
	pid := uint32(os.Getpid())

	fd, err := unix.Openat(dirfd, "secret.txt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("openat fixture: %v", err)
	}
	_ = unix.Close(fd)
	file := readMatchingEvent(t, sensor, 10*time.Second, func(event RawProcessEvent) bool {
		return event.Kind == core.FileOpen && event.Process.PID == pid && event.Path == "secret.txt"
	})
	if !file.PathRelative || file.Access != core.AccessRead || file.OperationResult != core.ResultSuccess {
		t.Fatalf("openat event did not preserve observed intent/result: %+v", file)
	}
	if _, err := NormalizeEvent(file, "file-open", "run", time.Now()); err != nil {
		t.Fatalf("normalize file-open event: %v", err)
	}

	if _, err := unix.Openat(dirfd, "missing.txt", unix.O_RDONLY|unix.O_CLOEXEC, 0); err == nil {
		t.Fatal("openat missing fixture unexpectedly succeeded")
	}
	failed := readMatchingEvent(t, sensor, 10*time.Second, func(event RawProcessEvent) bool {
		return event.Kind == core.FileOpen && event.Process.PID == pid && event.Path == "missing.txt"
	})
	if failed.OperationResult != core.ResultFailure {
		t.Fatalf("failed openat result was not preserved: %+v", failed)
	}

	how := &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC)}
	fd, err = unix.Openat2(dirfd, "secret.txt", how)
	if err != nil {
		t.Fatalf("openat2 fixture: %v", err)
	}
	_ = unix.Close(fd)
	openat2 := readMatchingEvent(t, sensor, 10*time.Second, func(event RawProcessEvent) bool {
		return event.Kind == core.FileOpen && event.Process.PID == pid && event.Path == "secret.txt"
	})
	if openat2.OperationResult != core.ResultSuccess {
		t.Fatalf("openat2 result was not preserved: %+v", openat2)
	}

	sockfd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockfd)
	sockaddr := &unix.SockaddrInet4{Port: address.Port}
	copy(sockaddr.Addr[:], address.IP.To4())
	if err := unix.Connect(sockfd, sockaddr); err != nil {
		t.Fatalf("connect local IPv4 listener: %v", err)
	}
	connected := readMatchingEvent(t, sensor, 10*time.Second, func(event RawProcessEvent) bool {
		return event.Kind == core.NetworkConnect && event.Process.PID == pid && event.Port == uint16(address.Port)
	})
	if connected.Address != [4]byte{127, 0, 0, 1} || connected.OperationResult != core.ResultSuccess {
		t.Fatalf("successful IPv4 connect not preserved: %+v", connected)
	}
	if _, err := NormalizeEvent(connected, "network-connect", "run", time.Now()); err != nil {
		t.Fatalf("normalize network-connect event: %v", err)
	}
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("listener did not accept the test connection")
	}

	closed, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := uint16(closed.Addr().(*net.TCPAddr).Port)
	_ = closed.Close()
	failfd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(failfd)
	failedAddr := &unix.SockaddrInet4{Port: int(closedPort)}
	copy(failedAddr.Addr[:], []byte{127, 0, 0, 1})
	if err := unix.Connect(failfd, failedAddr); err == nil {
		t.Fatal("connect to closed local port unexpectedly succeeded")
	}
	connectFailure := readMatchingEvent(t, sensor, 10*time.Second, func(event RawProcessEvent) bool {
		return event.Kind == core.NetworkConnect && event.Process.PID == pid && event.Port == closedPort
	})
	if connectFailure.OperationResult != core.ResultFailure {
		t.Fatalf("failed IPv4 connect result was not preserved: %+v", connectFailure)
	}
}

func readMatchingEvent(t *testing.T, sensor *Sensor, timeout time.Duration, match func(RawProcessEvent) bool) RawProcessEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		event := readProcessEvent(t, sensor, time.Until(deadline))
		if match(event) {
			return event
		}
	}
	t.Fatal("timed out waiting for matching sensor event")
	return RawProcessEvent{}
}

func TestProcessObserverThreadLifecycle(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("requires clang to compile the temporary pthread fixture")
	}
	fixture := filepath.Join(t.TempDir(), "thread-lifecycle")
	source := `#include <pthread.h>
#include <string.h>
#include <unistd.h>

static void *worker(void *arg) {
	const char *mode = arg;
	if (strcmp(mode, "exec") == 0) {
		char *args[] = {"/bin/true", 0};
		execv(args[0], args);
		_exit(101);
	}
	return 0;
}

int main(int argc, char **argv) {
	pthread_t thread;
	const char *mode = argc > 1 ? argv[1] : "exit";
	if (pthread_create(&thread, 0, worker, (void *)mode) != 0)
		return 102;
	if (pthread_join(thread, 0) != 0)
		return 103;
	return 0;
}
`
	sourcePath := fixture + ".c"
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	compile := exec.Command(clang, "-O2", "-pthread", sourcePath, "-o", fixture)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile pthread fixture: %v\n%s", err, output)
	}

	sensor, err := New(0xA17A)
	if err != nil {
		t.Fatalf("load and attach process observer: %v", err)
	}
	t.Cleanup(func() { _ = sensor.Close() })
	if err := sensor.TrackProcess(uint32(os.Getpid())); err != nil {
		t.Fatalf("track process observer test root: %v", err)
	}

	for _, mode := range []string{"exit", "exec"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(fixture, mode)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			pid := uint32(cmd.Process.Pid)
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			waited := false
			t.Cleanup(func() {
				if !waited {
					_ = cmd.Process.Kill()
					<-wait
				}
			})

			var birthID uint64
			var forked, initialExec, secondaryExec, exited bool
			expectedBinary, err := filepath.Abs(fixture)
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) && !exited {
				event := readProcessEvent(t, sensor, time.Until(deadline))
				if event.Kind == core.ProcessFork {
					normalized, err := NormalizeEvent(event, "fork-event", "run", time.Now())
					if err != nil {
						t.Fatalf("normalize process fork: %v", err)
					}
					encoded, err := json.Marshal(normalized)
					if err != nil {
						t.Fatalf("marshal process fork: %v", err)
					}
					if _, err := core.DecodeEvent(encoded); err != nil {
						t.Fatalf("core.DecodeEvent rejected observed fork %s: %v", encoded, err)
					}
				}
				if event.Kind == "process_fork" && event.Child.PID == pid {
					forked = true
					birthID = event.Child.BirthID
				}
				if event.Process.PID != pid {
					continue
				}
				if birthID == 0 {
					birthID = event.Process.BirthID
				}
				if event.Process.BirthID != birthID {
					t.Fatalf("birth identity changed for PID %d: got %d, want %d", pid, event.Process.BirthID, birthID)
				}
				if event.Kind == "process_exec" && event.Path == expectedBinary {
					initialExec = true
				}
				if event.Kind == "process_exec" && event.Path == "/bin/true" {
					secondaryExec = true
				}
				if event.Kind == "process_exit" {
					exited = true
				}
			}
			if !forked || !initialExec || !exited || (mode == "exec" && !secondaryExec) {
				t.Fatalf("missing lifecycle events: fork=%t initial_exec=%t secondary_exec=%t exit=%t", forked, initialExec, secondaryExec, exited)
			}
			if err := <-wait; err != nil {
				t.Fatalf("fixture %s: %v", mode, err)
			}
			waited = true
			if err := sensor.objects.ProcessBirths.Lookup(&pid, new(uint64)); !errors.Is(err, ebpf.ErrKeyNotExist) {
				t.Fatalf("process birth map retained exited PID %d: %v", pid, err)
			}
		})
	}
	if err := sensor.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sensor.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func readProcessEvent(t *testing.T, sensor *Sensor, timeout time.Duration) RawProcessEvent {
	t.Helper()
	if err := sensor.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set process event read deadline: %v", err)
	}
	defer sensor.SetReadDeadline(time.Time{})
	event, err := sensor.Read()
	if err != nil {
		drops, dropsErr := sensor.DropCounts()
		t.Fatalf("read process event (drops=%v drops_error=%v): %v", drops, dropsErr, err)
	}
	return event
}

func TestRawNormalizationSeparatesPIDBirths(t *testing.T) {
	first := validRawRecord(binary.LittleEndian, rawKindExit)
	second := append([]byte(nil), first...)
	binary.LittleEndian.PutUint64(second[rawHeaderSize+16:rawHeaderSize+24], 78)
	firstRaw, err := DecodeRawProcessEvent(first)
	if err != nil {
		t.Fatal(err)
	}
	secondRaw, err := DecodeRawProcessEvent(second)
	if err != nil {
		t.Fatal(err)
	}
	firstEvent, err := NormalizeProcessEvent(firstRaw, "event-a", "run-a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	secondEvent, err := NormalizeProcessEvent(secondRaw, "event-b", "run-a", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	firstKey := firstEvent.(*core.ProcessExitEvent).Metadata.Process.ProcessKey
	secondKey := secondEvent.(*core.ProcessExitEvent).Metadata.Process.ProcessKey
	if firstKey == secondKey {
		t.Fatal("same PID with different birth IDs normalized to the same process key")
	}
}
