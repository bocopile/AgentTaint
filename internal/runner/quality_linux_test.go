//go:build linux

package runner

import (
	"testing"

	"github.com/bocopile/AgentTaint/internal/core"
)

func TestAuditCoverageLossRemainsStickyAfterQueueRecovers(t *testing.T) {
	quality := &coverageQuality{}
	queue := make(chan core.Event, 1)
	queued := core.FileOpenEvent{Metadata: core.Metadata{EventID: "queued-before-loss"}}
	if err := enqueueAudit(queue, queued, quality); err != nil {
		t.Fatalf("enqueue initial audit event: %v", err)
	}
	if err := enqueueAudit(queue, core.FileOpenEvent{Metadata: core.Metadata{EventID: "dropped"}}, quality); err == nil {
		t.Fatal("full audit queue did not report event loss")
	}

	<-queue
	continued := core.FileOpenEvent{Metadata: core.Metadata{EventID: "continued-after-loss"}}
	if err := enqueueAudit(queue, continued, quality); err != nil {
		t.Fatalf("audit queue did not accept an event after capacity returned: %v", err)
	}
	got, ok := (<-queue).(core.FileOpenEvent)
	if !ok || got.Metadata.EventID != continued.Metadata.EventID {
		t.Fatalf("continued event was not preserved after recovery: %#v", got)
	}

	incomplete, detail := quality.report()
	if !incomplete || detail != "writer-queue-full=1" {
		t.Fatalf("audit loss was not retained after queue recovery: incomplete=%t detail=%q", incomplete, detail)
	}

	process := core.ProcessContext{ProcessKey: core.ProcessKey{PID: 301, BirthID: "loss-recovery", ScopeID: "scope"}, Comm: "agent"}
	processor := newProcessor(t, "deny")
	processEvent(t, processor, fileOpen(sourcePath, process, core.ResultSuccess))
	processor.MarkIncomplete(detail)
	for i := range 2 {
		event := connectEvent(process, core.ResultSuccess)
		event.EventID = "after-loss-" + string(rune('0'+i))
		got := processEvent(t, processor, event)
		if len(got) != 1 || violation(t, got) != nil {
			t.Fatalf("incomplete history produced a policy verdict on continuation %d: %#v", i, got)
		}
	}
	if incomplete, detail = quality.report(); !incomplete || detail != "writer-queue-full=1" {
		t.Fatalf("later processing cleared or changed the original loss: incomplete=%t detail=%q", incomplete, detail)
	}
}
