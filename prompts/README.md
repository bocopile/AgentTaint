# Phase Prompts

AgentTaint는 하나의 거대한 프롬프트로 구현하지 않는다. 각 Phase는 하나의
검증 가능한 vertical change이고, AI 코딩 에이전트는 **현재 Phase만**
수행한다.

## 사용 방법

새 세션에서 AI 코딩 에이전트에게 이렇게 시작한다.

```
Read:
- AGENTS.md
- docs/ARCHITECTURE.md
- docs/SECURITY_MODEL.md
- prompts/0N-xxx.md

Execute only the current phase (0N). Do not start later phases.
```

## 문서 관계

```
docs/ROADMAP.md      사람이 보는 전체 방향 (무엇을 만들 것인가)
docs/ARCHITECTURE.md 어떤 구조로 만들 것인가
docs/SECURITY_MODEL.md 어떤 보안 의미를 가져야 하는가
AGENTS.md             AI가 항상 지켜야 하는 규칙 (프로젝트 헌법)
prompts/0N-xxx.md     이번 작업에서 정확히 무엇을 변경할 것인가 (implementation contract)
```

`docs/`와 `AGENTS.md`는 규범(normative)이다. 구현이 여기에 없는 걸
필요로 하면, Phase 프롬프트를 조용히 벗어나지 말고 AGENTS.md의 "문서는
규범이다" 절차를 따른다.

## Phase 목록

| Phase | 파일 | 상태 |
|---|---|---|
| 0 | [00-bootstrap.md](./00-bootstrap.md) | 상세 |
| 1 | [01-core-event-model.md](./01-core-event-model.md) | 상세 |
| 2 | [02-linux-observer.md](./02-linux-observer.md) | 상세 |
| 3 | [03-taint-engine.md](./03-taint-engine.md) | 상세 |
| 4 | [04-linux-enforcement.md](./04-linux-enforcement.md) | 상세 |
| 5 | [05-macos-observer.md](./05-macos-observer.md) | 스텁 (Phase 4 완료 후 상세화) |
| 6 | [06-rich-flow.md](./06-rich-flow.md) | 스텁 |
| 7 | [07-kubernetes.md](./07-kubernetes.md) | 스텁 |

Phase 0~4가 "Linux MVP"다. 5 이후는 지금 상세히 쓰지 않는다 — 아직 오지
않은 Phase를 미리 세밀하게 설계하면 그 사이에 나온 결정(Phase 3에서
확정될 Event/Label 구조 등)과 어긋날 수 있다. 해당 Phase 시작 직전에
`docs/ROADMAP.md`의 방향을 기준으로 이 템플릿에 맞춰 상세화한다.

## 모든 상세 Phase 프롬프트가 따르는 템플릿

```
# Phase N — 이름

## Goal
## Context
## Architecture
## In Scope
## Out of Scope
## Files
## Technical Requirements
## Security Requirements
## Acceptance Criteria
## Tests
## Documentation
## Required Final Report   (AGENTS.md의 형식을 그대로 참조)
## Stop Conditions
```

**Out of Scope**가 가장 중요한 섹션이다. AI 코딩 에이전트는 빈 공간을
보면 기능을 채워 넣으려는 경향이 있다 — 다음 Phase에 속한 것을 명시적으로
나열해서 막는다.
