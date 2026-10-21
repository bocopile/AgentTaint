package policy

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bocopile/AgentTaint/internal/core"
)

const validPolicy = `sources:
  - path: ../fixtures/secret.txt
    label: SECRET
rules:
  - name: audit-sensitive-egress
    when:
      process_has: SECRET
    sink:
      network:
        external:
          - 203.0.113.0/24
        ports: [80, 443]
    action: audit
  - name: allow-sensitive-egress
    when:
      process_has: SECRET
    sink:
      network:
        external:
          - 203.0.113.0/24
        ports: [443]
    action: allow
  - name: deny-sensitive-egress
    when:
      process_has: SECRET
    sink:
      network:
        external:
          - 203.0.113.128/25
        ports: [443]
    action: deny
`

func writePolicy(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadResolvesSourceAndFreezesSnapshot(t *testing.T) {
	path := writePolicy(t, validPolicy)
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(filepath.Dir(path), "..", "fixtures", "secret.txt")
	if got := snapshot.SourcePaths(); len(got) != 1 || got[0] != filepath.Clean(expected) {
		t.Fatalf("resolved source path = %v, want %s", got, filepath.Clean(expected))
	}
	digest := snapshot.Digest()
	if len(digest) != 64 {
		t.Fatalf("SHA-256 digest length = %d", len(digest))
	}
	paths := snapshot.SourcePaths()
	paths[0] = "/changed"
	if snapshot.SourcePaths()[0] == "/changed" {
		t.Fatal("SourcePaths exposed mutable snapshot storage")
	}
	if err := os.WriteFile(path, []byte("invalid: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision, err := snapshot.Evaluate(connect("203.0.113.200"), []core.Label{core.LabelSecret})
	if err != nil {
		t.Fatal(err)
	}
	if decision != (Decision{Action: Deny, Rule: "deny-sensitive-egress"}) || snapshot.Digest() != digest {
		t.Fatalf("loaded snapshot changed: decision=%+v digest=%s", decision, snapshot.Digest())
	}
}

func TestKernelProjectionContainsOnlyBoundedDenyMatches(t *testing.T) {
	snapshot, err := Load(writePolicy(t, validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := snapshot.KernelProjection()
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Sources) != 1 || len(projection.Denies) != 1 {
		t.Fatalf("projection sources/denies = %d/%d, want 1/1", len(projection.Sources), len(projection.Denies))
	}
	want := KernelRule{Address: [4]byte{203, 0, 113, 128}, PrefixLen: 25, Port: 443}
	if projection.Denies[0] != want {
		t.Fatalf("kernel deny = %+v, want %+v", projection.Denies[0], want)
	}
}

func TestLoadProjectsDirectFileDenyWithoutNetworkSources(t *testing.T) {
	path := writePolicy(t, "deny_files:\n  - ../fixtures/denied.txt\n")
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := snapshot.KernelProjection()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(filepath.Dir(path), "..", "fixtures", "denied.txt"))
	if len(projection.Sources) != 0 || len(projection.Denies) != 0 || len(projection.DenyFiles) != 1 || projection.DenyFiles[0] != want {
		t.Fatalf("direct file-deny projection = %+v, want only %q", projection, want)
	}

	for name, text := range map[string]string{
		"duplicate denied path": "deny_files:\n  - /tmp/denied\n  - /tmp/denied\n",
		"source overlap":        strings.Replace(validPolicy, "rules:\n", "deny_files:\n  - ../fixtures/secret.txt\nrules:\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writePolicy(t, text)); err == nil {
				t.Fatal("expected conflicting direct file-deny policy to be rejected")
			}
		})
	}
}

func TestKernelProjectionAllowsPoliciesWithoutDenyRules(t *testing.T) {
	text := strings.Replace(validPolicy, "action: deny", "action: allow", 1)
	snapshot, err := Load(writePolicy(t, text))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := snapshot.KernelProjection()
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Denies) != 0 {
		t.Fatalf("projection has %d deny rules, want none", len(projection.Denies))
	}
}

