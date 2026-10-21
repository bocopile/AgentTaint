package runner

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/policy"
)

const sourcePath = "/synthetic/secret.txt"

func newProcessor(t *testing.T, action string) *policyAuditProcessor {
	t.Helper()
	policyText := "sources:\n  - path: " + sourcePath + "\n    label: SECRET\nrules:\n  - name: sensitive-egress\n    when: {process_has: SECRET}\n    sink: {network: {external: [203.0.113.0/24]}}\n    action: " + action + "\n"
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(policyText), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := newPolicyAuditProcessor(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func pipelineMetadata(pid core.ProcessID, birth, comm string, stage core.ObservationStage, result core.OperationResult) core.Metadata {
	return core.Metadata{
		SchemaVersion: core.SchemaVersion, EventID: "synthetic-event", RunID: "synthetic-run",
		Timestamp: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), SessionID: 41,
		ObservationStage: stage, OperationResult: result,
		Process: core.ProcessContext{ProcessKey: core.ProcessKey{PID: pid, BirthID: birth, ScopeID: "scope"}, Comm: comm},
	}
}

func fileOpen(path string, process core.ProcessContext, result core.OperationResult) core.FileOpenEvent {
	metadata := pipelineMetadata(process.PID, process.BirthID, process.Comm, core.StageCompleted, result)
	metadata.Process = process
	return core.FileOpenEvent{Metadata: metadata, Resource: core.FileResource{Path: path}, Access: core.AccessRead}
}

func connectEvent(process core.ProcessContext, result core.OperationResult) core.NetworkConnectEvent {
	metadata := pipelineMetadata(process.PID, process.BirthID, process.Comm, core.StageCompleted, result)
	metadata.Process = process
	return core.NetworkConnectEvent{Metadata: metadata, Destination: core.NetworkDestination{Address: netip.MustParseAddr("203.0.113.9"), Port: 443}}
}

func processEvent(t *testing.T, processor *policyAuditProcessor, event core.Event) []core.Event {
	t.Helper()
	got, err := processor.Process(event)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func violation(t *testing.T, events []core.Event) *core.PolicyViolationEvent {
	t.Helper()
	for _, event := range events {
		if record, ok := event.(core.PolicyViolationEvent); ok {
			return &record
		}
		if record, ok := event.(*core.PolicyViolationEvent); ok {
			return record
		}
	}
	return nil
}

func TestPolicyAuditPipelineFourScenarios(t *testing.T) {
	parent := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 101, BirthID: "parent", ScopeID: "scope"}, Comm: "agent"}
	child := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 102, BirthID: "child", ScopeID: "scope"}, PPID: 101, Comm: "worker"}

	t.Run("normal flow", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		if got := processEvent(t, processor, connectEvent(parent, core.ResultSuccess)); len(got) != 1 {
			t.Fatalf("clean connection produced derived record: %#v", got)
		}
	})

	t.Run("normal source remains clean through child", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		processEvent(t, processor, fileOpen("/synthetic/normal.txt", parent, core.ResultSuccess))
		forkMeta := pipelineMetadata(parent.PID, parent.BirthID, parent.Comm, core.StageCompleted, core.ResultSuccess)
		forkMeta.Process = parent
		processEvent(t, processor, core.ProcessForkEvent{Metadata: forkMeta, Child: child})
		if got := processEvent(t, processor, connectEvent(child, core.ResultSuccess)); len(got) != 1 {
			t.Fatalf("non-source file tainted child: %#v", got)
		}
	})

	t.Run("direct source to sink is audit only", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		processEvent(t, processor, fileOpen(sourcePath, parent, core.ResultSuccess))
		got := processEvent(t, processor, connectEvent(parent, core.ResultSuccess))
		record := violation(t, got)
		if record == nil || record.Decision != "deny" || record.OperationResult != core.ResultSuccess || record.Source.Path != sourcePath || record.Sink.Address.String() != "203.0.113.9" {
			t.Fatalf("deny verdict lost actual successful operation or provenance: %#v", got)
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := core.DecodeEvent(encoded); err != nil {
			t.Fatalf("derived record is not a valid audit event: %s: %v", encoded, err)
		}
	})

	t.Run("child inherits source snapshot", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		processEvent(t, processor, fileOpen(sourcePath, parent, core.ResultSuccess))
		forkMeta := pipelineMetadata(parent.PID, parent.BirthID, parent.Comm, core.StageCompleted, core.ResultSuccess)
		forkMeta.Process = parent
		processEvent(t, processor, core.ProcessForkEvent{Metadata: forkMeta, Child: child})
		if record := violation(t, processEvent(t, processor, connectEvent(child, core.ResultFailure))); record == nil || record.Process.PID != child.PID || record.OperationResult != core.ResultFailure {
			t.Fatalf("child sink or actual failed result was lost: %#v", record)
		}
	})

	t.Run("audit action records without enforcement", func(t *testing.T) {
		processor := newProcessor(t, "audit")
		processEvent(t, processor, fileOpen(sourcePath, parent, core.ResultSuccess))
		if record := violation(t, processEvent(t, processor, connectEvent(parent, core.ResultSuccess))); record == nil || record.Decision != "audit" {
			t.Fatalf("audit action missing: %#v", record)
		}
	})
}

