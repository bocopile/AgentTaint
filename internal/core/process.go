package core

// ProcessID identifies a process, not an individual thread. Zero is invalid for
// the observed process, but may represent an unknown parent in ProcessContext.
type ProcessID uint32

// ProcessGroupID is the observed process group. Zero means unknown.
type ProcessGroupID uint32

// SessionID is the POSIX session ID, never an AgentTaint run identifier.
type SessionID uint32

// RunID is an opaque identifier for one AgentTaint run, independent of setsid.
type RunID string

// ProcessKey distinguishes PID reuse and observation scopes. BirthID and ScopeID
// must be stable identities assigned or normalized by the future sensor. BirthID
// identifies a process lifetime; ScopeID identifies the scope in which PID and
// birth identity are meaningful. Neither can fall back to PID alone. Their OS
// representation and acquisition are outside core. Exec retains this key.
// Within a record, equal ScopeID values must refer to the same PID space; a
// producer must not change scope interpretation between the parent and child.
type ProcessKey struct {
	PID     ProcessID `json:"pid"`
	BirthID string    `json:"birth_id"`
	ScopeID string    `json:"scope_id"`
}

// ProcessContext describes the observed process. PPID alone is informational,
// not stable parent identity or proof of run membership. Comm is an observed
// process name, not a command line. No arguments, environment, or labels are kept.
type ProcessContext struct {
	ProcessKey
	PPID ProcessID      `json:"ppid"`
	PGID ProcessGroupID `json:"pgid"`
	Comm string         `json:"comm"`
}
