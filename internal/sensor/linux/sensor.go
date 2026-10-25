//go:build linux

package linux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

// Sensor owns the observer's BPF maps, programs, links, and reader.
// It observes process/file/network events and performs no policy or enforcement.
type Sensor struct {
	objects  processObjects
	reader   *ringbuf.Reader
	links    []link.Link
	once     sync.Once
	closeErr error
}

// DecodeError identifies an invalid ring-buffer record. Consumers may count it
// as a permanent coverage loss and continue reading subsequent records.
type DecodeError struct{ Err error }

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decode observer ring-buffer record: %v", e.Err)
}
func (e *DecodeError) Unwrap() error { return e.Err }

// DropCounts reports cumulative per-CPU BPF-side loss counters in stable reason
// order. A failed lookup is unknown coverage, never an implicit zero.
type DropCounts [13]uint64

func (s *Sensor) DropCounts() (DropCounts, error) {
	var counts DropCounts
	if s == nil || s.objects.ProcessDrops == nil {
		return counts, errors.New("Linux sensor drop counters are unavailable")
	}
	cpuCount, err := ebpf.PossibleCPU()
	if err != nil {
		return counts, fmt.Errorf("read possible CPU count: %w", err)
	}
	perCPU := make([]uint64, cpuCount)
	for reason := uint32(0); reason < uint32(len(counts)); reason++ {
		if err := s.objects.ProcessDrops.Lookup(&reason, &perCPU); err != nil {
			return DropCounts{}, fmt.Errorf("read BPF drop counter %d: %w", reason, err)
		}
		for _, count := range perCPU {
			counts[reason] += count
		}
	}
	return counts, nil
}

// Barrier consumes the ring through a supervisor-owned exec marker. Once it
// returns, records reserved before the marker are drained before a target PID
// can be selected as this run's root.
func (s *Sensor) Barrier(timeout time.Duration) error {
	if s == nil || s.reader == nil {
		return errors.New("Linux sensor is closed")
	}
	if timeout <= 0 {
		return errors.New("sensor barrier timeout must be positive")
	}
	s.reader.SetDeadline(time.Now().Add(timeout))
	defer s.reader.SetDeadline(time.Time{})
	markerPath, err := exec.LookPath("true")
	if err != nil {
		return fmt.Errorf("find sensor barrier marker executable: %w", err)
	}
	marker := exec.Command(markerPath)
	if err := marker.Start(); err != nil {
		return fmt.Errorf("start sensor barrier marker: %w", err)
	}
	readEvents := 0
	decodeErrors := 0
	markerExecEvents := 0
	var last RawProcessEvent
	for {
		event, err := s.Read()
		if err != nil {
			var decodeErr *DecodeError
			if errors.As(err, &decodeErr) {
				decodeErrors++
				continue
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				drops, dropsErr := s.DropCounts()
				_ = marker.Wait()
				return fmt.Errorf("sensor barrier exec marker was not observed before timeout (marker_pid=%d events=%d decode_errors=%d marker_exec_events=%d last_kind=%s last_pid=%d drops=%v drops_error=%v): %w", marker.Process.Pid, readEvents, decodeErrors, markerExecEvents, last.Kind, last.Process.PID, drops, dropsErr, err)
			}
			_ = marker.Wait()
			return fmt.Errorf("read sensor barrier: %w", err)
		}
		readEvents++
		last = event
		if event.Kind == core.ProcessExec && event.Process.PID == uint32(marker.Process.Pid) && event.Path == markerPath {
			markerExecEvents++
			if err := marker.Wait(); err != nil {
				return fmt.Errorf("wait for sensor barrier marker: %w", err)
			}
			return nil
		}
	}
}

// SetReadDeadline bounds a ring-buffer read so supervisor health can be polled.
func (s *Sensor) SetReadDeadline(deadline time.Time) error {
	if s == nil || s.reader == nil {
		return errors.New("Linux sensor is closed")
	}
	s.reader.SetDeadline(deadline)
	return nil
}

