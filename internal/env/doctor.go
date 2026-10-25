// Package env provides read-only environment diagnostics and executable lookup.
package env

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// KnownCLIs is only a diagnostic convenience; runner accepts arbitrary commands.
var KnownCLIs = []string{"claude", "codex"}

const DoctorNotice = "Static checks do not execute discovered programs and do not verify BPF operation or protection readiness."
const RunNotice = "Pass-through run: no monitoring or blocking is active."
const ProbeNotice = "Opt-in toolchain version probes execute explicitly supplied binaries; they are not sandboxed and do not verify BPF capability."

const probeOutputLimit = 8 * 1024

var probeTimeout = 5 * time.Second

var probeVersion = regexp.MustCompile(`(?i)(?:version\s+|(?:^|\s)v)([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)

type ToolSpec struct {
	Name string
	Path string
}

type ToolchainProbe struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Status    string `json:"status"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Truncated bool   `json:"output_truncated,omitempty"`
}

func ParseToolSpec(value string) (ToolSpec, error) {
	name, path, ok := strings.Cut(value, "=")
	if !ok || name == "" || path == "" {
		return ToolSpec{}, errors.New("--tool requires NAME=/absolute/path")
	}
	return ToolSpec{Name: name, Path: path}, nil
}

func ValidateToolSpecs(specs []ToolSpec) error {
	if len(specs) == 0 {
		return errors.New("--probe-toolchain requires at least one --tool")
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if _, ok := toolProbeArgs(spec.Name); !ok {
			return fmt.Errorf("unsupported tool %q; choose clang, llvm-strip, or bpftool", spec.Name)
		}
		if _, exists := seen[spec.Name]; exists {
			return fmt.Errorf("tool %q was specified more than once", spec.Name)
		}
		seen[spec.Name] = struct{}{}
		if !filepath.IsAbs(spec.Path) {
			return fmt.Errorf("tool %q path must be absolute", spec.Name)
		}
		info, err := os.Stat(spec.Path)
		if err != nil {
			return fmt.Errorf("tool %q path: %w", spec.Name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("tool %q path is not a regular file", spec.Name)
		}
		if info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("tool %q path is not marked executable", spec.Name)
		}
	}
	return nil
}

func toolProbeArgs(name string) ([]string, bool) {
	switch name {
	case "clang", "llvm-strip":
		return []string{"--version"}, true
	case "bpftool":
		return []string{"version"}, true
	default:
		return nil, false
	}
}

// ProbeToolchain executes only caller-supplied, allowlisted absolute paths.
// It does not search PATH or claim to sandbox the selected programs.
func ProbeToolchain(specs []ToolSpec) ([]ToolchainProbe, error) {
	if err := ValidateToolSpecs(specs); err != nil {
		return nil, err
	}
	results := make([]ToolchainProbe, 0, len(specs))
	for _, spec := range specs {
		args, _ := toolProbeArgs(spec.Name)
		results = append(results, runToolProbe(spec, args))
	}
	return results, nil
}

type boundedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		b.data = append(b.data, p[:remaining]...)
	}
	if remaining < len(p) {
		b.truncated = true
	}
	return len(p), nil
}

func runToolProbe(spec ToolSpec, args []string) ToolchainProbe {
	result := ToolchainProbe{Name: spec.Name, Path: spec.Path}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Path, args...)
	cmd.Dir = string(os.PathSeparator)
	cmd.Env = []string{}
	cmd.WaitDelay = 100 * time.Millisecond
	stdout := &boundedBuffer{limit: probeOutputLimit}
	stderr := &boundedBuffer{limit: probeOutputLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	result.Truncated = stdout.truncated || stderr.truncated
	if ctx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Detail = "version query exceeded 5 seconds"
		return result
	}
	if result.Truncated {
		result.Status = "output_truncated"
		result.Detail = "stdout or stderr exceeded 8 KiB"
		return result
	}
	if err != nil {
		result.Status = "execution_failed"
		result.Detail = err.Error()
		return result
	}
	match := probeVersion.FindSubmatch(stdout.data)
	if match == nil {
		result.Status = "version_unrecognized"
		result.Detail = "command succeeded but stdout did not contain a recognizable version"
		return result
	}
	result.Version = string(match[1])
	if spec.Name == "clang" || spec.Name == "llvm-strip" {
		parts := strings.SplitN(result.Version, ".", 2)
		major, parseErr := strconv.Atoi(parts[0])
		if parseErr != nil || major < 12 {
			result.Status = "below_minimum"
			result.Detail = "LLVM 12 or newer is required"
			return result
		}
	}
	result.Status = "reported"
	result.Detail = "version reported; BPF target and kernel capability were not tested"
	return result
}

func ToolchainProbeFailed(results []ToolchainProbe) bool {
	for _, result := range results {
		if result.Status != "reported" {
			return true
		}
	}
	return false
}

// Resolver searches PATH first, then ordered directory patterns. It never changes PATH.
// A zero Resolver searches PATH only. CandidateDirs may contain filepath.Glob patterns.
type Resolver struct{ CandidateDirs []string }

