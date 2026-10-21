package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
	"github.com/bocopile/AgentTaint/internal/taint"
	"golang.org/x/sys/unix"
)

const (
	blockChildEnv   = "AGENTTAINT_BLOCK_CHILD"
	blockSourceEnv  = "AGENTTAINT_BLOCK_SOURCE"
	blockAddressEnv = "AGENTTAINT_BLOCK_ADDRESS"
	blockPortEnv    = "AGENTTAINT_BLOCK_PORT"
	blockExpectEnv  = "AGENTTAINT_BLOCK_EXPECT"
	blockWorkersEnv = "AGENTTAINT_BLOCK_WORKERS"
	blockRaceEnv    = "AGENTTAINT_BLOCK_RACE"
)

// TestKernelBlockChild is re-executed by the enforcement integration test as
// the unprivileged target process. The parent test filters it out.
func TestKernelBlockChild(t *testing.T) {
	if os.Getenv(blockChildEnv) != "1" {
		return
	}

	path := os.Getenv(blockSourceEnv)
	fd, openErr := syscall.Open(path, syscall.O_RDONLY, 0)
	if openErr == nil {
		_ = syscall.Close(fd)
	}
	if os.Getenv(blockWorkersEnv) != "" {
		os.Exit(runConcurrentBlockChild(openErr))
	}
	if os.Getenv(blockExpectEnv) == "file-deny" {
		_, _ = fmt.Fprintf(os.Stdout, "open_errno=%d\n", blockErrno(openErr))
		if openErr != syscall.EPERM {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv(blockExpectEnv) == "source-open-fails" && openErr == nil {
		_, _ = fmt.Fprintf(os.Stdout, "open_errno=0 expected_source_open_failure\n")
		os.Exit(1)
	}
	address, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort(os.Getenv(blockAddressEnv), os.Getenv(blockPortEnv)))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "open_errno=%d resolve_error=%q\n", blockErrno(openErr), err)
		os.Exit(2)
	}
	socket, socketErr := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if socketErr != nil {
		_, _ = fmt.Fprintf(os.Stdout, "open_errno=%d socket_errno=%d\n", blockErrno(openErr), blockErrno(socketErr))
		os.Exit(2)
	}
	connectErr := syscall.Connect(socket, &syscall.SockaddrInet4{
		Port: address.Port,
		Addr: [4]byte{127, 0, 0, 1},
	})
	_ = syscall.Close(socket)
	_, _ = fmt.Fprintf(os.Stdout, "open_errno=%d connect_errno=%d\n", blockErrno(openErr), blockErrno(connectErr))
	want := "allow"
	if os.Getenv(blockExpectEnv) == "deny" || os.Getenv(blockExpectEnv) == "source-open-fails" {
		want = "deny"
	}
	if openErr != nil || (want == "deny" && connectErr != syscall.EPERM) || (want == "allow" && connectErr != nil) {
		os.Exit(1)
	}
	os.Exit(0)
}

