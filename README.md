# AgentTaint

AI 에이전트와 그 하위 프로세스의 민감정보 접근·전파를 OS 수준(eBPF)에서
추적하고, 위험한 정보 흐름을 정책에 따라 감사·차단하는 runtime security
system을 목표로 한다. **현재 구현은 Phase 0 진단·실행 도구와 Phase 1
플랫폼 독립 이벤트 모델이며 감시·차단 기능은 없다.**

> **Status: 🚧 Phase 1 완료 — core 모델·codec 로컬 검증 통과, 감시·차단은 아직 없음**
> 진행 단계는 [docs/ROADMAP.md](./docs/ROADMAP.md), AI 코딩 에이전트가
> 따르는 규칙은 [AGENTS.md](./AGENTS.md), Phase별 작업 지시는
> [prompts/](./prompts/) 참고. 스택은 **Go + C/CO-RE(cilium/ebpf)**로
> 확정.

## 현재 사용할 수 있는 것

Phase 1에서 프로세스·파일·네트워크의 7종 raw event, PID 생애 식별 모델,
strict JSON codec을 추가했다. 아직 센서가 발생시키는 실제 이벤트는 없고
CLI는 기존 `doctor`/`run` 그대로다. core는 label·정책·보안 판단을 하지 않는다.

Go **1.26 이상**에서 빌드한다. 환경 진단과 일반 커맨드 실행은 macOS/Linux를
대상으로 한다. 현재 실제 실행 검증은 macOS arm64에서 완료했으며, Linux
amd64/arm64는 cross-build만 통과했다. 원격 CI는 아직 실행하지 않았다.
관리자/root 권한은 필요하지 않으며, 이 Phase를
사용한다고 AI 실행이 보호되는 것은 아니다.

```sh
go build -o ./agenttaint ./cmd/agenttaint
./agenttaint doctor --json
./agenttaint run -- /usr/bin/printf '%s\n' hello
```

- `doctor`: Linux의 커널 버전·BTF 파일·활성 LSM 목록을 정적으로 점검하고
  알려진 CLI의 경로를 조회한다. 발견한 CLI를 실행하지 않으므로 버전은
  `unknown`, 이유는 `not_executed_read_only`다. BPF load/attach는 하지 않는다.
- `run`: `--bin`의 literal 파일 경로 → PATH → 문서화된 후보 경로 순서로
  실행 파일을 선택한다. 잘못된 명시 경로나 안전하지 않은 상대 PATH 결과는
  fallback하지 않는다. argv와 stdin/stdout/stderr를 그대로 연결하고, 자체
  안내는 stderr로 보낸다. shell 보간·PATH 변경을 하지 않는다.
- 종료 코드: target의 정상 종료 코드를 보존한다. 실행/조회 실패 또는
  signal 종료는 `1`, CLI 사용법 오류는 `2`다. `doctor`는 진단 보고서를
  출력하면 `0`이며, 이 값은 보호 가능 판정이 아니다. 보고서 출력 실패는 `1`이다.
- 프로세스 트리 감시·잔류 자식 정리·signal 전달을 책임지는 supervisor가
  아니다. native macOS 센서, Linux eBPF, taint, 정책 평가, enforcement는
  아직 구현하지 않았다.

```sh
make check
make race
```

이 명령은 portable 형식·정적 검사·빌드·테스트를 수행한다. Linux 실제 BPF
작동이나 차단을 검증하지 않는다. AI CLI·스킬을 설치하거나 사용자 설정을
변경하지 않는다. 개발·검증의 현재 상태와 남은 작업은
[통합 구현 계획서](./docs/IMPLEMENTATION_MASTER_PLAN.md)의 12.10절을 참고한다.

## 핵심 모델

```
Sensitive Source → AI Agent → Process Taint → Propagation → Sink → Decision
```

향후 구현할 모델은 민감 파일에 접근한 프로세스에 taint label을 부여하고, fork/exec을 통해
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
추적하며, 위험한 정보 흐름이 발생할 경우 이를 탐지하고 차단하는 것이 목표다.
Byte-level taint tracking을 주장하지 않으며, 사후 감지·kill을 사전 차단이라고
부르지 않는다. 현재 Phase 1까지의 구현은 이 감지·차단 자체를 제공하지 않는다.

## Docs

- [AGENTS.md](./AGENTS.md) — AI 코딩 에이전트가 항상 지켜야 하는 규칙 (프로젝트 헌법)
- [docs/THREAT_MODEL.md](./docs/THREAT_MODEL.md) — 보호 대상, 공격자 모델, non-goals
- [docs/SECURITY_MODEL.md](./docs/SECURITY_MODEL.md) — taint 모델, 정책/이벤트 스키마 초안
- [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md) — 기술 스택, 디렉터리 구조, doctor/run 역할 분리
- [docs/ROADMAP.md](./docs/ROADMAP.md) — Phase 0~7+ 전체 로드맵
- [docs/IMPLEMENTATION_MASTER_PLAN.md](./docs/IMPLEMENTATION_MASTER_PLAN.md) — 상세 결정·작업 계약·검증·현재 인계 상태
- [prompts/](./prompts/) — Phase별 AI 작업 지시 (implementation contract)
