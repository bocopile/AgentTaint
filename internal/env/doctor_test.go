package env

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func executable(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolverPriorityAndErrors(t *testing.T) {
	root := t.TempDir()
	pathDir, fallbackDir := filepath.Join(root, "path"), filepath.Join(root, "fallback")
	pathFile := executable(t, filepath.Join(pathDir, "arbitrary-agent"))
	fallback := executable(t, filepath.Join(fallbackDir, "arbitrary-agent"))
	bin := executable(t, filepath.Join(root, "explicit"))
	t.Setenv("PATH", pathDir)
	t.Setenv("HOME", root)
	r := Resolver{[]string{fallbackDir}}
	for _, test := range []struct{ name, command, bin, want, source string }{
		{"bin", "arbitrary-agent", bin, bin, "explicit_bin"},
		{"path", "arbitrary-agent", "", pathFile, "path"},
		{"slash", fallback, "", fallback, "explicit_path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := r.Resolve(test.command, test.bin)
			if err != nil || got.Path != test.want || got.Source != test.source {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
	nonexec := filepath.Join(root, "nonexec")
	if err := os.WriteFile(nonexec, nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, bin := range []string{filepath.Join(root, "missing"), root, nonexec, "arbitrary-agent"} {
		if got, err := r.Resolve("arbitrary-agent", bin); err == nil {
			t.Fatalf("invalid --bin %q selected %+v", bin, got)
		}
	}
	if _, err := r.Resolve(filepath.Join(root, "missing", "arbitrary-agent"), ""); err == nil {
		t.Fatal("invalid explicit command fell back")
	}
	t.Setenv("PATH", filepath.Join(root, "empty"))
	got, err := r.Resolve("arbitrary-agent", "")
	if err != nil || got.Path != fallback || got.Source != "fallback" {
		t.Fatalf("fallback: %+v %v", got, err)
	}
	if _, err := r.Resolve("missing", ""); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := r.Resolve("", ""); err == nil {
		t.Fatal("accepted empty command")
	}
}

func TestResolverGlobOrderAndSkippedCandidates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "empty"))
	name := "novel-cli"
	first := executable(t, filepath.Join(root, "node", "v10", "bin", name))
	executable(t, filepath.Join(root, "node", "v9", "bin", name))
	dirCandidate := filepath.Join(root, "directory")
	if err := os.MkdirAll(filepath.Join(dirCandidate, name), 0755); err != nil {
		t.Fatal(err)
	}
	nonexecDir := filepath.Join(root, "nonexec")
	nonexec := executable(t, filepath.Join(nonexecDir, name))
	if err := os.Chmod(nonexec, 0644); err != nil {
		t.Fatal(err)
	}
	r := Resolver{[]string{dirCandidate, nonexecDir, filepath.Join(root, "node", "*", "bin")}}
	got, err := r.Resolve(name, "")
	if err != nil || got.Path != first {
		t.Fatalf("glob lexical order: %+v %v", got, err)
	}
	preferred := executable(t, filepath.Join(root, "preferred", name))
	r.CandidateDirs = append([]string{filepath.Dir(preferred)}, r.CandidateDirs...)
	got, err = r.Resolve(name, "")
	if err != nil || got.Path != preferred {
		t.Fatalf("directory priority: %+v %v", got, err)
	}
}

func TestResolverPreservesSymlinkParentTraversal(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PATH", filepath.Join(root, "empty"))
	actual := executable(t, filepath.Join(root, "real", "tool"))
	executable(t, filepath.Join(root, "tool"))
	if err := os.Mkdir(filepath.Join(root, "real", "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "child"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(actual)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"link/../tool", root + "/link/../tool"} {
		for _, explicit := range []bool{false, true} {
			command, bin := path, ""
			if explicit {
				command, bin = "tool", path
			}
			got, err := (Resolver{}).Resolve(command, bin)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(got.Path)
			if err != nil || !os.SameFile(info, want) {
				t.Fatalf("explicit=%t path=%q resolved a different file: %+v %v", explicit, path, got, err)
			}
		}
	}
	got, err := (Resolver{[]string{root + "/link/.."}}).Resolve("tool", "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(got.Path)
	if err != nil || !os.SameFile(info, want) {
		t.Fatalf("fallback resolved a different file: %+v %v", got, err)
	}
}

func TestResolverChecksEffectiveExecutePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may execute a file with any execute bit; owner permission regression requires non-root")
	}
	root := t.TempDir()
	t.Setenv("PATH", filepath.Join(root, "empty"))
	denied := executable(t, filepath.Join(root, "denied", "tool"))
	if err := os.Chmod(denied, 0641); err != nil {
		t.Fatal(err)
	}
	allowed := executable(t, filepath.Join(root, "allowed", "tool"))
	r := Resolver{[]string{filepath.Dir(denied), filepath.Dir(allowed)}}
	for _, explicit := range []bool{false, true} {
		command, bin := denied, ""
		if explicit {
			command, bin = "tool", denied
		}
		if _, err := r.Resolve(command, bin); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("explicit=%t: owner without execute permission accepted: %v", explicit, err)
		}
	}
	got, err := r.Resolve("tool", "")
	if err != nil || got.Path != allowed {
		t.Fatalf("fallback did not skip inaccessible executable: %+v %v", got, err)
	}
}

func TestDoctorRecommendationDoesNotGenerateShellCommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	t.Setenv("PATH", filepath.Join(root, "empty"))
	dir := filepath.Join(root, "$(touch marker-dollar)`touch marker-backtick`'quoted'")
	file := executable(t, filepath.Join(dir, "codex"))
	before := snapshot(t, root)
	report := diagnose("darwin", Resolver{[]string{dir}}, nil, nil)
	cli := report.CLIs[1]
	if !cli.Found || cli.Path != file || !strings.Contains(cli.Recommendation, "literal --bin argument") {
		t.Fatalf("unexpected report: %+v", cli)
	}
	for _, shellText := range []string{dir, "$(", "`", "agenttaint run --bin"} {
		if strings.Contains(cli.Recommendation, shellText) {
			t.Fatalf("recommendation embeds shell text %q: %s", shellText, cli.Recommendation)
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("malicious-looking path caused a filesystem change")
	}
}

func TestResolverRejectsImplicitRelativePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	executable(t, filepath.Join(root, "relative-agent"))
	executable(t, filepath.Join(root, "subdir", "relative-agent"))
	fallback := executable(t, filepath.Join(root, "fallback", "relative-agent"))
	r := Resolver{[]string{filepath.Dir(fallback)}}
	for _, path := range []string{".", ":/missing", "subdir"} {
		for _, debug := range []string{"execerrdot=1", "execerrdot=0"} {
			t.Setenv("PATH", path)
			t.Setenv("GODEBUG", debug)
			if _, err := r.Resolve("relative-agent", ""); !errors.Is(err, exec.ErrDot) {
				t.Fatalf("PATH=%q GODEBUG=%q: %v", path, debug, err)
			}
		}
	}
	got, err := r.Resolve("./relative-agent", "")
	if err != nil || got.Source != "explicit_path" {
		t.Fatalf("explicit relative path: %+v %v", got, err)
	}
}

func TestDefaultCandidatesAndLiteralHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home[1]*?")
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty"))
	r := NewResolver()
	want := []string{globLiteral(home) + "/.local/bin", globLiteral(home) + "/go/bin", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", globLiteral(home) + "/.nvm/versions/node/*/bin", globLiteral(home) + "/.volta/bin", globLiteral(home) + "/.fnm/aliases/default/bin", globLiteral(home) + "/.asdf/shims", globLiteral(home) + "/.local/pipx/venvs/*/bin"}
	if !reflect.DeepEqual(r.CandidateDirs, want) {
		t.Fatalf("candidate order: %q", r.CandidateDirs)
	}
	file := executable(t, filepath.Join(home, ".local", "bin", "fixture-home-literal"))
	got, err := r.Resolve("fixture-home-literal", "")
	if err != nil || got.Path != file {
		t.Fatalf("literal home: %+v %v", got, err)
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var data []byte
		if !entry.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[path] = fmt.Sprintf("%s:%s:%x", info.Mode(), info.ModTime(), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDoctorNeverExecutesOrMutates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	pathDir, fallback := filepath.Join(root, "path"), filepath.Join(root, "fallback")
	for _, file := range []string{filepath.Join(pathDir, "claude"), filepath.Join(fallback, "codex")} {
		executable(t, file)
		body := fmt.Sprintf("#!/bin/sh\nprintf ran > %q\n", filepath.Join(root, "executed-marker"))
		if err := os.WriteFile(file, []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", pathDir)
	beforeEnv, beforeFiles := os.Environ(), snapshot(t, root)
	report := diagnose("darwin", Resolver{[]string{fallback}}, nil, nil)
	if report.Linux != nil || !strings.Contains(report.Platform, "unsupported") {
		t.Fatalf("platform: %+v", report)
	}
	if !report.CLIs[0].Found || !report.CLIs[0].AvailableInPath || !report.CLIs[1].Found || report.CLIs[1].AvailableInPath {
		t.Fatalf("CLIs: %+v", report.CLIs)
	}
	if report.CLIs[1].Recommendation == "" {
		t.Fatal("missing fallback recommendation")
	}
	for _, cli := range report.CLIs {
		if cli.Version != "unknown" || cli.VersionReason != "not_executed_read_only" {
			t.Fatalf("version: %+v", cli)
		}
	}
	for _, jsonOutput := range []bool{false, true} {
		var out bytes.Buffer
		if err := report.Write(&out, jsonOutput); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "not_executed_read_only") {
			t.Fatal(out.String())
		}
		if jsonOutput {
			var decoded Report
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded, report) {
				t.Fatalf("JSON roundtrip: %v %+v", err, decoded)
			}
		}
	}
	if !reflect.DeepEqual(beforeEnv, os.Environ()) {
		t.Fatal("doctor changed environment")
	}
	if !reflect.DeepEqual(beforeFiles, snapshot(t, root)) {
		t.Fatal("doctor changed files or executed marker")
	}
	missing := diagnose("darwin", Resolver{}, nil, nil)
	if missing.CLIs[1].Found || !strings.HasPrefix(missing.CLIs[1].Reason, "missing:") {
		t.Fatalf("missing: %+v", missing.CLIs[1])
	}
	t.Chdir(pathDir)
	t.Setenv("PATH", ".")
	unsafe := diagnose("darwin", Resolver{[]string{fallback}}, nil, nil)
	if unsafe.CLIs[0].Found || !strings.HasPrefix(unsafe.CLIs[0].Reason, "unsafe_relative_path:") {
		t.Fatalf("unsafe: %+v", unsafe.CLIs[0])
	}
}

func TestLinuxStaticChecks(t *testing.T) {
	for _, test := range []struct{ version, kernel, ring, lsm string }{
		{"5.7.19", "read", "unsupported", "below_minimum"},
		{"5.8.0", "read", "meets_minimum", "below_minimum"},
		{"5.12.99", "read", "meets_minimum", "below_minimum"},
		{"5.13.0-custom", "read", "meets_minimum", "meets_minimum"},
		{"6.8.0-101-generic\n", "read", "meets_minimum", "meets_minimum"},
		{"4.19.0", "read", "unsupported", "below_minimum"},
		{"garbage", "unknown", "unknown", "unknown"},
		{"5.13.bad", "unknown", "unknown", "unknown"},
		{"99999999999999999999999.13.0", "unknown", "unknown", "unknown"},
	} {
		t.Run(test.version, func(t *testing.T) {
			read := func(path string) ([]byte, error) {
				if path == "/proc/sys/kernel/osrelease" {
					return []byte(test.version), nil
				}
				if path != "/sys/kernel/security/lsm" {
					t.Fatalf("unexpected read: %s", path)
				}
				return []byte("lockdown,capability,bpf\n"), nil
			}
			probe := func(path string) error {
				if path != "/sys/kernel/btf/vmlinux" {
					t.Fatalf("unexpected probe: %s", path)
				}
				return nil
			}
			got := diagnoseLinux(read, probe)
			if got.Kernel.Status != test.kernel || got.RingBuffer.Status != test.ring || got.BPFLSMKernel.Status != test.lsm || got.BTF.Status != "readable" || got.BPFLSM.Status != "enabled" {
				t.Fatalf("%+v", got)
			}
		})
	}
	for _, test := range []struct{ list, want string }{{"capability,notbpf", "disabled"}, {"bpf,capability", "enabled"}, {"capability, bpf\n", "enabled"}, {"", "unknown"}, {"bpf,", "unknown"}, {"bpf garbage", "unknown"}} {
		got := diagnoseLinux(func(path string) ([]byte, error) {
			if path == "/proc/sys/kernel/osrelease" {
				return []byte("6.8.0"), nil
			}
			return []byte(test.list), nil
		}, func(string) error { return nil })
		if got.BPFLSM.Status != test.want {
			t.Fatalf("LSM %q: %+v", test.list, got.BPFLSM)
		}
	}
	for _, test := range []struct {
		err  error
		want string
	}{{os.ErrNotExist, "missing"}, {os.ErrPermission, "permission_denied"}, {io.ErrUnexpectedEOF, "unknown"}} {
		got := diagnoseLinux(func(path string) ([]byte, error) { return nil, &os.PathError{Op: "read", Path: path, Err: test.err} }, func(path string) error { return &os.PathError{Op: "open", Path: path, Err: test.err} })
		if got.Kernel.Status != test.want || got.BTF.Status != test.want || got.BPFLSM.Status != test.want || got.RingBuffer.Status != "unknown" {
			t.Fatalf("%s: %+v", test.want, got)
		}
	}
}

func TestReadableProbe(t *testing.T) {
	file := filepath.Join(t.TempDir(), "btf")
	if err := readable(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	if err := os.WriteFile(file, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := readable(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := readable(file); !errors.Is(err, io.EOF) {
		t.Fatalf("empty: %v", err)
	}
}
