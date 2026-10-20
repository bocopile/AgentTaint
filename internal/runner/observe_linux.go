//go:build linux

package runner

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/enforce"
	"github.com/bocopile/AgentTaint/internal/policy"
	"github.com/bocopile/AgentTaint/internal/sensor/linux"
	"golang.org/x/sys/unix"
)

const (
	maxAuditBytes      int64 = 64 << 20
	auditQueueCapacity       = 256
	maxRunProcesses          = 65536
	readHealthInterval       = 100 * time.Millisecond
	rootExitGrace            = 300 * time.Millisecond
	childDrainGrace          = 5 * time.Second
)

type auditWriter struct {
	file    *auditFile
	written int64
	failed  error
}

func (w *auditWriter) write(event core.Event) error {
	if w.failed != nil {
		return w.failed
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		w.failed = fmt.Errorf("marshal audit event: %w", err)
		return w.failed
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) > maxAuditBytes-w.written {
		w.failed = fmt.Errorf("audit output exceeded the %d-byte limit", maxAuditBytes)
		return w.failed
	}
	n, err := w.file.file.Write(encoded)
	w.written += int64(n)
	if err != nil {
		w.failed = fmt.Errorf("write audit event: %w", err)
		return w.failed
	}
	if n != len(encoded) {
		w.failed = io.ErrShortWrite
		return w.failed
	}
	return nil
}

type coverageQuality struct {
	mu      sync.Mutex
	counts  map[string]uint64
	unknown bool
}

func (q *coverageQuality) lose(reason string) {
	q.loseN(reason, 1)
}

func (q *coverageQuality) loseN(reason string, count uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.counts == nil {
		q.counts = make(map[string]uint64)
	}
	q.counts[reason] += count
}

func (q *coverageQuality) loseUnknown() {
	q.mu.Lock()
	q.unknown = true
	q.mu.Unlock()
}

func (q *coverageQuality) report() (bool, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.counts) == 0 && !q.unknown {
		return false, ""
	}
	parts := make([]string, 0, len(q.counts)+1)
	for reason, count := range q.counts {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, count))
	}
	if q.unknown {
		parts = append(parts, "drop-counter=unknown")
	}
	sort.Strings(parts)
	return true, strings.Join(parts, ", ")
}

func runObservedPlatform(ctx context.Context, path string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity) (int, error) {
	return runProtectedPlatform(ctx, path, args, stdin, stdout, stderr, auditPath, snapshot, identity, false)
}

func runBlockedPlatform(ctx context.Context, path string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity) (int, error) {
	return runProtectedPlatform(ctx, path, args, stdin, stdout, stderr, auditPath, snapshot, identity, true)
}

