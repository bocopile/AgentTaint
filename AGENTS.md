# AGENTS.md — AgentTaint 개발 규칙

이 파일은 AgentTaint에서 작업하는 모든 AI 코딩 에이전트(Claude Code, Codex
CLI 등)가 어떤 작업을 하든 항상 지켜야 하는 규칙이다. Phase별 작업 지시는
`prompts/`에 있지만, 그 지시가 이 파일과 충돌할 경우 이 파일이 우선한다.

## Product Principle

AgentTaint does not trust the intent of AI agents.

It observes and controls actual system effects to ensure sensitive
information is used only within permitted boundaries. 프롬프트 분류나 LLM
판단으로 위험을 걸러내지 않는다 — 실제 OS-level effect(파일 접근, 프로세스
생성, 네트워크 연결)만 근거로 삼는다.

## Core Security Model — 절대 다른 모델로 대체하지 말 것

```
Source -> Label -> Propagation -> Sink -> Decision
```

자세한 정의는 [docs/SECURITY_MODEL.md](./docs/SECURITY_MODEL.md). 이
모델을 payload scanning, LLM classification, prompt 분석으로 대체하지
않는다. 이런 기능이 필요해 보여도 먼저 이 문서와 충돌 여부를 확인하고,
임의로 추가하지 않는다 (아래 "문서는 규범이다" 참고).

## Tech Stack (변경 시 이 파일부터 갱신)

- 유저스페이스: **Go** + [`cilium/ebpf`](https://github.com/cilium/ebpf)
- 커널부: **C + libbpf CO-RE** (BCC 스타일 런타임 컴파일 금지 — 커널마다
  다시 컴파일하면 Kubernetes 노드 배포가 깨진다)
- 왜 Go인가: Kubernetes 생태계(client-go, controller-runtime)가 Go
  중심이고, `cilium/ebpf` + CO-RE가 Tetragon이 검증한 조합이다. Rust(aya)도
  대안으로 검토했으나 k8s 글루 코드 생태계가 더 얇아서 채택하지 않았다.

## Architecture Rules

1. `internal/core`는 플랫폼(Linux/macOS)을 알아서는 안 된다. Linux나 macOS
   패키지를 import하지 않는다. 이 규칙은 코드 리뷰와 lint(추후
   `depguard` 규칙)로 강제한다.
2. Linux(및 향후 macOS) 센서는 `internal/core`에 정의된 정규화된 Event만
   내보낸다. 센서 코드 안에 정책 로직을 두지 않는다.
3. Taint propagation(라벨 전파)은 하나의 패키지(`internal/taint`)에만
   존재한다. 센서나 CLI에 흩어놓지 않는다.
4. Policy 평가는 하나의 패키지(`internal/policy`)에만 존재한다.
5. CLI(`cmd/agenttaint`)는 보안 판단(allow/audit/deny)을 직접 내리지
   않는다. policy/taint 패키지를 호출만 한다.
6. eBPF(C) 코드는 최소한으로 유지한다. 복잡한 파싱·정책 준비는 유저스페이스
   Go 코드에서 한다 — 커널 안에서 정규식/문자열 처리 금지.

## eBPF 절대 규칙

- 모든 eBPF 프로그램은 커널 verifier를 통과해야 한다.
- unbounded loop 금지. 모든 반복은 상한이 고정돼야 한다.
- 모든 메모리 접근 전 경계 검사(bounds check) 필수.
- 커널→유저스페이스 전달은 `BPF_MAP_TYPE_RINGBUF` 사용.
- 커널 함수 시그니처를 추측하지 않는다. `vmlinux.h`와 CO-RE relocation을
  사용한다.

## Security Semantics — 절대 과장하지 않는다

- **Byte-level taint tracking을 주장하지 않는다.** Tainted process는 "이
  프로세스가 민감 데이터에 접근했다"는 뜻이지, "이 프로세스가 내보내는
  모든 바이트에 민감 데이터가 들어있다"는 뜻이 아니다.
- **post-event detection을 prevention이라고 부르지 않는다.** 선택한 OS
  훅이 실제로 pre-operation enforcement를 제공할 때만 "차단(block)"이라고
  표현한다. tracepoint 기반 관찰은 kill/notify이지 block이 아니다.
- 이 두 문장은 모든 Phase의 Final Report에 명시적으로 등장해야 한다
  ("무엇이 감지되는가 / 무엇이 차단되는가 / 무엇이 보장되지 않는가").

## Scope Order — 순서를 건너뛰지 않는다

```
1. Linux
2. macOS (Linux VM 안에서 실행 — 네이티브 Endpoint Security는 별도 결정 전까지 non-goal)
3. Kubernetes
```

로컬 Linux에서 information-flow 모델(Source→Label→Propagation→Sink→
Decision)이 완성되기 전에는 Kubernetes 관련 기능을 구현하지 않는다.
자세한 Phase 순서는 [docs/ROADMAP.md](./docs/ROADMAP.md).

## Development Workflow

**작업 시작 전:**
1. `AGENTS.md`(이 파일) 확인
2. 관련 `docs/`(ARCHITECTURE.md, SECURITY_MODEL.md, THREAT_MODEL.md) 확인
3. 현재 Phase 프롬프트(`prompts/NN-*.md`) 확인
4. 기존 구현 상태 확인
5. 가정(assumption)을 먼저 명시
6. 완료 기준을 만족하는 **가장 작은 변경**만 수행

**작업 후:**
1. `gofmt` / `go vet`
2. 빌드
3. 테스트 실행
4. 아래 "Required Final Report" 형식으로 보고

**절대 스코프를 조용히 늘리지 않는다.** 현재 Phase의 "Out of Scope"에
있는 것은 다음 Phase를 기다린다.

## 문서는 규범이다 (Docs Are Normative)

`docs/ARCHITECTURE.md`와 `docs/SECURITY_MODEL.md`는 설계의 source of
truth다. 구현하다가 이 문서들과 충돌하면:

1. **조용히 문서를 코드에 맞춰 고치지 않는다.**
2. 충돌 지점을 식별한다.
3. 왜 충돌하는지 설명한다.
4. 가장 작은 architecture 변경안을 제안한다.
5. 그 변경을 적용하기 **전에 멈추고** 사람의 확인을 받는다.

## Required Final Report

모든 Phase 작업이 끝나면 정확히 이 형식으로 보고한다.

```
## Changed
- ...

## Architecture Decisions
- ...

## Tests
- gofmt:
- go vet:
- go test:

## Security Semantics
- what is detected:
- what is blocked:
- what is NOT guaranteed:

## Limitations
- ...

## Next Phase
Do not implement it. Only describe the next expected step.
```

## Stop Conditions

다음 상황에서는 추측해서 진행하지 말고 멈춘 뒤 질문한다.

- 현재 Phase 프롬프트의 "In Scope"만으로 요구사항이 애매할 때
- `docs/`와 충돌이 발견됐을 때 (위 "문서는 규범이다" 절차를 따르고 멈춘다)
- eBPF verifier를 통과시키기 위해 Security Semantics 절의 원칙을 깨야 할
  것 같을 때
- 이전 Phase가 완료되지 않았는데 다음 Phase 작업을 시작하게 될 때
