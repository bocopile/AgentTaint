---
name: documentation-and-adrs
description: Record significant AgentTaint design decisions and update docs when contracts or user-visible behavior change, following existing project conventions.
---

# Documentation and decisions (AgentTaint)

Use for significant design choices, contract changes, and shipped behavior.
First check `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`,
`docs/THREAT_MODEL.md`, and the relevant roadmap or Phase prompt. Follow existing
document locations and formats; do not create an ADR directory by default.
Separate implemented, verified, and unverified claims. If implementation
conflicts with normative docs, stop and report the smallest proposed change.

The upstream template and examples are available in [`UPSTREAM.md`](UPSTREAM.md)
at commit
[`9d0c60d`](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/documentation-and-adrs/SKILL.md).
