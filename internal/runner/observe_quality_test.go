//go:build linux

package runner

import (
	"strings"
	"testing"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/sensor/linux"
)

func TestCoverageQualityDoesNotDoubleCountPathCaptureDiagnostics(t *testing.T) {
	quality := &coverageQuality{}
	quality.losePathCapture(
		"file-openat-path-read-helper-errno-14-syscall-success",
		"file-openat-path-unreadable-success",
	)
	quality.loseN("file-openat-path-unreadable-success", 1)

	incomplete, detail := quality.report()
	if !incomplete {
		t.Fatal("report marked path capture loss complete")
	}
	if !strings.Contains(detail, "file-openat-path-read-helper-errno-14-syscall-success=1") {
		t.Fatalf("report omitted path capture diagnostic: %s", detail)
	}
	if strings.Contains(detail, "file-openat-path-unreadable-success=") {
		t.Fatalf("report counted diagnostic and aggregate counter twice: %s", detail)
	}

	quality.loseN("file-openat-path-unreadable-success", 1)
	_, detail = quality.report()
	if !strings.Contains(detail, "file-openat-path-unreadable-success=1") {
		t.Fatalf("report did not retain aggregate losses without matching diagnostics: %s", detail)
	}
}

func TestCoverageQualityDropPollingUsesOnlyNewDeltas(t *testing.T) {
	quality := &coverageQuality{}
	lastSeen := linux.DropCounts{0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	current := lastSeen
	current[9]++
	recordBPFDropDeltas(&lastSeen, current, quality)

	quality.losePathCapture(
		"file-openat-path-read-helper-errno-14-syscall-success",
		"file-openat-path-unreadable-success",
	)
	recordBPFDropDeltas(&lastSeen, current, quality)

	incomplete, detail := quality.report()
	if !incomplete {
		t.Fatal("report marked repeated drop polling complete")
	}
	if !strings.Contains(detail, "file-openat-path-read-helper-errno-14-syscall-success=1") {
		t.Fatalf("report omitted path capture diagnostic: %s", detail)
	}
	if strings.Contains(detail, "file-openat-path-unreadable-success=") {
		t.Fatalf("repeated polling double-counted diagnostic overlap: %s", detail)
	}

	current[0] = 3
	recordBPFDropDeltas(&lastSeen, current, quality)
	current[0] = 5
	recordBPFDropDeltas(&lastSeen, current, quality)
	_, detail = quality.report()
	if !strings.Contains(detail, "process-ring-reserve=5") {
		t.Fatalf("report did not accumulate only new process drops: %s", detail)
	}
	recordBPFDropDeltas(&lastSeen, current, quality)
	_, detail = quality.report()
	if strings.Contains(detail, "process-ring-reserve=10") || !strings.Contains(detail, "process-ring-reserve=5") {
		t.Fatalf("repeated polling changed the accumulated process drops: %s", detail)
	}

	current[0] = 4
	recordBPFDropDeltas(&lastSeen, current, quality)
	_, detail = quality.report()
	if !strings.Contains(detail, "drop-counter=unknown") {
		t.Fatalf("report did not flag a decreasing cumulative counter: %s", detail)
	}
}

func TestPollDropsBeforeCompletedSinkSkipsOtherEvents(t *testing.T) {
	events := []core.Event{
		core.FileOpenEvent{},
		core.NetworkConnectEvent{Metadata: core.Metadata{ObservationStage: core.StageAttempt}},
	}
	for _, event := range events {
		quality := &coverageQuality{}
		pollDropsBeforeCompletedSink(nil, nil, quality, event)
		if incomplete, detail := quality.report(); incomplete || detail != "" {
			t.Fatalf("non-completed-sink event changed coverage quality: event=%T incomplete=%t detail=%q", event, incomplete, detail)
		}
	}
}

func TestDropCounterReadFailureSuppressesSubsequentPolicyVerdict(t *testing.T) {
	process := core.ProcessContext{
		ProcessKey: core.ProcessKey{PID: 380, BirthID: "drop-counter-read-failure", ScopeID: "scope"},
		Comm:       "agent",
	}
	processor := newProcessor(t, "deny")
	processEvent(t, processor, fileOpen(sourcePath, process, core.ResultSuccess))
	if !processor.engine.Complete() {
		t.Fatal("processor was incomplete before the drop counter read")
	}
	labels := processor.engine.Labels(process.ProcessKey)
	if len(labels) != 1 || labels[0] != "SECRET" {
		t.Fatalf("source event did not label the process before the drop counter read: %v", labels)
	}

	control := newProcessor(t, "deny")
	processEvent(t, control, fileOpen(sourcePath, process, core.ResultSuccess))
	if got := processEvent(t, control, connectEvent(process, core.ResultSuccess)); violation(t, got) == nil {
		t.Fatal("complete-history control did not produce the expected deny verdict")
	}

	quality := &coverageQuality{}
	lastSeen := linux.DropCounts{}
	events := processPolicyAuditEvent(nil, &lastSeen, quality, processor, connectEvent(process, core.ResultSuccess))

	incomplete, detail := quality.report()
	if !incomplete || detail != "drop-counter=unknown" {
		t.Fatalf("failed drop counter read was not recorded as unknown coverage: incomplete=%t detail=%q", incomplete, detail)
	}
	if processor.engine.Complete() {
		t.Fatal("processor remained complete after the drop counter read failed")
	}
	if violation(t, events) != nil {
		t.Fatalf("unknown observer history produced a policy verdict: %#v", events)
	}
}
