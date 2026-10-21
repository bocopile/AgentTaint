//go:build linux

package runner

import (
	"bytes"
	"testing"

	"github.com/bocopile/AgentTaint/internal/core"
)

func TestPolicyModeActionsAreDistinctAndExplicit(t *testing.T) {
	deny := core.PolicyViolationEvent{Metadata: core.Metadata{EventID: "event-deny"}, Policy: "deny-egress", Decision: "deny"}
	audit := core.PolicyViolationEvent{Metadata: core.Metadata{EventID: "event-audit"}, Policy: "audit-egress", Decision: "audit"}

	t.Run("monitor records no action", func(t *testing.T) {
		var out bytes.Buffer
		actions := &modeActions{}
		if err := applyPolicyModeAction(ModeMonitor, deny, &out, nil, actions, &coverageQuality{}); err != nil {
			t.Fatal(err)
		}
		if actions.notifications != 0 || actions.killed || out.Len() != 0 {
			t.Fatalf("monitor acted on decision: actions=%+v output=%q", actions, out.String())
		}
	})

	t.Run("notify reports the observed decision", func(t *testing.T) {
		var out bytes.Buffer
		actions := &modeActions{}
		if err := applyPolicyModeAction(ModeNotify, deny, &out, nil, actions, &coverageQuality{}); err != nil {
			t.Fatal(err)
		}
		if actions.notifications != 1 || actions.killed || !bytes.Contains(out.Bytes(), []byte("audit_event_id=event-deny")) || !bytes.Contains(out.Bytes(), []byte("policy=deny-egress")) || out.Len() == 0 {
			t.Fatalf("notify action missing: actions=%+v output=%q", actions, out.String())
		}
	})

	t.Run("kill acts only on explicit deny and only once", func(t *testing.T) {
		calls := 0
		kill := func() error { calls++; return nil }
		actions := &modeActions{}
		quality := &coverageQuality{}
		if err := applyPolicyModeAction(ModeKill, audit, nil, kill, actions, quality); err != nil {
			t.Fatal(err)
		}
		if calls != 0 || actions.killed {
			t.Fatalf("audit decision triggered kill: calls=%d actions=%+v", calls, actions)
		}
		for range 2 {
			if err := applyPolicyModeAction(ModeKill, deny, nil, kill, actions, quality); err != nil {
				t.Fatal(err)
			}
		}
		if calls != 1 || !actions.killed {
			t.Fatalf("deny should trigger one kill: calls=%d actions=%+v", calls, actions)
		}
	})

	t.Run("kill fails closed without a run scope", func(t *testing.T) {
		if err := applyPolicyModeAction(ModeKill, deny, nil, nil, &modeActions{}, &coverageQuality{}); err == nil {
			t.Fatal("kill without a private run scope was accepted")
		}
	})
}
