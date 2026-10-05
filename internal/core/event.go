package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode/utf8"
)

const SchemaVersion = 1

// Valid events are shallow. Bound the token walk before schema validation; the
// streaming decoder's Token method does not impose a nesting limit itself.
const maxJSONDepth = 32

type EventKind string

const (
	ProcessExec    EventKind = "process_exec"
	ProcessFork    EventKind = "process_fork"
	ProcessExit    EventKind = "process_exit"
	FileOpen       EventKind = "file_open"
	FileRead       EventKind = "file_read"
	FileWrite      EventKind = "file_write"
	NetworkConnect EventKind = "network_connect"
)

type Event interface {
	Kind() EventKind
}

type ObservationStage string

const (
	StageUnknown      ObservationStage = "unknown"
	StageAttempt      ObservationStage = "attempt"
	StagePreOperation ObservationStage = "pre_operation"
	StageCompleted    ObservationStage = "completed"
)

type OperationResult string

const (
	ResultUnknown OperationResult = "unknown"
	ResultSuccess OperationResult = "success"
	ResultFailure OperationResult = "failure"
)

// Metadata is common raw-event context. Timestamp is an observed wall-clock
// instant (RFC3339 JSON), not a raw kernel clock. Neither it nor EventID ensures
// total ordering, uniqueness, or complete observation. Producers supply values;
// core does not mint identities or infer results from observation stages.
type Metadata struct {
	SchemaVersion    int              `json:"schema_version"`
	EventID          string           `json:"event_id"`
	RunID            RunID            `json:"run_id"`
	Timestamp        time.Time        `json:"timestamp"`
	SessionID        SessionID        `json:"session_id,string"`
	ObservationStage ObservationStage `json:"observation_stage"`
	OperationResult  OperationResult  `json:"operation_result"`
	Process          ProcessContext   `json:"process"`
}

type ProcessExecEvent struct {
	Metadata
	Executable FileResource `json:"executable"`
}

// ProcessForkEvent describes parent Process and child Child. No label snapshot
// or run membership is propagated by this data type.
type ProcessForkEvent struct {
	Metadata
	Child ProcessContext `json:"child"`
}

// ProcessExitEvent refers to process lifetime termination, not a thread exit.
// ExitCode is absent if unavailable (including signal-only termination), never
// silently replaced with successful status zero. It is not a raw wait status.
// OperationResult describes the observed operation, not whether ExitCode is zero:
// a successfully observed termination can carry a nonzero application exit code.
type ProcessExitEvent struct {
	Metadata
	ExitCode *int32 `json:"exit_code,omitempty"`
}

type FileOpenEvent struct {
	Metadata
	Resource FileResource `json:"resource"`
	Access   FileAccess   `json:"access"`
}

type FileReadEvent struct {
	Metadata
	Resource FileResource `json:"resource"`
}

type FileWriteEvent struct {
	Metadata
	Resource FileResource `json:"resource"`
}

type NetworkConnectEvent struct {
	Metadata
	Destination NetworkDestination `json:"destination"`
}

func (ProcessExecEvent) Kind() EventKind    { return ProcessExec }
func (ProcessForkEvent) Kind() EventKind    { return ProcessFork }
func (ProcessExitEvent) Kind() EventKind    { return ProcessExit }
func (FileOpenEvent) Kind() EventKind       { return FileOpen }
func (FileReadEvent) Kind() EventKind       { return FileRead }
func (FileWriteEvent) Kind() EventKind      { return FileWrite }
func (NetworkConnectEvent) Kind() EventKind { return NetworkConnect }

