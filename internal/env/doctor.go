// Package env provides read-only environment diagnostics and executable lookup.
package env

import (
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
)

// KnownCLIs is only a diagnostic convenience; runner accepts arbitrary commands.
var KnownCLIs = []string{"claude", "codex"}

const Notice = "Phase 0: no monitoring or blocking. Static checks do not verify BPF operation or protection readiness."

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
	OS       string       `json:"os"`
	Notice   string       `json:"notice"`
	Platform string       `json:"platform"`
	Linux    *LinuxReport `json:"linux,omitempty"`
	CLIs     []CLIReport  `json:"clis"`
}

// Diagnose only reads static system files and executable metadata. It never execs.
func Diagnose(resolver Resolver) Report {
	return diagnose(runtime.GOOS, resolver, os.ReadFile, readable)
}

func diagnose(goos string, resolver Resolver, read func(string) ([]byte, error), probe func(string) error) Report {
	report := Report{OS: goos, Notice: Notice, CLIs: make([]CLIReport, 0, len(KnownCLIs))}
	if goos == "linux" {
		report.Platform = "Linux static diagnostics only; sensor not implemented in Phase 0"
		linux := diagnoseLinux(read, probe)
		report.Linux = &linux
	} else {
		report.Platform = "v0.1 Linux sensor unsupported on this host; run in a Linux VM (Phase 0 still has no protection)"
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
	_, err := io.WriteString(w, out.String())
	return err
}
