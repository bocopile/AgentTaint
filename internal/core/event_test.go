package core

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixtures() []Event {
	metadata := Metadata{
		SchemaVersion: SchemaVersion, EventID: "event-1", RunID: RunID("run-a"),
		Timestamp: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC), SessionID: SessionID(4021),
		ObservationStage: StageUnknown, OperationResult: ResultUnknown,
		Process: ProcessContext{ProcessKey: ProcessKey{PID: ProcessID(19342), BirthID: "birth-a", ScopeID: "scope-a"}, PPID: ProcessID(19300), PGID: ProcessGroupID(19300), Comm: "python"},
	}
	completed := metadata
	completed.ObservationStage, completed.OperationResult = StageCompleted, ResultSuccess
	exitCode := int32(23)
	return []Event{
		&ProcessExecEvent{Metadata: metadata, Executable: FileResource{Path: "/usr/bin/python"}},
		&ProcessForkEvent{Metadata: completed, Child: ProcessContext{ProcessKey: ProcessKey{PID: ProcessID(19343), BirthID: "birth-b", ScopeID: "scope-a"}, PPID: ProcessID(19342), PGID: ProcessGroupID(19300), Comm: "python"}},
		&ProcessExitEvent{Metadata: completed, ExitCode: &exitCode},
		&FileOpenEvent{Metadata: metadata, Resource: FileResource{Path: "/workspace/.env"}, Access: AccessRead},
		&FileReadEvent{Metadata: metadata, Resource: FileResource{Path: "/workspace/.env"}},
		&FileWriteEvent{Metadata: metadata, Resource: FileResource{Path: "./link/../result $HOME *"}},
		&NetworkConnectEvent{Metadata: metadata, Destination: NetworkDestination{Address: netip.MustParseAddr("1.2.3.4"), Port: 443}},
	}
}