func runConcurrentBlockChild(openErr error) int {
	workers, err := strconv.Atoi(os.Getenv(blockWorkersEnv))
	if err != nil || workers < 2 || workers > 64 {
		_, _ = fmt.Fprintf(os.Stdout, "invalid_worker_count=%q\n", os.Getenv(blockWorkersEnv))
		return 2
	}
	address, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort(os.Getenv(blockAddressEnv), os.Getenv(blockPortEnv)))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "resolve_error=%q\n", err)
		return 2
	}
	runtime.GOMAXPROCS(workers)
	ready := sync.WaitGroup{}
	ready.Add(workers)
	start := make(chan struct{})
	var allowed unix.CPUSet
	if err := unix.SchedGetaffinity(0, &allowed); err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "get_affinity_error=%q\n", err)
		return 2
	}
	cpus := make([]int, 0, allowed.Count())
	for cpu := 0; cpu < len(allowed)*64; cpu++ {
		if allowed.IsSet(cpu) {
			cpus = append(cpus, cpu)
		}
	}
	if len(cpus) < 2 {
		_, _ = fmt.Fprintf(os.Stdout, "available_cpus=%d need_at_least=2\n", len(cpus))
		return 2
	}
	type workerResult struct {
		err      error
		cpu      int
		setupErr error
	}
	results := make(chan workerResult, workers)
	for worker := range workers {
		cpu := cpus[worker%len(cpus)]
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			var original unix.CPUSet
			if err := unix.SchedGetaffinity(0, &original); err != nil {
				ready.Done()
				results <- workerResult{setupErr: err, cpu: cpu}
				return
			}
			defer unix.SchedSetaffinity(0, &original)
			var pinned unix.CPUSet
			pinned.Set(cpu)
			if err := unix.SchedSetaffinity(0, &pinned); err != nil {
				ready.Done()
				results <- workerResult{setupErr: err, cpu: cpu}
				return
			}
			ready.Done()
			<-start
			fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
			if err != nil {
				results <- workerResult{err: err, cpu: cpu}
				return
			}
			err = syscall.Connect(fd, &syscall.SockaddrInet4{Port: address.Port, Addr: [4]byte{127, 0, 0, 1}})
			_ = syscall.Close(fd)
			results <- workerResult{err: err, cpu: cpu}
		}()
	}
	ready.Wait()
	close(start)
	denied, connected, unexpected := 0, 0, 0
	usedCPUs := make(map[int]struct{}, len(cpus))
	for range workers {
		result := <-results
		usedCPUs[result.cpu] = struct{}{}
		if result.setupErr != nil {
			_, _ = fmt.Fprintf(os.Stdout, "worker_cpu=%d setup_error=%q\n", result.cpu, result.setupErr)
			return 2
		}
		switch result.err {
		case syscall.EPERM:
			denied++
		case nil:
			connected++
		default:
			unexpected++
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "open_errno=%d workers=%d cpus=%d denied=%d connected=%d unexpected=%d\n", blockErrno(openErr), workers, len(usedCPUs), denied, connected, unexpected)
	if openErr != nil || denied != workers || connected != 0 || unexpected != 0 || len(usedCPUs) < 2 {
		return 1
	}
	return 0
}

func TestKernelBlockSourceConnectRaceChild(t *testing.T) {
	if os.Getenv(blockRaceEnv) != "1" {
		return
	}
	var allowed unix.CPUSet
	if err := unix.SchedGetaffinity(0, &allowed); err != nil {
		t.Fatalf("inspect target CPU affinity: %v", err)
	}
	cpus := make([]int, 0, allowed.Count())
	for cpu := 0; cpu < len(allowed)*64; cpu++ {
		if allowed.IsSet(cpu) {
			cpus = append(cpus, cpu)
		}
	}
	if len(cpus) < 2 {
		t.Fatalf("source/connect race fixture needs two allowed CPUs, found %d", len(cpus))
	}
	address, err := net.ResolveTCPAddr("tcp4", net.JoinHostPort(os.Getenv(blockAddressEnv), os.Getenv(blockPortEnv)))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		err error
		cpu int
	}
	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(2)
	sourceResult := make(chan result, 1)
	connectResult := make(chan result, 1)
	worker := func(cpu int, run func() error, results chan<- result) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var original unix.CPUSet
		if err := unix.SchedGetaffinity(0, &original); err != nil {
			ready.Done()
			results <- result{err: err, cpu: cpu}
			return
		}
		defer unix.SchedSetaffinity(0, &original)
		var pinned unix.CPUSet
		pinned.Set(cpu)
		if err := unix.SchedSetaffinity(0, &pinned); err != nil {
			ready.Done()
			results <- result{err: err, cpu: cpu}
			return
		}
		ready.Done()
		<-start
		results <- result{err: run(), cpu: cpu}
	}
	go worker(cpus[0], func() error {
		fd, err := syscall.Open(os.Getenv(blockSourceEnv), syscall.O_RDONLY, 0)
		if err == nil {
			_ = syscall.Close(fd)
		}
		return err
	}, sourceResult)
	go worker(cpus[1], func() error {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if err != nil {
			return err
		}
		err = syscall.Connect(fd, &syscall.SockaddrInet4{Port: address.Port, Addr: [4]byte{127, 0, 0, 1}})
		_ = syscall.Close(fd)
		return err
	}, connectResult)
	ready.Wait()
	close(start)
	source := <-sourceResult
	traced := <-connectResult
	if source.err != nil {
		t.Fatalf("configured source open failed in race fixture: %v", source.err)
	}
	if traced.err != nil && traced.err != syscall.EPERM {
		t.Fatalf("racing connect returned an unrelated error: %v", traced.err)
	}
	postSocket, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	postErr := syscall.Connect(postSocket, &syscall.SockaddrInet4{Port: address.Port, Addr: [4]byte{127, 0, 0, 1}})
	_ = syscall.Close(postSocket)
	if postErr != syscall.EPERM {
		t.Fatalf("connect after completed source-open did not observe the sticky source state: %v", postErr)
	}
	fmt.Printf("source_errno=0 race_connect_errno=%d post_connect_errno=%d source_cpu=%d connect_cpu=%d\n", blockErrno(traced.err), blockErrno(postErr), source.cpu, traced.cpu)
}

