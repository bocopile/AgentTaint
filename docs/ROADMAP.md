# Roadmap

사람이 보는 전체 방향이다. "무엇을 만들 것인가"만 담는다 — 어떤 구조로
만들지는 [ARCHITECTURE.md](./ARCHITECTURE.md), 어떤 보안 의미를 가져야
하는지는 [SECURITY_MODEL.md](./SECURITY_MODEL.md), 이번 Phase에서 정확히
무엇을 바꿀지는 [../prompts/](../prompts/)를 본다.

각 Phase는 이전 Phase가 완료된 뒤에만 시작한다 ([AGENTS.md](../AGENTS.md)
의 Scope Order 규칙).

## Linux MVP

- [ ] **Phase 0 — Bootstrap** · [prompts/00-bootstrap.md](../prompts/00-bootstrap.md)
      Go 모듈 골격, `doctor`(읽기 전용 진단), `run`(명시적 실행)
- [ ] **Phase 1 — Core Event Model** · [prompts/01-core-event-model.md](../prompts/01-core-event-model.md)
      플랫폼 독립 Event 타입 (`internal/core`)
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

## 순서를 지키는 이유

- macOS는 Linux MVP(Phase 0~4) 완료 후 시작한다 — 두 플랫폼을 병렬로
  만들면 보안 semantics를 어느 쪽 기준으로 검증했는지 불명확해진다.
- Kubernetes는 로컬 information-flow 모델이 끝난 뒤에만 시작한다 —
  배포 방식(DaemonSet)을 먼저 고민하는 건 시기상조다.
- TLS/페이로드 관찰(예: `SSL_write` uprobe)은 이 로드맵에 없다. 필요성이
  생기면 AGENTS.md의 "문서는 규범이다" 절차를 따라 별도로 논의하고
  추가한다 — 조용히 어느 Phase에 끼워 넣지 않는다.
