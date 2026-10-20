package policy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	maxKernelSources    = 64
	maxKernelFileDenies = 64
	maxKernelPath       = 255
	maxKernelDenies     = 256
)

// KernelProjection returns the bounded subset understood by the Linux
// enforcement ABI. Snapshot validation remains authoritative; unsupported
// capacities fail here instead of silently omitting policy entries.
func (s *Snapshot) KernelProjection() (KernelProjection, error) {
	if s == nil {
		return KernelProjection{}, errors.New("nil policy snapshot")
	}
	if len(s.sources) > maxKernelSources {
		return KernelProjection{}, fmt.Errorf("kernel projection supports at most %d sources", maxKernelSources)
	}
	if len(s.denyFiles) == 0 && len(s.sources) == 0 {
		return KernelProjection{}, errors.New("kernel projection has no source or denied file path")
	}
	if len(s.denyFiles) > maxKernelFileDenies {
		return KernelProjection{}, fmt.Errorf("kernel projection supports at most %d denied file paths", maxKernelFileDenies)
	}
	projection := KernelProjection{Sources: make([]string, 0, len(s.sources)), DenyFiles: make([]string, 0, len(s.denyFiles))}
	for _, source := range s.sources {
		if len(source.path) == 0 || len(source.path) > maxKernelPath || strings.IndexByte(source.path, 0) >= 0 {
			return KernelProjection{}, fmt.Errorf("source path %q exceeds kernel path limit or contains NUL", source.path)
		}
		projection.Sources = append(projection.Sources, source.path)
	}
	for _, path := range s.denyFiles {
		if len(path) == 0 || len(path) > maxKernelPath || strings.IndexByte(path, 0) >= 0 {
			return KernelProjection{}, fmt.Errorf("denied file path %q exceeds kernel path limit or contains NUL", path)
		}
		projection.DenyFiles = append(projection.DenyFiles, path)
	}
	for _, candidate := range s.rules {
		if candidate.action != Deny {
			continue
		}
		for _, prefix := range candidate.prefixes {
			if !prefix.IsValid() || !prefix.Addr().Is4() || prefix != prefix.Masked() {
				return KernelProjection{}, fmt.Errorf("rule %q has unsupported kernel CIDR %q", candidate.name, prefix)
			}
			address := prefix.Addr().As4()
			if len(candidate.ports) == 0 {
				if len(projection.Denies) == maxKernelDenies {
					return KernelProjection{}, fmt.Errorf("kernel projection exceeds %d deny matches", maxKernelDenies)
				}
				projection.Denies = append(projection.Denies, KernelRule{Address: address, PrefixLen: uint8(prefix.Bits()), AnyPort: true})
				continue
			}
			ports := make([]uint16, 0, len(candidate.ports))
			for port := range candidate.ports {
				ports = append(ports, port)
			}
			slices.Sort(ports)
			for _, port := range ports {
				if port == 0 {
					return KernelProjection{}, fmt.Errorf("rule %q has invalid kernel port 0", candidate.name)
				}
				if len(projection.Denies) == maxKernelDenies {
					return KernelProjection{}, fmt.Errorf("kernel projection exceeds %d deny matches", maxKernelDenies)
				}
				projection.Denies = append(projection.Denies, KernelRule{Address: address, PrefixLen: uint8(prefix.Bits()), Port: port})
			}
		}
	}
	return projection, nil
}