func TestRunBlockSourceConnectRaceAcrossPinnedCPUs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load BPF programs and create the block-mode run cgroup")
	}
	var allowed unix.CPUSet
	if err := unix.SchedGetaffinity(0, &allowed); err != nil || allowed.Count() < 2 {
		t.Skipf("requires two CPUs available to the integration test: cpus=%d err=%v", allowed.Count(), err)
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-source-connect-race-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: source-connect-race\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
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
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(blockSourceEnv, secret)
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockRaceEnv, "1")
	var stdout, stderr bytes.Buffer
	code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, target, []string{target, "-test.run=^TestKernelBlockSourceConnectRaceChild$"}, nil, &stdout, &stderr, filepath.Join(root, "audit.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock)
	if status.ActiveMode != ModeBlock || status.ActualAction != "pre_op_lsm_enforcement_active" {
		t.Fatalf("race run did not have active block enforcement: code=%d status=%+v stderr=%q err=%v", code, status, stderr.String(), runErr)
	}
	if status.AuditCoverage == "incomplete" {
		if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
			t.Fatalf("incomplete audit coverage was not reported: status=%+v err=%v", status, runErr)
		}
	} else if status.AuditCoverage != "complete" || runErr != nil {
		t.Fatalf("unexpected run result: status=%+v err=%v", status, runErr)
	}
	line := ""
	for _, outputLine := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(outputLine, "source_errno=0 race_connect_errno=") {
			line = outputLine
			break
		}
	}
	if line == "" || !strings.Contains(line, "post_connect_errno=1") || !strings.Contains(line, "source_cpu=") || !strings.Contains(line, "connect_cpu=") {
		t.Fatalf("race target did not report both pinned CPUs and a denied post-source connect: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
	}
	racingAllowed := strings.Contains(line, "race_connect_errno=0")
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if racingAllowed {
		if acceptErr != nil {
			t.Fatalf("racing connect succeeded but receiver saw no connection: %v line=%q", acceptErr, line)
		}
		_ = conn.Close()
		if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		conn, acceptErr = listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
			t.Fatal("post-source connect unexpectedly reached receiver")
		}
	} else if acceptErr == nil {
		_ = conn.Close()
		t.Fatalf("racing connect was denied but receiver observed a connection: %q", line)
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("controlled receiver returned unexpected result after race: %v", acceptErr)
	}
	t.Logf("source/connect operations raced across CPUs: %s; receiver effect agreed with syscall result", line)
}

