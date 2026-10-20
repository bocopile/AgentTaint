package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
)

const (
	blockChildEnv   = "AGENTTAINT_BLOCK_CHILD"
	blockSourceEnv  = "AGENTTAINT_BLOCK_SOURCE"
	blockAddressEnv = "AGENTTAINT_BLOCK_ADDRESS"
	blockPortEnv    = "AGENTTAINT_BLOCK_PORT"
	blockExpectEnv  = "AGENTTAINT_BLOCK_EXPECT"
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
	if os.Getenv(blockExpectEnv) == "deny" {
		want = "deny"
	}
	if openErr != nil || (want == "deny" && connectErr != syscall.EPERM) || (want == "allow" && connectErr != nil) {
		os.Exit(1)
	}
	os.Exit(0)
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