// TrackProcess seeds one process as the root of this sensor's run scope.
// Descendants are enrolled from fork events; unrelated host processes are
// ignored after tracking is enabled.
func (s *Sensor) TrackProcess(pid uint32) error {
	if s == nil || s.objects.TrackedProcesses == nil || s.objects.ProcessTrackingEnabled == nil {
		return errors.New("Linux sensor process tracking is unavailable")
	}
	if pid == 0 {
		return errors.New("Linux sensor tracked PID must be nonzero")
	}
	scopeID := uint64(0)
	if err := s.objects.ProcessScope.Lookup(new(uint32), &scopeID); err != nil {
		return fmt.Errorf("read process observer scope: %w", err)
	}
	if err := s.objects.TrackedProcesses.Update(&pid, &scopeID, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("track process %d: %w", pid, err)
	}
	enabled := uint32(1)
	if err := s.objects.ProcessTrackingEnabled.Update(new(uint32), &enabled, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("enable scoped process tracking: %w", err)
	}
	return nil
}

// New loads the process observer and attaches all required typed tracepoints
// before returning. scopeID is an opaque nonzero ID for this observation scope.
func New(scopeID uint64) (*Sensor, error) {
	if scopeID == 0 {
		return nil, errors.New("Linux sensor scope ID must be nonzero")
	}
	if runtime.GOARCH != "arm64" {
		return nil, fmt.Errorf("Linux syscall observer is not supported on %s: generated tp_btf programs use the arm64 syscall ABI", runtime.GOARCH)
	}

	sensor := &Sensor{}
	if err := loadProcessObjects(&sensor.objects, nil); err != nil {
		return nil, fmt.Errorf("load process observer: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = sensor.Close()
		}
	}()

	zero := uint32(0)
	if err := sensor.objects.ProcessScope.Update(&zero, &scopeID, ebpf.UpdateAny); err != nil {
		return nil, fmt.Errorf("configure process observer scope: %w", err)
	}
	reader, err := ringbuf.NewReader(sensor.objects.ProcessEvents)
	if err != nil {
		return nil, fmt.Errorf("open process event ring buffer: %w", err)
	}
	sensor.reader = reader
	for _, target := range []struct {
		name    string
		program *ebpf.Program
	}{
		{name: "sched_process_fork", program: sensor.objects.OnProcessFork},
		{name: "sched_process_exec", program: sensor.objects.OnProcessExec},
		{name: "sched_process_exit", program: sensor.objects.OnProcessExit},
	} {
		attached, err := link.AttachTracing(link.TracingOptions{Program: target.program})
		if err != nil {
			return nil, fmt.Errorf("attach process tracepoint %s: %w", target.name, err)
		}
		sensor.links = append(sensor.links, attached)
	}
	pathEntry, err := link.Kprobe("getname_flags", sensor.objects.OnGetnameFlags, nil)
	if err != nil {
		return nil, fmt.Errorf("attach pathname capture entry probe: %w", err)
	}
	sensor.links = append(sensor.links, pathEntry)
	pathReturn, err := link.Kretprobe("getname_flags", sensor.objects.OnGetnameFlagsReturn, nil)
	if err != nil {
		return nil, fmt.Errorf("attach pathname capture return probe: %w", err)
	}
	sensor.links = append(sensor.links, pathReturn)
	for _, target := range []struct {
		name    string
		program *ebpf.Program
	}{
		{"sys_enter", sensor.objects.OnSysEnter},
		{"sys_exit", sensor.objects.OnSysExit},
	} {
		attached, err := link.AttachTracing(link.TracingOptions{Program: target.program})
		if err != nil {
			return nil, fmt.Errorf("attach arm64 syscall tp_btf %s: %w", target.name, err)
		}
		sensor.links = append(sensor.links, attached)
	}
	cleanup = false
	return sensor, nil
}

// Read blocks until the next raw event record or a reader error. It does not
// fabricate a core event if the kernel record is malformed or incomplete.
func (s *Sensor) Read() (RawProcessEvent, error) {
	if s == nil || s.reader == nil {
		return RawProcessEvent{}, errors.New("Linux sensor is closed")
	}
	record, err := s.reader.Read()
	if err != nil {
		return RawProcessEvent{}, err
	}
	event, err := DecodeRawEvent(record.RawSample)
	if err != nil {
		return RawProcessEvent{}, &DecodeError{Err: err}
	}
	return event, nil
}

// Close releases only resources created by this sensor and is safe to repeat.
func (s *Sensor) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		var errs []error
		if s.reader != nil {
			if err := s.reader.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close process ring buffer: %w", err))
			}
		}
		for i := len(s.links) - 1; i >= 0; i-- {
			if err := s.links[i].Close(); err != nil {
				errs = append(errs, fmt.Errorf("close process link: %w", err))
			}
		}
		if err := s.objects.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close process objects: %w", err))
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}
