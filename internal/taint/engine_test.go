package taint

import (
	"testing"
	"time"

	"github.com/bocopile/AgentTaint/internal/core"
)

func testProcess(pid core.ProcessID, birth string) core.ProcessContext {
	return core.ProcessContext{ProcessKey: core.ProcessKey{PID: pid, BirthID: birth, ScopeID: "scope-a"}, Comm: "fixture"}
}

func testMetadata(process core.ProcessContext, stage core.ObservationStage, result core.OperationResult) core.Metadata {
	return core.Metadata{SchemaVersion: core.SchemaVersion, EventID: "fixture", Timestamp: time.Unix(1, 0), SessionID: 1, ObservationStage: stage, OperationResult: result, Process: process}
}

func TestEngineLabelsOnlySuccessfulCompletedSourceOpen(t *testing.T) {
	engine, err := NewEngine([]string{"/workspace/secret.txt"})
	if err != nil {
		t.Fatal(err)
	}
	process := testProcess(10, "birth-1")
	for _, tc := range []struct {
		stage  core.ObservationStage
		result core.OperationResult
	}{
		{core.StageAttempt, core.ResultUnknown}, {core.StageCompleted, core.ResultFailure},
	} {
		event := core.FileOpenEvent{Metadata: testMetadata(process, tc.stage, tc.result), Resource: core.FileResource{Path: "/workspace/secret.txt"}, Access: core.AccessRead}
		if err := engine.Apply(event); err != nil {
			t.Fatal(err)
		}
		if engine.HasLabel(process.ProcessKey, core.LabelSecret) {
			t.Fatalf("labeled after %s/%s", tc.stage, tc.result)
		}
	}
	event := core.FileOpenEvent{Metadata: testMetadata(process, core.StageCompleted, core.ResultSuccess), Resource: core.FileResource{Path: "/workspace/secret.txt"}, Access: core.AccessRead}
	if err := engine.Apply(event); err != nil {
		t.Fatal(err)
	}
	if !engine.HasLabel(process.ProcessKey, core.LabelSecret) {
		t.Fatal("successful source open did not label process")
	}
}

func TestEngineForkSnapshotExecContinuityAndExitCleanup(t *testing.T) {
	engine, _ := NewEngine([]string{"/secret"})
	parent, child := testProcess(10, "parent-birth"), testProcess(11, "child-birth")
	open := core.FileOpenEvent{Metadata: testMetadata(parent, core.StageCompleted, core.ResultSuccess), Resource: core.FileResource{Path: "/secret"}}
	if err := engine.Apply(open); err != nil {
		t.Fatal(err)
	}
	fork := core.ProcessForkEvent{Metadata: testMetadata(parent, core.StageCompleted, core.ResultSuccess), Child: child}
	if err := engine.Apply(fork); err != nil {
		t.Fatal(err)
	}
	if !engine.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("child did not inherit fork-time label")
	}
	exec := core.ProcessExecEvent{Metadata: testMetadata(child, core.StageCompleted, core.ResultSuccess)}
	if err := engine.Apply(exec); err != nil {
		t.Fatal(err)
	}
	if !engine.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("exec lost process label")
	}
	exit := core.ProcessExitEvent{Metadata: testMetadata(child, core.StageCompleted, core.ResultSuccess)}
	if err := engine.Apply(exit); err != nil {
		t.Fatal(err)
	}
	if engine.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("exit did not clear exact process key")
	}
	if !engine.HasLabel(parent.ProcessKey, core.LabelSecret) {
		t.Fatal("child exit changed parent state")
	}
}

func TestEngineForkUsesCurrentStateAndDoesNotBackfillChildren(t *testing.T) {
	engine, _ := NewEngine([]string{"/secret"})
	parent, child := testProcess(10, "parent-birth"), testProcess(11, "child-birth")
	fork := core.ProcessForkEvent{Metadata: testMetadata(parent, core.StageCompleted, core.ResultSuccess), Child: child}
	if err := engine.Apply(fork); err != nil {
		t.Fatal(err)
	}
	open := core.FileOpenEvent{Metadata: testMetadata(parent, core.StageCompleted, core.ResultSuccess), Resource: core.FileResource{Path: "/secret"}}
	if err := engine.Apply(open); err != nil {
		t.Fatal(err)
	}
	if !engine.HasLabel(parent.ProcessKey, core.LabelSecret) {
		t.Fatal("parent did not receive source label")
	}
	if engine.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("later parent label was incorrectly backfilled into earlier child")
	}
}

