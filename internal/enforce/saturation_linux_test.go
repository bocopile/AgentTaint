//go:build linux

package enforce

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/policy"
	"github.com/cilium/ebpf"
)

const (
	saturationChildEnv    = "AGENTTAINT_ENFORCE_SATURATION_CHILD"
	saturationSourceEnv   = "AGENTTAINT_ENFORCE_SATURATION_SOURCE"
	saturationAddressEnv  = "AGENTTAINT_ENFORCE_SATURATION_ADDRESS"
	saturationPortEnv     = "AGENTTAINT_ENFORCE_SATURATION_PORT"
	saturationSourceText  = "synthetic saturated source\n"
	saturationChildTarget = "TestEnforceMapSaturationTarget"
)

func TestEnforceMapSaturationTarget(t *testing.T) {
	if os.Getenv(saturationChildEnv) != "1" {
		return
	}
	fd, openErr := syscall.Open(os.Getenv(saturationSourceEnv), syscall.O_RDONLY, 0)
	if openErr == nil {
		_ = syscall.Close(fd)
	}
	address, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort(os.Getenv(saturationAddressEnv), os.Getenv(saturationPortEnv)))
	if err != nil {
		t.Fatalf("resolve controlled receiver: %v", err)
	}
	socket, socketErr := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if socketErr != nil {
		t.Fatalf("create IPv4 socket: %v", socketErr)
	}
	connectErr := syscall.Connect(socket, &syscall.SockaddrInet4{Port: address.Port, Addr: [4]byte{127, 0, 0, 1}})
	_ = syscall.Close(socket)
	fmt.Printf("open_errno=%d connect_errno=%d\n", saturationErrno(openErr), saturationErrno(connectErr))
	if openErr != syscall.EPERM || connectErr != syscall.EPERM {
		t.Fatalf("saturated enforcement state was not fail-closed: open=%v connect=%v", openErr, connectErr)
	}
}

func TestEnforceProcessMapSaturationFailsClosed(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges and an active BPF-LSM to exercise enforcement map saturation")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-map-saturation-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(source, []byte(saturationSourceText), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	cgroupPath := filepath.Join("/sys/fs/cgroup", fmt.Sprintf("agenttaint-enforce-saturation-%d", os.Getpid()))
	if err := os.Mkdir(cgroupPath, 0o700); err != nil {
		t.Skipf("cannot create a test-owned cgroup v2 scope: %v", err)
	}
	var cgroupDir *os.File
	var enforcer *Enforcer
	cleanup := func() error {
		var errs []error
		if cgroupPath != "" {
			control, err := os.OpenFile(filepath.Join(cgroupPath, "cgroup.kill"), os.O_WRONLY, 0)
			if err == nil {
				_, err = control.Write([]byte("1\n"))
				errs = append(errs, err, control.Close())
			} else if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if data, readErr := os.ReadFile(filepath.Join(cgroupPath, "cgroup.events")); readErr == nil && cgroupIsEmpty(string(data)) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		if enforcer != nil {
			errs = append(errs, enforcer.Close())
			enforcer = nil
		}
		if cgroupDir != nil {
			errs = append(errs, cgroupDir.Close())
			cgroupDir = nil
		}
		if cgroupPath != "" {
			errs = append(errs, os.Remove(cgroupPath))
			cgroupPath = ""
		}
		return errors.Join(errs...)
	}
	defer func() { _ = cleanup() }()
	cgroupDir, err = os.Open(cgroupPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := cgroupDir.Stat()
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino == 0 {
		t.Fatal("kernel did not provide a usable cgroup v2 ID")
	}
	projection := policy.KernelProjection{
		Sources: []string{source},
		Denies:  []policy.KernelRule{{Address: [4]byte{127, 0, 0, 0}, PrefixLen: 8, AnyPort: true}},
	}
	enforcer, err = New(projection, stat.Ino)
	if err != nil {
		t.Fatalf("load enforcement hooks for saturation test: %v", err)
	}
	mapInfo, err := enforcer.objects.EnforceProcesses.Info()
	if err != nil {
		t.Fatal(err)
	}
	if mapInfo.MaxEntries < 2 {
		t.Fatalf("unexpected enforcement process map capacity: %d", mapInfo.MaxEntries)
	}
	for i := uint32(0); i < mapInfo.MaxEntries; i++ {
		key := enforceEnforceProcessKey{Tgid: i + 100000, BirthId: uint64(i) + 1}
		value := enforceEnforceProcessState{}
		if err := enforcer.objects.EnforceProcesses.Update(&key, &value, ebpf.UpdateNoExist); err != nil {
			t.Fatalf("fill process map at entry %d/%d: %v", i, mapInfo.MaxEntries, err)
		}
	}
	overflowKey := enforceEnforceProcessKey{Tgid: 3000000000, BirthId: ^uint64(0)}
	if err := enforcer.objects.EnforceProcesses.Update(&overflowKey, &enforceEnforceProcessState{}, ebpf.UpdateNoExist); err == nil {
		t.Fatal("process map accepted an entry beyond its declared capacity")
	}

	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "target")
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	_, copyErr := output.ReadFrom(input)
	closeInputErr := input.Close()
	closeOutputErr := output.Close()
	if err := errors.Join(copyErr, closeInputErr, closeOutputErr); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(targetPath, "-test.run=^"+saturationChildTarget+"$")
	command.Env = append(os.Environ(),
		saturationChildEnv+"=1",
		saturationSourceEnv+"="+source,
		saturationAddressEnv+"="+endpoint.IP.String(),
		saturationPortEnv+"="+fmt.Sprint(endpoint.Port),
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential:  &syscall.Credential{Uid: 65534, Gid: 65534, NoSetGroups: true},
		Setpgid:     true,
		Pdeathsig:   syscall.SIGKILL,
		UseCgroupFD: true,
		CgroupFD:    int(cgroupDir.Fd()),
	}
	var stdout, stderr strings.Builder
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start protected saturation target: %v", err)
	}
	_ = cgroupDir.Close()
	cgroupDir = nil
	if err := command.Wait(); err != nil {
		t.Fatalf("target did not complete its fail-closed checks: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "open_errno=1 connect_errno=1\n") {
		t.Fatalf("saturated source-state insertion did not deny both source access and later connect: %q stderr=%q", stdout.String(), stderr.String())
	}
	var zero uint32
	var fault uint64
	if err := enforcer.objects.EnforceFaultMap.Lookup(&zero, &fault); err != nil {
		t.Fatalf("read enforcement fault state after map saturation: %v", err)
	}
	if fault == 0 {
		t.Fatal("source-state map saturation did not set the sticky enforcement fault")
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted a connection after enforcement state saturation")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("saturation receiver check: %v", acceptErr)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("clean test-owned saturated enforcement scope: %v", err)
	}
	t.Logf("enforcement process map saturated at %d entries; source open and subsequent IPv4 connect returned EPERM; sticky fault=%#x", mapInfo.MaxEntries, fault)
}

func saturationErrno(err error) int {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return int(errno)
	}
	return -1
}

func cgroupIsEmpty(events string) bool {
	for _, line := range strings.Split(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "populated" {
			return fields[1] == "0"
		}
	}
	return false
}
