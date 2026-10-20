package linux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bocopile/AgentTaint/internal/core"
)

const (
	rawMagic                uint32 = 0x41544745
	rawVersion              uint16 = 1
	rawHeaderSize                  = 24
	rawProcessSize                 = 48
	rawExecPathSize                = 256
	rawForkSize                    = rawHeaderSize + 2*rawProcessSize
	rawExecSize                    = rawHeaderSize + rawProcessSize + rawExecPathSize
	rawExitSize                    = rawHeaderSize + rawProcessSize
	rawFileOpenSize                = rawHeaderSize + rawProcessSize + rawExecPathSize + 8
	rawNetworkConnectSize          = rawHeaderSize + rawProcessSize + 8
	rawKindFork             byte   = 1
	rawKindExec             byte   = 2
	rawKindExit             byte   = 3
	rawKindFileOpen         byte   = 4
	rawKindNetworkConnect   byte   = 5
	rawFlagPathTruncated           = 1
	rawFlagPathRelative            = 2
	rawFlagOperationFailed         = 4
	rawFlagOperationUnknown        = 8
)

// RawProcessEvent is the decoded, versioned ring-buffer ABI. MonotonicNS orders
// kernel observations within one boot; it is not a wall-clock timestamp.
type RawProcessEvent struct {
	Kind            core.EventKind
	MonotonicNS     uint64
	Process         RawProcess
	Child           RawProcess
	Path            string
	PathTruncated   bool
	PathRelative    bool
	Access          core.FileAccess
	Address         [4]byte
	Port            uint16
	OperationResult core.OperationResult
}

// RawProcess contains kernel-observed process identity fields. BirthID is the
// group leader's start_boottime; ScopeID is supplied by the sensor instance.
type RawProcess struct {
	PID       uint32
	PPID      uint32
	PGID      uint32
	SessionID uint32
	BirthID   uint64
	ScopeID   uint64
	Comm      string
}