func TestPolicyAuditPipelineProcessesRepeatedConnectRecordsIndependently(t *testing.T) {
	processor := newProcessor(t, "deny")
	process := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 201, BirthID: "process-birth", ScopeID: "scope"}, Comm: "fixture"}
	processEvent(t, processor, fileOpen(sourcePath, process, core.ResultSuccess))
	connect := connectEvent(process, core.ResultSuccess)
	connect.EventID = "replayed-connect"

	for i := 0; i < 2; i++ {
		got := processEvent(t, processor, connect)
		record := violation(t, got)
		if record == nil || record.Decision != "deny" || record.Process.ProcessKey != process.ProcessKey {
			t.Fatalf("repeated input record %d did not independently produce the same policy decision: %#v", i, got)
		}
	}
}

func TestPolicyAuditPipelineIncompleteAndUnsupportedFlows(t *testing.T) {
	parent := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 201, BirthID: "parent", ScopeID: "scope"}, Comm: "agent"}
	child := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 202, BirthID: "child", ScopeID: "scope"}, PPID: 201, Comm: "worker"}
	t.Run("failed source open does not label", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		processEvent(t, processor, fileOpen(sourcePath, parent, core.ResultFailure))
		if got := processEvent(t, processor, connectEvent(parent, core.ResultSuccess)); len(got) != 1 {
			t.Fatalf("failed source open created a violation: %#v", got)
		}
	})
	t.Run("child to parent flow is unsupported", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		forkMeta := pipelineMetadata(parent.PID, parent.BirthID, parent.Comm, core.StageCompleted, core.ResultSuccess)
		forkMeta.Process = parent
		processEvent(t, processor, core.ProcessForkEvent{Metadata: forkMeta, Child: child})
		processEvent(t, processor, fileOpen(sourcePath, child, core.ResultSuccess))
		if got := processEvent(t, processor, connectEvent(parent, core.ResultSuccess)); len(got) != 1 {
			t.Fatalf("child label flowed back to parent: %#v", got)
		}
	})
	t.Run("incomplete history yields no policy verdict", func(t *testing.T) {
		processor := newProcessor(t, "deny")
		processEvent(t, processor, fileOpen(sourcePath, parent, core.ResultSuccess))
		processor.MarkIncomplete("synthetic observer loss")
		if got := processEvent(t, processor, connectEvent(parent, core.ResultSuccess)); len(got) != 1 {
			t.Fatalf("incomplete history produced a policy verdict: %#v", got)
		}
	})
}
