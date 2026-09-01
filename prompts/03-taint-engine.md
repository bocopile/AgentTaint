# Phase 3 — Taint Engine (AgentTaint MVP)

## Goal

Phase 2가 만드는 이벤트 스트림 위에서 실제로 Source → Label →
Propagation → Sink → Decision을 구현한다. 이 Phase가 끝나면 AgentTaint가
처음으로 "이름값을 하는" 상태가 된다. 아직 아무것도 차단하지 않는다 —
탐지(audit)까지만.

## Context

`docs/SECURITY_MODEL.md`의 핵심 모델을 그대로 구현한다.

```
Sensitive File (SOURCE, 정책에 지정)
       │ open/read   (Phase 2 이벤트)
       ▼
Process Label 부여        ── internal/taint
       │ fork/exec       (Phase 2 이벤트)
       ▼
Child Process로 Label 전파 ── internal/taint
       │ connect         (Phase 2 이벤트)
       ▼
외부 네트워크 연결 (SINK)
       │
       ▼
internal/policy 평가 → Violation 이벤트 (DECISION: audit)
```

## In Scope

- `internal/taint`: 프로세스별 label 상태 저장(메모리 내 map: pid →
  label set), `FileOpen` 이벤트로 label 부여, `ProcessFork`/`ProcessExec`
  이벤트로 부모→자식 label 전파, 프로세스 종료 시 정리.
- `internal/policy`: `docs/SECURITY_MODEL.md`의 Policy Model(YAML)을
  파싱하고, `NetworkConnect` 이벤트 + 현재 프로세스의 label을 보고
  `allow`/`audit`/`deny` 중 판정 (이 Phase에서 `deny`는 판정만 하고
  실제 연결을 막지는 않는다 — Phase 4가 실제 enforcement).
- `docs/SECURITY_MODEL.md`의 Event Schema대로 violation 이벤트를 JSON
  으로 출력.
- `policies/example.yaml` 작성 (실제 사용 예시).

## Out of Scope

- 실제 네트워크 차단, 프로세스 종료 (Phase 4)
- LSM 기반 pre-op enforcement (Phase 4)
- file write/read propagation, pipe propagation (Phase 6/7)
- 다중 label (`CREDENTIAL`, `SOURCE_CODE` 등 — Phase 8)
- macOS, Kubernetes

## Files

`internal/taint/engine.go`, `internal/policy/model.go`,
`internal/policy/parser.go`, `internal/policy/evaluator.go`,
`policies/example.yaml`, `examples/normal/`, `examples/sensitive-read/`,
`examples/subprocess-exfil/` (Acceptance Criteria에서 쓰는 재현 스크립트).

## Technical Requirements

- `internal/taint`와 `internal/policy`는 `internal/sensor/*`를 import
  하지 않는다 — `internal/core.Event`만 소비한다 (AGENTS.md 규칙 2, 3, 4).
- label 저장은 이 Phase에서는 단일 프로세스 내 메모리로 충분하다
  (분산/영속화는 범위 밖).

## Security Requirements

- taint label은 "이 프로세스가 민감 데이터에 접근했다"를 의미하며,
  "이 프로세스가 내보내는 모든 바이트에 민감 데이터가 있다"를 의미하지
  않는다 — 이 문장을 코드 주석이나 문서가 아니라 Final Report에
  명시한다 (AGENTS.md Security Semantics).
- `deny` 판정이 나와도 이 Phase는 실제로 막지 않는다. Final Report의
  "what is blocked"는 반드시 "없음(nothing) — 이 Phase는 감지만 한다"로
  적는다.

## Acceptance Criteria — 4개 시나리오 모두 통과해야 완료

**TEST 1 — 정상 흐름**
```
python normal.py → external connect → decision: allow, violation 없음
```

**TEST 2 — 직접 유출**
```
process reads secret.txt → external connect → decision: deny (audit only), violation 발생
```

**TEST 3 — 서브프로세스 유출 (label propagation 검증)**
```
parent reads secret.txt → fork → child connects → decision: deny, violation 발생
```

**TEST 4 — 오탐 없음 검증**
```
parent reads normal.txt → fork → child connects → decision: allow, violation 없음
```

이 4개는 `test/integration/`에 자동화된 테스트로 존재해야 하며, 각각
`examples/`의 스크립트를 사용한다. "대충 구현했다"고 넘어갈 수 없도록
TEST 3(전파)과 TEST 4(오탐 없음)를 특히 엄격하게 검증한다.

## Tests

- `internal/taint`: label 부여/전파/정리 단위 테스트.
- `internal/policy`: YAML 파싱 + 평가 단위 테스트 (allow/audit/deny 각
  케이스).
- 위 4개 통합 테스트.

## Documentation

- `docs/SECURITY_MODEL.md`의 Policy Model/Event Schema와 실제 구현이
  다르면, 조용히 코드에 맞춰 문서를 고치지 않는다. AGENTS.md 절차대로
  차이를 보고하고 승인 후 갱신한다.

## Required Final Report

AGENTS.md 형식 그대로. "what is detected"에 4개 시나리오 결과를 표로
요약한다.

## Stop Conditions

- `deny` 판정에서 실제로 소켓을 닫거나 프로세스를 죽이고 싶어질 때 —
  멈춘다, 그건 Phase 4다.
- label 종류를 여러 개(`CREDENTIAL` 등)로 늘리고 싶어질 때 — 멈춘다,
  `SECRET` 단일 label로 4개 테스트를 통과시키는 것이 이 Phase의 전부다.