// DecodeRawProcessEvent validates and decodes one native-endian process record.
// The byte order is identified from the magic value, so bpfel and bpfeb objects
// use the same decoder without guessing from the userspace architecture.
func DecodeRawEvent(data []byte) (RawProcessEvent, error) {
	if len(data) < rawHeaderSize {
		return RawProcessEvent{}, errors.New("raw process event: truncated header")
	}

	order, err := rawByteOrder(data[:4])
	if err != nil {
		return RawProcessEvent{}, err
	}
	version := order.Uint16(data[4:6])
	length := int(order.Uint16(data[6:8]))
	monotonicNS := order.Uint64(data[8:16])
	kind, flags := data[16], data[17]
	if version != rawVersion {
		return RawProcessEvent{}, fmt.Errorf("raw process event: unsupported version %d", version)
	}
	if monotonicNS == 0 {
		return RawProcessEvent{}, errors.New("raw process event: zero monotonic timestamp")
	}
	if order.Uint16(data[18:20]) != 0 || order.Uint32(data[20:24]) != 0 {
		return RawProcessEvent{}, errors.New("raw process event: nonzero reserved field")
	}
	if length != len(data) {
		return RawProcessEvent{}, fmt.Errorf("raw process event: header length %d does not match record length %d", length, len(data))
	}

	event := RawProcessEvent{MonotonicNS: monotonicNS}
	switch kind {
	case rawKindFork:
		if length != rawForkSize || flags != 0 {
			return RawProcessEvent{}, errors.New("raw process event: invalid fork size or flags")
		}
		event.Kind = core.ProcessFork
		event.OperationResult = core.ResultSuccess
		event.Process, err = decodeRawProcess(order, data[rawHeaderSize:rawHeaderSize+rawProcessSize])
		if err == nil {
			event.Child, err = decodeRawProcess(order, data[rawHeaderSize+rawProcessSize:])
		}
	case rawKindExec:
		if length != rawExecSize || flags&^byte(rawFlagPathTruncated) != 0 {
			return RawProcessEvent{}, errors.New("raw process event: invalid exec size or flags")
		}
		event.Kind = core.ProcessExec
		event.OperationResult = core.ResultSuccess
		event.PathTruncated = flags&rawFlagPathTruncated != 0
		event.Process, err = decodeRawProcess(order, data[rawHeaderSize:rawHeaderSize+rawProcessSize])
		if err == nil {
			event.Path, err = decodeRawText(data[rawHeaderSize+rawProcessSize:], "executable path")
		}
	case rawKindExit:
		if length != rawExitSize || flags != 0 {
			return RawProcessEvent{}, errors.New("raw process event: invalid exit size or flags")
		}
		event.Kind = core.ProcessExit
		event.OperationResult = core.ResultSuccess
		event.Process, err = decodeRawProcess(order, data[rawHeaderSize:])
	case rawKindFileOpen:
		const allowed = byte(rawFlagPathTruncated | rawFlagPathRelative | rawFlagOperationFailed | rawFlagOperationUnknown)
		if length != rawFileOpenSize || flags&^allowed != 0 || flags&rawFlagOperationFailed != 0 && flags&rawFlagOperationUnknown != 0 {
			return RawProcessEvent{}, errors.New("raw process event: invalid file-open size or flags")
		}
		event.Kind = core.FileOpen
		event.PathTruncated = flags&rawFlagPathTruncated != 0
		event.PathRelative = flags&rawFlagPathRelative != 0
		event.OperationResult = operationResult(flags)
		event.Process, err = decodeRawProcess(order, data[rawHeaderSize:rawHeaderSize+rawProcessSize])
		if err == nil {
			event.Path, err = decodeRawText(data[rawHeaderSize+rawProcessSize:rawHeaderSize+rawProcessSize+rawExecPathSize], "file path")
		}
		accessOffset := rawHeaderSize + rawProcessSize + rawExecPathSize
		if err == nil {
			switch data[accessOffset] {
			case 0:
				event.Access = core.AccessUnknown
			case 1:
				event.Access = core.AccessRead
			case 2:
				event.Access = core.AccessWrite
			case 3:
				event.Access = core.AccessReadWrite
			default:
				err = errors.New("raw process event: unknown file access mode")
			}
		}
		if err == nil {
			for _, value := range data[accessOffset+1:] {
				if value != 0 {
					err = errors.New("raw process event: nonzero file-open padding")
					break
				}
			}
		}
	case rawKindNetworkConnect:
		const allowed = byte(rawFlagOperationFailed | rawFlagOperationUnknown)
		if length != rawNetworkConnectSize || flags&^allowed != 0 || flags&rawFlagOperationFailed != 0 && flags&rawFlagOperationUnknown != 0 {
			return RawProcessEvent{}, errors.New("raw process event: invalid network-connect size or flags")
		}
		event.Kind = core.NetworkConnect
		event.OperationResult = operationResult(flags)
		event.Process, err = decodeRawProcess(order, data[rawHeaderSize:rawHeaderSize+rawProcessSize])
		if err == nil {
			copy(event.Address[:], data[rawHeaderSize+rawProcessSize:rawHeaderSize+rawProcessSize+4])
			event.Port = order.Uint16(data[rawHeaderSize+rawProcessSize+4 : rawHeaderSize+rawProcessSize+6])
			if order.Uint16(data[rawHeaderSize+rawProcessSize+6:]) != 0 {
				err = errors.New("raw process event: nonzero network-connect padding")
			}
		}
	default:
		return RawProcessEvent{}, fmt.Errorf("raw process event: unknown kind %d", kind)
	}
	if err != nil {
		return RawProcessEvent{}, err
	}
	return event, nil
}

// DecodeRawProcessEvent is retained for callers compiled against the initial
// process-only observer API. The record ABI now also carries file/network events.
func DecodeRawProcessEvent(data []byte) (RawProcessEvent, error) {
	return DecodeRawEvent(data)
}

func operationResult(flags byte) core.OperationResult {
	if flags&rawFlagOperationUnknown != 0 {
		return core.ResultUnknown
	}
	if flags&rawFlagOperationFailed != 0 {
		return core.ResultFailure
	}
	return core.ResultSuccess
}

