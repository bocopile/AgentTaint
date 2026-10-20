package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bocopile/AgentTaint/internal/env"
)

func TestCLIParsing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{nil, {"unknown"}, {"doctor", "unexpected"}, {"doctor", "--json", "extra"}, {"run"}, {"run", "echo"}, {"run", "--"}, {"run", "--bin"}, {"run", "--bin", "", "--", "echo"}, {"run", "--bin", "--", "echo"}, {"run", "--bin=", "--", "echo"}, {"run", "--bad", "--", "echo"}, {"run", "--bin", "/file", "--bin", "/other", "--", "echo"}, {"run", "--observe", "--", "echo"}, {"run", "--observe", "--audit-file", "audit", "--", "echo"}, {"run", "--policy", "policy", "--", "echo"}, {"run", "--observe", "--policy"}, {"run", "--observe", "--policy", "policy", "--policy", "other", "--audit-file", "audit", "--", "echo"}, {"run", "--audit-file", "audit", "--", "echo"}, {"run", "--observe", "--audit-file"}, {"run", "--observe", "--observe", "--policy", "policy", "--audit-file", "audit", "--", "echo"}, {"run", "--run-as"}, {"run", "--run-as", "65534:65534", "--", "echo"}, {"run", "--observe", "--policy", "policy", "--run-as", "65534", "--audit-file", "audit", "--", "echo"}, {"run", "--observe", "--policy", "policy", "--run-as", "65534:65534", "--run-as", "65534:65534", "--audit-file", "audit", "--", "echo"}} {
		var out, diagnostic bytes.Buffer
		if code := run(args, nil, &out, &diagnostic, env.Resolver{}); code != 2 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "usage:") {
			t.Fatalf("%q: %d %q %q", args, code, out.String(), diagnostic.String())
		}
	}
	var out, diagnostic bytes.Buffer
	policyPath := filepath.Join(t.TempDir(), "missing.yaml")
	if code := run([]string{"run", "--observe", "--policy", policyPath, "--audit-file", filepath.Join(t.TempDir(), "audit.jsonl"), "--", "/bin/true"}, nil, &out, &diagnostic, env.Resolver{}); code != 1 || !strings.Contains(diagnostic.String(), "load policy") {
		t.Fatalf("invalid policy did not fail before launch: code=%d stdout=%q stderr=%q", code, out.String(), diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	if code := run([]string{"--help"}, nil, &out, &diagnostic, env.Resolver{}); code != 0 || !strings.Contains(out.String(), "doctor") {
		t.Fatalf("help: %d %s", code, out.String())
	}
	for _, args := range [][]string{{"doctor"}, {"doctor", "--json"}} {
		out.Reset()
		diagnostic.Reset()
		if code := run(args, nil, &out, &diagnostic, env.Resolver{}); code != 0 {
			t.Fatalf("doctor: %d %s", code, diagnostic.String())
		}
		if !strings.Contains(out.String(), "not_executed_read_only") || !strings.Contains(diagnostic.String(), env.Notice) {
			t.Fatalf("doctor: %s %s", out.String(), diagnostic.String())
		}
		if len(args) == 2 {
			var report env.Report
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestParseTargetIdentity(t *testing.T) {
	identity, err := parseTargetIdentity("65534:123")
	if err != nil || identity == nil || identity.UID != 65534 || identity.GID != 123 {
		t.Fatalf("parse identity: %#v %v", identity, err)
	}
	for _, value := range []string{"", "1", ":2", "1:", "-1:2", "1:+2", "4294967296:2", "1:2:3"} {
		if identity, err := parseTargetIdentity(value); err == nil || identity != nil {
			t.Fatalf("accepted invalid identity %q: %#v %v", value, identity, err)
		}
	}
}

func TestCLIChild(t *testing.T) {
	if os.Getenv("AGENTTAINT_CLI_FIXTURE") != "1" {
		return
	}
	_, _ = io.Copy(os.Stdout, os.Stdin)
	_, _ = io.WriteString(os.Stderr, "target diagnostic\n")
	os.Exit(23)
}

func TestCLIRunRouting(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root)
	t.Setenv("HOME", root)
	t.Setenv("AGENTTAINT_CLI_FIXTURE", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, "arbitrary-cli")
	if err := os.Symlink(self, name); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"run", "--", "arbitrary-cli", "-test.run=^TestCLIChild$"},
		{"run", "--bin", name, "--", "arbitrary-cli", "-test.run=^TestCLIChild$"},
		{"run", "--bin=" + name, "--", "arbitrary-cli", "-test.run=^TestCLIChild$"},
	} {
		var out, diagnostic bytes.Buffer
		input := []byte{0, 255, 10, 'x'}
		code := run(args, bytes.NewReader(input), &out, &diagnostic, env.Resolver{})
		if code != 23 || !bytes.Equal(out.Bytes(), input) || !strings.Contains(diagnostic.String(), env.Notice) || !strings.HasSuffix(diagnostic.String(), "target diagnostic\n") {
			t.Fatalf("run: %d %q %q", code, out.Bytes(), diagnostic.String())
		}
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"run", "--bin", filepath.Join(root, "missing"), "--", "arbitrary-cli"}, nil, &out, &diagnostic, env.Resolver{}); code != 1 || out.Len() != 0 {
		t.Fatalf("missing binary: %d %q", code, out.String())
	}
}