func encodeFixture(t *testing.T, event Event) []byte {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAllEventsJSONRoundTrip(t *testing.T) {
	for _, original := range fixtures() {
		t.Run(string(original.Kind()), func(t *testing.T) {
			data := encodeFixture(t, original)
			t.Log(string(data))
			decoded, err := DecodeEvent(data)
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(decoded) != reflect.TypeOf(original) || !reflect.DeepEqual(decoded, original) {
				t.Fatalf("round trip differs: %#v != %#v", decoded, original)
			}
			if decoded.Kind() != original.Kind() {
				t.Fatal("kind changed")
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatal(err)
			}
			if string(object["event"]) != `"`+string(original.Kind())+`"` || string(object["session_id"]) != `"4021"` || string(object["run_id"]) != `"run-a"` {
				t.Fatalf("wire identifiers: %s", data)
			}
			// Value and pointer forms must both invoke the discriminator codec.
			value := reflect.ValueOf(original).Elem().Interface()
			valueJSON, err := json.Marshal(value)
			if err != nil || !bytes.Equal(data, valueJSON) {
				t.Fatalf("value encoding differs: %s %v", valueJSON, err)
			}
		})
	}
}

func changeField(t *testing.T, data []byte, path string, value any, remove bool) []byte {
	t.Helper()
	parts := strings.Split(path, ".")
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	target := root
	for _, part := range parts[:len(parts)-1] {
		target = target[part].(map[string]any)
	}
	if remove {
		delete(target, parts[len(parts)-1])
	} else {
		target[parts[len(parts)-1]] = value
	}
	result, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDecodeRejectsMissingAndNullFields(t *testing.T) {
	common := []string{"event", "schema_version", "event_id", "run_id", "timestamp", "session_id", "observation_stage", "operation_result", "process", "process.pid", "process.birth_id", "process.scope_id", "process.ppid", "process.pgid", "process.comm"}
	variantFields := map[EventKind][]string{
		ProcessExec:    {"executable", "executable.path"},
		ProcessFork:    {"child", "child.pid", "child.birth_id", "child.scope_id", "child.ppid", "child.pgid", "child.comm"},
		ProcessExit:    {},
		FileOpen:       {"resource", "resource.path", "access"},
		FileRead:       {"resource", "resource.path"},
		FileWrite:      {"resource", "resource.path"},
		NetworkConnect: {"destination", "destination.address", "destination.port"},
	}
	for _, event := range fixtures() {
		data := encodeFixture(t, event)
		fields := append(append([]string{}, common...), variantFields[event.Kind()]...)
		for _, field := range fields {
			for _, remove := range []bool{true, false} {
				if result, err := DecodeEvent(changeField(t, data, field, nil, remove)); err == nil || result != nil {
					t.Fatalf("%s %s remove=%t accepted: %v", event.Kind(), field, remove, result)
				}
			}
		}
	}
}

func TestDecodeRejectsWrongTypesAndValues(t *testing.T) {
	file := encodeFixture(t, fixtures()[3])
	for _, test := range []struct {
		field string
		value any
	}{
		{"event", "new_event"}, {"event", "policy_violation"}, {"event", 1},
		{"schema_version", 0}, {"schema_version", 2}, {"schema_version", "1"}, {"schema_version", 1.5},
		{"event_id", ""}, {"event_id", 1}, {"run_id", ""}, {"run_id", 4021},
		{"timestamp", "bad-time"}, {"timestamp", "0001-01-01T00:00:00Z"}, {"timestamp", 123},
		{"session_id", 4021}, {"session_id", "0"}, {"session_id", "-1"}, {"session_id", "4294967296"},
		{"process", "not-an-object"}, {"process.pid", 0}, {"process.pid", -1}, {"process.pid", json.Number("4294967296")}, {"process.pid", "19342"},
		{"process.ppid", -1}, {"process.pgid", json.Number("4294967296")},
		{"process.birth_id", ""}, {"process.scope_id", ""}, {"process.comm", ""},
		{"observation_stage", "observed"}, {"operation_result", "allowed"},
		{"access", "execute"}, {"access", 1}, {"resource.path", ""}, {"resource.path", []string{"path"}},
		{"unexpected", true}, {"process.labels", []string{"SECRET"}}, {"resource.contents", "secret"},
	} {
		if event, err := DecodeEvent(changeField(t, file, test.field, test.value, false)); err == nil || event != nil {
			t.Fatalf("%s=%v accepted", test.field, test.value)
		}
	}
	network := encodeFixture(t, fixtures()[6])
	for _, address := range []any{"::1", "::ffff:1.2.3.4", "not-an-ip", "", []int{1, 2, 3, 4}} {
		if _, err := DecodeEvent(changeField(t, network, "destination.address", address, false)); err == nil {
			t.Fatalf("address %v accepted", address)
		}
	}
	for _, port := range []any{-1, 65536, "443", 1.5} {
		if _, err := DecodeEvent(changeField(t, network, "destination.port", port, false)); err == nil {
			t.Fatalf("port %v accepted", port)
		}
	}
	if _, err := DecodeEvent(changeField(t, network, "destination.port", 0, false)); err != nil {
		t.Fatalf("port zero attempt: %v", err)
	}
}

func TestDecodeRejectsAmbiguousJSON(t *testing.T) {
	file := encodeFixture(t, fixtures()[3])
	for _, data := range [][]byte{
		nil, []byte("null"), []byte("[]"), []byte("{}"), []byte("true"),
		append(append([]byte{}, file...), []byte(" {}")...),
		append(append([]byte{}, file...), []byte(" trailing")...),
		bytes.Replace(file, []byte(`"event":"file_open"`), []byte(`"event":"file_open","event":"file_read"`), 1),
		bytes.Replace(file, []byte(`"pid":19342`), []byte(`"pid":19342,"pid":19343`), 1),
		bytes.Replace(file, []byte(`"pid":19342`), []byte(`"pid":19342,"PID":19343`), 1),
		bytes.Replace(file, []byte(`"schema_version":1`), []byte(`"schema_version":1,"SCHEMA_VERSION":2`), 1),
		bytes.Replace(file, []byte(`"event":"file_open"`), []byte(`"Event":"file_open"`), 1),
	} {
		if event, err := DecodeEvent(data); err == nil || event != nil {
			t.Fatalf("ambiguous/invalid JSON accepted: %s", data)
		}
	}
	var nilEvent *FileOpenEvent
	data, err := json.Marshal(nilEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeEvent(data); err == nil {
		t.Fatal("typed nil event accepted")
	}
}

func TestCodecRejectsInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff})
	for _, change := range []func(*FileOpenEvent){
		func(e *FileOpenEvent) { e.EventID = invalid },
		func(e *FileOpenEvent) { e.RunID = RunID(invalid) },
		func(e *FileOpenEvent) { e.Process.BirthID = invalid },
		func(e *FileOpenEvent) { e.Process.ScopeID = invalid },
		func(e *FileOpenEvent) { e.Process.Comm = invalid },
		func(e *FileOpenEvent) { e.Resource.Path = invalid },
	} {
		event := fixtures()[3].(*FileOpenEvent)
		change(event)
		if _, err := json.Marshal(event); err == nil {
			t.Fatal("invalid UTF-8 was silently replaced")
		}
	}
	fork := fixtures()[1].(*ProcessForkEvent)
	fork.Child.BirthID = invalid
	if _, err := json.Marshal(fork); err == nil {
		t.Fatal("invalid child birth identity accepted")
	}
	data := encodeFixture(t, fixtures()[3])
	data = bytes.Replace(data, []byte("birth-a"), []byte{0xff}, 1)
	if _, err := DecodeEvent(data); err == nil {
		t.Fatal("invalid incoming UTF-8 accepted")
	}
	valid := encodeFixture(t, fixtures()[3])
	for _, escaped := range []string{`\ud800`, `\udfff`, `\ud800\u0041`, `\ud800\ud800`} {
		data := bytes.Replace(valid, []byte("birth-a"), []byte(escaped), 1)
		if _, err := DecodeEvent(data); err == nil {
			t.Fatalf("unpaired surrogate accepted: %s", escaped)
		}
	}
	for _, escaped := range []string{`\ud83d\ude00`, `\\ud800`} {
		data := bytes.Replace(valid, []byte("birth-a"), []byte(escaped), 1)
		if _, err := DecodeEvent(data); err != nil {
			t.Fatalf("valid escape rejected: %s: %v", escaped, err)
		}
	}
}