func runProtectedPlatform(ctx context.Context, path string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity, blocking bool) (code int, returnErr error) {
	credential, expectedIdentity, err := targetCredential(identity)
	if err != nil {
		return 1, err
	}
	var scope *runCgroup
	var blocker *enforce.Enforcer
	targetStarted := false
	targetFinished := false
	if blocking {
		if snapshot == nil {
			return 1, errors.New("block mode requires a validated policy snapshot")
		}
		projection, projectionErr := snapshot.KernelProjection()
		if projectionErr != nil {
			return 1, fmt.Errorf("compile enforcement policy subset: %w", projectionErr)
		}
		scope, err = createRunCgroup()
		if err != nil {
			return 1, fmt.Errorf("prepare run cgroup scope: %w", err)
		}
		defer func() {
			if targetStarted && !targetFinished {
				if killErr := scope.Kill(); killErr != nil {
					returnErr = errors.Join(returnErr, fmt.Errorf("terminate run cgroup after failure: %w", killErr))
				}
			}
			if blocker != nil {
				returnErr = errors.Join(returnErr, blocker.Close())
			}
			returnErr = errors.Join(returnErr, scope.Close())
		}()
		blocker, err = enforce.New(projection, scope.ID())
		if err != nil {
			return 1, fmt.Errorf("prepare kernel enforcement hooks: %w", err)
		}
	}
	output, err := createAuditFile(auditPath)
	if err != nil {
		return 1, err
	}
	keepAudit := false
	defer func() {
		if !keepAudit {
			_ = output.discard()
		}
	}()

	runIDBytes, err := randomBytes(16)
	if err != nil {
		return 1, fmt.Errorf("create run identity: %w", err)
	}
	scopeBytes, err := randomBytes(8)
	if err != nil {
		return 1, fmt.Errorf("create observation scope: %w", err)
	}
	scopeID := binary.LittleEndian.Uint64(scopeBytes)
	if scopeID == 0 {
		scopeID = 1
	}
	runID := core.RunID(hex.EncodeToString(runIDBytes))
	sensor, err := linux.New(scopeID)
	if err != nil {
		return 1, fmt.Errorf("load Linux observer (run `agenttaint doctor` for environment diagnostics): %w", err)
	}
	defer func() { _ = sensor.Close() }()
	if err := sensor.Barrier(5 * time.Second); err != nil {
		return 1, fmt.Errorf("prepare observer event boundary for scope %d: %w", scopeID, err)
	}
	baselineDrops, err := sensor.DropCounts()
	if err != nil {
		return 1, fmt.Errorf("read initial observer health: %w", err)
	}

	quality := &coverageQuality{}
	var processor *policyAuditProcessor
	if snapshot != nil {
		processor, err = newPolicyAuditProcessor(snapshot)
		if err != nil {
			return 1, fmt.Errorf("prepare policy audit processor: %w", err)
		}
	}
	queue := make(chan core.Event, auditQueueCapacity)
	writer := &auditWriter{file: output}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for event := range queue {
			if err := writer.write(event); err != nil {
				quality.lose("audit-write")
			}
		}
	}()

	var cgroupFD []int
	if scope != nil {
		cgroupFD = append(cgroupFD, scope.FD())
	}
	cmd, controlFD, nonce, err := startObserveHelper(ctx, path, args, stdin, stdout, stderr, credential, expectedIdentity, cgroupFD...)
	if err != nil {
		close(queue)
		<-writerDone
		return 1, fmt.Errorf("prepare target launch: %w", err)
	}
	if err := sensor.TrackProcess(uint32(cmd.Process.Pid)); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		close(queue)
		<-writerDone
		return 1, fmt.Errorf("scope observer to target process: %w", err)
	}
	targetStarted = true
	keepAudit = true
	defer unix.Close(controlFD)

	waitDone := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(waitDone)
	}()
	scopeComplete := make(chan struct{})
	cancelMonitorDone := make(chan struct{})
	go func() {
		defer close(cancelMonitorDone)
		select {
		case <-scopeComplete:
			return
		case <-ctx.Done():
			quality.lose("run-canceled")
		}
		for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL} {
			_ = syscall.Kill(-cmd.Process.Pid, signal)
			if signal == syscall.SIGKILL && scope != nil {
				if err := scope.Kill(); err != nil {
					quality.lose("cgroup-kill")
				}
			}
			delay := 3 * time.Second
			if signal == syscall.SIGTERM {
				delay = time.Second
			}
			if signal == syscall.SIGKILL {
				return
			}
			timer := time.NewTimer(delay)
			select {
			case <-scopeComplete:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	eventReaderReady := make(chan struct{})
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- consumeRunEvents(sensor, uint32(cmd.Process.Pid), runID, baselineDrops, queue, quality, waitDone, eventReaderReady, processor, scope)
	}()
	select {
	case <-eventReaderReady:
	case consumeErr := <-consumeDone:
		quality.lose("observer-readiness")
		if consumeErr != nil {
			quality.lose("observer-read")
		}
		_ = cmd.Process.Kill()
		<-waitDone
		close(queue)
		<-writerDone
		return 1, fmt.Errorf("observer event reader failed before target launch: %w", consumeErr)
	}
	if err := writeLaunchPermit(controlFD, helperStartFrame(nonce)); err != nil {
		quality.lose("launch-permit")
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	launchErr := waitHelperExec(controlFD, 5*time.Second)
	if launchErr != nil {
		quality.lose("target-exec")
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	consumeErr := <-consumeDone
	close(scopeComplete)
	<-cancelMonitorDone
	if consumeErr != nil {
		quality.lose("observer-read")
	}
	close(queue)
	<-writerDone
	for i := 0; i < 10; i++ {
		if err := pollBPFDropCounts(sensor, baselineDrops, quality); err == nil {
			break
		}
		if i == 9 {
			quality.loseUnknown()
		}
		time.Sleep(10 * time.Millisecond)
	}
	closeErr := sensor.Close()
	if closeErr != nil {
		quality.lose("observer-close")
	}
	auditCloseErr := output.close()
	if auditCloseErr != nil {
		quality.lose("audit-close")
	}
	<-waitDone
	if consumeErr == nil && waitErr == nil {
		targetFinished = true
	}
	code = targetExitCode(waitErr, stderr)
	incomplete, detail := quality.report()
	var errs []error
	if launchErr != nil {
		errs = append(errs, launchErr)
	}
	if consumeErr != nil {
		errs = append(errs, consumeErr)
	}
	if writer.failed != nil {
		errs = append(errs, writer.failed)
	}
	if closeErr != nil {
		errs = append(errs, closeErr)
	}
	if auditCloseErr != nil {
		errs = append(errs, auditCloseErr)
	}
	if incomplete {
		errs = append(errs, fmt.Errorf("audit coverage is incomplete: %s", detail))
	}
	if joined := errors.Join(errs...); joined != nil {
		if code == 0 {
			return 1, joined
		}
		return code, joined
	}
	return code, nil
}

func consumeRunEvents(sensor *linux.Sensor, rootPID uint32, runID core.RunID, baseline linux.DropCounts, queue chan<- core.Event, quality *coverageQuality, waitDone <-chan struct{}, readerReady chan<- struct{}, processor *policyAuditProcessor, scope *runCgroup) error {
	members := make(map[core.ProcessKey]struct{})
	var rootKey core.ProcessKey
	rootSeen, rootExited, targetDone := false, false, false
	readySent := false
	var rootGraceUntil, childGraceUntil time.Time
	checkDrops := func() { _ = pollBPFDropCounts(sensor, baseline, quality) }
	for {
		if err := sensor.SetReadDeadline(time.Now().Add(readHealthInterval)); err != nil {
			return fmt.Errorf("set observer read deadline: %w", err)
		}
		if !readySent {
			close(readerReady)
			readySent = true
		}
		raw, err := sensor.Read()
		if err != nil {
			var decodeErr *linux.DecodeError
			if errors.As(err, &decodeErr) {
				quality.lose("raw-decode")
				continue
			}
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				return fmt.Errorf("read observer event: %w", err)
			}
			checkDrops()
			select {
			case <-waitDone:
				targetDone = true
				if !rootSeen {
					quality.lose("root-exec-unobserved")
					return nil
				}
				if !rootExited && rootGraceUntil.IsZero() {
					rootGraceUntil = time.Now().Add(rootExitGrace)
				}
			default:
			}
			if targetDone && !rootExited && !time.Now().Before(rootGraceUntil) {
				quality.lose("root-exit-unobserved")
				delete(members, rootKey)
				rootExited = true
			}
			if rootExited && len(members) == 0 {
				return nil
			}
			if rootExited && !childGraceUntil.IsZero() && !time.Now().Before(childGraceUntil) {
				quality.lose("descendant-remained-after-drain-limit")
				childGraceUntil = time.Time{}
				if scope != nil {
					if err := scope.Kill(); err != nil {
						return fmt.Errorf("terminate remaining run descendants: %w", err)
					}
				}
			}
			continue
		}

		if !rootSeen {
			if raw.Kind != core.ProcessExec || raw.Process.PID != rootPID {
				continue
			}
			rootKey = processKey(raw.Process)
			members[rootKey] = struct{}{}
			rootSeen = true
		}

		member := false
		if raw.Kind == core.ProcessFork {
			if _, ok := members[processKey(raw.Process)]; ok {
				childKey := processKey(raw.Child)
				if childKey.PID == core.ProcessID(raw.Process.PID) {
					quality.lose("unrepresentable-thread-fork")
					continue
				}
				member = true
				if len(members) >= maxRunProcesses {
					quality.lose("run-process-capacity")
				} else {
					members[childKey] = struct{}{}
				}
			}
		} else {
			key := processKey(raw.Process)
			_, member = members[key]
			if raw.Kind == core.ProcessExit && member {
				delete(members, key)
				if key == rootKey {
					rootExited = true
					childGraceUntil = time.Now().Add(childDrainGrace)
				}
			}
		}
		if !member {
			continue
		}
		eventID, err := randomBytes(16)
		if err != nil {
			quality.lose("event-id-generation")
			continue
		}
		event, err := linux.NormalizeEvent(raw, hex.EncodeToString(eventID), runID, time.Now())
		if err != nil {
			quality.lose("event-normalization")
			continue
		}
		auditEvents := []core.Event{event}
		if processor != nil {
			incomplete, detail := quality.report()
			if incomplete {
				processor.MarkIncomplete(detail)
			}
			var processErr error
			auditEvents, processErr = processor.Process(event)
			if processErr != nil {
				quality.lose("policy-audit-processing")
			}
		}
		for _, auditEvent := range auditEvents {
			_ = enqueueAudit(queue, auditEvent, quality)
		}
		if rootExited && len(members) == 0 {
			checkDrops()
			return nil
		}
	}
}

func enqueueAudit(queue chan<- core.Event, event core.Event, quality *coverageQuality) error {
	select {
	case queue <- event:
		return nil
	default:
		quality.lose("writer-queue-full")
		return errors.New("audit queue full")
	}
}

func targetCredential(identity *TargetIdentity) (*syscall.Credential, TargetIdentity, error) {
	if os.Geteuid() == 0 {
		if identity == nil {
			return nil, TargetIdentity{}, fmt.Errorf("root observer requires explicit --run-as UID:GID; target is never started as root")
		}
		if identity.UID == 0 || identity.GID == 0 {
			return nil, TargetIdentity{}, fmt.Errorf("observed target UID and GID must be nonzero")
		}
		return &syscall.Credential{Uid: identity.UID, Gid: identity.GID, Groups: []uint32{}}, *identity, nil
	}
	if identity != nil && (identity.UID != uint32(os.Geteuid()) || identity.GID != uint32(os.Getegid())) {
		return nil, TargetIdentity{}, fmt.Errorf("non-root observer cannot change target identity")
	}
	if identity == nil {
		identity = &TargetIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	}
	return nil, *identity, nil
}

func startObserveHelper(ctx context.Context, targetPath string, targetArgs []string, stdin io.Reader, stdout, stderr io.Writer, credential *syscall.Credential, identity TargetIdentity, cgroupFD ...int) (*exec.Cmd, int, string, error) {
	if ctx == nil {
		return nil, -1, "", fmt.Errorf("nil observation context")
	}
	if err := ctx.Err(); err != nil {
		return nil, -1, "", fmt.Errorf("observation canceled before target launch: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, -1, "", fmt.Errorf("locate observer launch helper: %w", err)
	}
	nonceBytes, err := randomBytes(16)
	if err != nil {
		return nil, -1, "", fmt.Errorf("create launch permit identity: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, -1, "", fmt.Errorf("create launch control channel: %w", err)
	}
	parentFD, childFD := fds[0], fds[1]
	childFile := os.NewFile(uintptr(childFD), "observe-launch-child")
	if err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		_ = childFile.Close()
		_ = unix.Close(parentFD)
		return nil, -1, "", fmt.Errorf("mark supervisor descriptors close-on-exec: %w", err)
	}
	clearGroups := credential != nil
	helperArgs := append([]string{observeHelperArg, targetPath, strconv.FormatUint(uint64(identity.UID), 10), strconv.FormatUint(uint64(identity.GID), 10), strconv.FormatBool(clearGroups)}, targetArgs...)
	cmd := exec.Command(executable, helperArgs...)
	cmd.ExtraFiles = []*os.File{childFile}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.Env = helperEnvironment(os.Environ(), nonce)
	attributes := &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL, Credential: credential}
	if len(cgroupFD) > 0 && cgroupFD[0] >= 0 {
		attributes.UseCgroupFD = true
		attributes.CgroupFD = cgroupFD[0]
	}
	cmd.SysProcAttr = attributes
	if err := cmd.Start(); err != nil {
		_ = childFile.Close()
		_ = unix.Close(parentFD)
		return nil, -1, "", fmt.Errorf("start gated target helper: %w", err)
	}
	_ = childFile.Close()
	if err := waitControlFrame(ctx, parentFD, []byte("READY"), 5*time.Second); err != nil {
		_ = unix.Close(parentFD)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, -1, "", fmt.Errorf("target helper did not become ready: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = unix.Close(parentFD)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, -1, "", fmt.Errorf("observation canceled before launch permit: %w", err)
	}
	return cmd, parentFD, nonce, nil
}

func helperEnvironment(environment []string, nonce string) []string {
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, observeHelperEnv+"=") {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, observeHelperEnv+"="+nonce)
}

func helperStartFrame(nonce string) []byte { return []byte("START:" + nonce) }

func waitControlFrame(ctx context.Context, fd int, expected []byte, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	frame := make([]byte, len(expected))
	read := 0
	for read < len(frame) {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return os.ErrDeadlineExceeded
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		millis := int(remaining / time.Millisecond)
		if millis > 100 {
			millis = 100
		}
		ready, err := unix.Poll(poll, millis)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if ready == 0 {
			continue
		}
		n, err := unix.Read(fd, frame[read:])
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		read += n
	}
	if string(frame) != string(expected) {
		return fmt.Errorf("invalid target helper response %q", frame)
	}
	return nil
}

func writeLaunchPermit(fd int, permit []byte) error {
	for len(permit) > 0 {
		n, err := unix.Write(fd, permit)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		permit = permit[n:]
	}
	return nil
}

func waitHelperExec(fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var response []byte
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("target exec handshake timed out")
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		millis := int(remaining / time.Millisecond)
		if millis > 100 {
			millis = 100
		}
		ready, err := unix.Poll(poll, millis)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("wait for target exec handshake: %w", err)
		}
		if ready == 0 {
			continue
		}
		var chunk [256]byte
		n, err := unix.Read(fd, chunk[:])
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return fmt.Errorf("read target exec handshake: %w", err)
		}
		if n == 0 {
			if len(response) == 0 {
				return nil
			}
			return fmt.Errorf("target helper rejected exec: %s", response)
		}
		response = append(response, chunk[:n]...)
		if len(response) > 4096 {
			return fmt.Errorf("target helper response exceeded limit")
		}
	}
}

func processKey(process linux.RawProcess) core.ProcessKey {
	return core.ProcessKey{
		PID:     core.ProcessID(process.PID),
		BirthID: strconv.FormatUint(process.BirthID, 10),
		ScopeID: strconv.FormatUint(process.ScopeID, 10),
	}
}

func pollBPFDropCounts(sensor *linux.Sensor, baseline linux.DropCounts, quality *coverageQuality) error {
	current, err := sensor.DropCounts()
	if err != nil {
		quality.loseUnknown()
		return err
	}
	reasons := [...]string{"process-ring-reserve", "process-capture", "file-ring-reserve", "file-capture", "network-ring-reserve", "network-capture", "file-syscall-args", "file-process", "file-pending-map", "file-path-unreadable-success", "file-path-unreadable-other-error"}
	for index, count := range current {
		if count > baseline[index] {
			quality.loseN(reasons[index], count-baseline[index])
		}
	}
	return nil
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}

func targetExitCode(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			fmt.Fprintf(stderr, "agenttaint: target terminated by signal: %v\n", status.Signal())
			return 128 + int(status.Signal())
		}
	}
	return 1
}
