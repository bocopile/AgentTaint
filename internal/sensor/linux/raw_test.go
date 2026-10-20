package linux

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
)

func TestDecodeAndNormalizeProcessEvents(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{name: "little endian", order: binary.LittleEndian},
		{name: "big endian", order: binary.BigEndian},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []byte{rawKindFork, rawKindExec, rawKindExit} {
				raw := validRawRecord(tc.order, kind)
				decoded, err := DecodeRawProcessEvent(raw)
				if err != nil {
					t.Fatalf("DecodeRawProcessEvent: %v", err)
				}
				if decoded.MonotonicNS != 123456 || decoded.Process.BirthID != 77 || decoded.Process.ScopeID != 99 {
					t.Fatalf("decoded identity/timestamp: %+v", decoded)
				}
				event, err := NormalizeProcessEvent(decoded, "event-a", "run-a", time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC))
				if err != nil {
					t.Fatalf("NormalizeProcessEvent: %v", err)
				}
				encoded, err := json.Marshal(event)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				if _, err := core.DecodeEvent(encoded); err != nil {
					t.Fatalf("core.DecodeEvent rejected normalized event %s: %v", encoded, err)
				}
			}
		})
	}
}

func TestDecodeAndNormalizeOperationEvents(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{name: "little endian", order: binary.LittleEndian},
		{name: "big endian", order: binary.BigEndian},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, kind := range []byte{rawKindFileOpen, rawKindNetworkConnect} {
				data := validOperationRecord(tc.order, kind)
				raw, err := DecodeRawEvent(data)
				if err != nil {
					t.Fatalf("DecodeRawEvent: %v", err)
				}
				var event core.Event
				if kind == rawKindFileOpen {
					if raw.Path != "fixtures/secret.txt" || !raw.PathRelative || raw.Access != core.AccessRead || raw.OperationResult != core.ResultSuccess {
						t.Fatalf("decoded file open: %+v", raw)
					}
					event, err = NormalizeFileOpenEvent(raw, "event-file", "run-a", time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC))
				} else {
					if raw.Address != [4]byte{192, 0, 2, 10} || raw.Port != 443 || raw.OperationResult != core.ResultSuccess {
						t.Fatalf("decoded network connect: %+v", raw)
					}
					event, err = NormalizeNetworkConnectEvent(raw, "event-net", "run-a", time.Date(2026, 10, 19, 1, 2, 3, 0, time.UTC))
				}
				if err != nil {
					t.Fatalf("normalize operation event: %v", err)
				}
				encoded, err := json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := core.DecodeEvent(encoded); err != nil {
					t.Fatalf("core.DecodeEvent rejected %s: %v", encoded, err)
				}
			}
		})
	}
}

func TestOperationEventErrorsAndPathQuality(t *testing.T) {
	order := binary.LittleEndian
	base := validOperationRecord(order, rawKindFileOpen)
	tests := []struct {
		name   string
		change func([]byte)
	}{
		{"truncated path", func(b []byte) { b[17] |= rawFlagPathTruncated }},
		{"invalid UTF-8 path", func(b []byte) { b[rawHeaderSize+rawProcessSize] = 0xff }},
		{"invalid access", func(b []byte) { b[rawHeaderSize+rawProcessSize+rawExecPathSize] = 4 }},
		{"nonzero padding", func(b []byte) { b[len(b)-1] = 1 }},
		{"contradictory result flags", func(b []byte) { b[17] = rawFlagOperationFailed | rawFlagOperationUnknown }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), base...)
			tc.change(data)
			raw, err := DecodeRawEvent(data)
			if err == nil {
				if _, normalizeErr := NormalizeFileOpenEvent(raw, "event", "run", time.Now()); normalizeErr == nil {
					t.Fatal("invalid operation event was accepted")
				}
			}
		})
	}

	failed := append([]byte(nil), base...)
	failed[17] = rawFlagOperationFailed
	raw, err := DecodeRawEvent(failed)
	if err != nil || raw.OperationResult != core.ResultFailure {
		t.Fatalf("failed syscall result: raw=%+v err=%v", raw, err)
	}
	network := validOperationRecord(order, rawKindNetworkConnect)
	network[17] = rawFlagOperationUnknown
	raw, err = DecodeRawEvent(network)
	if err != nil || raw.OperationResult != core.ResultUnknown {
		t.Fatalf("in-progress syscall result: raw=%+v err=%v", raw, err)
	}
}

