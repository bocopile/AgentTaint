# Phase 4 — Linux Enforcement

## Goal

Phase 3의 `deny` 판정을 실제 조치로 연결한다. DETECT에서 ENFORCE로
넘어가는 Phase다. 이 Phase가 끝나면 "Linux MVP"가 완성된다.

## Context

Phase 3까지는 판정만 하고 아무것도 막지 않았다. 이제 실제로 막되,
AGENTS.md의 Security Semantics 규칙을 절대 어기지 않는다:

> post-event detection을 prevention이라고 부르지 않는다. 선택한 OS
> 훅이 실제로 pre-operation enforcement를 제공할 때만 "차단(block)"
> 이라고 표현한다.

즉 두 가지 조치를 구분해서 구현하고, 각각을 정확한 이름으로 부른다.

```
민감 파일 open 시도
        │
        ▼
   BPF-LSM (file_open 훅)
        │  ← pre-operation, 커밋 전에 거부 가능
        ▼
   ALLOW / DENY (진짜 "block")


SECRET 프로세스의 socket connect
        │
        ▼
   BPF-LSM (socket_connect 훅) 사용 가능하면
        │  ← pre-operation "block"
        ▼
   불가능하면: connect 이후 감지 → 소켓 종료/SIGKILL
        │  ← post-operation, 이건 "kill"이지 "block"이 아니다
        ▼
```

## In Scope

- `bpf/file.bpf.c`를 tracepoint에서 BPF-LSM(`file_open`)으로 전환 (또는
  병행) — 정책에 지정된 민감 파일에 대해 pre-op으로 거부(`-EPERM`) 가능한
  경로를 구현.
- `bpf/network.bpf.c`에 BPF-LSM(`socket_connect`)이 커널/환경에서
  사용 가능하면 pre-op block을, 사용 불가능하면 post-op kill(연결
  종료 또는 `bpf_send_signal`)로 폴백하는 두 경로를 모두 구현하고
  **실제로 어느 경로가 활성화됐는지 런타임에 보고**한다.
- `internal/enforce`: `internal/policy`의 Decision을 받아 위 두 경로 중
  하나를 실행. 강제 모드는 `monitor`(로그만, 기본값) / `notify` /
  `block`(pre-op 가능할 때만) / `kill` 4단계로 분리.
- `agenttaint doctor`에 BPF-LSM 활성화 여부를 이미 Phase 0에서
  점검하게 되어 있다 — 이 Phase에서 그 결과를 `internal/enforce`가
  실제로 참조해서 block/kill 중 무엇이 가능한지 스스로 판단하게 만든다.

## Out of Scope

- file write/read propagation, pipe propagation (Phase 6)
- 다중 label (Phase 8)
- macOS enforcement (Phase 5/6)
- Kubernetes (Phase 9)
- 인라인 리댁션(요청 내용만 지우고 통과시키는 것) — 범위 밖, 필요해지면
  별도 논의

## Files

`bpf/file.bpf.c`, `bpf/network.bpf.c` (수정), `internal/enforce/enforce.go`,
`internal/env`(doctor)의 BPF-LSM 감지 결과를 `internal/enforce`가
소비하도록 인터페이스 조정.

## Technical Requirements

- 기본 강제 모드는 항상 `monitor`. `block`/`kill`은 명시적으로 켜야
  한다 — 실수로 개발 중 자기 자신의 세션을 죽이는 사고를 방지한다.
- eBPF 절대 규칙(AGENTS.md) 동일 적용.

## Security Requirements

- **모든 로그/출력/문서에서 실제 메커니즘과 다른 강도로 표현하지
  않는다.** BPF-LSM이 없어서 kill로 폴백한 경우, 그 이벤트 로그에
  `"action": "kill"`이라고 정확히 남기고 `"action": "block"`이라고
  쓰지 않는다.
- Final Report의 "what is blocked"에는 pre-op으로 확실히 막히는
  케이스만 나열하고, "what is NOT guaranteed"에는 post-op kill의
  한계(탐지~조치 사이의 짧은 지연 창에서 이미 일부 데이터가 나갔을
  수 있음)를 명시한다.

## Acceptance Criteria

- `monitor` 모드(기본값)에서는 Phase 3과 동일하게 아무것도 막지 않는다.
- `block` 모드에서, BPF-LSM이 활성화된 커널에서 지정 민감 파일 `open()`
  이 `-EPERM`으로 실패한다.
- `kill` 모드에서, `SECRET` 라벨이 붙은 프로세스가 외부로 connect를
  시도하면 해당 프로세스가 종료된다 (Phase 3의 TEST 2/3 시나리오 재사용,
  이번엔 실제로 프로세스가 죽는지 확인).
- BPF-LSM이 없는 환경에서 `block` 모드를 요청하면, block이 안 되고
  kill/notify로만 동작한다는 것을 명확한 에러/경고로 알린다 (조용히
  아무 일도 안 하고 "성공"이라 답하지 않는다).

## Tests

- Phase 3의 4개 시나리오를 `monitor`/`kill`/`block` 각 모드로 재실행하는
  통합 테스트.
- BPF-LSM 없는 환경을 시뮬레이션해 kill로 정확히 폴백하는지 확인하는
  테스트.

## Documentation

- `docs/SECURITY_MODEL.md`나 `docs/THREAT_MODEL.md`의 "막는 것/못 막는
  것" 절과 실제 구현 결과가 다르면 AGENTS.md 절차를 따라 갱신 여부를
  먼저 논의한다.

## Required Final Report

AGENTS.md 형식 그대로. 특히 "what is NOT guaranteed"에 post-op kill의
지연 창 한계를 반드시 포함한다.

## Stop Conditions

- 인라인 리댁션이나 TLS 개입을 구현하고 싶어질 때 — 멈춘다, 범위 밖이다.
- BPF-LSM 유무와 무관하게 항상 "block"이라고 로그를 남기고 싶어질 때 —
  절대 하지 않는다, 멈추고 보고한다 (Security Requirements 위반).
- 이 Phase 완료 후 바로 macOS나 Kubernetes를 시작하고 싶어질 때 —
  멈춘다, Linux MVP 완료를 먼저 사람에게 보고하고 확인받는다.
