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

규범의 적용 범위는 다음과 같이 구분한다. `docs/` 아래 모든 파일이 자동으로
규범이 되는 것은 아니다.

- `AGENTS.md`: 모든 작업의 공통 규칙이며 충돌 시 우선한다.
- `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`: 구조·보안 모델의
  source of truth다. 변경 시 AGENTS.md의 "문서는 규범이다" 절차를 따른다.
- `docs/THREAT_MODEL.md`: 보호 가정·공격자·한계의 계약이다.
- `docs/ROADMAP.md`: Phase 순서와 범위의 기준이다.
- 현재 `prompts/NN-*.md`: 해당 Phase의 구현·검증 계약이며 위 규범을
  조용히 덮어쓰지 않는다.
- `docs/RELATED_WORK.md`, `docs/SUBORCHESTRATOR.md`: 조사·선택적 도구
  안내이며 보안 모델이나 구현 권한을 변경하지 않는다.
- `docs/IMPLEMENTATION_MASTER_PLAN.md`: 채택 기본안·작업 지도·검증 계획이다.
  기존 규범의 자동 대체물이 아니며 관련 수정 사항은 해당 Phase 착수 전에
  규범에 명시적으로 반영한다.

구현에 필요한 내용이 규범·현재 Phase 범위와 충돌하면 이를 식별하고
AGENTS.md의 "문서는 규범이다" 절차를 따른다.

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
| 8 | (없음) | 미작성 — [docs/ROADMAP.md](../docs/ROADMAP.md)의 Agent Feedback Loop 참고 |
| 9 | [09-local-web-dashboard.md](./09-local-web-dashboard.md) | 스텁 |
| 10 | [10-payload-aware-llm-sink.md](./10-payload-aware-llm-sink.md) | 스텁 (조건부 — 필요성 확인 전까지 미착수) |

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