func TestEngineReplayUsesInputOrderAndDuplicateStateEventsAreIdempotent(t *testing.T) {
	parent, child := testProcess(10, "parent-birth"), testProcess(11, "child-birth")
	open := core.FileOpenEvent{Metadata: testMetadata(parent, core.StageCompleted, core.ResultSuccess), Resource: core.FileResource{Path: "/secret"}}
	open.Timestamp = time.Unix(20, 0)
	fork := core.ProcessForkEvent{Metadata: testMetadata(parent, core.StageCompleted, core.ResultSuccess), Child: child}
	fork.Timestamp = time.Unix(10, 0)

	engine, err := NewEngine([]string{"/secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{open, open, fork, fork} {
		if err := engine.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	if !engine.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("input-ordered source then fork did not propagate state despite out-of-order timestamps")
	}
	if got := engine.SourcePaths(child.ProcessKey); len(got) != 1 || got[0] != "/secret" {
		t.Fatalf("duplicate source/fork records changed provenance cardinality: %v", got)
	}

	replayed, err := NewEngine([]string{"/secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{open, open, fork, fork} {
		if err := replayed.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	if got := replayed.SourcePaths(child.ProcessKey); len(got) != 1 || got[0] != "/secret" || !replayed.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatalf("replaying the same ordered input produced different state: labels=%v sources=%v", replayed.Labels(child.ProcessKey), got)
	}

	reordered, err := NewEngine([]string{"/secret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{fork, open} {
		if err := reordered.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	if reordered.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("timestamp sorting or backfill changed fork-before-source input semantics")
	}
}

func TestEngineProcessKeySeparatesPIDReuseAndScope(t *testing.T) {
	engine, _ := NewEngine([]string{"/secret"})
	old := testProcess(42, "birth-old")
	newBirth := testProcess(42, "birth-new")
	newScope := testProcess(42, "birth-old")
	newScope.ScopeID = "scope-b"
	if err := engine.Apply(core.FileOpenEvent{Metadata: testMetadata(old, core.StageCompleted, core.ResultSuccess), Resource: core.FileResource{Path: "/secret"}}); err != nil {
		t.Fatal(err)
	}
	if engine.HasLabel(newBirth.ProcessKey, core.LabelSecret) || engine.HasLabel(newScope.ProcessKey, core.LabelSecret) {
		t.Fatal("PID reuse or another scope inherited stale label")
	}
}

func TestEngineMissingForkIsNotInferredAndMarksExternalLoss(t *testing.T) {
	engine, _ := NewEngine([]string{"/secret"})
	child := testProcess(11, "child-birth")
	if err := engine.Apply(core.NetworkConnectEvent{Metadata: testMetadata(child, core.StageCompleted, core.ResultSuccess)}); err != nil {
		t.Fatal(err)
	}
	if engine.HasLabel(child.ProcessKey, core.LabelSecret) {
		t.Fatal("engine inferred a missing fork/source event")
	}
	engine.MarkIncomplete("observer dropped events")
	if engine.Complete() {
		t.Fatal("external event loss was reported as complete")
	}
	if got := engine.Problems(); len(got) != 1 || got[0] != "observer dropped events" {
		t.Fatalf("unexpected problems: %v", got)
	}
}

func TestEngineRejectsThreadForkAndInvalidKeys(t *testing.T) {
	engine, _ := NewEngine(nil)
	proc := testProcess(7, "same")
	if err := engine.Apply(core.ProcessForkEvent{Metadata: testMetadata(proc, core.StageCompleted, core.ResultSuccess), Child: proc}); err == nil {
		t.Fatal("expected same-PID thread fork rejection")
	}
	bad := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 8, ScopeID: "scope-a"}}
	if err := engine.Apply(core.ProcessExitEvent{Metadata: testMetadata(bad, core.StageCompleted, core.ResultSuccess)}); err == nil {
		t.Fatal("expected invalid ProcessKey rejection")
	}
	otherScope := testProcess(8, "child")
	otherScope.ScopeID = "scope-b"
	if err := engine.Apply(core.ProcessForkEvent{Metadata: testMetadata(proc, core.StageCompleted, core.ResultSuccess), Child: otherScope}); err == nil {
		t.Fatal("expected cross-scope fork rejection")
	}
}

func TestNewEngineRejectsEmptySource(t *testing.T) {
	if _, err := NewEngine([]string{""}); err == nil {
		t.Fatal("expected empty source path rejection")
	}
}
