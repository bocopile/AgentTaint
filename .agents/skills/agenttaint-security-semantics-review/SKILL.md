---
name: agenttaint-security-semantics-review
description: Use for AgentTaint changes that affect event interpretation, sources, labels, propagation, sinks, policy decisions, enforcement, or security claims.
---

# AgentTaint security semantics review

Compare claimed protection with the actual event, state, hook, and test evidence. Review the requested diff; do not implement fixes.

## Inputs

- Change diff or exact files
- Claimed detection, audit, deny, block, or kill behavior
- Tests and environment evidence actually available

## Procedure

1. Read `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`, `docs/THREAT_MODEL.md`, and the active Phase prompt/card.
2. Map each security claim through `Source → Label → Propagation → Sink → Decision` to the concrete event/state and operating-system mechanism.
3. For each effect, record whether the hook is pre-operation or post-event, what action it can take, what evidence demonstrates that action, and what failures or dropped events leave unknown.
4. Check that process taint is not described as byte-level taint; that tracepoint observation or post-event kill is not called prevention; and that a successful kill is not claimed to undo effects already made.
5. Compare each claim to the exact test evidence. Separate portable fixtures, host inspection, guest runtime, verifier, load, attach, and end-to-end results.

## Report

Provide a table with `claim`, `mechanism/hook and timing`, `evidence`, `supported wording`, and `limitation`. List findings with exact locations and severity. Mark untested paths `not_tested`.

## Stop conditions

- The requested claim exceeds observed behavior or contradicts the security model: report the smallest wording/design correction and do not make the change.
- A required hook, kernel layout, or permission is unknown: do not guess; identify the missing source or runtime evidence.
- A proposed change would add payload scanning, LLM classification, or another security model: stop and identify the conflict with `AGENTS.md`.
