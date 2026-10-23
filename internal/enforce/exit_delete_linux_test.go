//go:build linux

package enforce

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/policy"
)

const (
	exitDeleteChildEnv    = "AGENTTAINT_ENFORCE_EXIT_DELETE_CHILD"
	exitDeleteConnectEnv  = "AGENTTAINT_ENFORCE_EXIT_DELETE_CONNECT"
	exitDeleteAddressEnv  = "AGENTTAINT_ENFORCE_EXIT_DELETE_ADDRESS"
	exitDeletePortEnv     = "AGENTTAINT_ENFORCE_EXIT_DELETE_PORT"
	exitDeleteRequiredEnv = "AGENTTAINT_REQUIRE_ENFORCE_EXIT_DELETE"
	exitDeleteReadyMarker = "enforcement-exit-delete-ready"
)

var exitDeleteRetainedEnforcers []*Enforcer

func TestEnforceProcessExitDeleteTarget(t *testing.T) {
	if os.Getenv(exitDeleteChildEnv) != "1" {
		return
	}
	fmt.Println(exitDeleteReadyMarker)
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatalf("wait for parent to release exit target: %v", err)
	}
}

func TestEnforceProcessExitDeleteConnectTarget(t *testing.T) {
	if os.Getenv(exitDeleteConnectEnv) != "1" {
		return
	}
	address, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort(os.Getenv(exitDeleteAddressEnv), os.Getenv(exitDeletePortEnv)))
	if err != nil {
		t.Fatalf("resolve controlled receiver: %v", err)
	}
	socket, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("create IPv4 socket: %v", err)
	}
	connectErr := syscall.Connect(socket, &syscall.SockaddrInet4{Port: address.Port, Addr: [4]byte{127, 0, 0, 1}})
	_ = syscall.Close(socket)
	fmt.Printf("connect_errno=%d\n", saturationErrno(connectErr))
	if connectErr != syscall.EPERM {
		t.Fatalf("connect after enforcement-state cleanup failure was not denied: %v", connectErr)
	}
}