func (e ProcessExecEvent) MarshalJSON() ([]byte, error) {
	type plain ProcessExecEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e), e.Executable.Path)
}
func (e ProcessForkEvent) MarshalJSON() ([]byte, error) {
	type plain ProcessForkEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e), processText(e.Child)...)
}
func (e ProcessExitEvent) MarshalJSON() ([]byte, error) {
	type plain ProcessExitEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e))
}
func (e FileOpenEvent) MarshalJSON() ([]byte, error) {
	type plain FileOpenEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e), e.Resource.Path)
}
func (e FileReadEvent) MarshalJSON() ([]byte, error) {
	type plain FileReadEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e), e.Resource.Path)
}
func (e FileWriteEvent) MarshalJSON() ([]byte, error) {
	type plain FileWriteEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e), e.Resource.Path)
}
func (e NetworkConnectEvent) MarshalJSON() ([]byte, error) {
	type plain NetworkConnectEvent
	return marshalEvent(e.Kind(), e.Metadata, plain(e))
}

func processText(process ProcessContext) []string {
	return []string{process.BirthID, process.ScopeID, process.Comm}
}

func marshalEvent(kind EventKind, metadata Metadata, value any, text ...string) ([]byte, error) {
	text = append(text, metadata.EventID, string(metadata.RunID), string(metadata.ObservationStage), string(metadata.OperationResult))
	text = append(text, processText(metadata.Process)...)
	for _, value := range text {
		if !utf8.ValidString(value) {
			return nil, errors.New("event text must be valid UTF-8; invalid bytes cannot be replaced silently")
		}
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	// Kind is one of the fixed constants above. Decode validates the same schema
	// for outbound and inbound events, including nested resources and identities.
	data := append([]byte(`{"event":"`+string(kind)+`",`), body[1:]...)
	if _, err := DecodeEvent(data); err != nil {
		return nil, err
	}
	return data, nil
}

// DecodeEvent decodes exactly one versioned raw event into a concrete pointer.
// It rejects unknown kinds, versions and fields; missing/null required fields;
// duplicate object keys; invalid types; and contradictory stage/result claims.
// Unknown observations must use the explicit "unknown" enum values.
func DecodeEvent(data []byte) (Event, error) {
	if err := checkJSON(data); err != nil {
		return nil, err
	}
	fields, err := requiredObject(data, "event", "schema_version", "event_id", "run_id", "timestamp", "session_id", "observation_stage", "operation_result", "process")
	if err != nil {
		return nil, err
	}
	var kind EventKind
	if err := json.Unmarshal(fields["event"], &kind); err != nil {
		return nil, fmt.Errorf("event: %w", err)
	}
	var event Event
	var metadata *Metadata
	switch kind {
	case ProcessExec:
		e := &ProcessExecEvent{}
		event, metadata = e, &e.Metadata
	case ProcessFork:
		e := &ProcessForkEvent{}
		event, metadata = e, &e.Metadata
	case ProcessExit:
		e := &ProcessExitEvent{}
		event, metadata = e, &e.Metadata
	case FileOpen:
		e := &FileOpenEvent{}
		event, metadata = e, &e.Metadata
	case FileRead:
		e := &FileReadEvent{}
		event, metadata = e, &e.Metadata
	case FileWrite:
		e := &FileWriteEvent{}
		event, metadata = e, &e.Metadata
	case NetworkConnect:
		e := &NetworkConnectEvent{}
		event, metadata = e, &e.Metadata
	default:
		return nil, fmt.Errorf("unknown event kind %q", kind)
	}
	delete(fields, "event")
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	// https://pkg.go.dev/encoding/json#Decoder.DisallowUnknownFields
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(event); err != nil {
		return nil, err
	}
	if metadata.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported schema_version %d", metadata.SchemaVersion)
	}
	if metadata.EventID == "" || metadata.RunID == "" || metadata.Timestamp.IsZero() || metadata.SessionID == 0 {
		return nil, errors.New("event_id, run_id, timestamp and session_id must be nonzero")
	}
	switch metadata.ObservationStage {
	case StageUnknown, StageAttempt, StagePreOperation, StageCompleted:
	default:
		return nil, fmt.Errorf("unknown observation_stage %q", metadata.ObservationStage)
	}
	switch metadata.OperationResult {
	case ResultUnknown, ResultSuccess, ResultFailure:
	default:
		return nil, fmt.Errorf("unknown operation_result %q", metadata.OperationResult)
	}
	if metadata.ObservationStage != StageCompleted && metadata.OperationResult != ResultUnknown {
		return nil, errors.New("non-completed observation cannot claim success or failure")
	}
	if err := validateProcess(fields["process"], metadata.Process); err != nil {
		return nil, fmt.Errorf("process: %w", err)
	}
	switch e := event.(type) {
	case *ProcessExecEvent:
		err = validateFile(fields["executable"], e.Executable)
	case *ProcessForkEvent:
		err = validateProcess(fields["child"], e.Child)
		if err == nil && e.Process.ScopeID == e.Child.ScopeID && e.Process.PID == e.Child.PID {
			err = errors.New("fork child must have a distinct PID within the same scope")
		}
	case *ProcessExitEvent:
		if raw, present := fields["exit_code"]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			err = errors.New("omit unavailable exit_code instead of null")
		}
		if e.ExitCode != nil && (*e.ExitCode < 0 || e.ObservationStage != StageCompleted) {
			err = errors.New("exit_code requires a completed observation and a nonnegative status")
		}
	case *FileOpenEvent:
		err = validateFile(fields["resource"], e.Resource)
		if err == nil {
			switch e.Access {
			case AccessUnknown, AccessRead, AccessWrite, AccessReadWrite:
			default:
				err = fmt.Errorf("missing or unknown access %q", e.Access)
			}
		}
	case *FileReadEvent:
		err = validateFile(fields["resource"], e.Resource)
	case *FileWriteEvent:
		err = validateFile(fields["resource"], e.Resource)
	case *NetworkConnectEvent:
		_, err = requiredObject(fields["destination"], "address", "port")
		if err == nil && !e.Destination.Address.Is4() {
			err = errors.New("destination address must be IPv4, not IPv6 or mapped IPv6")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	return event, nil
}

func validateProcess(raw []byte, process ProcessContext) error {
	if _, err := requiredObject(raw, "pid", "birth_id", "scope_id", "ppid", "pgid", "comm"); err != nil {
		return err
	}
	if process.PID == 0 || process.BirthID == "" || process.ScopeID == "" || process.Comm == "" {
		return errors.New("pid, birth_id, scope_id and comm must be nonzero")
	}
	return nil
}

func validateFile(raw []byte, file FileResource) error {
	if _, err := requiredObject(raw, "path"); err != nil {
		return err
	}
	if file.Path == "" {
		return errors.New("path must be nonempty")
	}
	return nil
}

func requiredObject(data []byte, names ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("expected a JSON object")
	}
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("missing or null field %q", name)
		}
	}
	return fields, nil
}