// NewResolver uses the candidate directory order specified in prompts/00-bootstrap.md.
func NewResolver() Resolver {
	home, _ := os.UserHomeDir()
	patterns := []string{"~/.local/bin", "~/go/bin", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "~/.nvm/versions/node/*/bin", "~/.volta/bin", "~/.fnm/aliases/default/bin", "~/.asdf/shims", "~/.local/pipx/venvs/*/bin"}
	var dirs []string
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "~/") {
			if !filepath.IsAbs(home) {
				continue
			}
			// The home directory is literal, even when its name contains glob characters.
			pattern = globLiteral(home) + pattern[1:]
		}
		dirs = append(dirs, pattern)
	}
	return Resolver{CandidateDirs: dirs}
}

func globLiteral(path string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[").Replace(path)
}

// Resolution describes the selected file, not its trustworthiness.
type Resolution struct {
	Path   string
	Source string
}

// Resolve accepts a literal explicit file path or a command name. Only ErrNotFound
// permits fallback; an implicit relative PATH result is always rejected.
func (r Resolver) Resolve(command, explicit string) (Resolution, error) {
	if explicit != "" {
		return explicitFile(explicit, "explicit_bin")
	}
	if command == "" {
		return Resolution{}, errors.New("empty command")
	}
	if strings.ContainsRune(command, '/') {
		return explicitFile(command, "explicit_path")
	}
	// LookPath's ErrDot contract: https://pkg.go.dev/os/exec#LookPath.
	path, err := exec.LookPath(command)
	if err == nil {
		// GODEBUG=execerrdot=0 must not weaken this application-level check.
		if !filepath.IsAbs(path) {
			return Resolution{}, fmt.Errorf("resolve %q: %w", command, exec.ErrDot)
		}
		return explicitFile(path, "path")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return Resolution{}, err
	}
	for _, pattern := range r.CandidateDirs {
		dirs, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return Resolution{}, fmt.Errorf("candidate directory: %w", globErr)
		}
		for _, dir := range dirs {
			if !filepath.IsAbs(dir) {
				return Resolution{}, fmt.Errorf("relative candidate directory %q is not allowed", dir)
			}
			if found, fileErr := explicitFile(dir+string(os.PathSeparator)+command, "fallback"); fileErr == nil {
				return found, nil
			}
		}
	}
	return Resolution{}, err
}

func explicitFile(path, source string) (Resolution, error) {
	// Do not clean: link/../file can name a different file from its lexical
	// simplification. Preserve the path the caller actually supplied.
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return Resolution{}, err
		}
		path = cwd + string(os.PathSeparator) + path
	}
	info, err := os.Stat(path)
	if err != nil {
		return Resolution{}, err
	}
	if !info.Mode().IsRegular() {
		return Resolution{}, fmt.Errorf("%q is not a regular executable file", path)
	}
	// An absolute path makes LookPath check this literal file without searching
	// PATH; its access check respects the caller's executable permissions.
	// https://pkg.go.dev/os/exec#LookPath
	checked, err := exec.LookPath(path)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Path: checked, Source: source}, nil
}

type Check struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type LinuxReport struct {
	Kernel       Check `json:"kernel"`
	RingBuffer   Check `json:"ring_buffer"`
	BPFLSMKernel Check `json:"bpf_lsm_kernel"`
	BTF          Check `json:"btf"`
	BPFLSM       Check `json:"bpf_lsm"`
}

type CLIReport struct {
	Name            string `json:"name"`
	Found           bool   `json:"found"`
	Path            string `json:"path"`
	Source          string `json:"source,omitempty"`
	Version         string `json:"version"`
	VersionReason   string `json:"version_reason"`
	AvailableInPath bool   `json:"available_in_path"`
	Reason          string `json:"reason,omitempty"`
	Recommendation  string `json:"recommendation,omitempty"`
}

type Report struct {
	OS              string           `json:"os"`
	Notice          string           `json:"notice"`
	Platform        string           `json:"platform"`
	Linux           *LinuxReport     `json:"linux,omitempty"`
	CLIs            []CLIReport      `json:"clis"`
	ToolchainProbes []ToolchainProbe `json:"toolchain_probes,omitempty"`
}

// Diagnose only reads static system files and executable metadata. It never execs.
func Diagnose(resolver Resolver) Report {
	return diagnose(runtime.GOOS, resolver, os.ReadFile, readable)
}

func diagnose(goos string, resolver Resolver, read func(string) ([]byte, error), probe func(string) error) Report {
	report := Report{OS: goos, Notice: DoctorNotice, CLIs: make([]CLIReport, 0, len(KnownCLIs))}
	if goos == "linux" {
		report.Platform = "Linux static environment checks; sensor and BPF operation are not tested"
		linux := diagnoseLinux(read, probe)
		report.Linux = &linux
	} else {
		report.Platform = "Linux observer and enforcement require Linux; this host provides no protected run"
	}
	for _, name := range KnownCLIs {
		cli := CLIReport{Name: name, Version: "unknown", VersionReason: "not_executed_read_only"}
		found, err := resolver.Resolve(name, "")
		if err != nil {
			cli.Reason = errorStatus(err) + ": " + err.Error()
			if errors.Is(err, exec.ErrDot) {
				cli.Reason = "unsafe_relative_path: " + err.Error()
			}
		} else {
			cli.Found, cli.Path, cli.Source = true, found.Path, found.Source
			cli.AvailableInPath = found.Source == "path"
			if !cli.AvailableInPath {
				cli.Recommendation = "Add the displayed path's directory to PATH, or pass the displayed path as the literal --bin argument to agenttaint run. Quote it for your shell; no shell command is generated."
			}
		}
		report.CLIs = append(report.CLIs, cli)
	}
	return report
}

