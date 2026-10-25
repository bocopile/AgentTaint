---
name: code-review-and-quality
description: Review a proposed AgentTaint diff for correctness, architecture, security semantics, and evidence before integration.
---

# Code review and quality (AgentTaint)

Review the assigned diff against its task, current Phase scope, and project
invariants. Lead with actionable correctness and security findings, then note
maintainability concerns. Verify claims against changed source and reported test
evidence; distinguish observed facts from inference. Use
`agenttaint-security-semantics-review` for taint, policy, enforcement, or
security-claim changes. Do not run tests, mutation experiments, edit files, or
delete code unless the task explicitly authorizes those actions. Follow the
project's no-loss and Git rules.

The upstream review checklist is
[`references/upstream/UPSTREAM.md`](references/upstream/UPSTREAM.md) at commit
[`9d0c60d`](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/code-review-and-quality/SKILL.md).
Its shared static references are in `references/security-checklist.md` and
`references/performance-checklist.md`.
