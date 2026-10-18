---
name: agenttaint-docs-contract-check
description: Use when reviewing or changing AgentTaint docs or Phase prompts for scope, version, example, status, and normative-reference consistency.
---

# AgentTaint documentation contract check

Review the supplied documentation diff against the repository's normative contract. This check does not silently resolve design conflicts.

## Inputs

- Changed `docs/` and/or `prompts/` files or a focused diff
- Related Phase/card and baseline when known

## Procedure

1. Read `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`, `docs/THREAT_MODEL.md`, and the relevant Phase prompt and plan card.
2. Check phase order and scope, pinned versions, paths and commands, examples and schema references, current status, and links to normative sources.
3. Compare each changed claim with its cited source and with the implementation status. Mark planned, implemented, verified, and `not_tested` evidence separately.
4. Distinguish factual corrections and status updates from changes to architecture or security meaning.

## Report

List each finding with file/section, conflicting or unsupported text, source evidence, impact, and minimal correction. State whether the diff is consistent, has factual fixes, or contains a normative conflict. Include unverified links or behaviors explicitly.

## Stop conditions

- A change alters a normative security guarantee or conflicts with `AGENTS.md`, `ARCHITECTURE.md`, or `SECURITY_MODEL.md`. Explain the conflict and propose the smallest change; do not apply it without user confirmation.
- A claim cannot be traced to a source or actual evidence. Mark it unverified instead of strengthening it.
- The requested edit would expand a Phase or create a path outside its approved scope.
