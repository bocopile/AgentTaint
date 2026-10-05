package core

import "net/netip"

// FileResource keeps the observed path verbatim. It does not resolve symlinks,
// clean paths, access the filesystem, or establish a stable file identity.
type FileResource struct {
	Path string `json:"path"`
}

// FileAccess is normalized intent, not a platform-specific flag or proof that
// bytes were read or written. Unknown must be represented explicitly.
type FileAccess string

const (
	AccessUnknown   FileAccess = "unknown"
	AccessRead      FileAccess = "read"
	AccessWrite     FileAccess = "write"
	AccessReadWrite FileAccess = "read_write"
)

// NetworkDestination represents an IPv4 destination and its numeric port. Port
// zero is representable because an attempted operation need not be valid. IPv6
// and IPv4-mapped IPv6 are outside the current model.
type NetworkDestination struct {
	Address netip.Addr `json:"address"`
	Port    uint16     `json:"port"`
}
