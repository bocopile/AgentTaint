//go:build linux

package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/enforce"
	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
	linuxsensor "github.com/bocopile/AgentTaint/internal/sensor/linux"
	"golang.org/x/sys/unix"
)

const (
	supervisorCrashFixtureEnv   = "AGENTTAINT_SUPERVISOR_CRASH_FIXTURE"
	supervisorCrashPolicyEnv    = "AGENTTAINT_SUPERVISOR_CRASH_POLICY"
	supervisorCrashTargetEnv    = "AGENTTAINT_SUPERVISOR_CRASH_TARGET"
	supervisorCrashScopePathEnv = "AGENTTAINT_SUPERVISOR_CRASH_SCOPE_PATH"
	supervisorTamperRunnerEnv   = "AGENTTAINT_SUPERVISOR_TAMPER_RUNNER"
	supervisorTamperTargetEnv   = "AGENTTAINT_SUPERVISOR_TAMPER_TARGET"
)

func TestObserveHelperClosesControlFDBeforeTargetExec(t *testing.T) {
	identity := &TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	if os.Geteuid() == 0 {
		identity = &TargetIdentity{UID: 65534, GID: 65534}
	}
	credential, expectedIdentity, err := targetCredential(identity)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-control-fd-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	markerFD, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	highFD, err := unix.FcntlInt(uintptr(markerFD), unix.F_DUPFD, 200)
	_ = unix.Close(markerFD)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(highFD)
	target := filepath.Join(root, "target")
	script := fmt.Sprintf("#!/bin/sh\nif [ -S /proc/self/fd/3 ]; then echo control_fd_unavailable=0; exit 1; fi\nif [ -e /proc/self/fd/%d ]; then echo unrelated_fd_unavailable=0; exit 1; fi\necho control_fd_unavailable=1 unrelated_fd_unavailable=1\n", highFD)
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd, controlFD, nonce, err := startObserveHelper(context.Background(), target, []string{target}, nil, &stdout, &stderr, credential, expectedIdentity)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(controlFD)
	if err := writeLaunchPermit(controlFD, helperStartFrame(nonce)); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("send exact launch permit: %v", err)
	}
	if err := waitHelperExec(controlFD, 5*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("target helper did not close its control channel on exec: %v stderr=%q", err, stderr.String())
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("target control-fd probe failed: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "control_fd_unavailable=1 unrelated_fd_unavailable=1") {
		t.Fatalf("target did not confirm supervisor and unrelated high FDs were unavailable: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunObservedSeparatesConcurrentRuns(t *testing.T) {
	requireBPF := s25RequiredBPFMode(t)
	if requireBPF && (runtime.GOOS != "linux" || runtime.GOARCH != "arm64") {
		t.Fatalf("required S25 concurrent-run fixture requires Linux/arm64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if os.Geteuid() != 0 {
		s25SkipUnlessRequired(t, requireBPF, "requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-observed-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	probeFD, err := unix.Open("/dev/null", unix.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	highFD, err := unix.FcntlInt(uintptr(probeFD), unix.F_DUPFD, 200)
	_ = unix.Close(probeFD)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(highFD)
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	if os.Geteuid() == 0 {
		identity = &TargetIdentity{UID: 65534, GID: 65534}
	}
	groupCheck := ""
	if os.Geteuid() == 0 {
		groupCheck = "[ -z \"$groups\" ] || exit 74\n"
	}
	type result struct {
		index int
		code  int
		out   []byte
		diag  []byte
		err   error
	}
	results := make(chan result, 2)
	seenRunIDs := make(map[int]core.RunID, 2)
	seenScopes := make(map[int]string, 2)
	auditIncomplete := false
	auditIncompleteDetails := make([]string, 0, 2)
	missingFixtureEvent := false
	var start sync.WaitGroup
	start.Add(1)
	for index := 0; index < 2; index++ {
		index := index
		fixture := filepath.Join(root, fmt.Sprintf("fixture-%d.txt", index))
		if err := os.WriteFile(fixture, []byte("synthetic\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(root, fmt.Sprintf("target-%d", index))
		source := "#!/bin/sh\nruid= euid= suid= rgid= egid= sgid= nnp= groups= cape= capp= capi= capa=\nwhile IFS=: read -r field value; do\n  case \"$field\" in\n    Uid) set -- $value; ruid=$1; euid=$2; suid=$3;;\n    Gid) set -- $value; rgid=$1; egid=$2; sgid=$3;;\n    NoNewPrivs) set -- $value; nnp=$1;;\n    Groups) set -- $value; groups=$*;;\n    CapEff) set -- $value; cape=$1;;\n    CapPrm) set -- $value; capp=$1;;\n    CapInh) set -- $value; capi=$1;;\n    CapAmb) set -- $value; capa=$1;;\n  esac\ndone < /proc/self/status\n[ \"$ruid:$euid:$suid\" = \"" + fmt.Sprint(identity.UID) + ":" + fmt.Sprint(identity.UID) + ":" + fmt.Sprint(identity.UID) + "\" ] || exit 71\n[ \"$rgid:$egid:$sgid\" = \"" + fmt.Sprint(identity.GID) + ":" + fmt.Sprint(identity.GID) + ":" + fmt.Sprint(identity.GID) + "\" ] || exit 72\n[ \"$nnp\" = 1 ] || exit 73\n" + groupCheck + "case \"$cape:$capp:$capi:$capa\" in *[!0:]* ) exit 75;; esac\n[ ! -e /proc/self/fd/" + fmt.Sprint(highFD) + " ] || exit 79\nwhile IFS= read -r line; do :; done < " + shellQuote(fixture) + "\nprintf 'target-" + fmt.Sprint(index) + "'\nprintf 'diagnostic-" + fmt.Sprint(index) + "' >&2\n"
		if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
			t.Fatal(err)
		}
		auditPath := filepath.Join(root, fmt.Sprintf("audit-%d.jsonl", index))
		go func() {
			start.Wait()
			var out, diagnostic bytes.Buffer
			code, err := RunObserved(context.Background(), env.Resolver{}, script, []string{script}, nil, &out, &diagnostic, auditPath, identity)
			results <- result{index: index, code: code, out: out.Bytes(), diag: diagnostic.Bytes(), err: err}
		}()
	}
	start.Done()
	for range 2 {
		got := <-results
		incomplete := got.err != nil && strings.Contains(got.err.Error(), "audit coverage is incomplete")
		if got.err != nil && !incomplete {
			t.Fatalf("run %d: code=%d err=%v", got.index, got.code, got.err)
		}
		if incomplete {
			auditIncompleteDetails = append(auditIncompleteDetails, got.err.Error())
		}
		if got.code != 0 && !(incomplete && got.code == 1) {
			t.Fatalf("run %d target failed: code=%d err=%v", got.index, got.code, got.err)
		}
		auditIncomplete = auditIncomplete || incomplete
		if string(got.out) != fmt.Sprintf("target-%d", got.index) {
			t.Fatalf("run %d target stdout changed: %q", got.index, got.out)
		}
		if string(got.diag) != fmt.Sprintf("diagnostic-%d", got.index) {
			t.Fatalf("run %d target stderr changed: %q", got.index, got.diag)
		}
		fixture := filepath.Join(root, fmt.Sprintf("fixture-%d.txt", got.index))
		other := filepath.Join(root, fmt.Sprintf("fixture-%d.txt", 1-got.index))
		data, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("audit-%d.jsonl", got.index)))
		if err != nil {
			t.Fatal(err)
		}
		if mode, err := os.Stat(filepath.Join(root, fmt.Sprintf("audit-%d.jsonl", got.index))); err != nil || mode.Mode().Perm() != 0o600 {
			t.Fatalf("run %d audit mode: %v %v", got.index, mode, err)
		}
		foundFixture := false
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			event, err := core.DecodeEvent(line)
			if err != nil {
				t.Fatalf("run %d invalid audit line %q: %v", got.index, line, err)
			}
			var context core.ProcessContext
			var runID core.RunID
			switch event := event.(type) {
			case *core.ProcessExecEvent:
				context = event.Process
				runID = event.RunID
			case *core.ProcessExitEvent:
				context = event.Process
				runID = event.RunID
			case *core.FileOpenEvent:
				context = event.Process
				runID = event.RunID
				if event.Resource.Path == fixture {
					if event.OperationResult != core.ResultSuccess {
						t.Fatalf("run %d synthetic file-open event was not successful: result=%s", got.index, event.OperationResult)
					}
					foundFixture = true
				}
				if event.Resource.Path == other {
					t.Fatalf("run %d audit contains sibling run file %s", got.index, other)
				}
			case *core.ProcessForkEvent:
				context = event.Process
				runID = event.RunID
			}
			if context.ProcessKey.PID == 0 || context.ProcessKey.ScopeID == "" {
				t.Fatalf("run %d event lacks process scope: %#v", got.index, event)
			}
			if runID == "" {
				t.Fatalf("run %d event lacks run identity: %#v", got.index, event)
			}
			if prior, ok := seenRunIDs[got.index]; ok && prior != runID {
				t.Fatalf("run %d audit mixes run IDs %q and %q", got.index, prior, runID)
			}
			if prior, ok := seenScopes[got.index]; ok && prior != context.ProcessKey.ScopeID {
				t.Fatalf("run %d audit mixes scopes %q and %q", got.index, prior, context.ProcessKey.ScopeID)
			}
			seenRunIDs[got.index] = runID
			seenScopes[got.index] = context.ProcessKey.ScopeID
		}
		if !foundFixture {
			if !incomplete {
				t.Fatalf("run %d audit lacks its synthetic file-open event: %s", got.index, strings.TrimSpace(string(data)))
			}
			missingFixtureEvent = true
		}
	}
	if first, second := seenRunIDs[0], seenRunIDs[1]; first != "" && second != "" && first == second {
		t.Fatalf("concurrent runs share run identity %q", first)
	}
	if first, second := seenScopes[0], seenScopes[1]; first != "" && second != "" && first == second {
		t.Fatalf("concurrent runs share observation scope %q", first)
	}
	missingRunMetadata := seenRunIDs[0] == "" || seenRunIDs[1] == "" || seenScopes[0] == "" || seenScopes[1] == ""
	if auditIncomplete || missingFixtureEvent || missingRunMetadata {
		reason := fmt.Sprintf("target output and descriptor checks passed, but complete run-isolation audit evidence is unavailable: audit_incomplete=%t audit_details=%q fixture_event_missing=%t run_metadata_missing=%t distinct_run_ids=%t distinct_scopes=%t", auditIncomplete, auditIncompleteDetails, missingFixtureEvent, missingRunMetadata, seenRunIDs[0] != "" && seenRunIDs[1] != "" && seenRunIDs[0] != seenRunIDs[1], seenScopes[0] != "" && seenScopes[1] != "" && seenScopes[0] != seenScopes[1])
		s25SkipUnlessRequired(t, requireBPF, reason)
	}
}

const (
	s25RequireBPFEnv = "AGENTTAINT_REQUIRE_S25_BPF"
	s25MmapPathEnv   = "AGENTTAINT_S25_MMAP_PATH"
	s25OpenSuccess   = "S25_OPEN_SUCCEEDED"
	s25MadviseSkip   = "S25_MADVISE_UNAVAILABLE"
	s25PathTestName  = "TestS25MmapPathTarget"
)

func s25RequiredBPFMode(t *testing.T) bool {
	t.Helper()
	value := os.Getenv(s25RequireBPFEnv)
	if value != "" && value != "1" {
		t.Fatalf("%s must be empty or 1, got %q", s25RequireBPFEnv, value)
	}
	return value == "1"
}

func s25SkipUnlessRequired(t *testing.T, required bool, reason string) {
	t.Helper()
	if required {
		t.Fatalf("required S25 BPF fixture prerequisite failed: %s", reason)
	}
	t.Skip(reason)
}

// TestRunObservedMmapPathReadFault creates a file-backed pathname page in the
// target and drops its user mapping before one raw openat. It verifies that the
// observer recovers the kernel-copied raw pathname and completes audit coverage.
func TestRunObservedMmapPathReadFault(t *testing.T) {
	requireBPF := s25RequiredBPFMode(t)
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		s25SkipUnlessRequired(t, requireBPF, fmt.Sprintf("S25 observer requires Linux/arm64, got %s/%s", runtime.GOOS, runtime.GOARCH))
	}
	if os.Geteuid() != 0 {
		s25SkipUnlessRequired(t, requireBPF, "root privileges are required to load and attach eBPF programs")
	}

	probe, err := linuxsensor.New(uint64(time.Now().UnixNano()))
	if err != nil {
		s25SkipUnlessRequired(t, requireBPF, fmt.Sprintf("BPF observer load/attach unavailable: %v", err))
	}
	if err := probe.Close(); err != nil {
		t.Fatalf("BPF observer could not close after capability probe: %v", err)
	}

	root, err := os.MkdirTemp("/tmp", "agenttaint-s25-mmap-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "synthetic-target")
	if err := os.WriteFile(targetPath, []byte("synthetic S25 target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backingPath := filepath.Join(root, "pathname-page")
	page := make([]byte, os.Getpagesize())
	copy(page, targetPath)
	if err := os.WriteFile(backingPath, page, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(backingPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(s25MmapPathEnv, backingPath)

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executableCopy := filepath.Join(root, "runner.test")
	sourceBinary, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceBinary.Close()
	targetBinary, err := os.OpenFile(executableCopy, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(targetBinary, sourceBinary); err != nil {
		_ = targetBinary.Close()
		t.Fatalf("copy runner test binary into target-readable directory: %v", err)
	}
	if err := targetBinary.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(executableCopy, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{executableCopy, "-test.run=^" + s25PathTestName + "$"}
	identity := &TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	if os.Geteuid() == 0 {
		identity = &TargetIdentity{UID: 65534, GID: 65534}
	}
	var stdout, stderr bytes.Buffer
	auditPath := filepath.Join(root, "audit.jsonl")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	code, runErr := RunObserved(ctx, env.Resolver{}, executableCopy, args, nil, &stdout, &stderr, auditPath, identity)
	if strings.Contains(stdout.String(), s25MadviseSkip) {
		s25SkipUnlessRequired(t, requireBPF, fmt.Sprintf("kernel/filesystem does not support the mmap discard prerequisite: stdout=%q", stdout.String()))
	}
	if !strings.Contains(stdout.String(), s25OpenSuccess) {
		t.Fatalf("target did not report successful raw openat: runner_code=%d err=%v stdout=%q stderr=%q", code, runErr, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("target test process did not exit successfully: runner_code=%d err=%v stdout=%q stderr=%q", code, runErr, stdout.String(), stderr.String())
	}
	if runErr != nil {
		t.Fatalf("RunObserved reported incomplete audit coverage after pathname recovery: code=%d err=%v stdout=%q stderr=%q", code, runErr, stdout.String(), stderr.String())
	}
	if code != 0 {
		t.Fatalf("RunObserved did not complete the recovered pathname audit successfully: code=%d", code)
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read completed audit output: %v", err)
	}
	if !strings.Contains(string(audit), targetPath) {
		t.Fatalf("audit output does not contain the exact raw pathname recovered from the mmap page: want %q, audit=%q", targetPath, audit)
	}
}

func TestS25MmapPathTarget(t *testing.T) {
	t.Helper()
	requireBPF := s25RequiredBPFMode(t)
	backingPath := os.Getenv(s25MmapPathEnv)
	if backingPath == "" {
		if requireBPF {
			t.Fatal("required S25 mmap target path is missing; select TestRunObservedMmapPathReadFault")
		}
		t.Skip("S25 mmap target is only run as a subprocess of the BPF observer fixture")
	}
	fd, err := unix.Open(backingPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open synthetic pathname backing file: %v", err)
	}
	defer unix.Close(fd)

	pathname, err := unix.Mmap(fd, 0, os.Getpagesize(), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		t.Fatalf("map synthetic pathname backing file: %v", err)
	}
	defer unix.Munmap(pathname)
	if err := unix.Madvise(pathname, unix.MADV_DONTNEED); err != nil {
		_, _ = fmt.Fprintln(os.Stdout, s25MadviseSkip)
		t.Skipf("MADV_DONTNEED on file-backed pathname mapping: %v", err)
	}

	// Do not read pathname after MADV_DONTNEED: the raw syscall must be the
	// first user-space access that can fault the file-backed page back in.
	dirfd := int(unix.AT_FDCWD)
	result, _, errno := unix.RawSyscall6(
		unix.SYS_OPENAT,
		uintptr(dirfd),
		uintptr(unsafe.Pointer(&pathname[0])),
		uintptr(unix.O_RDONLY|unix.O_CLOEXEC),
		0,
		0,
		0,
	)
	runtime.KeepAlive(pathname)
	if errno != 0 || int64(result) < 0 {
		t.Fatalf("raw openat failed after page discard: result=%d errno=%v", int64(result), errno)
	}
	if err := unix.Close(int(result)); err != nil {
		t.Fatalf("close raw openat result: %v", err)
	}
	if _, err := fmt.Fprintln(os.Stdout, s25OpenSuccess); err != nil {
		t.Fatalf("report raw openat success: %v", err)
	}
}

func TestRunObservedPolicyAuditPipeline(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-policy-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	normal := filepath.Join(root, "normal.txt")
	for path, content := range map[string]string{secret: "synthetic secret\n", normal: "ordinary data\n"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan struct{}, 4)
	go func() {
		for range 4 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	run := func(name, action, body string) ([]*core.PolicyViolationEvent, bool) {
		t.Helper()
		policyPath := filepath.Join(root, name+".yaml")
		policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: " + name + "\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: " + action + "\n"
		if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := policy.Load(policyPath)
		if err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(root, name+".sh")
		scriptText := "#!/bin/bash\nset -e\n" + body + "\n"
		if err := os.WriteFile(script, []byte(scriptText), 0o755); err != nil {
			t.Fatal(err)
		}
		auditPath := filepath.Join(root, name+".jsonl")
		code, err := RunObservedWithPolicy(context.Background(), env.Resolver{}, script, []string{script}, nil, io.Discard, io.Discard, auditPath, snapshot, identity)
		incomplete := err != nil && strings.Contains(err.Error(), "audit coverage is incomplete")
		if (incomplete && code != 1) || (!incomplete && (err != nil || code != 0)) {
			t.Fatalf("%s run: code=%d err=%v", name, code, err)
		}
		data, err := os.ReadFile(auditPath)
		if err != nil {
			t.Fatal(err)
		}
		// Under D07, a missing raw pathname keeps the run incomplete. A
		// completed audit requires the corresponding policy event; incomplete
		// history may omit that verdict but must never be reported as complete.
		var violations []*core.PolicyViolationEvent
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			event, err := core.DecodeEvent(line)
			if err != nil {
				t.Fatalf("%s invalid audit event %s: %v", name, line, err)
			}
			if violation, ok := event.(*core.PolicyViolationEvent); ok {
				violations = append(violations, violation)
			}
		}
		if incomplete {
			t.Logf("%s run reported incomplete audit coverage; policy verdict is unavailable", name)
		}
		return violations, incomplete
	}
	assertPolicyEvent := func(name string, got []*core.PolicyViolationEvent, incomplete bool, decision string) {
		t.Helper()
		if incomplete && len(got) == 0 {
			return
		}
		if len(got) != 1 || got[0].Decision != decision || got[0].OperationResult != core.ResultSuccess {
			t.Fatalf("%s policy event = %#v, want one successful %s event", name, got, decision)
		}
	}

	connect := "exec 4<>/dev/tcp/127.0.0.1/" + fmt.Sprint(port)
	if got, _ := run("normal", "deny", "exec 3<"+shellQuote(normal)+"\n/bin/bash -c "+shellQuote(connect)); len(got) != 0 {
		t.Fatalf("normal source flow was tainted: %#v", got)
	}
	got, incomplete := run("sensitive-read", "deny", "exec 3<"+shellQuote(secret)+"\n"+connect)
	assertPolicyEvent("direct deny", got, incomplete, "deny")
	got, incomplete = run("subprocess-exfil", "deny", "exec 3<"+shellQuote(secret)+"\n/bin/bash -c "+shellQuote(connect))
	assertPolicyEvent("child sink", got, incomplete, "deny")
	got, incomplete = run("audit-only", "audit", "exec 3<"+shellQuote(secret)+"\n"+connect)
	assertPolicyEvent("audit-only", got, incomplete, "audit")
	for i := 0; i < 4; i++ {
		select {
		case <-accepted:
		case <-time.After(2 * time.Second):
			t.Fatal("the deny/audit verdict prevented a local TCP connection")
		}
	}
}

func TestRunPolicyModesNotifyAndKillAfterPolicyEvent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load eBPF programs and create the kill-mode run cgroup")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-modes-")
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
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan error, 3)
	go func() {
		for range 3 {
			conn, err := listener.Accept()
			if err != nil {
				accepted <- err
				return
			}
			_ = conn.Close()
			accepted <- nil
		}
	}()
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: post-event-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	connect := "exec 3<" + shellQuote(secret) + "\nexec 4<>/dev/tcp/127.0.0.1/" + fmt.Sprint(port) + "\nsleep 1\nprintf survived"
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	for _, mode := range []PolicyMode{ModeMonitor, ModeNotify, ModeKill} {
		if mode == ModeKill {
			probe, probeErr := createRunCgroup()
			if probeErr != nil {
				marker := filepath.Join(root, "kill-mode-started")
				preflight := filepath.Join(root, "kill-preflight.sh")
				if err := os.WriteFile(preflight, []byte("#!/bin/sh\nprintf started > "+shellQuote(marker)+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, preflight, []string{preflight}, nil, &out, &diagnostic, filepath.Join(root, "kill-preflight.jsonl"), snapshot, identity, ModeKill)
				if runErr == nil || !strings.Contains(runErr.Error(), "prepare run cgroup scope") || status.ActiveMode != "none" || status.Result != "not_started" || code == 0 {
					t.Fatalf("kill mode did not fail safely before start when cgroup scope is unavailable: code=%d status=%+v stderr=%q err=%v", code, status, diagnostic.String(), runErr)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("kill target ran without a private cgroup scope: marker stat err=%v", err)
				}
				t.Logf("kill mode failed before target start because private cgroup scope is unavailable: %v", probeErr)
				return
			}
			if err := probe.Close(); err != nil {
				t.Fatalf("close kill-mode cgroup preflight probe: %v", err)
			}
		}
		script := filepath.Join(root, string(mode)+".sh")
		if err := os.WriteFile(script, []byte("#!/bin/bash\nset -e\n"+connect+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, script, []string{script}, nil, &stdout, &stderr, filepath.Join(root, string(mode)+".jsonl"), snapshot, identity, mode)
		if status.RequestedMode != mode || status.ActiveMode != mode {
			t.Fatalf("%s mode status does not report the active run: %+v", mode, status)
		}
		if status.AuditCoverage == "incomplete" {
			if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
				t.Fatalf("%s mode did not report incomplete audit coverage: status=%+v err=%v", mode, status, runErr)
			}
			t.Logf("%s mode action completed with audit coverage reported incomplete: code=%d err=%v", mode, code, runErr)
		} else if status.AuditCoverage != "complete" || runErr != nil {
			t.Fatalf("%s mode returned unexpected coverage/result: status=%+v err=%v", mode, status, runErr)
		}
		data, err := os.ReadFile(filepath.Join(root, string(mode)+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		var foundDeny bool
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			event, err := core.DecodeEvent(line)
			if err != nil {
				t.Fatalf("%s invalid audit event %s: %v", mode, line, err)
			}
			if violation, ok := event.(*core.PolicyViolationEvent); ok && violation.Decision == "deny" && violation.OperationResult == core.ResultSuccess {
				foundDeny = true
				if mode == ModeNotify && !strings.Contains(stderr.String(), "audit_event_id="+string(violation.EventID)) {
					t.Fatalf("notification is not linked to audit event %s: %q", violation.EventID, stderr.String())
				}
			}
		}
		if !foundDeny && status.AuditCoverage != "incomplete" {
			t.Fatalf("%s mode lacks a successful post-event deny record", mode)
		}
		switch mode {
		case ModeMonitor:
			if status.ActualAction != "observe_only" || status.Notifications != 0 || !strings.Contains(stdout.String(), "survived") {
				t.Fatalf("monitor changed the target or reported an action: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		case ModeNotify:
			if foundDeny {
				if status.ActualAction != "post_event_notification" || status.Notifications != 1 || !strings.Contains(stderr.String(), "audit_event_id=") || !strings.Contains(stdout.String(), "survived") {
					t.Fatalf("notify did not report its linked post-event action: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
				}
			} else if status.ActualAction != "none" || status.Notifications != 0 || !strings.Contains(stdout.String(), "survived") {
				t.Fatalf("notify acted without a complete-history deny record: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		case ModeKill:
			if foundDeny {
				if status.ActualAction != "post_event_kill" || status.Result != "cgroup_killed_after_policy_event" || strings.Contains(stdout.String(), "survived") {
					t.Fatalf("kill did not terminate the run after the policy event: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
				}
			} else if status.ActualAction != "none" || !strings.Contains(stdout.String(), "survived") {
				t.Fatalf("kill acted without a successful post-event deny record: status=%+v stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		}
		select {
		case acceptErr := <-accepted:
			if acceptErr != nil {
				t.Fatalf("%s mode receiver did not confirm the connection effect: %v", mode, acceptErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s mode connection did not reach the controlled receiver", mode)
		}
	}
}

func TestRunPolicyModeBlockReportsPreOperationEnforcement(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load eBPF programs and create the block-mode run cgroup")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-mode-block-")
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
	policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: block-mode-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(blockChildEnv, "1")
	t.Setenv(blockSourceEnv, secret)
	t.Setenv(blockAddressEnv, endpoint.IP.String())
	t.Setenv(blockPortEnv, fmt.Sprint(endpoint.Port))
	t.Setenv(blockExpectEnv, "deny")
	target := os.Args[0]
	var stdout, stderr bytes.Buffer
	code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, target, []string{target, "-test.run=^TestKernelBlockChild$"}, nil, &stdout, &stderr, filepath.Join(root, "block.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock)
	if status.RequestedMode != ModeBlock || status.ActiveMode != ModeBlock || status.ActualAction != "pre_op_lsm_enforcement_active" {
		t.Fatalf("block status does not identify active pre-operation enforcement: code=%d status=%+v stderr=%q err=%v", code, status, stderr.String(), runErr)
	}
	if !strings.Contains(stdout.String(), "open_errno=0 connect_errno=1") {
		t.Fatalf("block mode did not preserve the source open and pre-operation EPERM result: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
	}
	if status.AuditCoverage == "incomplete" {
		if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
			t.Fatalf("block mode did not report incomplete audit coverage: status=%+v err=%v", status, runErr)
		}
	} else if status.AuditCoverage != "complete" || runErr != nil {
		t.Fatalf("block mode returned unexpected coverage/result: status=%+v err=%v", status, runErr)
	}
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, acceptErr := listener.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("receiver accepted the connect denied by block mode")
	}
	if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("block-mode receiver check: %v", acceptErr)
	}
}

func TestRunPolicyModeScenarioMatrix(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load eBPF programs and create private run cgroups")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-mode-matrix-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.txt")
	normal := filepath.Join(root, "normal.txt")
	for path, data := range map[string]string{secret: "synthetic secret\n", normal: "ordinary data\n"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	identity := &TargetIdentity{UID: 65534, GID: 65534}
	type scenario struct {
		name       string
		source     string
		forkedSink bool
	}
	scenarios := []scenario{
		{name: "normal", source: normal},
		{name: "direct-source", source: secret},
		{name: "fork-child", source: secret, forkedSink: true},
		{name: "audit-only", source: secret},
	}
	modes := []PolicyMode{ModeMonitor, ModeNotify, ModeBlock, ModeKill}
	for _, scenario := range scenarios {
		for _, mode := range modes {
			t.Run(string(mode)+"/"+scenario.name, func(t *testing.T) {
				listener, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				port := listener.Addr().(*net.TCPAddr).Port
				action := "deny"
				if scenario.name == "audit-only" {
					action = "audit"
				}
				policyPath := filepath.Join(root, string(mode)+"-"+scenario.name+".yaml")
				policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: mode-matrix\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: %s\n", secret, action)
				if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
					t.Fatal(err)
				}
				snapshot, err := policy.Load(policyPath)
				if err != nil {
					t.Fatal(err)
				}
				connect := "exec 4<>/dev/tcp/127.0.0.1/" + fmt.Sprint(port)
				var sink string
				if scenario.forkedSink {
					sink = "if /bin/bash -c " + shellQuote(connect) + "; then printf child_connected; else printf child_connect_failed; fi"
				} else {
					sink = "if " + connect + "; then printf connected; else printf connect_failed; fi"
				}
				script := filepath.Join(root, string(mode)+"-"+scenario.name+".sh")
				scriptBody := "#!/bin/bash\nset +e\nif exec 3<" + shellQuote(scenario.source) + "; then printf source_opened; else printf source_open_failed; fi\n" + sink + "\nsleep 0.2\nprintf survived\n"
				if scenario.name == "audit-only" {
					scriptBody = strings.Replace(scriptBody, "printf survived", "printf audited", 1)
				}
				if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				auditPath := filepath.Join(root, string(mode)+"-"+scenario.name+".jsonl")
				auditSummary := func() string {
					data, _ := os.ReadFile(auditPath)
					var lines []string
					for _, line := range bytes.Split(data, []byte{'\n'}) {
						if bytes.Contains(line, []byte(secret)) || bytes.Contains(line, []byte(`"event":"network_connect"`)) || bytes.Contains(line, []byte(`"event":"policy_violation"`)) {
							lines = append(lines, string(line))
						}
					}
					return strings.Join(lines, "\n")
				}
				code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, script, []string{script}, nil, &stdout, &stderr, auditPath, snapshot, identity, mode)
				if status.RequestedMode != mode || status.ActiveMode != mode {
					t.Fatalf("mode status does not report the active run: code=%d status=%+v stderr=%q err=%v", code, status, stderr.String(), runErr)
				}
				if !strings.Contains(stdout.String(), "source_opened") {
					t.Fatalf("scenario source open did not succeed: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
				}
				if status.AuditCoverage == "incomplete" {
					if runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") {
						t.Fatalf("incomplete audit coverage was not surfaced: status=%+v err=%v", status, runErr)
					}
				} else if status.AuditCoverage != "complete" || runErr != nil {
					t.Fatalf("unexpected audit coverage result: status=%+v err=%v", status, runErr)
				}
				tainted := scenario.name != "normal"
				data, err := os.ReadFile(auditPath)
				if err != nil {
					t.Fatal(err)
				}
				var foundDecision, foundDeny bool
				for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
					if len(line) == 0 {
						continue
					}
					event, err := core.DecodeEvent(line)
					if err != nil {
						t.Fatalf("%s invalid audit event %s: %v", mode, line, err)
					}
					if violation, ok := event.(*core.PolicyViolationEvent); ok && violation.OperationResult == core.ResultSuccess {
						foundDecision = true
						if violation.Decision == "deny" {
							foundDeny = true
						}
					}
				}
				if status.AuditCoverage == "complete" && mode != ModeBlock && foundDecision != tainted {
					t.Fatalf("complete audit has policy decision=%t, want tainted=%t: status=%+v audit=%s", foundDecision, tainted, status, auditSummary())
				}
				denied := tainted && action == "deny"
				blocked := mode == ModeBlock && denied
				killed := mode == ModeKill && foundDeny
				connected := !blocked
				// Post-event kill may run before the target resumes to print after
				// connect; receiver Accept below verifies the completed effect.
				if blocked {
					if scenario.forkedSink {
						if !strings.Contains(stdout.String(), "child_connect_failed") {
							t.Fatalf("fork child connect was not rejected: stdout=%q stderr=%q", stdout.String(), stderr.String())
						}
					} else if !strings.Contains(stdout.String(), "connect_failed") {
						t.Fatalf("direct connect was not rejected: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
					}
				} else if mode != ModeKill && !strings.Contains(stdout.String(), "connected") {
					t.Fatalf("expected target connection to complete: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
				}
				continued := strings.Contains(stdout.String(), "survived") || strings.Contains(stdout.String(), "audited")
				if killed == continued {
					t.Fatalf("kill result does not match the target's post-event continuation: code=%d status=%+v stdout=%q stderr=%q err=%v relevant_audit=%s", code, status, stdout.String(), stderr.String(), runErr, auditSummary())
				}
				if scenario.name == "audit-only" && !strings.Contains(stdout.String(), "audited") {
					t.Fatalf("audit-only policy changed target continuation: stdout=%q", stdout.String())
				}
				switch mode {
				case ModeMonitor:
					if status.ActualAction != "observe_only" || status.Notifications != 0 {
						t.Fatalf("monitor reported an enforcement action: %+v", status)
					}
				case ModeNotify:
					expectedNotifications := uint64(0)
					if foundDecision {
						expectedNotifications = 1
					}
					if status.Notifications != expectedNotifications {
						t.Fatalf("notify count=%d, want %d: code=%d status=%+v stdout=%q stderr=%q err=%v relevant_audit=%s", status.Notifications, expectedNotifications, code, status, stdout.String(), stderr.String(), runErr, auditSummary())
					}
					if expectedNotifications == 1 && (!strings.Contains(status.ActualAction, "notification") || !strings.Contains(stderr.String(), "audit_event_id=")) {
						t.Fatalf("notify did not reference an audit event: status=%+v stderr=%q", status, stderr.String())
					}
				case ModeBlock:
					if status.ActualAction != "pre_op_lsm_enforcement_active" {
						t.Fatalf("block did not report pre-operation enforcement: %+v", status)
					}
				case ModeKill:
					if killed && (status.ActualAction != "post_event_kill" || status.Result != "cgroup_killed_after_policy_event") {
						t.Fatalf("explicit kill did not report its post-event result: %+v", status)
					}
					if !killed && status.ActualAction != "none" {
						t.Fatalf("kill mode acted without a deny event: %+v", status)
					}
				}
				if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				conn, acceptErr := listener.Accept()
				_ = listener.(*net.TCPListener).SetDeadline(time.Time{})
				if connected {
					if acceptErr != nil {
						t.Fatalf("expected connection did not reach receiver: %v", acceptErr)
					}
					_ = conn.Close()
				} else {
					if acceptErr == nil {
						_ = conn.Close()
						t.Fatalf("block mode allowed a denied connection to reach the receiver: code=%d status=%+v stdout=%q stderr=%q err=%v relevant_audit=%s", code, status, stdout.String(), stderr.String(), runErr, auditSummary())
					}
					if timeout, ok := acceptErr.(net.Error); !ok || !timeout.Timeout() {
						t.Fatalf("receiver block check returned %v", acceptErr)
					}
				}
			})
		}
	}
}

func TestRunEnforcementModesFailBeforeTargetWithoutRunScopePrivilege(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged Linux caller to verify run-scope preflight failure")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-mode-preflight-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: preflight-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	identity := &TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	for _, mode := range []PolicyMode{ModeBlock, ModeKill} {
		marker := filepath.Join(root, string(mode)+"-started")
		var stdout, stderr bytes.Buffer
		code, status, runErr := RunWithPolicyMode(context.Background(), env.Resolver{}, "/usr/bin/touch", []string{"/usr/bin/touch", marker}, nil, &stdout, &stderr, filepath.Join(root, string(mode)+".jsonl"), snapshot, identity, mode)
		if runErr == nil || !strings.Contains(runErr.Error(), "prepare run cgroup scope") || status.ActiveMode != "none" || status.ActualAction != "none" || status.Result != "not_started" || code == 0 {
			t.Fatalf("%s mode did not fail before target start without private-scope privilege: code=%d status=%+v stdout=%q stderr=%q err=%v", mode, code, status, stdout.String(), stderr.String(), runErr)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("%s mode started the target without private-scope privilege: marker stat err=%v", mode, err)
		}
	}
}

func TestRunBlockHookPreparationFailureDoesNotStartTarget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to verify block hook preparation failure")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-hook-preflight-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: hook-preflight-test\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	scopePath := filepath.Join(root, "synthetic-cgroup")
	createScope := func() (*runCgroup, error) {
		if err := os.Mkdir(scopePath, 0o700); err != nil {
			return nil, err
		}
		return &runCgroup{path: scopePath, id: 1}, nil
	}
	newEnforcer := func(_ policy.KernelProjection, _ uint64) (*enforce.Enforcer, error) {
		return nil, errors.New("synthetic required socket_connect hook attach failure")
	}
	marker := filepath.Join(root, "target-started")
	auditPath := filepath.Join(root, "audit.jsonl")
	var stdout, stderr bytes.Buffer
	status := ModeStatus{RequestedMode: ModeBlock, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	code, runErr := runProtectedPlatformWithDependencies(context.Background(), "/usr/bin/touch", []string{"/usr/bin/touch", marker}, nil, &stdout, &stderr, auditPath, snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock, &status, createScope, newEnforcer)
	if code == 0 || runErr == nil || !strings.Contains(runErr.Error(), "prepare kernel enforcement hooks") || !strings.Contains(runErr.Error(), "synthetic required socket_connect hook attach failure") {
		t.Fatalf("block did not report the hook preparation failure: code=%d status=%+v stdout=%q stderr=%q err=%v", code, status, stdout.String(), stderr.String(), runErr)
	}
	if status.ActiveMode != "none" || status.ActualAction != "none" || status.Result != "not_started" {
		t.Fatalf("failed block preflight reported an active mode or action: %+v", status)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("target started after the required hook failed to attach: marker stat err=%v", err)
	}
	if _, err := os.Stat(auditPath); !os.IsNotExist(err) {
		t.Fatalf("audit output was created before block hook preflight completed: stat err=%v", err)
	}
	if _, err := os.Stat(scopePath); !os.IsNotExist(err) {
		t.Fatalf("synthetic run scope was not cleaned up after preflight failure: stat err=%v", err)
	}
}

func TestRunObservedKeepsBinaryStreamsSeparateFromAudit(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load the Linux observer")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-binary-streams-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	script := "#!/bin/sh\ncat\nprintf '\\377\\000'\nprintf '\\000\\376err' >&2\n"
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	identity := &TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	if os.Geteuid() == 0 {
		identity = &TargetIdentity{UID: 65534, GID: 65534}
	}
	stdin := []byte{0x00, 0xff, 'j', 's', 'o', 'n', '\n'}
	wantStdout := append(append([]byte(nil), stdin...), 0xff, 0x00)
	wantStderr := []byte{0x00, 0xfe, 'e', 'r', 'r'}
	var stdout, stderr bytes.Buffer
	auditPath := filepath.Join(root, "audit.jsonl")
	code, runErr := RunObserved(context.Background(), env.Resolver{}, target, []string{target}, bytes.NewReader(stdin), &stdout, &stderr, auditPath, identity)
	if code != 0 && !(code == 1 && runErr != nil && strings.Contains(runErr.Error(), "audit coverage is incomplete")) {
		t.Fatalf("target failed for a reason other than reported audit incompleteness: code=%d err=%v stdout=%v stderr=%v", code, runErr, stdout.Bytes(), stderr.Bytes())
	}
	if !bytes.Equal(stdout.Bytes(), wantStdout) {
		t.Fatalf("binary stdin/stdout changed: got=%v want=%v", stdout.Bytes(), wantStdout)
	}
	if !bytes.Equal(stderr.Bytes(), wantStderr) {
		t.Fatalf("binary stderr changed or contains non-target diagnostics: got=%v want=%v", stderr.Bytes(), wantStderr)
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(audit), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if _, err := core.DecodeEvent(line); err != nil {
			t.Fatalf("audit contains target stream bytes or an invalid event: %q: %v", line, err)
		}
	}
}

func TestUnprivilegedTargetCannotModifyPolicyAuditOrControl(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a privileged observer and a separate unprivileged target")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-target-file-boundary-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policyContents := []byte("sources: []\nrules: []\n")
	if err := os.WriteFile(policyPath, policyContents, 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(root, "audit.jsonl")
	t.Setenv("AGENTTAINT_TEST_POLICY_PATH", policyPath)
	t.Setenv("AGENTTAINT_TEST_AUDIT_PATH", auditPath)
	target := filepath.Join(root, "target")
	script := "#!/bin/sh\nif printf tampered >> \"$AGENTTAINT_TEST_POLICY_PATH\" 2>/dev/null; then echo policy_writable=1; exit 1; fi\nif printf tampered >> \"$AGENTTAINT_TEST_AUDIT_PATH\" 2>/dev/null; then echo audit_writable=1; exit 1; fi\nif [ -S /proc/self/fd/3 ]; then echo control_socket_visible=1; exit 1; fi\nprintf 'policy_write_denied=1 audit_write_denied=1 control_fd_hidden=1\\n'\n"
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code, runErr := RunObserved(context.Background(), env.Resolver{}, target, []string{target}, nil, &stdout, &stderr, auditPath, &TargetIdentity{UID: 65534, GID: 65534})
	if code != 0 && !(code == 1 && runErr != nil && strings.Contains(runErr.Error(), "audit coverage is incomplete")) {
		t.Fatalf("target failed for a reason other than reported audit incompleteness: code=%d err=%v stdout=%q stderr=%q", code, runErr, stdout.String(), stderr.String())
	}
	if stdout.String() != "policy_write_denied=1 audit_write_denied=1 control_fd_hidden=1\n" || !strings.Contains(stderr.String(), "policy.yaml: Permission denied") || !strings.Contains(stderr.String(), "audit.jsonl: Permission denied") {
		t.Fatalf("target control-boundary probes did not all pass: stdout=%q stderr=%q code=%d err=%v", stdout.String(), stderr.String(), code, runErr)
	}
	policyAfter, err := os.ReadFile(policyPath)
	if err != nil || !bytes.Equal(policyAfter, policyContents) {
		t.Fatalf("target modified policy file: bytes=%q err=%v", policyAfter, err)
	}
	mode, err := os.Stat(auditPath)
	if err != nil || mode.Mode().Perm() != 0o600 {
		t.Fatalf("audit file mode = %v, err=%v; want 0600", mode, err)
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(audit), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if _, err := core.DecodeEvent(line); err != nil {
			t.Fatalf("target altered audit file contents: %q: %v", line, err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("observer did not leave any independently decodable audit event")
	}
}

func TestAuditWriteFailureKeepsTargetOutputAndPartialFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-audit-write-failure-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	startedPath := filepath.Join(os.TempDir(), "."+filepath.Base(root)+"-target-started")
	if _, err := os.Stat(startedPath); !os.IsNotExist(err) {
		t.Fatalf("marker path is not safely absent: %s: %v", startedPath, err)
	}
	defer os.Remove(startedPath)
	releasePath := filepath.Join(os.TempDir(), "."+filepath.Base(root)+"-release-target")
	finishedPath := filepath.Join(os.TempDir(), "."+filepath.Base(root)+"-target-finished")
	for _, path := range []string{releasePath, finishedPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("marker path is not safely absent: %s: %v", path, err)
		}
		defer os.Remove(path)
	}
	auditPath := filepath.Join(root, "audit.jsonl")
	output := &auditFailureWriter{err: syscall.ENOSPC, waitPath: startedPath}
	failureObserved := make(chan error, 1)
	newWriter := func(file *auditFile) *auditWriter {
		output.output = file.file
		return &auditWriter{file: file, output: output, onFailure: func(err error) { failureObserved <- err }}
	}
	var stdout, stderr bytes.Buffer
	status := ModeStatus{RequestedMode: ModeMonitor, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := "touch " + strconv.Quote(startedPath) +
		"; printf 'target-started\\n'; printf 'target-stderr\\n' >&2; " +
		"while [ ! -e " + strconv.Quote(releasePath) + " ]; do sleep 0.01; done; " +
		"touch " + strconv.Quote(finishedPath) + "; printf 'target-finished\\n'"
	result := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, runErr := runProtectedPlatformWithAuditWriterFactory(
			runCtx, "/bin/sh", []string{"sh", "-c", command}, nil,
			&stdout, &stderr, auditPath, nil, &TargetIdentity{UID: 65534, GID: 65534},
			ModeMonitor, &status, nil, enforce.New, newWriter,
		)
		result <- struct {
			code int
			err  error
		}{code: code, err: runErr}
	}()
	select {
	case failureErr := <-failureObserved:
		if !errors.Is(failureErr, syscall.ENOSPC) {
			t.Fatalf("observed audit writer error = %v, want ENOSPC", failureErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the audit writer to record ENOSPC")
	}
	if _, err := os.Stat(finishedPath); !os.IsNotExist(err) {
		t.Fatalf("target finished before it was released after audit failure: %s: %v", finishedPath, err)
	}
	if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
		t.Fatalf("release target after recorded audit failure: %v", err)
	}
	var runResult struct {
		code int
		err  error
	}
	select {
	case runResult = <-result:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for protected target completion")
	}
	code, runErr := runResult.code, runResult.err
	if code != 1 || runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete") || !errors.Is(runErr, syscall.ENOSPC) {
		t.Fatalf("audit write failure result: code=%d status=%+v err=%v", code, status, runErr)
	}
	if status.AuditCoverage != "incomplete" || status.Result != "target_exit_code=0" || status.ActualAction != "observe_only" {
		t.Fatalf("unexpected status after audit write failure: %+v", status)
	}
	if stdout.String() != "target-started\ntarget-finished\n" || stderr.String() != "target-stderr\n" {
		t.Fatalf("target streams changed or audit was redirected: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(finishedPath); err != nil {
		t.Fatalf("target did not finish after release: %v", err)
	}
	if output.calls != 1 || output.Len() == 0 {
		t.Fatalf("sticky writer state: calls=%d partial-bytes=%d", output.calls, output.Len())
	}
	partialFile, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("partial audit file was not retained: %v", err)
	}
	if !bytes.Equal(partialFile, output.Bytes()) || len(partialFile) != 4 {
		t.Fatalf("partial audit bytes differ: file=%q injected=%q", partialFile, output.Bytes())
	}
}

func TestRunObservedNoNewPrivilegesBlocksSetuidTarget(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to set up a setuid fixture")
	}
	root, err := os.MkdirTemp(os.TempDir(), "agenttaint-setuid-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Flags&unix.ST_NOSUID != 0 {
		t.Skipf("temporary filesystem is mounted nosuid (flags=%#x mask=%#x)", fs.Flags, unix.ST_NOSUID)
	}
	idBinary, err := os.ReadFile("/usr/bin/id")
	if err != nil {
		t.Fatal(err)
	}
	setuidBinary := filepath.Join(root, "id")
	if err := os.WriteFile(setuidBinary, idBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(setuidBinary, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(setuidBinary, os.ModeSetuid|0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(setuidBinary)
	if err != nil || info.Mode()&os.ModeSetuid == 0 {
		t.Fatalf("setuid fixture is not active: %v %v", info, err)
	}
	var out, diagnostic bytes.Buffer
	identity := &TargetIdentity{UID: 1000, GID: 1000}
	code, runErr := RunObserved(context.Background(), env.Resolver{}, setuidBinary, []string{setuidBinary, "-u"}, nil, &out, &diagnostic, filepath.Join(root, "audit.jsonl"), identity)
	auditIncomplete := runErr != nil && strings.Contains(runErr.Error(), "audit coverage is incomplete")
	if (code != 0 && !(code == 1 && auditIncomplete)) || (runErr != nil && !auditIncomplete) || strings.TrimSpace(out.String()) != "1000" {
		t.Fatalf("setuid target regained privileges: code=%d err=%v stdout=%q stderr=%q", code, runErr, out.String(), diagnostic.String())
	}
	policyPath := filepath.Join(root, "block-policy.yaml")
	policyText := "sources:\n  - path: " + setuidBinary + "\n    label: SECRET\nrules:\n  - name: setuid-block-profile\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var blockOut, blockDiagnostic bytes.Buffer
	blockCode, blockStatus, blockErr := RunWithPolicyMode(context.Background(), env.Resolver{}, setuidBinary, []string{setuidBinary, "-u"}, nil, &blockOut, &blockDiagnostic, filepath.Join(root, "block-audit.jsonl"), snapshot, identity, ModeBlock)
	blockAuditIncomplete := blockErr != nil && strings.Contains(blockErr.Error(), "audit coverage is incomplete")
	if blockStatus.ActiveMode != ModeBlock || blockStatus.ActualAction != "pre_op_lsm_enforcement_active" || blockStatus.Result != "target_exit_code=0" || strings.TrimSpace(blockOut.String()) != "1000" {
		t.Fatalf("block-mode setuid target regained privileges or block mode was not active: code=%d status=%+v err=%v stdout=%q stderr=%q", blockCode, blockStatus, blockErr, blockOut.String(), blockDiagnostic.String())
	}
	if (blockCode != 0 && !(blockCode == 1 && blockAuditIncomplete)) || (blockErr != nil && !blockAuditIncomplete) {
		t.Fatalf("block-mode setuid target returned unexpected audit/result status: code=%d status=%+v err=%v", blockCode, blockStatus, blockErr)
	}
}

func TestRunObservedForwardsCancellationToTargetGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-cancel-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("/tmp", fmt.Sprintf("agenttaint-target-started-%d", os.Getpid()))
	_ = os.Remove(marker)
	defer os.Remove(marker)
	script := filepath.Join(root, "target")
	source := "#!/bin/sh\n: > " + shellQuote(marker) + "\nexec /bin/sleep 30\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := RunObserved(ctx, env.Resolver{}, script, []string{script}, nil, io.Discard, io.Discard, filepath.Join(root, "audit.jsonl"), &TargetIdentity{UID: 65534, GID: 65534})
		result <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("target did not start before cancellation deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case got := <-result:
		if got.code != 130 || got.err == nil || !strings.Contains(got.err.Error(), "run-canceled") {
			t.Fatalf("canceled target result: code=%d err=%v", got.code, got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("target group did not terminate after cancellation")
	}
}

func TestRunBlockForwardsCancellationAndReportsProtectedMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load BPF programs and create the run cgroup")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-block-cancel-")
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
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: cancel-block-target\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	markerBase := fmt.Sprintf("/tmp/agenttaint-block-cancel-%d", os.Getpid())
	readyPath := markerBase + ".ready"
	pidPath := markerBase + ".pid"
	_ = os.Remove(readyPath)
	_ = os.Remove(pidPath)
	defer os.Remove(readyPath)
	defer os.Remove(pidPath)
	target := filepath.Join(root, "target.sh")
	script := "#!/bin/sh\necho $$ > " + shellQuote(pidPath) + "\n: > " + shellQuote(readyPath) + "\nexec /bin/sleep 30\n"
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		code   int
		status ModeStatus
		err    error
	}
	done := make(chan result, 1)
	auditPath := filepath.Join(root, "audit.jsonl")
	go func() {
		code, status, runErr := RunWithPolicyMode(ctx, env.Resolver{}, target, []string{target}, nil, io.Discard, io.Discard, auditPath, snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock)
		done <- result{code: code, status: status, err: runErr}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			got := <-done
			t.Fatalf("block target did not reach its ready marker: code=%d status=%+v err=%v", got.code, got.status, got.err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		cancel()
		<-done
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		cancel()
		<-done
		t.Fatalf("parse target pid %q: %v", pidData, err)
	}
	cancel()
	got := <-done
	if got.code != 130 || got.status.ActiveMode != ModeBlock || got.status.ActualAction != "pre_op_lsm_enforcement_active" || got.status.Result != "target_exit_code=130" || got.status.AuditCoverage != "incomplete" {
		t.Fatalf("canceled protected run returned unexpected status: code=%d status=%+v err=%v", got.code, got.status, got.err)
	}
	if got.err == nil || !strings.Contains(got.err.Error(), "run-canceled") {
		t.Fatalf("cancellation was not preserved as an incomplete run condition: status=%+v err=%v", got.status, got.err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("target process %d remains after cancellation: %v", pid, err)
	}
}

func TestRunObservedKeepsObserverUntilSetsidDescendantExits(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to load and attach eBPF programs")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("requires util-linux setsid")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-descendant-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("/tmp", fmt.Sprintf("agenttaint-descendant-exit-%d", os.Getpid()))
	_ = os.Remove(marker)
	defer os.Remove(marker)
	script := filepath.Join(root, "target")
	source := "#!/bin/sh\n" + shellQuote(setsid) + " /bin/sh -c 'sleep 5.2; : > " + shellQuote(marker) + "' </dev/null >/dev/null 2>&1 &\nexit 0\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	code, runErr := RunObserved(context.Background(), env.Resolver{}, script, []string{script}, nil, io.Discard, io.Discard, filepath.Join(root, "audit.jsonl"), &TargetIdentity{UID: 65534, GID: 65534})
	if elapsed := time.Since(started); elapsed < childDrainGrace {
		t.Fatalf("observer detached before descendant drain limit: elapsed=%s", elapsed)
	}
	if code != 1 || runErr == nil || !strings.Contains(runErr.Error(), "descendant-remained-after-drain-limit") {
		t.Fatalf("long-lived descendant result: code=%d err=%v", code, runErr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("observer returned while setsid descendant remained alive: marker=%v", err)
	}
}

func TestRunKillModeCleansScopeAfterDetachedDescendantDrainLimit(t *testing.T) {
	runProtectedModeCleansScopeAfterDetachedDescendantDrainLimit(t, ModeKill)
}

func TestRunBlockModeCleansScopeAfterDetachedDescendantDrainLimit(t *testing.T) {
	runProtectedModeCleansScopeAfterDetachedDescendantDrainLimit(t, ModeBlock)
}

func runProtectedModeCleansScopeAfterDetachedDescendantDrainLimit(t *testing.T, mode PolicyMode) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to create and clean the protected run cgroup")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("requires util-linux setsid")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-kill-descendant-")
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
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: detached-descendant\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	readyFIFO := filepath.Join(root, "descendant-ready")
	if err := unix.Mkfifo(readyFIFO, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readyFIFO, 0o666); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "descendant-survived")
	script := filepath.Join(root, "target")
	body := fmt.Sprintf("#!/bin/sh\n%s /bin/sh -c 'printf ready > %s; /bin/sleep 30; : > %s' </dev/null >/dev/null 2>&1 &\nIFS= read -r _ < %s\nexit 0\n", shellQuote(setsid), shellQuote(readyFIFO), shellQuote(marker), shellQuote(readyFIFO))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	var scopePath string
	createScope := func() (*runCgroup, error) {
		scope, err := createRunCgroup()
		if err == nil {
			scopePath = scope.path
		}
		return scope, err
	}
	status := ModeStatus{RequestedMode: mode, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	started := time.Now()
	code, runErr := runProtectedPlatformWithDependencies(context.Background(), script, []string{script}, nil, io.Discard, io.Discard, filepath.Join(root, "audit.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, mode, &status, createScope, enforce.New)
	if time.Since(started) < childDrainGrace {
		t.Fatalf("runner detached before its descendant drain limit: elapsed=%s status=%+v err=%v", time.Since(started), status, runErr)
	}
	if runErr == nil || !strings.Contains(runErr.Error(), "descendant-remained-after-drain-limit") || code == 0 {
		t.Fatalf("detached descendant did not produce the configured drain-limit result: code=%d status=%+v err=%v", code, status, runErr)
	}
	if mode == ModeBlock && (status.ActiveMode != ModeBlock || status.ActualAction != "pre_op_lsm_enforcement_active") {
		t.Fatalf("block mode was not actively enforced while draining descendants: status=%+v err=%v", status, runErr)
	}
	if scopePath == "" {
		t.Fatal("run cgroup was not created")
	}
	if _, err := os.Stat(scopePath); !os.IsNotExist(err) {
		t.Fatalf("run cgroup scope was not removed after descendant cleanup: path=%q stat err=%v", scopePath, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("detached descendant survived run-scope cleanup: marker stat err=%v", err)
	}
	t.Logf("%s mode drain-limit cleanup: elapsed=%s cgroup_removed=true descendant_marker_absent=true status=%+v", mode, time.Since(started), status)
}

// TestRunKillModeSupervisorCrashChild is launched by the companion test and
// deliberately left running until the parent process kills this supervisor.
func TestRunKillModeSupervisorCrashChild(t *testing.T) {
	if os.Getenv(supervisorCrashFixtureEnv) != "1" {
		return
	}
	snapshot, err := policy.Load(os.Getenv(supervisorCrashPolicyEnv))
	if err != nil {
		t.Fatal(err)
	}
	var scopePath string
	createScope := func() (*runCgroup, error) {
		scope, err := createRunCgroup()
		if err != nil {
			return nil, err
		}
		scopePath = scope.path
		if err := os.WriteFile(os.Getenv(supervisorCrashScopePathEnv), []byte(scopePath), 0o600); err != nil {
			_ = scope.Kill()
			_ = scope.Close()
			return nil, err
		}
		return scope, nil
	}
	var stdout, stderr bytes.Buffer
	status := ModeStatus{RequestedMode: ModeKill, ActiveMode: "none", ActualAction: "none", Result: "not_started", AuditCoverage: "unknown"}
	code, runErr := runProtectedPlatformWithDependencies(context.Background(), os.Getenv(supervisorCrashTargetEnv), []string{os.Getenv(supervisorCrashTargetEnv)}, nil, &stdout, &stderr, os.Getenv(supervisorCrashPolicyEnv)+".audit", snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeKill, &status, createScope, enforce.New)
	t.Errorf("supervisor fixture exited before the test killed it: code=%d status=%+v err=%v stdout=%q stderr=%q scope=%q", code, status, runErr, stdout.String(), stderr.String(), scopePath)
}

func TestRunKillModeSupervisorCrashLeavesDescendantScopeForExternalCleanup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges to inspect and clean the protected cgroup after supervisor death")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("requires util-linux setsid")
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-supervisor-crash-")
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
	policyPath := filepath.Join(root, "policy.yaml")
	policyText := fmt.Sprintf("sources:\n  - path: %q\n    label: SECRET\nrules:\n  - name: supervisor-crash\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n", secret)
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	targetStarted := filepath.Join(root, "target-started")
	childStarted := filepath.Join(root, "child-started")
	for _, path := range []string{targetStarted, childStarted} {
		if err := os.WriteFile(path, []byte("not-started"), 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "target")
	child := "printf child-started > " + shellQuote(childStarted) + "; exec /bin/sleep 30"
	body := fmt.Sprintf("#!/bin/sh\nprintf target-started > %s\n%s /bin/sh -c %s </dev/null >/dev/null 2>&1 &\nwait\n", shellQuote(targetStarted), shellQuote(setsid), shellQuote(child))
	if err := os.WriteFile(target, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	scopePathFile := filepath.Join(root, "scope-path")
	var stdout, stderr bytes.Buffer
	supervisor := exec.Command(os.Args[0], "-test.run=^TestRunKillModeSupervisorCrashChild$")
	supervisor.Env = append(os.Environ(),
		supervisorCrashFixtureEnv+"=1",
		supervisorCrashPolicyEnv+"="+policyPath,
		supervisorCrashTargetEnv+"="+target,
		supervisorCrashScopePathEnv+"="+scopePathFile,
	)
	supervisor.Stdout, supervisor.Stderr = &stdout, &stderr
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	scopePath := ""
	defer func() {
		if supervisor.ProcessState == nil {
			_ = supervisor.Process.Kill()
			_ = supervisor.Wait()
		}
		if scopePath != "" {
			if scope, err := openRunCgroup(scopePath); err == nil {
				_ = scope.Kill()
				_ = scope.Close()
			}
		}
	}()
	waitForContent := func(path, expected string, timeout time.Duration) error {
		t.Helper()
		deadline := time.Now().Add(timeout)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if data, err := os.ReadFile(path); err == nil {
				got := strings.TrimSpace(string(data))
				if (expected == "" && got != "") || (expected != "" && got == expected) {
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for %s to contain %q", path, expected)
			}
			<-ticker.C
		}
	}
	if err := waitForContent(scopePathFile, "", 5*time.Second); err != nil {
		t.Fatalf("run scope was not created: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(scopePathFile)
	if err != nil {
		t.Fatalf("read private run cgroup path: %v", err)
	}
	scopePath = strings.TrimSpace(string(data))
	if scopePath == "" {
		t.Fatal("supervisor recorded an empty run cgroup path")
	}
	if err := waitForContent(targetStarted, "target-started", 5*time.Second); err != nil {
		t.Fatalf("target did not start: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	if err := waitForContent(childStarted, "child-started", 5*time.Second); err != nil {
		t.Fatalf("detached child did not start: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	if err := supervisor.Process.Kill(); err != nil {
		t.Fatalf("kill test-owned supervisor: %v", err)
	}
	if err := supervisor.Wait(); err == nil {
		t.Fatal("SIGKILL unexpectedly returned a successful supervisor exit")
	}
	if _, err := os.Stat(scopePath); err != nil {
		t.Fatalf("supervisor crash unexpectedly removed the cgroup before external cleanup: %v", err)
	}
	eventsPath := filepath.Join(scopePath, "cgroup.events")
	events, err := os.ReadFile(eventsPath)
	if err != nil || !strings.Contains(string(events), "populated 1") {
		t.Fatalf("expected the detached child to keep the abandoned run cgroup populated: events=%q err=%v", events, err)
	}
	scope, err := openRunCgroup(scopePath)
	if err != nil {
		t.Fatalf("open abandoned test-owned scope for cleanup: %v", err)
	}
	cleanupErr := errors.Join(scope.Kill(), scope.Close())
	if cleanupErr != nil {
		t.Fatalf("externally clean abandoned run scope: %v", cleanupErr)
	}
	if _, err := os.Stat(scopePath); !os.IsNotExist(err) {
		t.Fatalf("external cleanup did not remove abandoned scope: %v", err)
	}
	t.Logf("supervisor SIGKILL left detached descendant scope populated until explicit external cgroup.kill; test cleanup removed it")
}

func TestObserveTargetSupervisorTamperChild(t *testing.T) {
	if os.Getenv(supervisorTamperTargetEnv) != "1" {
		return
	}
	parent := os.Getppid()
	signalErr := syscall.Kill(parent, syscall.SIGTERM)
	ptraceErr := unix.PtraceAttach(parent)
	if ptraceErr == nil {
		_ = unix.PtraceDetach(parent)
	}
	var signalErrno, ptraceErrno syscall.Errno
	if signalErr != nil {
		_ = errors.As(signalErr, &signalErrno)
	}
	if ptraceErr != nil {
		_ = errors.As(ptraceErr, &ptraceErrno)
	}
	fmt.Printf("target_control parent_pid=%d signal_errno=%d ptrace_errno=%d\n", parent, signalErrno, ptraceErrno)
	if signalErrno != syscall.EPERM || ptraceErrno != syscall.EPERM {
		t.Fatalf("unprivileged target could control its privileged runner: signal=%v ptrace=%v", signalErr, ptraceErr)
	}
}

func TestSupervisorTamperRunnerChild(t *testing.T) {
	if os.Getenv(supervisorTamperRunnerEnv) != "1" {
		return
	}
	root, err := os.MkdirTemp("/tmp", "agenttaint-supervisor-tamper-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Chmod(root, 0o755); err != nil {
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
	t.Setenv(supervisorTamperTargetEnv, "1")
	var stdout, stderr bytes.Buffer
	code, runErr := RunObserved(context.Background(), env.Resolver{}, target, []string{target, "-test.run=^TestObserveTargetSupervisorTamperChild$"}, nil, &stdout, &stderr, filepath.Join(root, "audit.jsonl"), &TargetIdentity{UID: 65534, GID: 65534})
	if code != 0 && (runErr == nil || !strings.Contains(runErr.Error(), "audit coverage is incomplete")) {
		t.Fatalf("runner was terminated or target failed during the tampering probe: code=%d err=%v stdout=%q stderr=%q", code, runErr, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "signal_errno=1 ptrace_errno=1") {
		t.Fatalf("target could signal or ptrace the privileged runner, or probe output is missing: code=%d err=%v stdout=%q stderr=%q", code, runErr, stdout.String(), stderr.String())
	}
	fmt.Printf("tamper_probe code=%d audit_error=%v target=%q\n", code, runErr, strings.TrimSpace(stdout.String()))
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("synthetic secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "block-policy.yaml")
	policyText := "sources:\n  - path: " + secret + "\n    label: SECRET\nrules:\n  - name: supervisor-tamper-block\n    when: {process_has: SECRET}\n    sink: {network: {external: [127.0.0.0/8]}}\n    action: deny\n"
	if err := os.WriteFile(policyPath, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var blockStdout, blockStderr bytes.Buffer
	blockCode, blockStatus, blockErr := RunWithPolicyMode(context.Background(), env.Resolver{}, target, []string{target, "-test.run=^TestObserveTargetSupervisorTamperChild$"}, nil, &blockStdout, &blockStderr, filepath.Join(root, "block-audit.jsonl"), snapshot, &TargetIdentity{UID: 65534, GID: 65534}, ModeBlock)
	blockAuditIncomplete := blockErr != nil && strings.Contains(blockErr.Error(), "audit coverage is incomplete")
	if blockStatus.ActiveMode != ModeBlock || blockStatus.ActualAction != "pre_op_lsm_enforcement_active" || blockStatus.Result != "target_exit_code=0" || !strings.Contains(blockStdout.String(), "signal_errno=1 ptrace_errno=1") {
		t.Fatalf("block-mode runner control boundary failed: code=%d status=%+v err=%v stdout=%q stderr=%q", blockCode, blockStatus, blockErr, blockStdout.String(), blockStderr.String())
	}
	if (blockCode != 0 && !(blockCode == 1 && blockAuditIncomplete)) || (blockErr != nil && !blockAuditIncomplete) {
		t.Fatalf("block-mode tamper probe returned unexpected audit/result status: code=%d status=%+v err=%v", blockCode, blockStatus, blockErr)
	}
	t.Logf("block-mode tamper probe: audit_coverage=%s code=%d err=%v", blockStatus.AuditCoverage, blockCode, blockErr)
}

func TestUnprivilegedTargetCannotSignalOrPtraceSupervisor(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a privileged runner and a separate unprivileged target")
	}
	command := exec.Command(os.Args[0], "-test.run=^TestSupervisorTamperRunnerChild$", "-test.v")
	command.Env = append(os.Environ(), supervisorTamperRunnerEnv+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("runner harness did not survive the target's control attempts: err=%v output=%q", err, output)
	}
	if !bytes.Contains(output, []byte("signal_errno=1 ptrace_errno=1")) {
		t.Fatalf("guest harness did not record both permission denials: %q", output)
	}
	t.Logf("privileged runner survived; target signal and ptrace attempts returned EPERM: %s", strings.TrimSpace(string(output)))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestTargetCredentialRejectsRootAndUnknownIdentity(t *testing.T) {
	if os.Geteuid() == 0 {
		if _, _, err := targetCredential(nil); err == nil {
			t.Fatal("root observer accepted an unspecified target identity")
		}
		if _, _, err := targetCredential(&TargetIdentity{UID: 0, GID: 0}); err == nil {
			t.Fatal("root observer accepted a root target")
		}
		credential, identity, err := targetCredential(&TargetIdentity{UID: 65534, GID: 65534})
		if err != nil || credential == nil || credential.Uid != 65534 || credential.Gid != 65534 || len(credential.Groups) != 0 {
			t.Fatalf("explicit unprivileged target identity: %#v %v", credential, err)
		}
		if identity != (TargetIdentity{UID: 65534, GID: 65534}) {
			t.Fatalf("normalized identity: %#v", identity)
		}
		return
	}
	if credential, identity, err := targetCredential(nil); err != nil || credential != nil || identity.UID != uint32(os.Geteuid()) || identity.GID != uint32(os.Getegid()) {
		t.Fatalf("unprivileged caller identity: %#v %v", credential, err)
	}
	if _, _, err := targetCredential(&TargetIdentity{UID: 0, GID: 0}); err == nil {
		t.Fatal("unprivileged observer accepted identity escalation")
	}
}

func TestObserveHelperDoesNotExecWithoutExactPermit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"eof", "short", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "target-started")
			script := filepath.Join(root, "target")
			if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			parentFD, childFD := fds[0], fds[1]
			child := os.NewFile(uintptr(childFD), "test-helper-control")
			nonce := strings.Repeat("a", 32)
			cmd := exec.Command(executable, observeHelperArg, script, fmt.Sprint(os.Geteuid()), fmt.Sprint(os.Getegid()), "false", "target")
			cmd.ExtraFiles = []*os.File{child}
			cmd.Env = helperEnvironment(os.Environ(), nonce)
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			if err := cmd.Start(); err != nil {
				_ = child.Close()
				_ = unix.Close(parentFD)
				t.Fatal(err)
			}
			_ = child.Close()
			if err := waitControlFrame(context.Background(), parentFD, []byte("READY"), time.Second); err != nil {
				_ = unix.Close(parentFD)
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal(err)
			}
			if mode == "short" {
				_, _ = unix.Write(parentFD, []byte("S"))
			} else if mode == "invalid" {
				_, _ = unix.Write(parentFD, []byte("FAIL:"+nonce))
			}
			_ = unix.Close(parentFD)
			if err := cmd.Wait(); err == nil {
				t.Fatal("helper accepted missing or invalid launch permit")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("target ran without a valid permit: stat=%v stderr=%q", err, diagnostic.String())
			}
		})
	}
}

func TestStartObserveHelperRejectsCanceledContextBeforeChildStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	marker := filepath.Join(t.TempDir(), "target-started")
	cmd, fd, _, err := startObserveHelper(ctx, "/bin/sh", []string{"/bin/sh", "-c", "touch " + shellQuote(marker)}, nil, nil, nil, nil, TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())})
	if err == nil || cmd != nil || fd >= 0 {
		t.Fatalf("canceled launch unexpectedly created helper: cmd=%v fd=%d err=%v", cmd, fd, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("target started before launch permit: %v", statErr)
	}
}
