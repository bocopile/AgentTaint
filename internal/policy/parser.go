package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bocopile/AgentTaint/internal/core"
	"go.yaml.in/yaml/v3"
)

const (
	maxPolicyBytes     = 1 << 20
	maxPolicySources   = 1024
	maxPolicyRules     = 1024
	maxPolicyYAMLDepth = 64
	maxPolicyYAMLNodes = 20000
)

type policyDocument struct {
	Sources   []sourceDocument `yaml:"sources"`
	DenyFiles filePathList     `yaml:"deny_files"`
	Rules     []ruleDocument   `yaml:"rules"`
}

type filePathList []string

func (paths *filePathList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return errors.New("deny_files must be a non-empty sequence of paths")
	}
	values := make(filePathList, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || item.Value == "" {
			return errors.New("deny_files must contain non-empty string paths")
		}
		values = append(values, item.Value)
	}
	*paths = values
	return nil
}

type sourceDocument struct {
	Path  string `yaml:"path"`
	Label string `yaml:"label"`
}

type ruleDocument struct {
	Name string `yaml:"name"`
	When struct {
		ProcessHas string `yaml:"process_has"`
	} `yaml:"when"`
	Sink struct {
		Network struct {
			External []string `yaml:"external"`
			Ports    portList `yaml:"ports"`
		} `yaml:"network"`
	} `yaml:"sink"`
	Action string `yaml:"action"`
}

type portList []uint16

func (ports *portList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return errors.New("ports must be a non-empty sequence of integers")
	}
	values := make(portList, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!int" {
			return errors.New("ports must contain decimal integers")
		}
		value, err := strconv.ParseUint(item.Value, 10, 16)
		if err != nil || value == 0 {
			return fmt.Errorf("invalid TCP/UDP port %q", item.Value)
		}
		values = append(values, uint16(value))
	}
	*ports = values
	return nil
}

// Load reads, validates, and freezes a policy. Relative source paths are
// resolved against the policy file directory; event matching still uses the
// path visible in the event and does not resolve dirfd, symlink, or hardlink
// identity.
func Load(path string) (*Snapshot, error) {
	if path == "" {
		return nil, errors.New("policy path must not be empty")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve policy path: %w", err)
	}
	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open policy: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat policy: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("policy must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPolicyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	if len(data) > maxPolicyBytes {
		return nil, fmt.Errorf("policy exceeds %d-byte limit", maxPolicyBytes)
	}
	if err := validateYAMLShape(data); err != nil {
		return nil, err
	}
	var document policyDocument
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode policy: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("policy must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode trailing policy document: %w", err)
	}
	return buildSnapshot(absPath, data, document)
}

func validateYAMLShape(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("parse policy YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("policy must contain exactly one YAML document")
		}
		return fmt.Errorf("parse trailing policy YAML: %w", err)
	}
	if len(document.Content) != 1 {
		return errors.New("policy document is empty")
	}
	type pendingNode struct {
		node  *yaml.Node
		depth int
	}
	stack := []pendingNode{{node: &document}}
	visited := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		item := stack[last]
		stack = stack[:last]
		visited++
		if visited > maxPolicyYAMLNodes {
			return fmt.Errorf("policy YAML exceeds %d-node limit", maxPolicyYAMLNodes)
		}
		if item.depth > maxPolicyYAMLDepth {
			return fmt.Errorf("policy YAML exceeds %d-level nesting limit", maxPolicyYAMLDepth)
		}
		node := item.node
		if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Tag == "!!merge" || (node.Kind == yaml.ScalarNode && node.Value == "<<") {
			return fmt.Errorf("YAML anchors, aliases, and merge keys are unsupported at line %d", node.Line)
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				if key.Value == "ports" && value.Tag == "!!null" {
					return fmt.Errorf("ports cannot be null at line %d", value.Line)
				}
			}
		}
		for _, child := range node.Content {
			stack = append(stack, pendingNode{node: child, depth: item.depth + 1})
		}
	}
	return nil
}

func hasPathExpansion(path string) bool {
	for i := 0; i < len(path); i++ {
		if path[i] == '$' && i+1 < len(path) {
			next := path[i+1]
			if next == '{' || next == '_' || next >= 'A' && next <= 'Z' || next >= 'a' && next <= 'z' {
				return true
			}
		}
		if path[i] == '`' && strings.IndexByte(path[i+1:], '`') >= 0 {
			return true
		}
	}
	return false
}