func TestRunBlockPropagatesSourceToConcurrentWorkerThreads(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load BPF programs and create the block-mode run cgroup")
	}
	var allowedCPUs unix.CPUSet
	if err := unix.SchedGetaffinity(0, &allowedCPUs); err != nil {
		t.Skipf("cannot inspect CPUs available to this test: %v", err)
	}
	if allowedCPUs.Count() < 2 {
		t.Skipf("requires at least two available CPUs for pinned worker concurrency; found %d", allowedCPUs.Count())
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-block-workers-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: concurrent-worker-deny\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
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
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(blockChildEnv, "1")
	t.Setenv(blockSourceEnv, secret)
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockExpectEnv, "deny")
	const workers = 8
	t.Setenv(blockWorkersEnv, strconv.Itoa(workers))
	var stdout, stderr bytes.Buffer
	code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, target, []string{target, "-test.run=^TestKernelBlockChild$"}, nil, &stdout, &stderr, filepath.Join(root, "audit.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock)
	t.Logf("target=%q requested=%s active=%s action=%s result=%s audit_coverage=%s code=%d err=%v", stdout.String(), status.RequestedMode, status.ActiveMode, status.ActualAction, status.Result, status.AuditCoverage, code, runErr)
	if status.RequestedMode != ModeBlock || status.ActiveMode != ModeBlock || status.ActualAction != "pre_op_lsm_enforcement_active" {
		t.Fatalf("block mode did not report active pre-operation enforcement: code=%d status=%+v stderr=%q err=%v", code, status, stderr.String(), runErr)
	}
	want := fmt.Sprintf("open_errno=0 workers=%d cpus=%d denied=%d connected=0 unexpected=0", workers, min(workers, allowedCPUs.Count()), workers)
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("source state was not enforced for every concurrent target worker: want %q, stdout=%q stderr=%q code=%d err=%v", want, stdout.String(), stderr.String(), code, runErr)
	}
	if status.AuditCoverage == "incomplete" {
		if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
			t.Fatalf("incomplete audit coverage was not reported separately: status=%+v err=%v", status, runErr)
		}
	} else if status.AuditCoverage != "complete" || runErr != nil {
		t.Fatalf("unexpected run result: status=%+v err=%v", status, runErr)
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted a connection from a concurrent worker denied by block mode")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("concurrent-worker receiver check: %v", acceptErr)
	}
}

