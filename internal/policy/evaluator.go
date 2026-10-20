package policy

import (
	"errors"
	"net/netip"

	"github.com/bocopile/AgentTaint/internal/core"
)

// Evaluate applies deny > audit > allow. A missing match defaults to allow.
// This returns a policy verdict only; it does not block a network operation.
func (s *Snapshot) Evaluate(event core.NetworkConnectEvent, labels []core.Label) (Decision, error) {
	if s == nil {
		return Decision{}, errors.New("nil policy snapshot")
	}
	if !event.Destination.Address.Is4() {
		return Decision{}, errors.New("policy supports IPv4 destinations only")
	}
	labelSet := make(map[core.Label]struct{}, len(labels))
	for _, label := range labels {
		labelSet[label] = struct{}{}
	}
	decision := Decision{Action: Allow}
	for _, candidate := range s.rules {
		if _, has := labelSet[candidate.label]; !has || !matches(candidate, event.Destination.Address, event.Destination.Port) {
			continue
		}
		if rank(candidate.action) > rank(decision.Action) || (rank(candidate.action) == rank(decision.Action) && (decision.Rule == "" || candidate.name < decision.Rule)) {
			decision = Decision{Action: candidate.action, Rule: candidate.name}
		}
	}
	return decision, nil
}

func matches(candidate rule, address netip.Addr, port uint16) bool {
	if len(candidate.ports) > 0 {
		if _, ok := candidate.ports[port]; !ok {
			return false
		}
	}
	for _, prefix := range candidate.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func rank(action Action) int {
	switch action {
	case Deny:
		return 3
	case Audit:
		return 2
	case Allow:
		return 1
	default:
		return 0
	}
}