func TestEnforceProcessExitMapDeleteFailureFailsClosed(t *testing.T) {
	if os.Geteuid() != 0 {
		skipOrFailExitDeleteAcceptance(t, "requires root privileges and an active BPF-LSM to exercise enforcement map deletion failure")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-exit-delete-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	cgroupPath := filepath.Join("/sys/fs/cgroup", fmt.Sprintf("agenttaint-exit-delete-%d", os.Getpid()))
	if err := os.Mkdir(cgroupPath, 0o700); err != nil {
		skipOrFailExitDeleteAcceptance(t, fmt.Sprintf("cannot create a test-owned cgroup v2 scope: %v", err))
	}
	var cgroupDir *os.File
	var enforcer *Enforcer
	cleanup := func() error {
		var errs []error
		cgroupEmpty := true
		if cgroupPath != "" {
			control, err := os.OpenFile(filepath.Join(cgroupPath, "cgroup.kill"), os.O_WRONLY, 0)
			if err == nil {
				_, err = control.Write([]byte("1\n"))
				errs = append(errs, err, control.Close())
			} else if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			cgroupEmpty = waitForCgroupEmpty(cgroupPath, 5*time.Second)
			if !cgroupEmpty {
				errs = append(errs, errors.New("test cgroup did not become empty before cleanup deadline"))
			}
		}
		if enforcer != nil {
			if !cgroupEmpty {
				// Keep the hooks attached until this test process exits if a test
				// target could not be killed; closing them could leave it unguarded.
				exitDeleteRetainedEnforcers = append(exitDeleteRetainedEnforcers, enforcer)
			} else {
				errs = append(errs, enforcer.Close())
			}
			enforcer = nil
		}
		if cgroupDir != nil {
			errs = append(errs, cgroupDir.Close())
			cgroupDir = nil
		}
		if cgroupPath != "" {
			removeErr := os.Remove(cgroupPath)
			if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
				cgroupPath = ""
			} else {
				errs = append(errs, removeErr)
			}
		}
		return errors.Join(errs...)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Errorf("clean test-owned enforcement scope: %v", err)
		}
	}()
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
	enforcer, err = New(policy.KernelProjection{
		Sources: []string{"/__agenttaint_exit_delete_test_unused_source__"},
	}, stat.Ino)
	if err != nil {
		t.Fatalf("load enforcement hooks for exit-delete test: %v", err)
	}
	var zero uint32
	var fault uint64
	if err := enforcer.objects.EnforceFaultMap.Lookup(&zero, &fault); err != nil {
		t.Fatalf("read initial enforcement fault state: %v", err)
	}
	if fault != 0 {
		t.Fatalf("fresh enforcement scope started faulted: %#x", fault)
	}
	targetPath := copyEnforcementTestBinary(t, root)

	childCtx, cancelChild := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelChild()
	command := exec.CommandContext(childCtx, targetPath, "-test.run=^TestEnforceProcessExitDeleteTarget$")
	command.Env = append(os.Environ(), exitDeleteChildEnv+"=1")
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential:  &syscall.Credential{Uid: 65534, Gid: 65534, NoSetGroups: true},
		Setpgid:     true,
		Pdeathsig:   syscall.SIGKILL,
		UseCgroupFD: true,
		CgroupFD:    int(cgroupDir.Fd()),
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start protected exit-delete target: %v", err)
	}
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != exitDeleteReadyMarker {
		_ = command.Wait()
		t.Fatalf("wait for protected target readiness: line=%q err=%v", ready, err)
	}
	var processKey enforceEnforceProcessKey
	var processState enforceEnforceProcessState
	iterator := enforcer.objects.EnforceProcesses.Iterate()
	found := false
	for iterator.Next(&processKey, &processState) {
		if processKey.Tgid == uint32(command.Process.Pid) {
			found = true
			break
		}
	}
	if err := iterator.Err(); err != nil {
		cancelChild()
		_ = command.Wait()
		t.Fatalf("iterate enforcement state before injection: %v", err)
	}
	if !found {
		cancelChild()
		_ = command.Wait()
		t.Fatalf("process-exec hook did not create state for target pid %d", command.Process.Pid)
	}
	if err := enforcer.objects.EnforceProcesses.Delete(&processKey); err != nil {
		cancelChild()
		_ = command.Wait()
		t.Fatalf("remove target state before process exit: %v", err)
	}
	if _, err := io.WriteString(stdin, "exit\n"); err != nil {
		cancelChild()
		_ = command.Wait()
		t.Fatalf("release protected exit target: %v", err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("protected exit target failed: %v", err)
	}

	fault = 0
	if err := enforcer.objects.EnforceFaultMap.Lookup(&zero, &fault); err != nil {
		t.Fatalf("read enforcement fault state after failed process-state deletion: %v", err)
	}
	if fault == 0 {
		t.Fatal("missing process state at exit did not set the sticky enforcement fault")
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelConnect()
	command = exec.CommandContext(connectCtx, targetPath, "-test.run=^TestEnforceProcessExitDeleteConnectTarget$")
	command.Env = append(os.Environ(),
		exitDeleteConnectEnv+"=1",
		exitDeleteAddressEnv+"="+endpoint.IP.String(),
		exitDeletePortEnv+"="+fmt.Sprint(endpoint.Port),
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential:  &syscall.Credential{Uid: 65534, Gid: 65534, NoSetGroups: true},
		Setpgid:     true,
		Pdeathsig:   syscall.SIGKILL,
		UseCgroupFD: true,
		CgroupFD:    int(cgroupDir.Fd()),
	}
	var output strings.Builder
	command.Stdout = &output
	if err := command.Run(); err != nil {
		t.Fatalf("post-fault connect target failed: %v output=%q", err, output.String())
	}
	if !strings.Contains(output.String(), "connect_errno=1\n") {
		t.Fatalf("sticky enforcement fault did not deny subsequent connect: %q", output.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted a connection after enforcement-state deletion failed")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("post-fault receiver check: %v", acceptErr)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("clean test-owned enforcement scope: %v", err)
	}
	t.Logf("missing process state at exit set sticky enforcement fault=%#x; subsequent connect returned EPERM", fault)
}

func skipOrFailExitDeleteAcceptance(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv(exitDeleteRequiredEnv) == "1" {
		t.Fatalf("required enforcement exit-delete acceptance unavailable: %s", reason)
	}
	t.Skip(reason)
}

func waitForCgroupEmpty(cgroupPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(cgroupPath, "cgroup.events")); err == nil && cgroupIsEmpty(string(data)) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func copyEnforcementTestBinary(t *testing.T, directory string) string {
	t.Helper()
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(directory, "target")
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
	return targetPath
}
