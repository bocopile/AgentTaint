---
name: agenttaint-phase-preflight
description: Use before starting or resuming an AgentTaint Phase or implementation card to verify scope, prerequisites, allowed paths, and available evidence.
---

# AgentTaint phase preflight

Establish whether the requested Phase/card is ready to start. This is a planning check; it does not grant access or authorize changes.

## Inputs

- Phase ID and task/card ID
- Baseline commit or current branch
- The requested outcome and any user-approved paths

## Procedure

1. Read `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`, `docs/THREAT_MODEL.md`, and the relevant `docs/ROADMAP.md` sections.
2. Read the active `prompts/NN-*.md` and the matching implementation-plan card. Confirm the previous Phase and all listed prerequisites from evidence, not labels alone.
3. Inspect branch and worktree changes. Preserve pre-existing edits and identify which files the task may touch.
4. Compare requested files and actions to the architecture and Phase prompt. Treat listed future paths as plans, not blanket permission.
5. Separate available host facts from Linux guest facts and distinguish static checks from load, attach, or behavior evidence.

## Report

Return `ready`, `blocked`, or `needs_decision`; the Phase/card; baseline and existing changes; completed prerequisites with evidence; allowed files/actions; required validation and environment; unresolved decisions; and checks marked `not_tested`.

## Stop conditions

- A previous Phase or required gate is incomplete.
- The request exceeds the Phase's In Scope or allowed paths.
- A normative document conflicts with the requested change. Identify the conflict and smallest proposal, then stop before editing.
- A required environment or permission is unavailable. State what evidence or access is missing; do not infer success.