// NormalizeEvent dispatches one raw ring-buffer event to the frozen core schema.
func NormalizeEvent(raw RawProcessEvent, eventID string, runID core.RunID, observedAt time.Time) (core.Event, error) {
	switch raw.Kind {
	case core.ProcessFork, core.ProcessExec, core.ProcessExit:
		return NormalizeProcessEvent(raw, eventID, runID, observedAt)
	case core.FileOpen:
		return NormalizeFileOpenEvent(raw, eventID, runID, observedAt)
	case core.NetworkConnect:
		return NormalizeNetworkConnectEvent(raw, eventID, runID, observedAt)
	default:
		return nil, fmt.Errorf("unsupported raw event kind %q", raw.Kind)
	}
}

// NormalizeProcessEvent converts one complete kernel observation to the frozen
// core schema. observedAt is userspace receive time; MonotonicNS remains useful
// for ordering and is deliberately not misrepresented as wall-clock time.
func NormalizeProcessEvent(raw RawProcessEvent, eventID string, runID core.RunID, observedAt time.Time) (core.Event, error) {
	if raw.Kind != core.ProcessFork && raw.Kind != core.ProcessExec && raw.Kind != core.ProcessExit {
		return nil, fmt.Errorf("unsupported process event kind %q", raw.Kind)
	}
	if eventID == "" || runID == "" || observedAt.IsZero() {
		return nil, errors.New("process event metadata must be complete")
	}
	process, err := normalizeProcess(raw.Process)
	if err != nil {
		return nil, fmt.Errorf("process context: %w", err)
	}
	metadata := core.Metadata{
		SchemaVersion:    core.SchemaVersion,
		EventID:          eventID,
		RunID:            runID,
		Timestamp:        observedAt.UTC(),
		SessionID:        core.SessionID(raw.Process.SessionID),
		ObservationStage: core.StageCompleted,
		OperationResult:  raw.OperationResult,
		Process:          process,
	}
	if metadata.OperationResult == "" {
		metadata.OperationResult = core.ResultSuccess
	}
	switch raw.Kind {
	case core.ProcessFork:
		child, err := normalizeProcess(raw.Child)
		if err != nil {
			return nil, fmt.Errorf("fork child: %w", err)
		}
		return &core.ProcessForkEvent{Metadata: metadata, Child: child}, nil
	case core.ProcessExec:
		if raw.PathTruncated {
			return nil, errors.New("executable path was truncated")
		}
		if raw.Path == "" || !utf8.ValidString(raw.Path) {
			return nil, errors.New("executable path is empty or invalid UTF-8")
		}
		return &core.ProcessExecEvent{Metadata: metadata, Executable: core.FileResource{Path: raw.Path}}, nil
	case core.ProcessExit:
		return &core.ProcessExitEvent{Metadata: metadata}, nil
	default:
		return nil, fmt.Errorf("unsupported process event kind %q", raw.Kind)
	}
}

// NormalizeFileOpenEvent records the syscall's requested pathname and access
// intent. It does not resolve dirfd, symlinks, or prove bytes were read.
func NormalizeFileOpenEvent(raw RawProcessEvent, eventID string, runID core.RunID, observedAt time.Time) (core.Event, error) {
	if raw.Kind != core.FileOpen {
		return nil, fmt.Errorf("unsupported file event kind %q", raw.Kind)
	}
	if raw.PathTruncated {
		return nil, errors.New("file path was truncated")
	}
	if raw.Path == "" || !utf8.ValidString(raw.Path) {
		return nil, errors.New("file path is empty or invalid UTF-8")
	}
	metadata, err := normalizeOperationMetadata(raw, eventID, runID, observedAt)
	if err != nil {
		return nil, err
	}
	return &core.FileOpenEvent{Metadata: metadata, Resource: core.FileResource{Path: raw.Path}, Access: raw.Access}, nil
}

