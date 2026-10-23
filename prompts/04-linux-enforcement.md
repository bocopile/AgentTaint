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

P4-01 승인 계약: `internal/taint`는 성공한 completed `FileOpen`을 기준으로 하는
canonical Go reference/audit model로 유지한다. `internal/policy`는 canonical parser,
validation, snapshot, general evaluator를 소유한다. `internal/enforce`만 검증된
snapshot subset을 bounded kernel ABI로 준비하며, BPF는 동기 label-state projection과
지원 sink decision만 수행한다. Source `file_open` 시도는 집행 state를 보수적으로
taint할 수 있고 이후 open 실패 뒤에도 상태가 남을 수 있다. 이는 audit 성공 의미를
변경하지 않는다. 이 projection 자체는 kernel load/attach나 실제 blocking의 증거가
아니다.

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
   block을 제공할 수 없으면 target 시작 전에 실패
   │  kill은 사용자가 해당 mode를 명시한 경우에만 별도 실행
        ▼
```

## In Scope

- `bpf/file.bpf.c`를 tracepoint에서 BPF-LSM(`file_open`)으로 전환 (또는
  병행) — 정책에 지정된 민감 파일에 대해 pre-op으로 거부(`-EPERM`) 가능한
  경로를 구현.
- `bpf/network.bpf.c`에 지원되는 BPF-LSM(`socket_connect`) pre-op block을
  구현한다. 요청한 `block` capability를 제공할 수 없으면 target을 시작하기
  전에 실패한다. post-op kill은 사용자가 `kill` mode를 명시한 경우에만
  별도 경로로 실행하며 block의 자동 폴백으로 사용하지 않는다. 요청 mode와
  실제 action을 런타임에 구분해 보고한다.
- `internal/enforce`: `internal/policy`의 Decision을 받아 위 두 경로 중
  하나를 실행. 강제 모드는 `monitor`(로그만, 기본값) / `notify` /
  `block`(pre-op 가능할 때만) / `kill` 4단계로 분리.
- Go가 policy snapshot 전체를 검증하고, 지원 가능한 제한 subset만 kernel에
  전달한다. 미지원 rule·source matcher·주소 family·port 표현은 묵묵히 생략하지
  않고 `block` target 시작 전에 거부한다. C에는 YAML, regex 또는 일반 rule engine을
  넣지 않는다.
- source `file_open` hook은 source access attempt에서 kernel state를 동기 갱신한다.
  기존 Go audit model은 syscall 완료 결과에 따라 성공 open만 taint한다. 이 차이는
  감사 출력과 UI에 섞지 않고 제한으로 표시한다.
- process lifecycle key는 PID 단독으로 사용하지 않는다. Run scope와 birth identity를
  포함하고, fork 전파·동시 hook ordering·map-full/update error는 bounded 상태로
  설계해 acceptance test로 확인한다. state update 또는 sink lookup이 불확실할 때
  clean/allow로 처리하지 않는다. 검증된 동기 처리를 제공할 수 없으면 해당 block
  profile은 target 시작 전에 실패한다.
- 초기 block profile은 observer의 singleton `process_scope` 대신 전용 cgroup v2를
  run membership 경계로 사용한다. target과 descendants를 해당 scope에 두고, target이
  cgroup control files를 수정하지 못하게 한다. attach 전 scope ID와 preallocated sticky
  fault state를 준비하고 cgroup 배치와 hook attach 이후에만 launch permit을 보낸다.
  cgroup 권한/격리 또는 required helper를 확인할 수 없으면 block target을 시작하지
  않는다. supervisor 비정상 종료 시 살아남은 descendants와 link 생애를 처리하는
  보장은 현재 없으므로, 이 장애를 해결하거나 명시적 보장 범위에서 제외한다.
- 환경 진단의 BPF-LSM 정보는 capability 판단 입력으로 사용하되, 정적 진단
  결과만으로 실제 hook attach나 집행 가능성을 증명하지 않는다. `block`의
  준비 단계에서 요청된 enforcement surface에 필요한 훅을 각각 실제로
  load/attach할 수 있는지 확인하고, 하나라도 불충족이면 target 시작 전에
  실패한다. 일반적인 BPF-LSM 활성 표시는 이 증거를 대신하지 않는다.
  `internal/enforce`가 요청 mode를 다른 action으로 자동 변경하지 않는다.

## Out of Scope

- file write/read propagation, pipe propagation (Phase 6)
- 다중 label (Phase 6)
- macOS 네이티브 지원 (Phase 5; 별도 결정 전까지 Linux VM 방향 유지)
- Kubernetes (Phase 7)
- 인라인 리댁션(요청 내용만 지우고 통과시키는 것) — 범위 밖, 필요해지면
  별도 논의

## Files

`bpf/file.bpf.c`, `bpf/network.bpf.c`, `bpf/enforce.bpf.c` (수정),
`internal/policy/kernel.go`, `internal/enforce/enforce_linux.go`,
`internal/enforce/enforce_unsupported.go`, `internal/enforce/enforce_bpfel.go`와
그 embed object, `internal/env`(doctor)의 BPF-LSM 감지 결과를 `internal/enforce`가
소비하도록 인터페이스 조정. 현재 생성 파일 경로는 저장소의 실제 패키지 구성에 맞춘다.

## Technical Requirements

- 기본 강제 모드는 항상 `monitor`. `block`/`kill`은 명시적으로 켜야
  한다 — 실수로 개발 중 자기 자신의 세션을 죽이는 사고를 방지한다.
- eBPF 절대 규칙(AGENTS.md) 동일 적용.

## Security Requirements

- **모든 로그/출력/문서에서 실제 메커니즘과 다른 강도로 표현하지
  않는다.** 사용자가 `kill` mode를 명시했을 때만 post-op kill을 실행하며,
  그 결과를 `kill`로 기록한다. 이를 `block`으로 표현하지 않는다.
- Final Report의 "what is blocked"에는 pre-op으로 확실히 막히는
  케이스만 나열하고, "what is NOT guaranteed"에는 post-op kill의
  한계(탐지~조치 사이의 짧은 지연 창에서 이미 일부 데이터가 나갔을
  수 있음)를 명시한다.

## Acceptance Criteria

- `monitor` 모드(기본값)에서는 Phase 3과 동일하게 아무것도 막지 않는다.
- `block` 모드에서, 요청된 file/network 집행에 필요한 각 BPF-LSM 훅이 실제
  load/attach된 환경에서 지정 민감 파일 `open()`과 지원되는 새 IPv4
  `connect()`가 `-EPERM`으로 실패하고 통제된 수신 측에 연결이 도달하지 않는다.
- 허용된 source `file_open` attempt는 kernel enforcement state를 보수적으로
  taint할 수 있다. 그 뒤 open이 실패하더라도 enforcement label이 남을 수 있다.
  Go audit reference는 failed event에 성공 label을 부여하지 않으며, 두 결과를
  differential scenario에서 구분한다.
- `kill` 모드에서, `SECRET` 라벨이 붙은 프로세스가 외부로 connect를
  시도하면 해당 프로세스가 종료된다 (Phase 3의 TEST 2/3 시나리오 재사용,
  이번엔 실제로 프로세스가 죽는지 확인).
- 요청한 `block` capability를 제공할 수 없는 환경에서는 target 시작 전에
  실패하고 어떤 `kill`/`notify` mode로도 자동 변경하지 않는다.
- `kill` mode는 사용자가 명시적으로 요청한 경우에만 동작하며, 이미 발생한
  effect가 되돌려지지 않을 수 있음을 알린다.

## Tests

- Phase 3의 4개 시나리오를 `monitor`/`notify`/`kill`/`block` 각 모드로 재실행하는
  통합 테스트.
- BPF-LSM capability가 없는 환경에서 `block` 요청이 target 시작 전에
  실패하고 자동 kill/notify가 없음을 확인하는 테스트.
- 필수 집행 훅 중 하나라도 load/attach되지 않는 환경에서 `block` 요청이
  target 시작 전에 실패하는 테스트.
- 사용자가 명시한 `kill` mode의 action/result를 `block`과 구분하는 테스트.

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