func TestRunFailedSourceOpenTaintsKernelButNotReference(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load BPF programs and create the run cgroup")
	}

	root, err := os.MkdirTemp("/dev", "agenttaint-failed-source-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "source-device")
	if err := syscall.Mknod(secret, syscall.S_IFCHR|0o666, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o666); err != nil {
		t.Fatal(err)
	}
	normal := filepath.Join(root, "normal.txt")
	if err := os.WriteFile(normal, []byte("ordinary data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: source-egress\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	snapshot, err := policy.Load(writeBlockPolicy(t, root, policyText))
	if err != nil {
		t.Fatal(err)
	}
	target := copyBlockTarget(t, root)
	t.Setenv(blockChildEnv, "1")
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockExpectEnv, "source-open-fails")
	t.Setenv(blockSourceEnv, secret)
	blockedOutput, blockedStderr := &bytes.Buffer{}, &bytes.Buffer{}
	blockedCode, blockedErr := RunBlockedWithPolicy(context.Background(), env.Resolver{}, target,
		[]string{target, "-test.run=^TestKernelBlockChild$"}, nil, blockedOutput, blockedStderr,
		filepath.Join(root, "blocked.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534})
	if !strings.Contains(blockedOutput.String(), "connect_errno=1") || strings.Contains(blockedOutput.String(), "open_errno=0") {
		t.Fatalf("failed source attempt did not precede an EPERM connect: code=%d err=%v stdout=%q stderr=%q", blockedCode, blockedErr, blockedOutput.String(), blockedStderr.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted a connection after the failed source attempt")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("failed-source receiver check: %v", acceptErr)
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !assertFailedSourceEventRemainsUnlabeled(t, filepath.Join(root, "blocked.jsonl"), snapshot, secret) {
		t.Fatal("audit did not capture the failed source open without labeling the Go reference state")
	}
	t.Logf("failed-source differential result: code=%d err=%v", blockedCode, blockedErr)

	t.Setenv(blockExpectEnv, "allow")
	t.Setenv(blockSourceEnv, normal)
	allowedOutput, allowedStderr := &bytes.Buffer{}, &bytes.Buffer{}
	allowedCode, allowedErr := RunBlockedWithPolicy(context.Background(), env.Resolver{}, target,
		[]string{target, "-test.run=^TestKernelBlockChild$"}, nil, allowedOutput, allowedStderr,
		filepath.Join(root, "allowed.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534})
	if !strings.Contains(allowedOutput.String(), "open_errno=0 connect_errno=0") {
		t.Fatalf("non-source control failed: code=%d err=%v stdout=%q stderr=%q", allowedCode, allowedErr, allowedOutput.String(), allowedStderr.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err = listener.Accept()
	if err != nil {
		t.Fatalf("non-source control did not reach receiver: %v", err)
	}
	_ = conn.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunBlockedFileOpenDenyWithAllowControl(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load BPF programs and create the run cgroup")
	}

	root, err := os.MkdirTemp("/tmp", "agenttaint-file-deny-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	normal := filepath.Join(root, "normal.txt")
	for path, contents := range map[string]string{secret: "synthetic secret\n", normal: "ordinary data\n"} {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("deny_files:\n  - %q\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
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
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(blockChildEnv, "1")
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockExpectEnv, "file-deny")
	t.Setenv(blockSourceEnv, secret)
	blockedOutput, blockedStderr := &bytes.Buffer{}, &bytes.Buffer{}
	blockedCode, blockedErr := RunBlockedWithPolicy(context.Background(), env.Resolver{}, target,
		[]string{target, "-test.run=^TestKernelBlockChild$"}, nil, blockedOutput, blockedStderr,
		filepath.Join(root, "blocked.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534})
	if !strings.Contains(blockedOutput.String(), "open_errno=1") {
		t.Fatalf("target did not report EPERM on denied file open: code=%d err=%v stdout=%q stderr=%q", blockedCode, blockedErr, blockedOutput.String(), blockedStderr.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted a connection from the file-denied target")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("file-denied receiver check: %v", acceptErr)
	}
	t.Logf("file-deny runner result: code=%d err=%v", blockedCode, blockedErr)

	t.Setenv(blockExpectEnv, "allow")
	t.Setenv(blockSourceEnv, normal)
	allowedOutput, allowedStderr := &bytes.Buffer{}, &bytes.Buffer{}
	allowedCode, allowedErr := RunBlockedWithPolicy(context.Background(), env.Resolver{}, target,
		[]string{target, "-test.run=^TestKernelBlockChild$"}, nil, allowedOutput, allowedStderr,
		filepath.Join(root, "allowed.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534})
	if !strings.Contains(allowedOutput.String(), "open_errno=0 connect_errno=0") {
		t.Fatalf("non-denied file control failed: code=%d err=%v stdout=%q stderr=%q", allowedCode, allowedErr, allowedOutput.String(), allowedStderr.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err = listener.Accept()
	if err != nil {
		t.Fatalf("non-denied file control connection did not reach receiver: %v", err)
	}
	_ = conn.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("file-deny allow-control runner result: code=%d err=%v", allowedCode, allowedErr)
}

func blockErrno(err error) int {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return int(errno)
	}
	return -1
}

func writeBlockPolicy(t *testing.T, root, text string) string {
	t.Helper()
	path := filepath.Join(root, "policy.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func copyBlockTarget(t *testing.T, root string) string {
	t.Helper()
	target := filepath.Join(root, "target")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
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
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func assertFailedSourceEventRemainsUnlabeled(t *testing.T, auditPath string, snapshot *policy.Snapshot, source string) bool {
	t.Helper()
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Errorf("read audit events: %v", err)
		return false
	}
	engine, err := taint.NewEngine(snapshot.SourcePaths())
	if err != nil {
		t.Errorf("create Go reference engine: %v", err)
		return false
	}
	found := false
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		event, err := core.DecodeEvent(line)
		if err != nil {
			t.Errorf("decode audit event: %v", err)
			return false
		}
		fileOpen, ok := event.(*core.FileOpenEvent)
		if !ok || fileOpen.Resource.Path != source {
			continue
		}
		if fileOpen.ObservationStage != core.StageCompleted || fileOpen.OperationResult != core.ResultFailure {
			t.Errorf("source open audit status = %s/%s, want completed/failure", fileOpen.ObservationStage, fileOpen.OperationResult)
			return false
		}
		if err := engine.Apply(fileOpen); err != nil {
			t.Errorf("apply failed source event to Go reference: %v", err)
			return false
		}
		if labels := engine.Labels(fileOpen.Process.ProcessKey); len(labels) != 0 {
			t.Errorf("Go reference labeled a failed source open: %v", labels)
			return false
		}
		found = true
	}
	return found
}

func TestRunBlockedSourceTaintDeniesConnectWithAllowControl(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load BPF programs and create the run cgroup")
	}

	root, err := os.MkdirTemp("/tmp", "agenttaint-block-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	normal := filepath.Join(root, "normal.txt")
	for path, contents := range map[string]string{secret: "synthetic secret\n", normal: "ordinary data\n"} {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := listener.Addr().(*net.TCPAddr)

	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %s\n    label: SECRET\nrules:\n  - name: source-egress\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	// The Go test binary lives below a root-only go-build directory. Copy it to
	// a searchable, executable temporary path before the runner drops identity.
	target := filepath.Join(root, "target")
	input, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
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
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv(blockChildEnv, "1")
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockExpectEnv, "deny")
	t.Setenv(blockSourceEnv, secret)
	blockedOutput, blockedStderr := &bytes.Buffer{}, &bytes.Buffer{}
	blockedCode, blockedErr := RunBlockedWithPolicy(context.Background(), env.Resolver{}, target,
		[]string{target, "-test.run=^TestKernelBlockChild$"}, nil, blockedOutput, blockedStderr,
		filepath.Join(root, "blocked.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534})
	if !strings.Contains(blockedOutput.String(), "open_errno=0 connect_errno=1") {
		t.Fatalf("target did not report successful source open followed by EPERM connect: code=%d err=%v stdout=%q stderr=%q", blockedCode, blockedErr, blockedOutput.String(), blockedStderr.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted a connection from the denied target")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("denied-target receiver check: %v", acceptErr)
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("block runner result: code=%d err=%v", blockedCode, blockedErr)

	t.Setenv(blockExpectEnv, "allow")
	t.Setenv(blockSourceEnv, normal)
	allowedOutput, allowedStderr := &bytes.Buffer{}, &bytes.Buffer{}
	allowedCode, allowedErr := RunBlockedWithPolicy(context.Background(), env.Resolver{}, target,
		[]string{target, "-test.run=^TestKernelBlockChild$"}, nil, allowedOutput, allowedStderr,
		filepath.Join(root, "allowed.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534})
	if !strings.Contains(allowedOutput.String(), "connect_errno=0") {
		t.Fatalf("unlabeled control did not report successful connect: code=%d err=%v stdout=%q stderr=%q", allowedCode, allowedErr, allowedOutput.String(), allowedStderr.String())
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	conn, err = listener.Accept()
	if err != nil {
		t.Fatalf("unlabeled control connection did not reach receiver: %v (runner code=%d err=%v stdout=%q stderr=%q)", err, allowedCode, allowedErr, allowedOutput.String(), allowedStderr.String())
	}
	_ = conn.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("allow-control runner result: code=%d err=%v", allowedCode, allowedErr)
}
