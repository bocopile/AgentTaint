//go:build linux && (386 || amd64 || arm || arm64 || loong64 || mips64le || mipsle || ppc64le || riscv64)

package enforce

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/bocopile/AgentTaint/internal/policy"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

const (
	maxSources   = 64
	maxDenyRules = 256
	pathSize     = 256
)

type enforceSourceValue struct {
	Path [pathSize]byte
}

func sourcePathHash(path string) uint64 {
	hash := uint64(14695981039346656037)
	for i := 0; i < len(path); i++ {
		hash ^= uint64(path[i])
		hash *= 1099511628211
	}
	return hash
}

// Enforcer owns one run's BPF maps, programs, and links. The runner must keep
// it alive until the target cgroup is empty.
type Enforcer struct {
	objects enforceObjects
	links   []link.Link
}

// New validates and loads the bounded projection before attaching any hook.
// cgroupID is the ID of the dedicated cgroup v2 run scope.
func New(projection policy.KernelProjection, cgroupID uint64) (*Enforcer, error) {
	if cgroupID == 0 {
		return nil, errors.New("enforcement cgroup ID must be nonzero")
	}
	if len(projection.Sources) == 0 || len(projection.Sources) > maxSources {
		return nil, fmt.Errorf("enforcement supports 1 to %d source paths", maxSources)
	}
	if len(projection.Denies) > maxDenyRules {
		return nil, fmt.Errorf("enforcement supports at most %d IPv4 deny matches", maxDenyRules)
	}
	for _, source := range projection.Sources {
		if len(source) == 0 || len(source) >= pathSize || containsNUL(source) {
			return nil, fmt.Errorf("source path %q does not fit the enforcement ABI", source)
		}
	}
	for _, deny := range projection.Denies {
		if deny.PrefixLen > 32 || (!deny.AnyPort && deny.Port == 0) {
			return nil, errors.New("policy projection contains an invalid IPv4 deny match")
		}
	}

	instance := &Enforcer{}
	if err := loadEnforceObjects(&instance.objects, nil); err != nil {
		return nil, fmt.Errorf("load enforcement BPF object: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = instance.Close()
		}
	}()
	zero := uint32(0)
	config := enforceEnforceConfig{CgroupId: cgroupID, DenyCount: uint32(len(projection.Denies))}
	if err := instance.objects.EnforceConfigMap.Update(&zero, &config, ebpf.UpdateAny); err != nil {
		return nil, fmt.Errorf("initialize enforcement configuration: %w", err)
	}
	var fault uint64
	if err := instance.objects.EnforceFaultMap.Update(&zero, &fault, ebpf.UpdateAny); err != nil {
		return nil, fmt.Errorf("initialize enforcement fault state: %w", err)
	}
	for _, source := range projection.Sources {
		key := sourcePathHash(source)
		var value enforceSourceValue
		copy(value.Path[:], source)
		if err := instance.objects.EnforceSources.Update(&key, &value, ebpf.UpdateNoExist); err != nil {
			return nil, fmt.Errorf("load enforcement source %q: %w", source, err)
		}
	}
	for index, match := range projection.Denies {
		key := uint32(index)
		rule := enforceEnforceDenyRule{
			Address:   binary.BigEndian.Uint32(match.Address[:]),
			Port:      match.Port,
			PrefixLen: match.PrefixLen,
		}
		if match.AnyPort {
			rule.AnyPort = 1
		}
		if err := instance.objects.EnforceDenies.Update(&key, &rule, ebpf.UpdateAny); err != nil {
			return nil, fmt.Errorf("load enforcement deny match %d: %w", index, err)
		}
	}
	for _, target := range []struct {
		name    string
		program *ebpf.Program
	}{
		{name: "sched_process_fork", program: instance.objects.EnforceProcessFork},
		{name: "sched_process_exec", program: instance.objects.EnforceProcessExec},
		{name: "sched_process_exit", program: instance.objects.EnforceProcessExit},
	} {
		attached, err := link.AttachTracing(link.TracingOptions{Program: target.program})
		if err != nil {
			return nil, fmt.Errorf("attach enforcement tracepoint %s: %w", target.name, err)
		}
		instance.links = append(instance.links, attached)
	}
	for _, target := range []struct {
		name    string
		program *ebpf.Program
	}{
		{name: "file_open", program: instance.objects.EnforceFileOpen},
		{name: "socket_connect", program: instance.objects.EnforceSocketConnect},
	} {
		attached, err := link.AttachLSM(link.LSMOptions{Program: target.program})
		if err != nil {
			return nil, fmt.Errorf("attach enforcement LSM hook %s: %w", target.name, err)
		}
		instance.links = append(instance.links, attached)
	}
	cleanup = false
	return instance, nil
}

func (e *Enforcer) Close() error {
	if e == nil {
		return nil
	}
	var errs []error
	for i := len(e.links) - 1; i >= 0; i-- {
		if err := e.links[i].Close(); err != nil {
			errs = append(errs, fmt.Errorf("detach enforcement link: %w", err))
		}
	}
	if err := e.objects.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close enforcement BPF object: %w", err))
	}
	return errors.Join(errs...)
}

func containsNUL(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return true
		}
	}
	return false
}