// checkJSON enforces unambiguous object keys and exactly one JSON value before
// decoding into structs; encoding/json otherwise accepts duplicate keys.
func checkJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("event JSON must be valid UTF-8")
	}
	if err := checkUnicodeEscapes(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > maxJSONDepth {
			return fmt.Errorf("JSON nesting exceeds %d levels", maxJSONDepth)
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("object key must be a string")
				}
				// encoding/json uses Unicode case folding for struct fields.
				// Restrict schema keys to ASCII so aliases such as long s
				// cannot override an already-present canonical scope_id.
				for _, char := range name {
					if char != '_' && (char < 'a' || char > 'z') {
						return fmt.Errorf("JSON field must use lowercase ASCII snake_case: %q", name)
					}
				}
				if seen[name] {
					return fmt.Errorf("duplicate object key %q", name)
				}
				seen[name] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("trailing JSON value")
	}
	return nil
}

// encoding/json also replaces unpaired UTF-16 surrogate escapes with U+FFFD.
// Refuse those before decoding so distinct opaque identities cannot collapse.
func checkUnicodeEscapes(data []byte) error {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' || i+4 >= len(data) {
			continue
		}
		unit, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			continue
		} // The JSON parser reports invalid escape syntax.
		i += 4
		if unit < 0xd800 || unit > 0xdfff {
			continue
		}
		if unit > 0xdbff || i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return errors.New("unpaired Unicode surrogate escape")
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return errors.New("unpaired Unicode surrogate escape")
		}
		i += 6
	}
	return nil
}
