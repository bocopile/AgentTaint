# Vendored skills

These project-local skills are short AgentTaint wrappers around selected
upstream workflows. Codex can discover them under `.agents/skills`; it loads
their full instructions when selected. The full upstream texts are retained as
`UPSTREAM.md` for audit and optional reference, not automatically loaded.

## Provenance

| Skill | Upstream file | Upstream commit | Local adaptation |
|---|---|---|---|
| `source-driven-development` | `addyosmani/agent-skills/skills/source-driven-development/SKILL.md` | `9d0c60d406b454a78ccc0a175b19932047aa4dac` | Pinned Go/eBPF sources; project security-review route |
| `documentation-and-adrs` | `addyosmani/agent-skills/skills/documentation-and-adrs/SKILL.md` | `9d0c60d406b454a78ccc0a175b19932047aa4dac` | Normative-doc stop rule; no default ADR directory |
| `code-review-and-quality` | `addyosmani/agent-skills/skills/code-review-and-quality/SKILL.md` | `9d0c60d406b454a78ccc0a175b19932047aa4dac` | AgentTaint review scope; no unauthorized tests, mutations, edits, or deletion |

Upstream repository license: MIT, copyright Addy Osmani (2025), retained in
[`THIRD_PARTY_LICENSES/addyosmani-agent-skills-MIT.txt`](THIRD_PARTY_LICENSES/addyosmani-agent-skills-MIT.txt).
The exact source-file and reference SHA-256 values are recorded in
[`docs/TOOLING_SKILLS.md`](../../docs/TOOLING_SKILLS.md).

## References and boundaries

The code-review upstream text links to shared `security-checklist.md` and
`performance-checklist.md`; both pinned files are retained under
`.agents/references/`. Other upstream skills named by the texts are not
vendored. For AgentTaint security semantics, use the local
`agenttaint-security-semantics-review` skill. The repository's `AGENTS.md` and
normative docs override upstream examples and instructions.

No installer, hook, script, plugin, global setting, or network runtime is added.
