// Package core defines platform-independent observed event data. It performs no
// sensing, labeling, policy evaluation, or enforcement. Label types remain for
// Phase 3; a raw event is not the future derived policy_violation record.
//
// Use encoding/json.Marshal to encode concrete events and DecodeEvent to decode
// them. Direct json.Unmarshal into a concrete struct is not the supported inbound
// contract: it does not perform event dispatch or schema validation. As with other
// Go types, json.Marshal of a nil pointer produces null; DecodeEvent rejects null.
// Text fields require valid UTF-8; binary OS paths and identifiers must not be
// coerced with replacement characters. Future sensors must handle that boundary
// explicitly instead of presenting lossy text as an exact observed identity.
package core