// NormalizeNetworkConnectEvent normalizes the IPv4 sockaddr and syscall result.
// It does not imply that the peer completed a handshake or received data.
func NormalizeNetworkConnectEvent(raw RawProcessEvent, eventID string, runID core.RunID, observedAt time.Time) (core.Event, error) {
	if raw.Kind != core.NetworkConnect {
		return nil, fmt.Errorf("unsupported network event kind %q", raw.Kind)
	}
	metadata, err := normalizeOperationMetadata(raw, eventID, runID, observedAt)
	if err != nil {
		return nil, err
	}
	return &core.NetworkConnectEvent{
		Metadata: metadata,
		Destination: core.NetworkDestination{
			Address: netip.AddrFrom4(raw.Address),
			Port:    raw.Port,
		},
	}, nil
}

func normalizeOperationMetadata(raw RawProcessEvent, eventID string, runID core.RunID, observedAt time.Time) (core.Metadata, error) {
	if eventID == "" || runID == "" || observedAt.IsZero() {
		return core.Metadata{}, errors.New("process event metadata must be complete")
	}
	process, err := normalizeProcess(raw.Process)
	if err != nil {
		return core.Metadata{}, fmt.Errorf("process context: %w", err)
	}
	result := raw.OperationResult
	if result == "" {
		result = core.ResultUnknown
	}
	return core.Metadata{
		SchemaVersion:    core.SchemaVersion,
		EventID:          eventID,
		RunID:            runID,
		Timestamp:        observedAt.UTC(),
		SessionID:        core.SessionID(raw.Process.SessionID),
		ObservationStage: core.StageCompleted,
		OperationResult:  result,
		Process:          process,
	}, nil
}

func rawByteOrder(magic []byte) (binary.ByteOrder, error) {
	switch {
	case binary.LittleEndian.Uint32(magic) == rawMagic:
		return binary.LittleEndian, nil
	case binary.BigEndian.Uint32(magic) == rawMagic:
		return binary.BigEndian, nil
	default:
		return nil, errors.New("raw process event: bad magic")
	}
}

func decodeRawProcess(order binary.ByteOrder, data []byte) (RawProcess, error) {
	if len(data) != rawProcessSize {
		return RawProcess{}, errors.New("raw process event: invalid process record size")
	}
	comm, err := decodeRawText(data[32:48], "comm")
	if err != nil {
		return RawProcess{}, err
	}
	process := RawProcess{
		PID:       order.Uint32(data[0:4]),
		PPID:      order.Uint32(data[4:8]),
		PGID:      order.Uint32(data[8:12]),
		SessionID: order.Uint32(data[12:16]),
		BirthID:   order.Uint64(data[16:24]),
		ScopeID:   order.Uint64(data[24:32]),
		Comm:      comm,
	}
	if process.PID == 0 || process.BirthID == 0 || process.ScopeID == 0 || process.SessionID == 0 {
		return RawProcess{}, errors.New("raw process event: missing pid, birth, scope, or session identity")
	}
	return process, nil
}

func decodeRawText(data []byte, field string) (string, error) {
	end := strings.IndexByte(string(data), 0)
	if end < 0 {
		return "", fmt.Errorf("raw process event: unterminated %s", field)
	}
	text := string(data[:end])
	if text == "" || !utf8.ValidString(text) {
		return "", fmt.Errorf("raw process event: empty or invalid UTF-8 %s", field)
	}
	return text, nil
}

func normalizeProcess(raw RawProcess) (core.ProcessContext, error) {
	if raw.PID == 0 || raw.BirthID == 0 || raw.ScopeID == 0 || raw.SessionID == 0 || raw.Comm == "" || !utf8.ValidString(raw.Comm) {
		return core.ProcessContext{}, errors.New("missing or invalid process identity")
	}
	return core.ProcessContext{
		ProcessKey: core.ProcessKey{
			PID:     core.ProcessID(raw.PID),
			BirthID: fmt.Sprintf("%d", raw.BirthID),
			ScopeID: fmt.Sprintf("%d", raw.ScopeID),
		},
		PPID: core.ProcessID(raw.PPID),
		PGID: core.ProcessGroupID(raw.PGID),
		Comm: raw.Comm,
	}, nil
}