func TestDecodeRejectsUnicodeFieldAliases(t *testing.T) {
	valid := encodeFixture(t, fixtures()[3])
	canonical := `"scope_id":"scope-a"`
	for _, alias := range []string{`"ſcope_id":"scope-b"`, `"\u017fcope_id":"scope-b"`} {
		for _, replacement := range []string{alias, canonical + "," + alias, alias + "," + canonical} {
			data := bytes.Replace(valid, []byte(canonical), []byte(replacement), 1)
			if event, err := DecodeEvent(data); err == nil || event != nil {
				t.Errorf("Unicode alias accepted: %s; decoded=%+v", replacement, event)
			}
		}
	}
}

func TestDecodeBoundsJSONNesting(t *testing.T) {
	depth := maxJSONDepth + 2
	for _, data := range []string{
		strings.Repeat("[", depth) + "null" + strings.Repeat("]", depth),
		strings.Repeat(`{"nested":`, depth) + "null" + strings.Repeat("}", depth),
	} {
		if event, err := DecodeEvent([]byte(data)); err == nil || event != nil || !strings.Contains(err.Error(), "nesting exceeds") {
			t.Fatalf("expected bounded nesting error, got %v / %v", event, err)
		}
	}
}

func TestObservationStageAndResult(t *testing.T) {
	base := fixtures()[3].(*FileOpenEvent)
	for _, stage := range []ObservationStage{StageUnknown, StageAttempt, StagePreOperation, StageCompleted} {
		for _, result := range []OperationResult{ResultUnknown, ResultSuccess, ResultFailure} {
			event := *base
			event.ObservationStage, event.OperationResult = stage, result
			_, err := json.Marshal(event)
			valid := stage == StageCompleted || result == ResultUnknown
			if (err == nil) != valid {
				t.Fatalf("stage=%s result=%s error=%v", stage, result, err)
			}
		}
	}
}

func TestIdentityAndExitSemantics(t *testing.T) {
	key := fixtures()[0].(*ProcessExecEvent).Process.ProcessKey
	nextBirth, nextScope := key, key
	nextBirth.BirthID, nextScope.ScopeID = "birth-next", "scope-next"
	identities := map[ProcessKey]bool{key: true, nextBirth: true, nextScope: true}
	if len(identities) != 3 {
		t.Fatal("PID reuse or distinct scope conflated")
	}
	fork := fixtures()[1].(*ProcessForkEvent)
	fork.Child.ProcessKey = nextBirth
	if _, err := json.Marshal(fork); err == nil {
		t.Fatal("fork parent and child share live PID in same scope")
	}
	exit := fixtures()[2].(*ProcessExitEvent)
	// Nonzero process exit status does not change successful observation result.
	if _, err := json.Marshal(exit); err != nil {
		t.Fatal(err)
	}
	exit.ExitCode = nil
	data := encodeFixture(t, exit)
	if bytes.Contains(data, []byte("exit_code")) {
		t.Fatal("unknown exit status fabricated")
	}
	if _, err := DecodeEvent(changeField(t, data, "exit_code", nil, false)); err == nil {
		t.Fatal("null exit status accepted")
	}
	negative := int32(-1)
	exit.ExitCode = &negative
	if _, err := json.Marshal(exit); err == nil {
		t.Fatal("raw negative wait status accepted")
	}
	zero := int32(0)
	exit.ExitCode = &zero
	exit.ObservationStage, exit.OperationResult = StageUnknown, ResultUnknown
	if _, err := json.Marshal(exit); err == nil {
		t.Fatal("exit code accepted before completion")
	}
}

func TestMarshalValidatesResourcesAndPreservesPaths(t *testing.T) {
	file := fixtures()[5].(*FileWriteEvent)
	data := encodeFixture(t, file)
	decoded, err := DecodeEvent(data)
	if err != nil || decoded.(*FileWriteEvent).Resource.Path != "./link/../result $HOME *" {
		t.Fatalf("path changed: %s %v", data, err)
	}
	file.Resource.Path = ""
	if _, err := json.Marshal(file); err == nil {
		t.Fatal("marshal accepted empty path")
	}
	network := fixtures()[6].(*NetworkConnectEvent)
	network.Destination.Address = netip.MustParseAddr("::ffff:1.2.3.4")
	if _, err := json.Marshal(network); err == nil {
		t.Fatal("marshal accepted mapped IPv6")
	}
	file.Resource.Path = "file"
	file.Process.BirthID = ""
	if _, err := json.Marshal(file); err == nil {
		t.Fatal("marshal accepted missing lifetime")
	}
}

func FuzzDecodeEvent(f *testing.F) {
	for _, event := range fixtures() {
		data, err := json.Marshal(event)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{"event":"unknown"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		event, err := DecodeEvent(data)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("decoded event cannot encode: %v", err)
		}
		again, err := DecodeEvent(encoded)
		if err != nil || !reflect.DeepEqual(event, again) {
			t.Fatalf("decoded event cannot roundtrip: %v", err)
		}
	})
}
