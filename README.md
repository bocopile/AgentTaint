# AgentTaint

AI 에이전트와 그 하위 프로세스의 민감정보 접근·전파를 OS 수준(eBPF)에서
추적하고, 위험한 정보 흐름을 정책에 따라 감사·차단하는 runtime security
system.

> **Status: 🚧 Early development — Phase 0 (설계·문서화 단계, 코드 없음)**
> 진행 단계는 [docs/ROADMAP.md](./docs/ROADMAP.md), AI 코딩 에이전트가
> 따르는 규칙은 [AGENTS.md](./AGENTS.md), Phase별 작업 지시는
> [prompts/](./prompts/) 참고. 스택은 **Go + C/CO-RE(cilium/ebpf)**로
> 확정.

## 핵심 모델

```
Sensitive Source → AI Agent → Process Taint → Propagation → Sink → Decision
```

민감 파일에 접근한 프로세스에 taint label을 부여하고, fork/exec을 통해
그 라벨을 자식 프로세스까지 전파하며, 라벨이 붙은 프로세스가 외부
네트워크 등 sink에 도달하면 정책에 따라 감사(audit)하거나 차단(deny)한다.
자세한 정의는 [docs/SECURITY_MODEL.md](./docs/SECURITY_MODEL.md),
막는 것/못 막는 것의 경계는 [docs/THREAT_MODEL.md](./docs/THREAT_MODEL.md)
참고.

## 제품 철학

AI를 신뢰하는 대신, 민감정보가 안전하게 사용되도록 실행 환경에서 제한하고
실제 정보 흐름을 추적·통제한다.

## 관련 프로젝트

- **AgentSight** — AI 에이전트용 `strace` + `top` + APM. AI 에이전트가 실제
  시스템에서 어떤 프로세스를 생성하고, 어떤 파일과 네트워크에 접근하며, 어떤
  시스템 자원을 사용하는지를 관찰하는 시스템 수준의 Observability 프로젝트다.
- **ActPlane** — AI 에이전트용 policy engine + sandbox/guardrail. AI
  에이전트와 그 하위 프로세스가 수행하는 실제 시스템 동작을 정책으로 정의하고,
  이를 OS 수준에서 허용·차단하는 Runtime Policy Enforcement 프로젝트다.
  eBPF로 fork/exec·file I/O를 따라 레이블을 전파하고 kill/block/notify로
  강제한다는 점에서 커널 메커니즘은 AgentTaint와 유사하지만, ActPlane은
  민감정보(secret/credential) 보호를 스스로 범위 밖으로 명시하고 "테스트 후
  커밋" 같은 에이전트 워크플로우 규칙 강제에 초점을 맞춘다.

AgentTaint는 이 둘과 달리 **민감정보 보호**에 초점을 맞춘 AI 실행 보안
환경을 목표로 한다. AI와 그 하위 프로세스가 민감정보에 접근하는 것을
제한하고, 허용된 민감정보가 프로세스·파일·네트워크를 통해 어떻게 이동하는지
추적하며, 위험한 정보 흐름이 발생할 경우 이를 탐지하고 차단한다.

## Docs

- [AGENTS.md](./AGENTS.md) — AI 코딩 에이전트가 항상 지켜야 하는 규칙 (프로젝트 헌법)
- [docs/THREAT_MODEL.md](./docs/THREAT_MODEL.md) — 보호 대상, 공격자 모델, non-goals
- [docs/SECURITY_MODEL.md](./docs/SECURITY_MODEL.md) — taint 모델, 정책/이벤트 스키마 초안
- [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md) — 기술 스택, 디렉터리 구조, doctor/run 역할 분리
- [docs/ROADMAP.md](./docs/ROADMAP.md) — Phase 0~7+ 전체 로드맵
- [prompts/](./prompts/) — Phase별 AI 작업 지시 (implementation contract)
