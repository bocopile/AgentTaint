# Phase 1 — Core Event Model

## Goal

플랫폼 독립적인 이벤트 모델을 `internal/core`에 만든다. 이 Phase에서는
eBPF도, macOS Endpoint Security도 구현하지 않는다 — 그 둘이 나중에
공통으로 채워 넣을 "그릇"만 정의한다.

## Context

Linux eBPF와 (향후) macOS Endpoint Security는 완전히 다른 raw 이벤트
포맷을 낸다. 정책 평가와 taint propagation이 특정 플랫폼을 몰라도
동작하려면, 그 전에 공통 표현으로 정규화해야 한다.

```
internal/sensor/linux ─────┐
                            │
                            ▼
                    internal/core.Event
                            │
                            ▼
              internal/taint, internal/policy
                            ▲
                            │
internal/sensor/darwin (미래) ───┘
```

## In Scope

`internal/core`에 다음을 구현한다.

- Event 타입 (최소 다음 variant/케이스를 표현):
  - `ProcessExec`, `ProcessFork`, `ProcessExit`
  - `FileOpen`, `FileRead`, `FileWrite`
  - `NetworkConnect`
- 식별자 타입: `ProcessID`, `ProcessGroupID`, `SessionID` (단순
  `int32`/`uint32` 래핑이 아니라 named type으로 타입 안전성 확보)
- 리소스 타입: `FileResource`(경로), `NetworkDestination`(IPv4 주소+포트)
- 이벤트는 이후 taint label을 붙일 수 있도록 `ProcessContext`(pid,
  ppid 등)를 포함한다. 단, 이 Phase에서 label 필드 자체는 정의하지
  않는다 (Phase 3에서 `internal/taint`가 추가).

Go 표현 예시 (정확한 필드는 구현하면서 확정, 이 모양을 유지):

```go
type Event interface {
    Kind() EventKind
}

type FileOpenEvent struct {
    Process  ProcessContext
    Resource FileResource
    Access   FileAccess
}

type NetworkConnectEvent struct {
    Process     ProcessContext
    Destination NetworkDestination
}
```

## Out of Scope

다음은 이 Phase에서 절대 구현하지 않는다.

- eBPF (`bpf/` 아래 어떤 것도)
- macOS Endpoint Security
- taint propagation / label 부여 로직
- policy 평가
- 네트워크 차단
- TLS/페이로드 관찰
- Kubernetes 개념
- HTTP 파싱

## Files

`internal/core/event.go`, `internal/core/process.go`,
`internal/core/resource.go`. `internal/core/label.go`는 만들되 비워두거나
Phase 3에서 채울 자리만 `doc.go` 주석으로 남긴다.

## Technical Requirements

- Linux `task_struct`, BPF 전용 구조체, macOS ES 전용 구조체가
  `internal/core`에 절대 등장하지 않는다 (AGENTS.md 규칙 1).
- 모든 Event 타입은 `encoding/json` 마샬링을 지원한다. JSON 출력은
  향후 audit 로그 포맷의 기반이 되므로 필드명을 신중히 정한다
  (`docs/SECURITY_MODEL.md`의 Event Schema 초안과 호환되게).

## Security Requirements

- 이 Phase는 순수 데이터 모델이라 보안 판단이 없다. 다만 필드 설계가
  이후 taint label 부여(Phase 3)와 정책 평가(Phase 3)에 필요한 정보
  (pid, ppid, 파일 경로, 목적지 주소)를 빠짐없이 담고 있는지 확인한다.

## Acceptance Criteria

- `go build ./...`, `go vet ./...` 통과.
- `internal/core`가 다른 어떤 `internal/` 패키지도 import하지 않는다
  (`go list -deps`로 확인 가능).
- 각 Event 타입을 JSON으로 직렬화한 예시가 Final Report에 포함된다.

## Tests

- `ProcessExec`, `FileOpen`, `NetworkConnect` 최소 3종의 직렬화 테스트.
- round-trip (marshal → unmarshal → 원본과 동일한지) 테스트 최소 1개.

## Documentation

- 새 문서 불필요. `docs/SECURITY_MODEL.md`의 Event Schema 초안과 실제
  구현이 달라졌다면 그 차이를 Final Report의 "Architecture Decisions"에
  적고, AGENTS.md 절차대로 문서 갱신 여부를 먼저 논의한다.

## Required Final Report

AGENTS.md의 "Required Final Report" 형식을 그대로 사용한다. "Security
Semantics" 절에는 이 Phase가 아직 아무것도 detect/block하지 않는다는
것을 명시한다.

## Stop Conditions

- eBPF 관련 타입이나 상수를 `internal/core`에 넣고 싶어질 때 — 멈추고
  왜 필요한지 보고한다 (대개는 `internal/sensor/linux`로 가야 한다).
- Phase 3에서 쓸 Label 타입 구조를 지금 다 확정하고 싶어질 때 — 자리만
  남기고 실제 설계는 Phase 3로 미룬다.
