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
├── .github/workflows/ci.yml       # Phase 0: 비특권 portable Go 검증만
│
├── cmd/
│   └── agenttaint/
│       └── main.go               # CLI: doctor / run 서브커맨드
│
├── internal/
│   ├── core/                     # 플랫폼 독립. Linux/macOS 패키지를 import 금지
│   │   ├── event.go              #   Event 타입 (ProcessExec/Fork/Exit, FileOpen, NetworkConnect ...)
│   │   ├── process.go            #   ProcessID, ProcessGroupID, SessionID
│   │   ├── resource.go           #   FileResource, NetworkDestination
│   │   └── label.go              #   Label 타입 (SECRET 등)
│   │
│   ├── sensor/
│   │   ├── linux/                # Linux eBPF observer와 raw event decode/normalization
│   │   │   ├── doc.go, sensor.go, raw.go
│   │   │   ├── process_bpfel.go / process_bpfel.o  [process.bpf.c 생성]
│   │   │   └── process_bpfeb.go / process_bpfeb.o  [process.bpf.c 생성]
│   │   └── darwin/                # Phase 5+ 자리만 확보. v0.1은 미구현 (VM 우회)
│   │
│   ├── taint/                    # Canonical Go reference propagation (Source→Label→Propagation)
│   │   └── engine.go
│   │
│   ├── policy/                   # 정책 파싱·평가 (Sink→Decision)
│   │   ├── model.go
│   │   ├── parser.go
│   │   ├── evaluator.go
│   │   └── kernel.go             # 제한된 enforcement projection 준비
│   │
│   ├── enforce/                  # Go policy snapshot → bounded kernel projection / lifecycle
│   │   ├── doc.go, enforce_linux.go, enforce_unsupported.go
│   │   └── enforce_bpfel.go / enforce_bpfel.o  [생성]
│   │
│   ├── web/                      # Phase 9(로컬 웹 대시보드) 전까지 자리만 확보 — 미구현
│   │   └── doc.go
│   │
│   ├── env/                      # 기본 doctor는 정적 조회; Phase 2 명시 toolchain probe는 별도 실행
│   │   └── doctor.go
│   │
│   └── runner/                   # run: `--` 뒤 커맨드 하나만 명시적 resolve 후 실행
│       └── runner.go
│
├── bpf/                          # eBPF C 소스 (CO-RE)
│   ├── process.bpf.c             # observer object 조립; process tracepoint + file/network include
│   ├── file.bpf.c                # process.bpf.c에 포함; file syscall tracepoint + getname_flags capture
│   ├── network.bpf.c             # process.bpf.c에 포함되는 connect tracepoint observer
│   ├── enforce.bpf.c             # 별도 BPF-LSM object: bounded source projection + pre-op sink decision
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
├── .codex/                        # Codex 프로젝트 전용 agent 정의만
│   └── agents/*.toml
│
├── .agents/skills/                # AgentTaint 프로젝트 전용 재사용 절차
│   └── <skill>/
│       ├── SKILL.md
│       └── references/             # 정적 참고자료; 채택한 upstream 원문/라이선스 포함 가능
│
└── docs/
    ├── ARCHITECTURE.md           # 이 문서
    ├── THREAT_MODEL.md
    ├── SECURITY_MODEL.md
    └── ROADMAP.md
```

이 트리는 지금 저장소에 전부 만들어져 있지 않다 — `prompts/00-bootstrap.md`
가 이 문서를 참조해서 실제로 생성한다. 새 파일을 이 트리 밖에 만들지
않는다. 단, 트리에 표시된 Go 패키지 안의 `doc.go`와 `*_test.go`는 허용한다.
Phase 0에서는 위 `Makefile`과 `.github/workflows/ci.yml`을 빌드·정적 검사·
portable 테스트 진입점으로 생성할 수 있다. 이 허용은 새 제품 패키지,
privileged BPF CI, 스킬 설치 경로 또는 미래 Phase 구현을 허용하지 않는다.

Codex 프로젝트 설정은 아래 범위만 저장소에 둘 수 있다. `.codex/agents/*.toml`은
프로젝트 agent 역할 정의, `.agents/skills/<skill>/SKILL.md`와 해당 skill의
`references/`는 재사용 절차와 정적 참고자료다. 선택 채택한 외부 skill은 provenance와
license를 기록한 뒤, 고정 upstream 원문 및 필요한 license/checklist를 해당 skill의
`references/upstream/` 아래 정적 파일로 보관할 수 있다. 이 디렉터리에는 발견 가능한
`SKILL.md`, 실행 스크립트, 전체 skill catalog를 두지 않는다. 이 제한된 예외는
agent 지침을 OS 수준 권한 격리로 간주하거나 전역 설정·MCP·hook·플러그인·임의 추가
경로를 허용하지 않는다. 더 넓은 경로가 필요하면 이 문서의 허용 범위를 먼저 검토한다.
저장소 agent는 현재 작업의 Phase·파일 범위·보안 의미를 따라야 하며, 검토 역할의
“읽기 전용” 지침만으로 실제 파일 권한이 제한된다고 주장하지 않는다.

Phase 2에는 위 `bpf/`의 C·헤더 및 생성 `vmlinux.h`,
`internal/sensor/linux/sensor.go`와 같은 패키지 안의 실제 `bpf2go` 생성
Go·embed object(`.go`/`.o`) 쌍, 기존 `test/fixtures/`·`test/integration/`의
해당 Phase fixture·테스트를 허용한다. 현재 sensor/enforcement 생성 ident,
architecture/endian suffix와 입력·생성 명령은 `docs/IMPLEMENTATION_MASTER_PLAN.md`
ENV-02 기록에 대조한다. generator ident·입력·출력 경로를 바꾸기 전에는 실제
명령과 생성물을 확인해 그 기록부터 갱신한다. 이름·API를 추측하지 않는다.
기존 `Makefile`의 빌드·생성·검증 진입점과 기존 `.github/workflows/ci.yml`의
비특권 portable 검사 수정도 허용한다. 새 패키지·스킬 설치 경로·별도 workflow나
privileged CI는 이 허용에 포함하지 않는다. 위에서 명시한 `.codex/agents/`와
`.agents/skills/`만 Codex 프로젝트 설정 경로 예외이며, Phase 구현 경로를 넓히지 않는다.

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
- Phase 4 enforcement는 이 Go reference/audit 경로와 별도인 제한된 kernel
  projection을 사용한다. `internal/policy`가 canonical snapshot을 검증하고
  `internal/enforce`가 지원 subset을 고정 ABI로 준비한다. BPF는 run scope,
  source matcher, 단일 `SECRET` 상태, 허용된 IPv4 CIDR/port 결정을 위한 bounded
  state만 보유한다. 일반 정책 parser/evaluator는 커널에 두지 않는다.
- Go reference는 성공한 completed `FileOpen` 이벤트에만 label을 준다. 동기
  `file_open` 집행 hook은 해당 source 접근 시도에서 보수적으로 kernel state를
  표시할 수 있으므로, 나중의 open 실패 뒤에도 kernel state는 tainted일 수 있다.
  이 집행 projection은 Go audit 결과를 덮어쓰거나 성공한 open으로 재기록하지
  않는다. PID 생애, fork/thread 전파, 동시 source/sink 순서, map 실패 동작은
  Phase 4 acceptance tests로 검증하기 전까지 지원 보장으로 표현하지 않는다.
- 초기 block profile의 run membership은 observer의 singleton `process_scope`를
  재사용하지 않는다. 전용 cgroup v2 scope에 target과 descendants를 두고,
  cgroup ID와 run별 preallocated sticky fault state를 동기 hook에서 확인하는 설계다.
  Target은 cgroup control files를 수정할 수 없어야 한다. 필요한 cgroup 권한/격리와
  BPF helper·hook 조합이 준비되지 않으면 block launch를 거부한다. supervisor 비정상
  종료 시 link 소멸과 남은 descendant를 안전하게 정리하는 보장은 아직 없으며,
  이를 검증하기 전 해당 장애 경로의 보호를 주장하지 않는다.
- 모든 필수 hook attach, scope/policy initialization, map capacity 및 지원 subset
  검사가 target 시작 전에 성공해야 한다. 필수 capability나 상태 갱신을 보장할 수
  없으면 block 요청은 실패한다. 이벤트 ring-buffer 손실은 집행 state와 별도로
  보고하며, audit event 처리가 kernel decision을 지연시키지 않는다.

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
| 기본 `doctor` | `internal/env` | 정적 진단: 커널/BTF/BPF-LSM 여부, 알려진 에이전트 CLI(claude, codex 등)의 경로 리포트 | **아니오** — 순수 조회 |
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

### Phase 0 진단·실행 계약

- `doctor`는 발견한 실행 파일을 **실행하지 않는다**. `--version`도 호출하지
  않고 `version: unknown`과 `version_reason: not_executed_read_only`를 보고한다.
  후보 실행 파일의 존재·실행 권한 검사는 그 파일의 신뢰성을 보증하지 않는다.
- Linux 진단은 `/proc/sys/kernel/osrelease`, `/sys/kernel/btf/vmlinux`,
  `/sys/kernel/security/lsm`을 정적으로 읽거나 존재·읽기 가능 여부를 확인한다.
  BPF load/attach, 모듈 로드, mount, 외부 명령 실행은 하지 않는다.
  커널 버전 5.13 미만은 안정 동작 기준 미달, 5.8 미만은 ring buffer 미지원으로
  표시한다. LSM 목록에 `bpf`가 있어야 활성화 확인으로 표시한다. 경로 부재
  (`missing`), 접근 거부(`permission_denied`), 파싱 실패·기타 판정 불가
  (`unknown`)를 구분하며, 확인 불가를 기능 부재로 단정하지 않는다.
  정적 점검 통과는 실제 센서나 차단 기능이 검증됐다는 뜻이 아니다.
- 바이너리 선택 우선순위는 명시적 `--bin` → PATH → Phase 0 프롬프트의
  후보 디렉터리 목록 순서다. 각 glob 계열 내부는 사전식 경로 순서로 검사하고
  첫 실행 가능한 파일을 선택한다. 의미론적 최신 버전 선택을 하지 않는다.
  slash를 포함하는 명시적 커맨드 경로 또는 `--bin`이 잘못됐으면 오류로
  종료한다. `exec.ErrDot` 같은 안전하지 않은 현재 디렉터리 탐지는 거부하며,
  이 경우 후보 경로로 우회하지 않는다. PATH fallback은 단순 미발견에만
  적용한다. `doctor`도 같은 안전한 탐지 순서와 실패 이유를 보고한다.
- `run`은 argv를 shell 문자열로 합치지 않고, 환경변수·틸드·와일드카드를
  target 인자에서 임의로 확장하지 않는다. 후보 목록의 홈 경로·glob 해석만
  Go 코드로 수행한다. 자식 stdin/stdout/stderr와 정상 종료 코드를 보존한다.
  진단·선택 경로·**pass-through 실행에는 감시와 차단이 활성화되지 않는다는
  안내**는 stderr로 보낸다.
  target stdout을 AgentTaint 출력으로 오염시키지 않으며 PATH를 수정하지 않는다.

### Phase 2 확장 계약 — 구현 및 검증 범위

아래는 Phase 2의 구현 계약이다. 구현·guest 검증 범위와 남은 한계는
`docs/IMPLEMENTATION_MASTER_PLAN.md` 12.10절에 기록한다. Phase 0의 기본
`doctor`·무보호 `run` 동작은 그대로 유지한다.

- 관찰 실행은 대상 stdin/stdout/stderr를 보존한다. `run --observe`에는
  정책 snapshot을 지정하는 `--policy PATH`와 별도 감사 파일을 명시하는
  `--audit-file PATH`가 필수다. 정책은 target 시작 전에 로드한다. 정규화한
  core event와 audit/deny 판정의 derived `policy_violation`을 파일에 JSON Lines로
  기록하며 대상 stdout/stderr로 감사 이벤트를 보내거나 실패 시 우회 출력하지 않는다.
  Phase 3 `deny`는 post-event verdict이고 실제 operation을 차단하지 않는다.
  AgentTaint 자체의 짧은 오류 진단과 감사 이벤트 스트림은 구분한다.
- 감사 파일은 새 일반 파일로 배타 생성하고 기존 파일 덮어쓰기·append·symlink
  추적·FIFO/장치 사용을 거부한다. 생성 mode는 `0600`이며 실제 소유자·실효 권한과
  부모 경로의 안전성을 확인한다. 검사와 생성 사이의 교체 경합, 대상과의 권한 경계,
  저장 공간 부족·쓰기 실패·크기 상한 도달의 동작은 검증 대상이다. 현재 writer는
  JSONL을 최대 64 MiB로 제한하고 marshal/write/short-write·close 오류를 sticky
  quality failure로 기록해 run을 incomplete/error로 반환한다. 초기 준비 실패면
  target을 시작하지 않고 새 audit 파일을 정리한다. target이 시작된 뒤 발생한 audit
  오류는 자동 kill이나 stdout/stderr fallback을 일으키지 않으며, 이미 기록된 부분
  파일은 보존된다. ENOSPC 반환 writer, short write, 정확한 64 MiB counter 경계와
  초과 write 거부는 failure-injection으로 검증했다. 실제 디스크 고갈, close 실패,
  end-to-end 64 MiB 파일 생성은 `not_tested`이며 정상·완전한 audit으로 표시하지 않는다.
  이미 발생한 target의 효과는 되돌릴 수 없으며, 감사 오류를 이유로 Phase 2에
  자동 보안 kill/차단을 추가하지 않는다.
- root supervisor는 `--run-as UID:GID`로 비특권 target identity를 명시해야 한다.
  UID 또는 GID가 0이면 거부한다. root supervisor가 지정한 target은 supplementary
  groups를 비우며, 비root supervisor target은 기존 groups를 상속한다. real/effective/
  saved IDs와 capability가 target exec 전에 검증된다. `no_new_privs`를 target
  exec 전에 적용한다. 비root supervisor는 자기 effective UID/GID로만 실행할 수
  있다. 현재 인터페이스는 추가 application FD 전달을 지원하지 않으며 FD 0~2만
  target에 전달한다. helper의 readiness와 정확한 one-time launch permit가 확인되기
  전에는 target을 exec하지 않는다.
- 관찰 target의 Ctrl-C/SIGTERM 취소는 target process group에 전달된다. root target이
  끝나도 관측된 자손이 남으면 observer를 유지한다. 5초 drain 한도를 넘긴 자손은
  coverage를 incomplete로 표시하지만 자동 종료하지 않으며, 관측된 생애가 끝날 때까지
  감시를 유지한다. 이미 setsid로 분리된 자손에게 process-group 취소가 전달된다는
  보장은 없다.
- 기본 `doctor`와 AI CLI 탐지는 계속 외부 파일을 실행하지 않는다. opt-in
  `doctor [--json] --probe-toolchain --tool NAME=/absolute/path [...]`만 명시된
  toolchain 실행을 허용한다. 허용 이름은 `clang`, `llvm-strip`, `bpftool`이며,
  적어도 하나를 지정하고 같은 이름은 한 번만 쓸 수 있다. 입력하지 않은 도구는
  PATH나 프로젝트에서 찾지 않는다. 고정 argv는 각각 `--version`, `--version`,
  `version`이다. 실행 파일은 최대 5초 동안, stdout/stderr 각각 최대 8 KiB를
  보관하며 빈 environment와 `/` working directory를 사용한다. shell 실행, PATH
  검색, 설치, 권한 상승, mount, BPF load/attach는 하지 않는다. 호출자가 명시한
  경로는 해당 파일을 신뢰하고 실행을 승인한 것으로 취급한다. 절대 경로·일반 파일·
  실행 권한만으로 파일의 신뢰성을 증명하지 않으며, probe는 임의 부작용을 격리하거나
  방지하지 않는다. timeout은 실행 중인 직접 자식에만 적용되며 자손이나 filesystem/
  network 부작용을 격리하지 않는다. JSON 결과는 `toolchain_probes`에 도구별
  path/status/version을 기록한다. clang/llvm-strip은 LLVM 12 미만이면
  `below_minimum`을 보고하고 명령은 실패 code를 반환한다. timeout, 출력 초과,
  실행 오류와 인식할 수 없는 version도 구분해 실패 code를 반환한다. 보고된 version이나
  성공 종료는 BPF target, BTF, verifier 또는 runtime capability를 증명하지 않는다.
  AGENTS의 고정 버전과 기본 진단 임계값은 변경하지 않는다.

## 플랫폼 백엔드

코어 모델은 플랫폼 중립이지만, **v0.1에서 실제로 구현하는 백엔드는
Linux(eBPF) 하나뿐**이다. 다른 플랫폼은 방향성만 문서화한다 — 지금
구현 대상이 아니다.

| 플랫폼 | 패키지 | 상태 | 접근 방식 |
|---|---|---|---|
| Linux | `internal/sensor/linux` | **v0.1 구현 대상** | eBPF (tracepoint: fork/exec/exit, LSM: file_open/socket_connect) |
| macOS | `internal/sensor/darwin` | v0.1은 미지원 — Linux VM(Lima/Colima/OrbStack) 안에서 실행. 디렉터리 자리만 확보, Phase 5 전까지 구현하지 않는다 | eBPF는 XNU 커널에 존재하지 않음. 네이티브 지원은 Endpoint Security 프레임워크 기반 완전히 별도 구현이 필요하며, 현재는 **non-goal**로 문서화만 해둔다 |
| Kubernetes | `deploy/` + `internal/sensor/linux` 재사용 | Phase 7 목표 | Linux eBPF 백엔드를 privileged DaemonSet으로 배포, cgroup id로 파드 매핑 |

macOS를 "Linux와 동급의 두 번째 백엔드"로 다루지 않는 이유: Endpoint
Security는 C API이며 Swift가 필수 언어는 아니다. 네이티브 구현에는
entitlement·서명·배포 및 공증(notarization) 요건을 별도로 검토해야 한다.
이는 Linux 백엔드와 다른 규모의 프로젝트다. 지금 아키텍처에 절반만 구현된 두 번째 백엔드를 넣는
것보다, Linux 백엔드를 제대로 만들고 macOS 사용자는 VM으로 우회하게
하는 편이 정직하고 실행 가능하다.

## Observer와 Enforcement 경로

```
Observer: bpf/process.bpf.c (+ file.bpf.c, network.bpf.c include)
        │  process_bpf*.o / BPF_MAP_TYPE_RINGBUF
        ▼
internal/sensor/linux → internal/core.Event → internal/taint → internal/policy

Enforcement: bpf/enforce.bpf.c → internal/enforce/enforce_bpfel*.o
        ├─ BPF-LSM source hook: bounded kernel-state projection
        └─ BPF-LSM sink hooks: supported pre-operation decision
```

Observer object는 `internal/sensor/linux/doc.go`의 `go:generate`가
`bpf/process.bpf.c`를 입력으로 생성한다. `process.bpf.c`는 `file.bpf.c`와
`network.bpf.c`를 include하므로 이 파일들은 현재 별도 object가 아니다.
Enforcement object는 `internal/enforce/doc.go`의 별도 `go:generate`가
`bpf/enforce.bpf.c`에서 생성한다. 따라서 file/connect 관찰용 syscall
tracepoint와 active BPF-LSM enforcement hook의 source 파일·생성 object를
혼동하지 않는다. syscall-entry nofault path copy가 실패한 경우 observer는
`getname_flags` kprobe/kretprobe에서 커널이 실제 복사한 raw pathname을 보완
수집한다. probe를 attach할 수 없으면 observer 시작을 실패 처리한다. 로컬
실행과 향후 Kubernetes DaemonSet 배포는 이 경로를 공유하며 배포 방식만 다르다.

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

## 후속 실행 보호 profile과 로컬 증거 화면 — 설계 계약

process 접근 사실과 별도의 run-scoped 보수적 집행 상태를 구분한다. taint/policy의
의미 소유 경계는 유지하고 실제 동기 gate·격리 제공자·외부 엔진 선택은 비교 실험과
G2 결정 이후 확정한다. 새 제품 패키지나 현재 CLI/schema 변경을 이 절만으로 허용하지 않는다.

실험은 기존 Linux arm64 기준, 합성 source/수신기, 실제 영향·allow 대조군과 cleanup을
사용한다. 후보 설치/특권 동작은 지정 실험 카드와 환경 권한 범위에 한정한다. 기존
acceptance를 새 run-wide/기존 연결/원격 source 보장의 근거로 재사용하지 않는다.

읽기 전용 로컬 증거 화면의 선행조건은 검증된 로컬 slice, Event/Decision/effect의
저장·조회·보존·권한 계약, 상세화된 Phase 9 prompt와 독립 보안 검토다. 이 조건을
충족하면 Kubernetes·Agent Feedback 구현 이전에도 시작할 수 있다. UI는 정책을
평가하거나 label을 해제하지 않으며 verdict, actual effect, audit coverage,
enforcement health를 분리한다. 이 순서 예외는 다른 플랫폼/Phase의 완료를 뜻하지 않는다.
