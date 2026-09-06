# Architecture

핵심 모델(Source → Label → Propagation → Sink → Decision)은
[SECURITY_MODEL.md](./SECURITY_MODEL.md)에 정의되어 있다. 이 문서는 그
모델을 실제로 어떤 컴포넌트·패키지가, 어떤 플랫폼에서 구현하는지를 다룬다.
이 문서와 [AGENTS.md](../AGENTS.md)는 규범(normative)이다 — 구현이
여기와 어긋나면 코드가 아니라 이 문서를 기준으로 맞추거나,
AGENTS.md의 "문서는 규범이다" 절차를 따라 먼저 논의한다.

## 기술 스택 (확정)

- 유저스페이스: **Go** + [`cilium/ebpf`](https://github.com/cilium/ebpf)
- 커널부: **C + libbpf CO-RE**
- 선택 이유와 Rust(aya) 대비 트레이드오프는 [AGENTS.md](../AGENTS.md)의
  "Tech Stack" 절 참고. 요약: Kubernetes 생태계(2차 목표)가 Go 중심이고,
  `cilium/ebpf`+CO-RE가 이 영역에서 Tetragon이 검증한 조합이다.
- 정확한 버전 고정(Go 1.26+, `cilium/ebpf` v0.22.0, clang/LLVM 12+,
  bpftool, 커널 최소 버전 5.13+)은 [AGENTS.md](../AGENTS.md)의
  "버전 고정" 절이 유일한 source of truth다 — 이 문서에서 중복 기록하지
  않는다.

## 디렉터리 구조

```
AgentTaint/
├── AGENTS.md                     # AI 작업 규칙 (규범)
├── README.md
├── go.mod
├── Makefile
│
├── cmd/
│   └── agenttaint/
│       └── main.go               # CLI: doctor / run / report 서브커맨드
│
├── internal/
│   ├── core/                     # 플랫폼 독립. Linux/macOS 패키지를 import 금지
│   │   ├── event.go              #   Event 타입 (ProcessExec/Fork/Exit, FileOpen, NetworkConnect ...)
│   │   ├── process.go            #   ProcessID, ProcessGroupID, SessionID
│   │   ├── resource.go           #   FileResource, NetworkDestination
│   │   └── label.go              #   Label 타입 (SECRET 등)
│   │
│   ├── sensor/
│   │   ├── linux/                # v0.1 구현 대상 — eBPF 로드/attach, core.Event로 정규화
│   │   │   └── sensor.go
│   │   └── darwin/                # Phase 5+ 자리만 확보. v0.1은 미구현 (VM 우회)
│   │
│   ├── taint/                    # Label propagation (Source→Label→Propagation)
│   │   └── engine.go
│   │
│   ├── policy/                   # 정책 파싱·평가 (Sink→Decision)
│   │   ├── model.go
│   │   ├── parser.go
│   │   └── evaluator.go
│   │
│   ├── enforce/                  # Decision → 실제 조치 (audit/monitor/notify/block/kill)
│   │   └── enforce.go
│   │
│   ├── web/                      # Phase 9(로컬 웹 대시보드) 전까지 자리만 확보 — 미구현
│   │   └── doc.go
│   │
│   ├── env/                      # doctor: 커널/BTF/BPF-LSM/에이전트 CLI 진단 (읽기 전용)
│   │   └── doctor.go
│   │
│   └── runner/                   # run: `--` 뒤 커맨드 하나만 명시적 resolve 후 실행
│       └── runner.go
│
├── bpf/                          # eBPF C 소스 (CO-RE)
│   ├── process.bpf.c             # tracepoint: fork/exec/exit
│   ├── file.bpf.c                # LSM/tracepoint: file open
│   ├── network.bpf.c             # LSM: socket connect
│   ├── maps.h
│   ├── events.h
│   └── vmlinux.h                 # 생성물, 직접 작성하지 않음
│
├── policies/
│   └── example.yaml
│
├── examples/                     # taint 검증용 시나리오 (acceptance test가 사용)
│   ├── normal/
│   ├── sensitive-read/
│   └── subprocess-exfil/
│
├── test/
│   ├── integration/
│   └── fixtures/
│
├── deploy/                       # Kubernetes 배포 (Phase 7 착수 전까지는 비어 있음)
│   ├── daemonset.yaml
│   ├── configmap.yaml
│   └── rbac.yaml
│
├── prompts/                      # Phase별 AI 작업 지시 (00-bootstrap.md ...)
│   └── README.md
│
└── docs/
    ├── ARCHITECTURE.md           # 이 문서
    ├── THREAT_MODEL.md
    ├── SECURITY_MODEL.md
    └── ROADMAP.md
```

이 트리는 지금 저장소에 전부 만들어져 있지 않다 — `prompts/00-bootstrap.md`
가 이 문서를 참조해서 실제로 생성한다. 새 파일을 이 트리 밖에 만들지
않는다.

## 컴포넌트 구조 (개념도)

```
              agenttaint (cmd/agenttaint, 단일 바이너리)
                       │
        ┌──────────────┼───────────────┐
        │              │               │
   internal/env    internal/runner  internal/policy
   (환경 진단)     (감시 대상 실행)   (정책 로드/평가)
        │              │               │
        └──────┬───────┴───────┬───────┘
               │               │
     internal/sensor/*    internal/core
     (플랫폼별 구현)      (정규화된 Event 타입)
               │               │
               └───────┬───────┘
                        ▼
               internal/taint (라벨 전파)
                        │
                        ▼
               internal/policy (평가)
                        │
                        ▼
               internal/enforce
                        │
                        ▼
              ALLOW / AUDIT / DENY
```

- **`internal/sensor/*`**만 플랫폼마다 다르다. 이벤트를 만들어내는 방식
  (eBPF hook 등)은 플랫폼 종속적이지만, `internal/core`에 정의된 이벤트
  형태와 그 위의 `internal/taint`/`internal/policy`는 플랫폼 독립적이다.
- **`internal/policy`**는 `internal/core`의 Event만 소비한다. 어떤 센서가
  이벤트를 만들었는지 몰라도 동작해야 한다 (AGENTS.md 규칙 1, 2).

## `agenttaint run -- <COMMAND>` 가 보안 경계다

AgentTaint가 감시하는 대상은 "AI 에이전트로 인식된 프로세스"가 아니라
**`run`에 명시적으로 넘긴 커맨드와 그 프로세스 트리 전체**다.

```
agenttaint run -- claude
agenttaint run -- python agent.py
agenttaint run -- ./my-custom-agent
agenttaint run -- bash
```

모두 동일하게 동작해야 한다. `claude`/`codex` 같은 특정 바이너리를 아는
것은 **`doctor`의 UX 편의 기능**일 뿐이고, 코어 아키텍처가 특정 AI CLI를
알아야 하는 구조가 되어서는 안 된다. 이렇게 분리하는 이유:

- 새 에이전트 CLI가 나올 때마다 코어를 고칠 필요가 없다.
- 보안 경계가 "이 프로세스가 AI처럼 보이는가"라는 휴리스틱이 아니라
  "사용자가 명시적으로 감시하라고 지정한 프로세스 트리"라는 명확한
  기준이 된다.

## `doctor` vs `run` — 역할을 명확히 분리한다

| | 패키지 | 역할 | 환경(PATH 등)을 바꾸는가 |
|---|---|---|---|
| `doctor` | `internal/env` | 진단만 한다: 커널/BTF/BPF-LSM 여부, 알려진 에이전트 CLI(claude, codex 등)가 어디 있는지 리포트 | **아니오** — 순수 조회 |
| `run` | `internal/runner` | `--` 뒤에 온 커맨드 하나를 실제로 실행 | 그 커맨드 하나를 resolve할 때만 explicit lookup |

`doctor`가 후보 경로를 스캔하다가 발견한 디렉터리를 세션 PATH 앞에
끼워넣는 설계는 채택하지 않는다. 진단 도구가 바이너리 resolution 순서에
관여하기 시작하면 그 자체가 새로운 attack surface가 된다. 대신 이렇게
출력한다.

```
$ agenttaint doctor
claude
  found: yes
  path: /Users/x/.local/bin/claude
  available_in_path: yes

codex
  found: yes
  path: /Users/x/.nvm/versions/node/v24.14.0/bin/codex
  available_in_path: no
  recommendation: add "/Users/x/.nvm/versions/node/v24.14.0/bin" to PATH,
                   or run: agenttaint run --bin /Users/x/.../codex -- codex
```

`run`이 실제로 실행할 바이너리를 못 찾을 때만, 그 한 커맨드에 한해서
후보 경로를 조회해 명시적으로 resolve한다 (전체 세션 PATH를 바꾸지 않고,
그 한 번의 `exec`에만 적용).

## 플랫폼 백엔드

코어 모델은 플랫폼 중립이지만, **v0.1에서 실제로 구현하는 백엔드는
Linux(eBPF) 하나뿐**이다. 다른 플랫폼은 방향성만 문서화한다 — 지금
구현 대상이 아니다.

| 플랫폼 | 패키지 | 상태 | 접근 방식 |
|---|---|---|---|
| Linux | `internal/sensor/linux` | **v0.1 구현 대상** | eBPF (tracepoint: fork/exec/exit, LSM: file_open/socket_connect) |
| macOS | `internal/sensor/darwin` | v0.1은 미지원 — Linux VM(Lima/Colima/OrbStack) 안에서 실행. 디렉터리 자리만 확보, Phase 5 전까지 구현하지 않는다 | eBPF는 XNU 커널에 존재하지 않음. 네이티브 지원은 Endpoint Security 프레임워크 기반 완전히 별도 구현이 필요하며, 현재는 **non-goal**로 문서화만 해둔다 |
| Kubernetes | `deploy/` + `internal/sensor/linux` 재사용 | Phase 9 목표 | Linux eBPF 백엔드를 privileged DaemonSet으로 배포, cgroup id로 파드 매핑 |

macOS를 "Linux와 동급의 두 번째 백엔드"로 다루지 않는 이유: Endpoint
Security는 Swift, entitlement, Apple 공증(notarization)이 필요한 별도
규모의 프로젝트다. 지금 아키텍처에 절반만 구현된 두 번째 백엔드를 넣는
것보다, Linux 백엔드를 제대로 만들고 macOS 사용자는 VM으로 우회하게
하는 편이 정직하고 실행 가능하다.

## Event Pipeline

```
bpf/*.bpf.c (kernel)
        │  BPF_MAP_TYPE_RINGBUF
        ▼
internal/sensor/linux (userspace, cilium/ebpf)
        │  internal/core.Event (Event Schema, SECURITY_MODEL.md)
        ▼
internal/taint (라벨 전파) → internal/policy (평가)
        │
        ▼
internal/enforce → ALLOW / AUDIT / DENY  +  구조화 로그
```

로컬 실행과 향후 Kubernetes DaemonSet 배포는 이 파이프라인을 그대로
공유한다. 배포 방식만 다르다 (단일 프로세스 vs 노드당 1개 DaemonSet +
cgroup→pod 매핑).

## 향후: 로컬 웹 대시보드 (스텁)

`internal/web/`은 Phase 9까지 자리만 확보해둔 빈 패키지다. 지금 설계를
확정하는 게 아니라, 나중에 이 컴포넌트를 실제로 만들 때 지켜야 할 방향만
남겨둔다.

- **AgentSight의 `frontend`+`controller` 구조를 그대로 따르지 않는다.**
  그쪽은 Next.js 프론트엔드 + Cloudflare Workers 기반 별도 SaaS
  백엔드(OAuth, relay, Postgres)까지 갖춘 멀티유저 제품이다. AgentTaint는
  로컬 단일 바이너리 정체성(위 "`run`이 보안 경계다" 절)을 유지하는 걸
  우선한다 — 클라우드 relay·멀티유저 기능은 이 스텁 단계에서 non-goal이다.
- 기본 방향은 `agenttaint`가 이미 만든 이벤트/정책 위반 로그를 로컬 HTTP
  서버로 띄워 브라우저에서 조회하는 것 — 별도 배포 없이 같은 바이너리
  안에서 `go:embed`된 정적 자산을 서빙하는 정도로 최소화한다.
- **선행 질문**: 지금 아키텍처에는 이벤트/Decision을 영속화하고 쿼리하는
  계층이 정의돼 있지 않다 (`internal/enforce`는 "구조화 로그"만 언급).
  웹 뷰어가 "조회"를 하려면 이 저장/쿼리 계층부터 먼저 설계해야 한다 —
  이 phase를 시작하는 사람이 가장 먼저 풀어야 할 문제다.
- 정확한 phase 번호·기술 스택·엔드포인트 설계는 이 스텁 단계에서
  확정하지 않는다. 착수 시점에 AGENTS.md의 "문서는 규범이다" 절차대로
  다시 논의한다.
