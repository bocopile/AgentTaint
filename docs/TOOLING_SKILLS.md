# AgentTaint project skills

This note records the TOOL-01 adoption of three workflows from
[`addyosmani/agent-skills`](https://github.com/addyosmani/agent-skills) at
commit `9d0c60d406b454a78ccc0a175b19932047aa4dac`.

## Vendored files

| File | SHA-256 | Use |
|---|---|---|
| `.agents/skills/source-driven-development/references/upstream/UPSTREAM.md` | `c59faf851377f0eeda45306398d16eb96d77ab1c2fd3e1a7fb58e9a0af33c1e9` | Version-aware use of primary docs and source |
| `.agents/skills/documentation-and-adrs/references/upstream/UPSTREAM.md` | `359b22460045583b0233ec3f80223459c4096b0fa513f5c29ade9c3985207d65` | Recording decisions and documentation |
| `.agents/skills/code-review-and-quality/references/upstream/UPSTREAM.md` | `2db1e850379f255fd3091ef278cde24cd6c7d2ba23e360cc52ea522c40d4396e` | Multi-axis review checklist |
| `.agents/skills/code-review-and-quality/references/security-checklist.md` | `37774056502fe8970dcd2fd27dca5fa31f2a55232649fc3056c89c3af7d62755` | Upstream review reference |
| `.agents/skills/code-review-and-quality/references/performance-checklist.md` | `40f564d1e62341e277c01ba42c42d95264b9ef3b8e5a23249dc6e121a7e70067` | Upstream review reference |
| `.agents/skills/code-review-and-quality/references/upstream/LICENSE.txt` | `6f202f8bd568cd730dbb2b0d1f8e243bc74c2fa1f64dbce9b2c7ea08bd5c9fd7` | MIT notice |

The upstream repository is MIT licensed; its exact license text is retained at
`.agents/skills/code-review-and-quality/references/upstream/LICENSE.txt`. These
hashes identify the unmodified upstream files. The discoverable local
`SKILL.md` files are short project-specific wrappers and are intentionally not
byte-identical to upstream.

## Adaptation and activation

Codex project skills belong under `.agents/skills/<name>/SKILL.md`. The local
wrappers keep task triggers and project guardrails concise; each points to its
full pinned upstream text under its static `references/upstream/`. The three
upstream bodies total 902 lines, while the active wrappers are 56 lines
combined. This is a file-line comparison, not a measured token or usage
reduction. OpenAI describes skills as showing metadata first and loading full
instructions when selected ([Codex skill workflow](https://developers.openai.com/blog/skills-agents-sdk)).
Keeping full checklists outside the active wrapper follows that progressive-
disclosure pattern.

The code-review wrapper routes AgentTaint security-semantic checks to
`agenttaint-security-semantics-review`, keeps the upstream security and
performance checklists available as references, and blocks unauthorized test
mutation or file deletion. The upstream `security-and-hardening` and
`performance-optimization` skills are not installed. The project’s `AGENTS.md`,
normative docs, authorization, no-loss requirements, and task scope override all
upstream instructions.

The repository path and `SKILL.md` frontmatter were checked against OpenAI's
documented project-skill layout and progressive-disclosure behavior. A fresh
Codex CLI `0.164.0` session with `--ephemeral --sandbox read-only` and
this worktree as its root listed all three skill names and exact `.agents/skills`
paths. The probe did not read full skill bodies or run repository commands.

## Token-use boundary

These workflows improve task routing and evidence quality; they are not a
lossless token compressor and no token savings are claimed. The discovery-only
Codex session reported `23,395 tokens used`; this is a single fresh-session
total, not a before/after comparison and not attributable to the three skills.
It includes session startup context and harness overhead. TOOL-02/03 and MCP-01
remain responsible for measuring context costs, checking the existing
codebase-memory MCP's freshness and outage fallback, and deciding whether
another OSS tool is justified. No installer, hook, script, plugin, global
setting, or runtime dependency was added.

## Removal scope

The vendored material can be removed by deleting the three selected skill
directories and reverting the TOOL-01 rows in `docs/IMPLEMENTATION_MASTER_PLAN.md`.
No external host configuration was changed.
