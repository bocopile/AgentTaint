package runner

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/bocopile/AgentTaint/internal/env"
)

type childResult struct {
	Args  []string
	Input []byte
	Path  string
}

func TestFixtureChild(t *testing.T) {
	if os.Getenv("AGENTTAINT_RUNNER_FIXTURE") != "1" {
		return
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(99)
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "raw" {
		_, _ = os.Stdout.Write(input)
	} else {
		_ = json.NewEncoder(os.Stdout).Encode(childResult{os.Args, input, os.Getenv("PATH")})
	}
	_, _ = os.Stderr.Write([]byte("child-stderr\x00\xff\n"))
	code, _ := strconv.Atoi(os.Getenv("AGENTTAINT_RUNNER_EXIT"))
	os.Exit(code)
}

func childLink(t *testing.T, path string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunPreservesArgsStreamsEnvironmentAndExit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("PATH", filepath.Join(root, "empty"))
	t.Setenv("AGENTTAINT_RUNNER_FIXTURE", "1")
	file := childLink(t, filepath.Join(root, "candidate", "unusual-agent"))
	args := []string{"unusual-agent", "-test.run=^TestFixtureChild$", "--", "a b", "$HOME", "~", "*", ";echo wrong", "", "--flag=value", "json"}
	input := []byte{0, 255, 1, 10, 'a'}
	for _, test := range []struct{ source, bin, path string }{
		{"explicit_bin", file, filepath.Join(root, "empty")},
		{"path", "", filepath.Dir(file)},
		{"fallback", "", filepath.Join(root, "empty")},
	} {
		for _, code := range []int{0, 7, 125, 255} {
			t.Run(test.source+strconv.Itoa(code), func(t *testing.T) {
				t.Setenv("PATH", test.path)
				t.Setenv("AGENTTAINT_RUNNER_EXIT", strconv.Itoa(code))
				before := os.Environ()
				var stdout, stderr bytes.Buffer
				got, err := Run(env.Resolver{CandidateDirs: []string{filepath.Dir(file)}}, test.bin, args, bytes.NewReader(input), &stdout, &stderr)
				if err != nil || got != code {
					t.Fatalf("exit %d %v; stderr=%s", got, err, stderr.String())
				}
				var child childResult
				if err := json.Unmarshal(stdout.Bytes(), &child); err != nil {
					t.Fatalf("stdout contaminated: %q %v", stdout.Bytes(), err)
				}
				if !reflect.DeepEqual(child.Args, args) || !bytes.Equal(child.Input, input) || child.Path != test.path {
					t.Fatalf("child: %+v", child)
				}
				if !strings.HasSuffix(stderr.String(), "child-stderr\x00\xff\n") || !strings.Contains(stderr.String(), env.Notice) || !strings.Contains(stderr.String(), "via "+test.source) {
					t.Fatalf("stderr: %q", stderr.Bytes())
				}
				if !reflect.DeepEqual(before, os.Environ()) {
					t.Fatal("runner changed environment")
				}
			})
		}
	}
	var stdout, stderr bytes.Buffer
	args[len(args)-1] = "raw"
	t.Setenv("AGENTTAINT_RUNNER_EXIT", "0")
	code, err := Run(env.Resolver{}, file, args, bytes.NewReader(input), &stdout, &stderr)
	if err != nil || code != 0 || !bytes.Equal(stdout.Bytes(), input) {
		t.Fatalf("binary stdout changed: %x %d %v", stdout.Bytes(), code, err)
	}
}

func TestRunFailuresDoNotExecuteFallback(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	t.Setenv("PATH", ".")
	t.Setenv("AGENTTAINT_RUNNER_FIXTURE", "1")
	childLink(t, filepath.Join(root, "some-agent"))
	fallback := childLink(t, filepath.Join(root, "fallback", "some-agent"))
	nonexec := filepath.Join(root, "nonexec")
	if err := os.WriteFile(nonexec, []byte("not executable"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		bin  string
		args []string
	}{
		{"", nil}, {"", []string{"some-agent"}}, {"missing", []string{"some-agent"}}, {root, []string{"some-agent"}}, {nonexec, []string{"some-agent"}}, {"", []string{"./missing/some-agent"}},
	} {
		var out, diagnostic bytes.Buffer
		code, err := Run(env.Resolver{CandidateDirs: []string{filepath.Dir(fallback)}}, test.bin, test.args, nil, &out, &diagnostic)
		if err == nil || code != 1 || out.Len() != 0 || strings.Contains(diagnostic.String(), "child-stderr") {
			t.Fatalf("unexpected execution: %d %v %q %q", code, err, out.String(), diagnostic.String())
		}
	}
	badFormat := filepath.Join(root, "bad-format")
	if err := os.WriteFile(badFormat, []byte("not an executable format"), 0755); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code, err := Run(env.Resolver{}, badFormat, []string{"some-agent"}, nil, &out, &diagnostic); err == nil || code != 1 {
		t.Fatalf("format error: %d %v", code, err)
	}
}

func TestRunPreservesSymlinkParentTraversal(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	t.Setenv("PATH", filepath.Join(root, "empty"))
	if err := os.MkdirAll(filepath.Join(root, "real", "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "child"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// Distinct executable fixtures make accidental lexical cleanup observable.
	for path, body := range map[string]string{
		filepath.Join(root, "real", "tool"): "#!/bin/sh\nprintf intended-target",
		filepath.Join(root, "tool"):         "#!/bin/sh\nprintf wrong-target",
	} {
		if err := os.WriteFile(path, []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"link/../tool", root + "/link/../tool"} {
		for _, explicit := range []bool{false, true} {
			command, bin := path, ""
			if explicit {
				command, bin = "tool", path
			}
			var out, diagnostic bytes.Buffer
			code, err := Run(env.Resolver{}, bin, []string{command}, nil, &out, &diagnostic)
			if err != nil || code != 0 || out.String() != "intended-target" {
				t.Fatalf("explicit=%t path=%q: code=%d err=%v output=%q stderr=%q", explicit, path, code, err, out.String(), diagnostic.String())
			}
		}
	}
}
