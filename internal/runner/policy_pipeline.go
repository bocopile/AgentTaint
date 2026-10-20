package runner

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/bocopile/AgentTaint/internal/core"
	"github.com/bocopile/AgentTaint/internal/policy"
	"github.com/bocopile/AgentTaint/internal/taint"
)

type policyAuditProcessor struct {
	snapshot *policy.Snapshot
	engine   *taint.Engine
}

func newPolicyAuditProcessor(snapshot *policy.Snapshot) (*policyAuditProcessor, error) {
	if snapshot == nil {
		return nil, errors.New("nil policy snapshot")
	}
	engine, err := taint.NewEngine(snapshot.SourcePaths())
	if err != nil {
		return nil, err
	}
	return &policyAuditProcessor{snapshot: snapshot, engine: engine}, nil
}

func (p *policyAuditProcessor) MarkIncomplete(reason string) {
	if p != nil && p.engine != nil {
		p.engine.MarkIncomplete(reason)
	}
}

// Process preserves each raw event and appends a derived audit record when a
// complete history produces an audit or deny verdict. It never enforces it.
func (p *policyAuditProcessor) Process(event core.Event) ([]core.Event, error) {
	if p == nil || p.snapshot == nil || p.engine == nil {
		return nil, errors.New("nil policy audit processor")
	}
	if err := p.engine.Apply(event); err != nil {
		p.engine.MarkIncomplete("taint-state")
		return []core.Event{event}, err
	}
	output := []core.Event{event}
	connect, ok := networkConnect(event)
	if !ok || connect.ObservationStage != core.StageCompleted || !p.engine.Complete() {
		return output, nil
	}
	decision, err := p.snapshot.Evaluate(connect, p.engine.Labels(connect.Process.ProcessKey))
	if err != nil {
		p.engine.MarkIncomplete("policy-evaluation")
		return output, err
	}
	if decision.Action != policy.Audit && decision.Action != policy.Deny {
		return output, nil
	}
	sources := p.engine.SourcePaths(connect.Process.ProcessKey)
	labels := p.engine.Labels(connect.Process.ProcessKey)
	if len(sources) == 0 || len(labels) == 0 {
		return output, errors.New("taint state lacks source provenance")
	}
	eventID := make([]byte, 16)
	if _, err := rand.Read(eventID); err != nil {
		p.engine.MarkIncomplete("event-id-generation")
		return output, err
	}
	output = append(output, core.PolicyViolationEvent{
		Metadata: core.Metadata{
			SchemaVersion:    core.SchemaVersion,
			EventID:          hex.EncodeToString(eventID),
			RunID:            connect.RunID,
			Timestamp:        connect.Timestamp,
			SessionID:        connect.SessionID,
			ObservationStage: connect.ObservationStage,
			OperationResult:  connect.OperationResult,
			Process:          connect.Process,
		},
		Labels:   labels,
		Source:   core.FileResource{Path: sources[0]},
		Sink:     connect.Destination,
		Decision: string(decision.Action),
		Policy:   decision.Rule,
	})
	return output, nil
}

func networkConnect(event core.Event) (core.NetworkConnectEvent, bool) {
	switch value := event.(type) {
	case core.NetworkConnectEvent:
		return value, true
	case *core.NetworkConnectEvent:
		if value != nil {
			return *value, true
		}
	}
	return core.NetworkConnectEvent{}, false
}