func TestDecodeRawProcessEventRejectsMalformedABI(t *testing.T) {
	order := binary.LittleEndian
	base := validRawRecord(order, rawKindExec)
	tests := []struct {
		name   string
		change func([]byte) []byte
	}{
		{name: "short header", change: func(b []byte) []byte { return b[:rawHeaderSize-1] }},
		{name: "bad magic", change: func(b []byte) []byte { b[0] ^= 0xff; return b }},
		{name: "unknown version", change: func(b []byte) []byte { order.PutUint16(b[4:6], rawVersion+1); return b }},
		{name: "length mismatch", change: func(b []byte) []byte { order.PutUint16(b[6:8], uint16(len(b)-1)); return b }},
		{name: "reserved bytes", change: func(b []byte) []byte { b[18] = 1; return b }},
		{name: "unknown kind", change: func(b []byte) []byte { b[16] = 9; return b }},
		{name: "unknown flags", change: func(b []byte) []byte { b[17] = 0x80; return b }},
		{name: "missing process birth", change: func(b []byte) []byte { order.PutUint64(b[rawHeaderSize+16:rawHeaderSize+24], 0); return b }},
		{name: "invalid comm utf8", change: func(b []byte) []byte { b[rawHeaderSize+32] = 0xff; b[rawHeaderSize+33] = 0; return b }},
		{name: "unterminated comm", change: func(b []byte) []byte {
			for i := rawHeaderSize + 32; i < rawHeaderSize+48; i++ {
				b[i] = 'a'
			}
			return b
		}},
		{name: "empty path", change: func(b []byte) []byte { b[rawHeaderSize+rawProcessSize] = 0; return b }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte(nil), base...)
			if _, err := DecodeRawProcessEvent(tc.change(data)); err == nil {
				t.Fatal("malformed event was accepted")
			}
		})
	}
	if _, err := DecodeRawProcessEvent(append(base, 0)); err == nil {
		t.Fatal("oversized event was accepted")
	}
}

func TestNormalizeProcessEventRejectsUnrepresentablePath(t *testing.T) {
	for _, raw := range []RawProcessEvent{
		{Kind: core.ProcessExec, Process: testRawProcess(), Path: "/bin/tool", PathTruncated: true},
		{Kind: core.ProcessExec, Process: testRawProcess(), Path: string([]byte{0xff})},
	} {
		if _, err := NormalizeProcessEvent(raw, "event", "run", time.Now()); err == nil {
			t.Fatal("unrepresentable path was accepted")
		}
	}
	if _, err := NormalizeProcessEvent(RawProcessEvent{Kind: core.ProcessExec, Process: testRawProcess(), Path: "/bin/tool"}, "", "run", time.Now()); err == nil {
		t.Fatal("missing event metadata was accepted")
	}
}

func validRawRecord(order binary.ByteOrder, kind byte) []byte {
	length := rawForkSize
	switch kind {
	case rawKindExec:
		length = rawExecSize
	case rawKindExit:
		length = rawExitSize
	}
	data := make([]byte, length)
	order.PutUint32(data[0:4], rawMagic)
	order.PutUint16(data[4:6], rawVersion)
	order.PutUint16(data[6:8], uint16(length))
	order.PutUint64(data[8:16], 123456)
	data[16] = kind
	putRawProcess(order, data[rawHeaderSize:rawHeaderSize+rawProcessSize], testRawProcess())
	switch kind {
	case rawKindFork:
		child := testRawProcess()
		child.PID, child.PPID, child.BirthID = 102, 101, 78
		putRawProcess(order, data[rawHeaderSize+rawProcessSize:], child)
	case rawKindExec:
		copy(data[rawHeaderSize+rawProcessSize:], append([]byte("/bin/tool"), 0))
	}
	return data
}

func validOperationRecord(order binary.ByteOrder, kind byte) []byte {
	length := rawFileOpenSize
	if kind == rawKindNetworkConnect {
		length = rawNetworkConnectSize
	}
	data := make([]byte, length)
	order.PutUint32(data[0:4], rawMagic)
	order.PutUint16(data[4:6], rawVersion)
	order.PutUint16(data[6:8], uint16(length))
	order.PutUint64(data[8:16], 123456)
	data[16] = kind
	putRawProcess(order, data[rawHeaderSize:rawHeaderSize+rawProcessSize], testRawProcess())
	if kind == rawKindFileOpen {
		copy(data[rawHeaderSize+rawProcessSize:], append([]byte("fixtures/secret.txt"), 0))
		data[17] = rawFlagPathRelative
		data[rawHeaderSize+rawProcessSize+rawExecPathSize] = 1
	} else {
		offset := rawHeaderSize + rawProcessSize
		copy(data[offset:offset+4], []byte{192, 0, 2, 10})
		order.PutUint16(data[offset+4:offset+6], 443)
	}
	return data
}

func putRawProcess(order binary.ByteOrder, data []byte, process RawProcess) {
	order.PutUint32(data[0:4], process.PID)
	order.PutUint32(data[4:8], process.PPID)
	order.PutUint32(data[8:12], process.PGID)
	order.PutUint32(data[12:16], process.SessionID)
	order.PutUint64(data[16:24], process.BirthID)
	order.PutUint64(data[24:32], process.ScopeID)
	copy(data[32:48], append([]byte(process.Comm), 0))
}

func testRawProcess() RawProcess {
	return RawProcess{PID: 101, PPID: 1, PGID: 101, SessionID: 99, BirthID: 77, ScopeID: 99, Comm: "agent"}
}

func TestRawEventErrorNamesStayUseful(t *testing.T) {
	data := validRawRecord(binary.LittleEndian, rawKindExec)
	data[rawHeaderSize+rawProcessSize] = 0
	_, err := DecodeRawProcessEvent(data)
	if err == nil || !strings.Contains(err.Error(), "executable path") {
		t.Fatalf("unexpected error: %v", err)
	}
}
