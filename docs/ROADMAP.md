# Roadmap

사람이 보는 전체 방향이다. "무엇을 만들 것인가"만 담는다 — 어떤 구조로
만들지는 [ARCHITECTURE.md](./ARCHITECTURE.md), 어떤 보안 의미를 가져야
하는지는 [SECURITY_MODEL.md](./SECURITY_MODEL.md), 이번 Phase에서 정확히
무엇을 바꿀지는 [../prompts/](../prompts/)를 본다.

각 Phase는 이전 Phase가 완료된 뒤에만 시작한다 ([AGENTS.md](../AGENTS.md)
의 Scope Order 규칙).

## Linux MVP

- [x] **Phase 0 — Bootstrap** · [prompts/00-bootstrap.md](../prompts/00-bootstrap.md)
      Go 모듈 골격, `doctor`(읽기 전용 진단), `run`(명시적 실행)
      — 로컬 수용 검증 완료(macOS arm64). Linux amd64/arm64는 cross-build만 통과; 원격 CI 미실행.
      실제 감시·차단은 없음. 검증·인계 기록은 [IMPLEMENTATION_MASTER_PLAN.md](./IMPLEMENTATION_MASTER_PLAN.md)의 12.10절 참고.
- [x] **Phase 1 — Core Event Model** · [prompts/01-core-event-model.md](../prompts/01-core-event-model.md)
      플랫폼 독립 Event 타입 (`internal/core`)
      — 7종 raw event·식별자·strict JSON codec 및 macOS arm64 독립 검증 완료.
      Linux cross-build만 통과; 센서·라벨·정책·차단과 원격 CI 실행은 없음.
- [ ] **Phase 2 — Linux Observer** · [prompts/02-linux-observer.md](../prompts/02-linux-observer.md)
      eBPF로 fork/exec/exit, file open, network connect 관찰
- [ ] **Phase 3 — Taint Engine** · [prompts/03-taint-engine.md](../prompts/03-taint-engine.md)
      Source→Label→Propagation→Sink→Decision 최초 구현 (audit only) — AgentTaint MVP
- [ ] **Phase 4 — Linux Enforcement** · [prompts/04-linux-enforcement.md](../prompts/04-linux-enforcement.md)
      BPF-LSM 기반 block, 폴백 kill, monitor/notify/block/kill 모드

## 확장

- [ ] **Phase 5 — macOS Observer** · [prompts/05-macos-observer.md](../prompts/05-macos-observer.md) (스텁)
- [ ] **Phase 6 — Rich Flow Propagation** · [prompts/06-rich-flow.md](../prompts/06-rich-flow.md) (스텁)
      file write/read, pipe, unix socket propagation; 다중 label(`CREDENTIAL`, `SOURCE_CODE`, `UNTRUSTED`)
- [ ] **Phase 7 — Kubernetes** · [prompts/07-kubernetes.md](../prompts/07-kubernetes.md) (스텁)
      동일 코어를 privileged DaemonSet으로, cgroup→pod 매핑, ConfigMap → CRD
- [ ] **Phase 8 — Agent Feedback Loop** (프롬프트 미작성)
      위반 발생 시 사람이 읽을 수 있는 이유를 에이전트에게 되돌려줘서
      (Claude Code/Codex hook, MCP 등) 다른 경로로 재시도하게 만든다.
      ActPlane의 corrective feedback 개념 참고 — 지금은 방향성만 기록.
- [ ] **Phase 9 — Local Web Dashboard** · [prompts/09-local-web-dashboard.md](../prompts/09-local-web-dashboard.md) (스텁)
      `agenttaint`가 이미 수집한 이벤트/정책 위반 로그를 로컬 브라우저에서
      조회하는 뷰어. AgentSight의 `frontend`+`controller`(별도 SaaS,
      OAuth/relay)를 그대로 따르지 않고, 클라우드 계층 없이 단일 바이너리
      안에 내장하는 것을 기본 방향으로 한다 — 자세한 내용은
      [ARCHITECTURE.md](./ARCHITECTURE.md)의 "향후: 로컬 웹 대시보드" 절
      참고. 이벤트 영속화/쿼리 계층이 먼저 정의되어야 착수 가능하다.
- [ ] **Phase 10 — Payload-aware LLM Sink (조건부)** · [prompts/10-payload-aware-llm-sink.md](../prompts/10-payload-aware-llm-sink.md) (조건부 스텁)
      `SSL_write`/`SSL_read` uprobe로 LLM API 요청의 목적지·모델명 등
      메타데이터를 Sink 판정에 사용하는 확장. **구현이 확정된 게 아니라
      조건부 스텁이다** — 필요성이 실제로 확인되기 전까지는 시작하지
      않는다. 지켜야 할 제약은
      [SECURITY_MODEL.md](./SECURITY_MODEL.md)의 관련 절 참고.

## 순서를 지키는 이유

- macOS는 Linux MVP(Phase 0~4) 완료 후 시작한다 — 두 플랫폼을 병렬로
  만들면 보안 semantics를 어느 쪽 기준으로 검증했는지 불명확해진다.
- Kubernetes는 로컬 information-flow 모델이 끝난 뒤에만 시작한다 —
  배포 방식(DaemonSet)을 먼저 고민하는 건 시기상조다.
- TLS/페이로드 관찰(예: `SSL_write` uprobe)은 실행이 확정된 Phase가 아니라
  Phase 10에 조건부 스텁으로만 남겨뒀다. 필요성이 실제로 확인되기 전까지는
  시작하지 않으며, 착수하려면 AGENTS.md의 "문서는 규범이다" 절차를 다시
  밟는다 — 조용히 다른 Phase에 끼워 넣지 않는다.