func buildSnapshot(absPath string, data []byte, document policyDocument) (*Snapshot, error) {
	if len(document.Sources) > maxPolicySources {
		return nil, fmt.Errorf("policy has more than %d sources", maxPolicySources)
	}
	if len(document.DenyFiles) > maxPolicySources {
		return nil, fmt.Errorf("policy has more than %d denied file paths", maxPolicySources)
	}
	if len(document.Sources) == 0 && len(document.DenyFiles) == 0 {
		return nil, errors.New("policy must define a source or denied file path")
	}
	if len(document.Rules) > maxPolicyRules || (len(document.Rules) == 0 && len(document.Sources) > 0 && len(document.DenyFiles) == 0) {
		return nil, fmt.Errorf("policy must contain between 1 and %d rules", maxPolicyRules)
	}
	if len(document.Rules) > 0 && len(document.Sources) == 0 {
		return nil, errors.New("network rules require at least one source")
	}
	base := filepath.Dir(absPath)
	snapshot := &Snapshot{}
	digest := sha256.Sum256(data)
	snapshot.digest = hex.EncodeToString(digest[:])
	seenSources := make(map[string]struct{}, len(document.Sources))
	for _, item := range document.Sources {
		if item.Path == "" || strings.HasPrefix(item.Path, "~") || hasPathExpansion(item.Path) {
			return nil, fmt.Errorf("source path %q is empty or uses unsupported expansion syntax", item.Path)
		}
		if item.Label != string(core.LabelSecret) {
			return nil, fmt.Errorf("unsupported source label %q", item.Label)
		}
		resolved := item.Path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(base, resolved)
		}
		resolved = filepath.Clean(resolved)
		if _, duplicate := seenSources[resolved]; duplicate {
			return nil, fmt.Errorf("duplicate source path %q", resolved)
		}
		seenSources[resolved] = struct{}{}
		snapshot.sources = append(snapshot.sources, source{path: resolved, label: core.LabelSecret})
	}
	seenDeniedFiles := make(map[string]struct{}, len(document.DenyFiles))
	for _, path := range document.DenyFiles {
		if path == "" || strings.HasPrefix(path, "~") || hasPathExpansion(path) {
			return nil, fmt.Errorf("denied file path %q is empty or uses unsupported expansion syntax", path)
		}
		resolved := path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(base, resolved)
		}
		resolved = filepath.Clean(resolved)
		if _, duplicate := seenDeniedFiles[resolved]; duplicate {
			return nil, fmt.Errorf("duplicate denied file path %q", resolved)
		}
		if _, source := seenSources[resolved]; source {
			return nil, fmt.Errorf("path %q cannot be both a source and a denied file", resolved)
		}
		seenDeniedFiles[resolved] = struct{}{}
		snapshot.denyFiles = append(snapshot.denyFiles, resolved)
	}
	seenRules := make(map[string]struct{}, len(document.Rules))
	for _, item := range document.Rules {
		if item.Name == "" {
			return nil, errors.New("rule name must not be empty")
		}
		if _, duplicate := seenRules[item.Name]; duplicate {
			return nil, fmt.Errorf("duplicate rule name %q", item.Name)
		}
		seenRules[item.Name] = struct{}{}
		if item.When.ProcessHas != string(core.LabelSecret) {
			return nil, fmt.Errorf("rule %q uses unsupported label %q", item.Name, item.When.ProcessHas)
		}
		if len(item.Sink.Network.External) == 0 {
			return nil, fmt.Errorf("rule %q must define an explicit external CIDR set", item.Name)
		}
		current := rule{name: item.Name, label: core.LabelSecret, action: Action(item.Action)}
		if current.action != Allow && current.action != Audit && current.action != Deny {
			return nil, fmt.Errorf("rule %q uses unsupported action %q", item.Name, item.Action)
		}
		for _, cidr := range item.Sink.Network.External {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return nil, fmt.Errorf("rule %q has invalid IPv4 CIDR %q", item.Name, cidr)
			}
			current.prefixes = append(current.prefixes, prefix.Masked())
		}
		if len(item.Sink.Network.Ports) > 0 {
			current.ports = make(map[uint16]struct{}, len(item.Sink.Network.Ports))
			for _, port := range item.Sink.Network.Ports {
				if port == 0 {
					return nil, fmt.Errorf("rule %q has invalid port 0", item.Name)
				}
				if _, duplicate := current.ports[port]; duplicate {
					return nil, fmt.Errorf("rule %q has duplicate port %d", item.Name, port)
				}
				current.ports[port] = struct{}{}
			}
		}
		snapshot.rules = append(snapshot.rules, current)
	}
	return snapshot, nil
}
