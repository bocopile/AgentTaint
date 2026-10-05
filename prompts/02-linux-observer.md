# Phase 2 — Linux Observer

## Goal

Linux에서 실제 eBPF로 `fork`/`exec`/`exit`, 파일 open, 소켓 connect
이벤트를 수집해서 Phase 1의 `internal/core.Event`로 정규화한다. 감지만
한다 — taint, policy, 차단은 없다.

## Context

Phase 1에서 정의한 그릇에 처음으로 실제 데이터를 채운다. 이 Phase가
성립하는 순간, 이전에 걸림돌이었던 "정적 링크 SSL이라 curl/python부터
시작해야 한다"는 제약이 사라진다 — 여기서 보는 건 SSL 함수가 아니라
범용 syscall/LSM 이벤트(fork/exec/file open/connect)이므로 `claude`,
`codex`, `bash`, `python` 등 어떤 프로세스든 링크 방식과 무관하게 동일하게
관찰된다.

## Architecture

```
bpf/process.bpf.c ──┐
bpf/file.bpf.c    ──┼── BPF_MAP_TYPE_RINGBUF ──► internal/sensor/linux
bpf/network.bpf.c ──┘                                    │
                                                          ▼
                                            internal/core.Event 스트림
```

## In Scope

- `bpf/process.bpf.c`: tracepoint로 fork/exec/exit 캡처.
- `bpf/file.bpf.c`: 파일 open 캡처 (LSM `file_open` 또는 tracepoint —
  이 Phase는 감지만 하므로 tracepoint로 충분, LSM 전환은 Phase 4에서
  block이 필요할 때 재검토).
- `bpf/network.bpf.c`: 소켓 connect 캡처 (IPv4).
- `internal/sensor/linux`: `cilium/ebpf`로 위 프로그램을 로드/attach하고,
  ring buffer에서 읽은 raw 이벤트를 `internal/core.Event`로 변환.
- `agenttaint run`에 `--observe`와 필수 `--audit-file PATH`를 추가해,
  실행한 프로세스 트리의 정규화 이벤트를 별도 파일에 JSON Lines로 기록.
  대상 stdin/stdout/stderr는 보존하고, 감사 이벤트를 stdout/stderr로
  출력하거나 파일 실패 시 그곳으로 fallback하지 않는다.
- `internal/env`에 명시적인 toolchain probe를 추가하는 범위:
  기본 doctor/AI CLI 탐지는 계속 비실행이며, 신뢰를 확인한 절대 경로의
  compiler/LLVM/bpftool에 고정 버전 조회 인자만 전달한다. 후보 옵션은
  `doctor --probe-toolchain`이며 정확한 입력 인터페이스는 구현 전에 기록한다.

