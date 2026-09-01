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
- `agenttaint run`에 `--observe` 플래그(또는 유사)를 추가해, 실행한
  프로세스 트리에서 발생하는 이벤트를 stdout에 JSON으로 스트리밍.

출력 예시 (그대로 최종 포맷일 필요는 없지만 이 정도 정보는 담는다):

```json
{"type": "file_open", "pid": 1834, "ppid": 1820, "path": "/workspace/.env"}
{"type": "network_connect", "pid": 1836, "ppid": 1834, "addr": "93.184.216.34", "port": 443}
```

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

## Technical Requirements

- AGENTS.md의 "eBPF 절대 규칙"을 반드시 지킨다: verifier 통과, unbounded
  loop 금지, 모든 메모리 접근 경계 검사, ring buffer로 전달, 커널
  함수 시그니처는 CO-RE relocation으로 (추측 금지).
- 커널 안에서 문자열 파싱/정규식 금지 — 경로/주소는 고정 크기 버퍼로
  복사만 하고, 해석은 `internal/sensor/linux`(유저스페이스)에서 한다.

## Security Requirements

- 이 Phase는 detect-only다. Final Report에 "무엇이 감지되는가"와
  "무엇이 아직 차단되지 않는가"를 명시한다 — tracepoint 기반 관찰을
  "차단"이라고 표현하지 않는다 (AGENTS.md Security Semantics 절).

## Acceptance Criteria

- 커널 5.15+ 환경에서 `agenttaint run --observe -- python3 -c "open('.env')"`
  실행 시 `file_open` 이벤트가 출력된다.
- `agenttaint run --observe -- bash -c "curl https://example.com"` 실행 시
  `network_connect` 이벤트가 출력된다 (curl이 자식 프로세스로 뜨는 경우도
  포함).
- eBPF 프로그램 로드 실패 시(권한 부족, BTF 없음 등) 명확한 에러 메시지와
  함께 종료하고, `agenttaint doctor`로 원인을 확인하라는 안내를 출력한다.

## Tests

- `bpf/`: 가능하면 `libbpf` 테스트 하네스로 verifier 통과 여부 CI 체크.
- `internal/sensor/linux`: raw 이벤트 바이트 → `internal/core.Event`
  변환 단위 테스트 (fixture 바이트열 사용).
- 통합 테스트: 실제 프로세스를 실행해 이벤트가 잡히는지 확인
  (root 권한 필요 — CI에서 스킵 조건 명시).

## Documentation

- 신규 문서 불필요.

## Required Final Report

AGENTS.md 형식 그대로. "what is NOT guaranteed"에 반드시 "이 Phase는
탐지만 하며 어떤 것도 차단하지 않는다"를 포함한다.

## Stop Conditions

- LSM 훅으로 바로 차단까지 구현하고 싶어질 때 — 멈춘다, 그건 Phase 4다.
- 이벤트에 taint label 필드를 추가하고 싶어질 때 — 멈춘다, `internal/core`
  구조는 Phase 1에서 고정됐고 label은 Phase 3에서 다룬다.