func readable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var b [1]byte
	_, err = io.ReadFull(f, b[:])
	return err
}

func errorStatus(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		return "missing"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	default:
		return "unknown"
	}
}

var kernelVersion = regexp.MustCompile(`^([0-9]+)\.([0-9]+)(?:\.[0-9]+)?(?:[-+][A-Za-z0-9._+-]+)?$`)
var lsmName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func diagnoseLinux(read func(string) ([]byte, error), probe func(string) error) LinuxReport {
	unknown := Check{Status: "unknown", Detail: "kernel version unavailable"}
	report := LinuxReport{RingBuffer: unknown, BPFLSMKernel: unknown}
	data, err := read("/proc/sys/kernel/osrelease")
	if err != nil {
		report.Kernel = Check{errorStatus(err), err.Error()}
	} else {
		version := strings.TrimSpace(string(data))
		match := kernelVersion.FindStringSubmatch(version)
		report.Kernel = Check{"unknown", "unrecognized kernel version: " + version}
		if match != nil {
			major, e1 := strconv.Atoi(match[1])
			minor, e2 := strconv.Atoi(match[2])
			if e1 == nil && e2 == nil {
				report.Kernel = Check{"read", version}
				report.RingBuffer = Check{"meets_minimum", "kernel >= 5.8; runtime support not tested"}
				report.BPFLSMKernel = Check{"meets_minimum", "kernel >= 5.13 stability baseline; runtime support not tested"}
				if major < 5 || major == 5 && minor < 8 {
					report.RingBuffer = Check{"unsupported", "kernel < 5.8"}
				}
				if major < 5 || major == 5 && minor < 13 {
					report.BPFLSMKernel = Check{"below_minimum", "kernel < 5.13; BPF-LSM may be unstable or unsupported"}
				}
			}
		}
	}
	if err := probe("/sys/kernel/btf/vmlinux"); err != nil {
		report.BTF = Check{errorStatus(err), err.Error()}
	} else {
		report.BTF = Check{"readable", "BTF file present and readable; contents and BPF loading not validated"}
	}
	data, err = read("/sys/kernel/security/lsm")
	if err != nil {
		report.BPFLSM = Check{errorStatus(err), err.Error()}
	} else {
		report.BPFLSM = Check{"disabled", "bpf absent from readable LSM list"}
		for _, item := range strings.Split(strings.TrimSpace(string(data)), ",") {
			item = strings.TrimSpace(item)
			if !lsmName.MatchString(item) {
				report.BPFLSM = Check{"unknown", "malformed LSM list"}
				break
			}
			if item == "bpf" {
				report.BPFLSM = Check{"enabled", "bpf listed; load/attach not tested"}
			}
		}
	}
	return report
}

// Write emits either a JSON report or a human-readable report.
func (r Report) Write(w io.Writer, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(r)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "OS: %s\n%s\n%s\n", r.OS, r.Platform, r.Notice)
	if r.Linux != nil {
		for _, check := range []struct {
			name  string
			value Check
		}{
			{"kernel", r.Linux.Kernel}, {"ring_buffer", r.Linux.RingBuffer}, {"bpf_lsm_kernel", r.Linux.BPFLSMKernel}, {"btf", r.Linux.BTF}, {"bpf_lsm", r.Linux.BPFLSM},
		} {
			fmt.Fprintf(&out, "%s: %s (%s)\n", check.name, check.value.Status, check.value.Detail)
		}
	}
	for _, cli := range r.CLIs {
		fmt.Fprintf(&out, "%s\n  found: %t\n  path: %s\n  available_in_path: %t\n  version: %s\n  version_reason: %s\n", cli.Name, cli.Found, cli.Path, cli.AvailableInPath, cli.Version, cli.VersionReason)
		if cli.Reason != "" {
			fmt.Fprintf(&out, "  reason: %s\n", cli.Reason)
		}
		if cli.Recommendation != "" {
			fmt.Fprintf(&out, "  recommendation: %s\n", cli.Recommendation)
		}
	}
	for _, probe := range r.ToolchainProbes {
		fmt.Fprintf(&out, "toolchain %s\n  path: %s\n  status: %s\n", probe.Name, probe.Path, probe.Status)
		if probe.Version != "" {
			fmt.Fprintf(&out, "  version: %s\n", probe.Version)
		}
		if probe.Detail != "" {
			fmt.Fprintf(&out, "  detail: %s\n", probe.Detail)
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}