func TestKernelProjectionRejectsUnsupportedCapacityAndPaths(t *testing.T) {
	snapshot, err := Load(writePolicy(t, validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	longPath := *snapshot
	longPath.sources = []source{{path: "/" + strings.Repeat("x", maxKernelPath), label: core.LabelSecret}}
	if _, err := longPath.KernelProjection(); err == nil {
		t.Fatal("overlong source path unexpectedly projected")
	}
	tooMany := *snapshot
	tooMany.rules = nil
	for i := 0; i <= maxKernelDenies; i++ {
		tooMany.rules = append(tooMany.rules, rule{
			name: fmt.Sprintf("deny-%d", i), label: core.LabelSecret,
			prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")},
			ports:    map[uint16]struct{}{443: {}}, action: Deny,
		})
	}
	if _, err := tooMany.KernelProjection(); err == nil {
		t.Fatal("oversized deny projection unexpectedly succeeded")
	}
}

func TestLoadRejectsIPv6DenyRuleRatherThanSilentlyIgnoringIt(t *testing.T) {
	text := strings.Replace(validPolicy, "203.0.113.128/25", "2001:db8::/32", 1)
	if _, err := Load(writePolicy(t, text)); err == nil || !strings.Contains(err.Error(), "invalid IPv4 CIDR") {
		t.Fatalf("policy loader silently accepted or ignored IPv6 deny rule: %v", err)
	}
}

func TestLoadAllowsLiteralDollarInSourcePath(t *testing.T) {
	text := strings.Replace(validPolicy, "../fixtures/secret.txt", "../fixtures/secret$5.txt", 1)
	snapshot, err := Load(writePolicy(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(snapshot.SourcePaths()[0], "secret$5.txt") {
		t.Fatalf("literal dollar path changed: %s", snapshot.SourcePaths()[0])
	}
}

func TestExamplePolicyLoads(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "policies", "example.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluatePrecedenceLabelsAndDefault(t *testing.T) {
	snapshot, err := Load(writePolicy(t, validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		addr   string
		labels []core.Label
		want   Decision
	}{
		{"deny beats audit", "203.0.113.200", []core.Label{core.LabelSecret}, Decision{Action: Deny, Rule: "deny-sensitive-egress"}},
		{"audit beats allow", "203.0.113.20", []core.Label{core.LabelSecret}, Decision{Action: Audit, Rule: "audit-sensitive-egress"}},
		{"clean process", "203.0.113.200", nil, Decision{Action: Allow}},
		{"outside explicit CIDRs", "198.51.100.7", []core.Label{core.LabelSecret}, Decision{Action: Allow}},
	}
	port80 := connect("203.0.113.200")
	port80.Destination.Port = 80
	if got, err := snapshot.Evaluate(port80, []core.Label{core.LabelSecret}); err != nil || got != (Decision{Action: Audit, Rule: "audit-sensitive-egress"}) {
		t.Fatalf("port-filtered audit decision = %+v, err=%v", got, err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := snapshot.Evaluate(connect(tc.addr), tc.labels)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("decision = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExplicitCIDRCanClassifyPrivateAddressWithoutImplicitException(t *testing.T) {
	text := strings.Replace(validPolicy, "203.0.113.0/24", "10.0.0.0/8", 1)
	text = strings.Replace(text, "203.0.113.128/25", "10.1.0.0/16", 1)
	snapshot, err := Load(writePolicy(t, text))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := snapshot.Evaluate(connect("10.1.2.3"), []core.Label{core.LabelSecret})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Deny {
		t.Fatalf("explicit private CIDR was implicitly trusted: %+v", decision)
	}
}

func TestLoadRejectsMalformedOrUnsupportedPolicy(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"duplicate key", strings.Replace(validPolicy, "label: SECRET", "label: SECRET\n    label: SECRET", 1)},
		{"unknown field", strings.Replace(validPolicy, "rules:", "unknown: true\nrules:", 1)},
		{"multiple documents", validPolicy + "---\n" + validPolicy},
		{"anchor", strings.Replace(validPolicy, "label: SECRET", "label: &label SECRET", 1)},
		{"alias", strings.Replace(validPolicy, "label: SECRET", "label: *label", 1)},
		{"merge key", strings.Replace(validPolicy, "rules:", "defaults: &defaults\n  action: deny\nrules:\n  - <<: *defaults", 1)},
		{"unknown label", strings.Replace(validPolicy, "label: SECRET", "label: PRIVATE_KEY", 1)},
		{"unknown action", strings.Replace(validPolicy, "action: audit", "action: block", 1)},
		{"invalid CIDR", strings.Replace(validPolicy, "203.0.113.0/24", "not-a-cidr", 1)},
		{"invalid port zero", strings.Replace(validPolicy, "ports: [80, 443]", "ports: [0, 443]", 1)},
		{"invalid port range", strings.Replace(validPolicy, "ports: [80, 443]", "ports: [65536]", 1)},
		{"empty port list", strings.Replace(validPolicy, "ports: [80, 443]", "ports: []", 1)},
		{"null port list", strings.Replace(validPolicy, "ports: [80, 443]", "ports: null", 1)},
		{"duplicate port", strings.Replace(validPolicy, "ports: [80, 443]", "ports: [443, 443]", 1)},
		{"IPv6 CIDR", strings.Replace(validPolicy, "203.0.113.0/24", "2001:db8::/32", 1)},
		{"duplicate rule", strings.Replace(validPolicy, "name: deny-sensitive-egress", "name: audit-sensitive-egress", 1)},
		{"unsupported expansion", strings.Replace(validPolicy, "../fixtures/secret.txt", "${HOME}/secret.txt", 1)},
		{"missing source", strings.Replace(validPolicy, "sources:\n  - path: ../fixtures/secret.txt\n    label: SECRET\n", "", 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writePolicy(t, tc.text)); err == nil {
				t.Fatal("expected policy rejection")
			}
		})
	}
	deep := strings.Repeat("[", maxPolicyYAMLDepth+2) + "null" + strings.Repeat("]", maxPolicyYAMLDepth+2)
	if _, err := Load(writePolicy(t, deep)); err == nil {
		t.Fatal("expected excessive YAML nesting rejection")
	}
}

func TestLoadRejectsSizeAndRuleCountLimits(t *testing.T) {
	if _, err := Load(writePolicy(t, validPolicy+strings.Repeat("# padding\n", maxPolicyBytes/9))); err == nil {
		t.Fatal("expected oversized policy rejection")
	}
	var many strings.Builder
	many.WriteString("sources:\n  - path: /secret\n    label: SECRET\nrules:\n")
	for i := 0; i <= maxPolicyRules; i++ {
		fmt.Fprintf(&many, "  - name: rule-%04d\n    when:\n      process_has: SECRET\n    sink:\n      network:\n        external: [203.0.113.0/24]\n    action: audit\n", i)
	}
	if _, err := Load(writePolicy(t, many.String())); err == nil {
		t.Fatal("expected excessive rule count rejection")
	}
	var sources strings.Builder
	sources.WriteString("sources:\n")
	for i := 0; i <= maxPolicySources; i++ {
		fmt.Fprintf(&sources, "  - path: /secret/%04d\n    label: SECRET\n", i)
	}
	sources.WriteString("rules:\n  - name: rule\n    when:\n      process_has: SECRET\n    sink:\n      network:\n        external: [203.0.113.0/24]\n    action: deny\n")
	if _, err := Load(writePolicy(t, sources.String())); err == nil {
		t.Fatal("expected excessive source count rejection")
	}
}

func TestLoadAcceptsPolicyAtByteLimit(t *testing.T) {
	policyText := validPolicy
	if !strings.HasSuffix(policyText, "\n") {
		policyText += "\n"
	}
	if len(policyText) > maxPolicyBytes {
		t.Fatalf("valid policy fixture already exceeds byte limit: %d", len(policyText))
	}
	policyText += strings.Repeat("#", maxPolicyBytes-len(policyText))
	if len(policyText) != maxPolicyBytes {
		t.Fatalf("constructed policy size = %d, want exactly %d", len(policyText), maxPolicyBytes)
	}
	if _, err := Load(writePolicy(t, policyText)); err != nil {
		t.Fatalf("policy at the inclusive byte limit was rejected: %v", err)
	}
}

func TestLoadAcceptsMaximumSourceAndRuleCounts(t *testing.T) {
	t.Run("maximum sources", func(t *testing.T) {
		var text strings.Builder
		text.WriteString("sources:\n")
		for i := 0; i < maxPolicySources; i++ {
			fmt.Fprintf(&text, "  - path: /secret/%04d\n    label: SECRET\n", i)
		}
		text.WriteString("rules:\n  - name: rule\n    when: {process_has: SECRET}\n    sink: {network: {external: [203.0.113.0/24]}}\n    action: deny\n")
		snapshot, err := Load(writePolicy(t, text.String()))
		if err != nil {
			t.Fatalf("policy at the inclusive source-count limit was rejected: %v", err)
		}
		if got := len(snapshot.SourcePaths()); got != maxPolicySources {
			t.Fatalf("source count = %d, want %d", got, maxPolicySources)
		}
	})

	t.Run("maximum rules", func(t *testing.T) {
		var text strings.Builder
		text.WriteString("sources:\n  - path: /secret\n    label: SECRET\nrules:\n")
		for i := 0; i < maxPolicyRules; i++ {
			fmt.Fprintf(&text, "  - name: rule-%04d\n    when: {process_has: SECRET}\n    sink: {network: {external: [203.0.113.0/24]}}\n    action: audit\n", i)
		}
		if _, err := Load(writePolicy(t, text.String())); err != nil {
			t.Fatalf("policy at the inclusive rule-count limit was rejected: %v", err)
		}
	})
}

func TestSameActionRuleSelectionIsIndependentOfDeclarationOrder(t *testing.T) {
	text := `sources:
  - path: /secret
    label: SECRET
rules:
  - name: zeta
    when: {process_has: SECRET}
    sink: {network: {external: [203.0.113.0/24]}}
    action: audit
  - name: alpha
    when: {process_has: SECRET}
    sink: {network: {external: [203.0.113.0/24]}}
    action: audit
`
	snapshot, err := Load(writePolicy(t, text))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := snapshot.Evaluate(connect("203.0.113.1"), []core.Label{core.LabelSecret})
	if err != nil {
		t.Fatal(err)
	}
	if decision != (Decision{Action: Audit, Rule: "alpha"}) {
		t.Fatalf("same-action selection is not deterministic: %+v", decision)
	}
}

func TestEvaluateRejectsNonIPv4Destination(t *testing.T) {
	snapshot, err := Load(writePolicy(t, validPolicy))
	if err != nil {
		t.Fatal(err)
	}
	event := connect("203.0.113.1")
	event.Destination.Address = netip.MustParseAddr("2001:db8::1")
	if _, err := snapshot.Evaluate(event, []core.Label{core.LabelSecret}); err == nil {
		t.Fatal("expected IPv6 destination rejection")
	}
}

func connect(address string) core.NetworkConnectEvent {
	return core.NetworkConnectEvent{Destination: core.NetworkDestination{Address: netip.MustParseAddr(address), Port: 443}}
}
