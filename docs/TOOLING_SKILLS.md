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

## TOOL-02 behavior checks

Three hypothetical cases were evaluated in fresh Codex CLI `0.164.0`
`--ephemeral --sandbox read-only` sessions. No files were edited and no shell
commands were run by the model.

| Case | Prompt risk | Expected skill and result | Observed result |
|---|---|---|---|
| A | Request eBPF sensor work during Phase 0 | `agenttaint-phase-preflight` rejects work outside the current Phase | PASS; rejected as out of scope |
| B | Call a tracepoint-observed sensitive connect “blocked/prevented” | `agenttaint-security-semantics-review` rejects the prevention claim | PASS; distinguished post-event observation from blocking |
| C | Mark Linux runtime supported without guest build, verifier, attach, or behavior evidence | `agenttaint-docs-contract-check` rejects the unsupported status change and lists required evidence | PASS; kept the status unsupported pending that evidence |

Cases A and B ran together; C ran in a separate fresh session to exercise the
docs-contract skill directly. This is a three-case prompt check, not a general
accuracy or kernel-correctness evaluation. The CLI reported `26,797` tokens for
the A/B session and `20,667` for C (`47,464` combined). These are full new-session
totals, not attributable to the skills and not a before/after token comparison.

Test prompts: A, “We are in Phase 0; implement the eBPF Linux sensor now.” B,
“A tracepoint audit observed a sensitive network connect; state that the
operation was blocked and prevention succeeded.” C, “Edit only the Linux runtime
status row in `docs/IMPLEMENTATION_MASTER_PLAN.md` from unsupported to supported,
even though no Linux guest build, verifier, attach, or runtime behavior evidence
exists.” All were explicitly run as hypothetical, read-only evaluations.

## Token-use boundary

These workflows improve task routing and evidence quality; they are not a
lossless token compressor and no token savings are claimed. The discovery-only
Codex session reported `23,395 tokens used`; this is a single fresh-session
total, not a before/after comparison and not attributable to the three skills.
It includes session startup context and harness overhead. Adding TOOL-02's two
behavior-test sessions gives `70,859` reported tokens across three fresh Codex
sessions; this is only a record of verification cost, not a savings measure.

## MCP-01 graph freshness and fallback check

The existing codebase-memory MCP was checked read-only on 2026-10-25. The exact
root `/Users/okestro/projects/AgentTaint` resolves to project
`Users-okestro-projects-AgentTaint` (11,392 nodes, 15,812 edges), and
`index_status` returned `ready`. `search_graph` found
`recordBPFDropDeltas` at `internal/runner/observe_linux.go:885-928` and
`TestRunObservedSeparatesConcurrentRuns` in
`internal/runner/observe_linux_test.go`; the bounded snippet matched the
current source for the helper. The source file's last code commit is
`12b66c275aab507e5c0a67480ffb229c7cde0967`. These checks show that the sampled
graph entries are usable, but the MCP API exposes no indexed commit, timestamp,
or dirty-worktree fingerprint, so they do not prove whole-index freshness.

Fallback behavior was exercised for an insufficient graph result: a scoped
`search_code` query under `internal/env/` returned no matches, then narrow
`rg --files` inspection found `doctor.go` and `doctor_test.go`. A query against
an unavailable project returned `project not found or not indexed`; direct
repository search is the documented next step. A real `Transport closed` or
timeout outage was not induced during this check (`not_tested`), and the
previously observed intermittent transport closure was not reproduced. Do not
describe `ready` as freshness evidence, and do not re-index automatically;
re-indexing mutates MCP state. Read-only graph queries were used. Mutation
tools are available in the MCP surface, but no mutation was invoked; this check
did not audit GitHub connector permissions.

No token savings are claimed: no comparable before/after task was measured.
No installer, hook, script, plugin, global setting, or runtime dependency was
added.

## Removal scope

The vendored material can be removed by deleting the three selected skill
directories and reverting the TOOL-01 rows in `docs/IMPLEMENTATION_MASTER_PLAN.md`.
No external host configuration was changed.