위 옵션들은 **아직 미구현인 Phase 2 인터페이스**다. 감사 파일의 각 줄은
Phase 1의 `internal/core` schema 1과 `DecodeEvent` 계약을 따른다.
`event` discriminator, 중첩 `process`의 PID·birth/scope identity,
POSIX `session_id`의 10진 문자열, 관측 stage/result와 각 payload를 보존한다.
파일은 `resource.path`, 연결은 `destination.address`/`port`를 사용한다.
실제 직렬화 예시는 [단일 구현 명세의 Phase 1 보고](../docs/IMPLEMENTATION_MASTER_PLAN.md#1210-작업-상태와-세션-인계)를
참조한다. 예전 `type`/평면 PID 출력이나 추측한 kernel raw layout을 새 JSON
계약으로 사용하지 않는다. 관측하지 못한 성공·read 사실을 만들어 넣지 않는다.

출력 위치 예: target의 `hello`는 기존 stdout으로, 해당 실행의 `file_open`과
`network_connect` JSON은 명시한 감사 파일로 간다. AgentTaint 오류 진단은
감사 이벤트와 구분하며 JSON을 대상 stderr에 섞지 않는다.

## Out of Scope

- taint propagation / label 부여 (Phase 3)
- policy engine (Phase 3)
- blocking/enforcement (Phase 4)
- TLS/페이로드 관찰 (SSL_write 등 — 계획에 없음, 필요해지면 별도 논의)
- macOS
- Kubernetes

## Files

`bpf/process.bpf.c`, `bpf/file.bpf.c`, `bpf/network.bpf.c`, `bpf/maps.h`,
`bpf/events.h`, `internal/sensor/linux/sensor.go`. `bpf/vmlinux.h`는
`bpftool btf dump` 등으로 생성 — 직접 손으로 작성하지 않는다.
같은 Linux sensor 패키지 안의 실제 bpf2go 생성 `.go`/`.o` 쌍,
기존 `test/fixtures`·`test/integration`, CLI·runner·env의 기존 파일과
패키지 테스트, 기존 Makefile·portable CI 수정은 관련 ARCHITECTURE 계약을 따른다.
ENV-02에서 고정 generator의 정확한 basename·suffix·생성 명령을 확인하여
생성 전에 기록한다. 새 패키지·스킬 경로·privileged CI는 자동 허용하지 않는다.

## Technical Requirements

- AGENTS.md의 "eBPF 절대 규칙"을 반드시 지킨다: verifier 통과, unbounded
  loop 금지, 모든 메모리 접근 경계 검사, ring buffer로 전달, 커널
  함수 시그니처는 CO-RE relocation으로 (추측 금지).
- 커널 안에서 문자열 파싱/정규식 금지 — 경로/주소는 고정 크기 버퍼로
  복사만 하고, 해석은 `internal/sensor/linux`(유저스페이스)에서 한다.
- 감사 파일은 새 일반 파일을 `0600`으로 배타 생성한다. 기존 파일 덮어쓰기/
  append, symlink 추적, FIFO/장치를 거부하고 소유자·실효 권한·부모 경로와
  교체 경합을 검증한다. 크기 상한과 실행 중 쓰기 실패 lifecycle은 구현 전에
  확정한다. 초기 준비 실패는 target 시작 전에 실패하며 실행 중 손실은 표시한다.
  이미 발생한 효과는 되돌릴 수 없다. 감사 실패에 자동 보안 kill/차단을 추가하지 않는다.
- Toolchain probe는 신뢰된 명시적 절대 경로만 사용한다. 도구별 고정 인자,
  timeout·출력 크기 상한과 신뢰/권한 기준을 먼저 기록한다. PATH/프로젝트
  fallback, shell, 권한 상승, 설치, mount, 모듈/BPF load/attach는 금지한다.
  subprocess를 실행하는 probe를 정적 읽기 전용 점검이라고 부르지 않는다.
  절대 경로·존재·실행 권한은 파일 신뢰의 증거가 아니며, 버전 조회도 코드 실행이므로
  부작용이 없다고 보장하지 않는다.
  버전 결과를 clang BPF target 또는 실제 BPF capability 확인으로 대신 쓰지 않는다.

## Security Requirements

- 이 Phase는 detect-only다. Final Report에 "무엇이 감지되는가"와
  "무엇이 아직 차단되지 않는가"를 명시한다 — tracepoint 기반 관찰을
  "차단"이라고 표현하지 않는다 (AGENTS.md Security Semantics 절).

## Acceptance Criteria

- 커널 5.15+ Linux 테스트 profile에서 통제된 synthetic 파일을 여는 fixture를
  `run --observe --audit-file PATH -- <fixture>`로 실행하면 `file_open`이
  감사 파일에 기록된다. 이 명령 형태는 구현 후 수용 시험용이며 현재 실행 불가다.
- readiness가 확인된 로컬 IPv4 수신 fixture에 target 및 그 자식이 연결하면
  `network_connect`가 감사 파일에 기록된다. 공용 인터넷·실제 자격증명에
  의존하지 않고, target의 JSON/binary stdout·stderr·stdin은 별도로 검증한다.
- 각 감사 줄을 core `DecodeEvent`로 읽을 수 있고 scope 밖 이벤트가 섞이지 않는다.
  `--audit-file` 누락·위험 경로·초기 파일 오류에서는 target을 시작하지 않는다.
- 기본 doctor와 AI CLI 탐지는 실행하지 않는다. 명시 probe만 허용한 절대
  경로·고정 인자로 실행하며 미신뢰/상대 경로, timeout·초과 출력·실행 실패를
  명시적으로 보고한다. 버전 성공을 observer 준비 완료로 표시하지 않는다.
- eBPF 프로그램 로드 실패 시(권한 부족, BTF 없음 등) 명확한 에러 메시지와
  함께 종료하고, `agenttaint doctor`로 원인을 확인하라는 안내를 출력한다.

## Tests

- `bpf/`: 가능하면 `libbpf` 테스트 하네스로 verifier 통과 여부 CI 체크.
- `internal/sensor/linux`: raw 이벤트 바이트 → `internal/core.Event`
  변환 단위 테스트 (fixture 바이트열 사용).
- 통합 테스트: 실제 프로세스를 실행해 이벤트가 잡히는지 확인
  (root 권한 필요 — CI에서 스킵 조건 명시).
- 감사 파일: 존재 파일 보존, symlink/교체 경합, FIFO/장치, 소유자·권한,
  크기 상한·디스크/쓰기 오류, stdout/stderr fallback 없음, binary stdio 보존.
- Probe: fixture 실행 파일로 명시 선택 여부·고정 argv·시간/출력 상한을 검증.
  기본 doctor 비실행 회귀와 PATH/프로젝트 fallback·shell 실행 부재를 확인한다.

## Documentation

- 신규 문서 불필요.

## Required Final Report

AGENTS.md 형식 그대로. "what is NOT guaranteed"에 반드시 "이 Phase는
탐지만 하며 어떤 것도 차단하지 않는다"를 포함한다.

## Stop Conditions

- LSM 훅으로 바로 차단까지 구현하고 싶어질 때 — 멈춘다, 그건 Phase 4다.
- 이벤트에 taint label 필드를 추가하고 싶어질 때 — 멈춘다, `internal/core`
  구조는 Phase 1에서 고정됐고 label은 Phase 3에서 다룬다.
