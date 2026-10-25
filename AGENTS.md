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

### 버전 고정 (Pinned Versions)

여기 적힌 숫자를 임의로 올리거나 내리지 않는다 — 바꿀 필요가 생기면 이유를
기록하고 이 절부터 갱신한다.

- **Go**: 1.26+ (`go.mod`의 `go` directive는 `1.26`). `cilium/ebpf`가
  요구하는 최소 버전(1.25) 이상을 만족해야 한다.
- **cilium/ebpf**: `v0.22.0` 고정. 버전을 올릴 때는 CHANGELOG에서 breaking
  change(특히 `bpf2go` 생성 코드 시그니처 변경) 여부를 먼저 확인한다.
- **clang/LLVM**: 최소 12+ (`bpf2go`가 요구하는 하한은 10이지만, CO-RE
  relocation과 엔디안 자동 판별이 12부터 안정적이다). `doctor`가 이 버전을
  확인 대상에 포함한다 (Phase 2 이후).
- **bpftool**: `vmlinux.h` 생성(`bpftool btf dump file
  /sys/kernel/btf/vmlinux format c`)에 필요. Ubuntu/Debian은
  `linux-tools-common` + `linux-tools-$(uname -r)`, Fedora/RHEL 계열은
  `bpftool` 패키지로 설치한다. Phase 2 착수 전 개발 환경에 반드시 있어야
  한다 (`docs/SUBORCHESTRATOR.md`가 이미 gap으로 기록한 항목).
- **커널 최소 버전**: BPF-LSM 훅은 5.7+부터 존재하지만 안정 동작 기준은
  **5.13+**로 잡는다. Ring buffer(`BPF_MAP_TYPE_RINGBUF`)는 5.8+ 필요.
  `doctor`(`internal/env`)는 이 숫자를 pass/fail 임계값으로 사용한다
  (자세한 내용은 `prompts/00-bootstrap.md`).

## Architecture Rules

1. `internal/core`는 플랫폼(Linux/macOS)을 알아서는 안 된다. Linux나 macOS
   패키지를 import하지 않는다. 이 규칙은 코드 리뷰와 lint(추후
   `depguard` 규칙)로 강제한다.
2. Linux(및 향후 macOS) 센서는 `internal/core`에 정의된 정규화된 Event만
   내보낸다. 센서 코드 안에 정책 로직을 두지 않는다.
3. Taint의 규범적 reference 의미와 관찰/audit 전파는 하나의 패키지
   (`internal/taint`)가 소유한다. Phase 4의 실제 pre-operation 집행에 필요한
   제한된 BPF kernel-state projection만 예외로 허용한다. projection은 성공한
   `FileOpen` 의미를 변경하지 않으며, 승인된 source `file_open` 시도를 보수적으로
   집행 상태에 반영할 수 있다. 새 label 의미나 일반 전파 엔진을 BPF·센서·CLI에
   복제하지 않는다.
4. Policy parsing, validation, canonical snapshot 및 일반 평가는 하나의 패키지
   (`internal/policy`)가 소유한다. Phase 4는 Go가 검증·컴파일한 snapshot 중 명시된
   제한 subset만 kernel에 전달할 수 있다. BPF 안에서 YAML parsing, rule 우선순위
   결정 또는 일반 policy evaluator를 구현하지 않는다.
5. CLI(`cmd/agenttaint`)는 보안 판단(allow/audit/deny)을 직접 내리지
   않는다. policy/taint 패키지를 호출만 한다.
6. eBPF(C) 코드는 최소한으로 유지한다. 복잡한 파싱·정책 준비는 유저스페이스
   Go 코드에서 한다. 승인된 Phase 4 subset에 한해 bounded state update와
   pre-operation sink decision을 허용하며, 커널 안에서 YAML parsing·정규식·일반
   정책 평가를 하지 않는다.

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

## Token-efficient Code Discovery

- 코드 발견은 연결된 OSS `codebase-memory-mcp` graph부터 사용한다: 좁은 `search_graph` → 필요한 경우 `trace_path` → 정확한 심볼의 `get_code_snippet` 순서로 조회한다.
- `file_pattern`, `path_filter`, `limit`으로 검색 범위를 제한한다. `bpf/vmlinux.h` 같은 생성 헤더 전체나 무관한 파일의 full source를 한 번에 반환하지 않는다.
- MCP 결과는 필요한 심볼명·파일·라인·본문만 추려서 사용한다. 응답 전체 JSON이나 원시 로그를 대화에 복사하지 않는다. 검색 결과가 비었거나 `Transport closed`, timeout, `project not found or not indexed` 등으로 쓸 수 없으면 현재 repository/worktree와 HEAD를 확인한 뒤 `rg`/`sed`로 해당 경로와 좁은 줄 범위만 조사한다. fallback 결과는 MCP가 확인한 결과와 구분하고 graph가 최신이라고 추정하지 않는다. `index_status=ready`는 서비스 상태일 뿐 코드가 현재 HEAD와 일치한다는 증거가 아니다. 인덱스 재생성은 외부 MCP 상태를 바꾸므로 명시 승인 없이 실행하지 않는다.
- `Repomix`는 구조 검색으로 해결되지 않는 인계용 묶음이 필요할 때만 파일 목록을 명시해 사용한다. 저장소 전체를 기본 입력으로 만들지 않는다.
- AGENTS.md, SECURITY_MODEL.md, policy/config, eBPF source와 acceptance assertion은 의미 손실이 있는 LLMLingua류 압축을 적용하지 않는다. 요약은 원문을 대체하지 않고 항상 원문 경로·근거를 함께 남긴다.

## Git / Worktree 작업 규칙

작업 기준은 항상 최신 `main`이다. `main` checkout은 기준선과 최종 통합에만
사용하고, 제품·문서 변경은 별도 branch와 worktree에서 수행한다. worktree는
같은 `.git` 저장소를 공유하므로 파일 작업은 격리하지만 권한·보안 경계는 아니다.

### 시작과 worktree 준비

1. 시작 시 저장소 루트, `git status --short --branch`, 현재 branch/HEAD,
   `git worktree list`를 확인한다. 기존 dirty 변경은 소유자를 식별할 때까지
   보존하고 stage·stash·reset·checkout으로 덮지 않는다.
2. `main` worktree가 clean일 때만 `git fetch origin` 후
   `git switch main` / `git pull --ff-only origin main`으로 최신 기준선을 맞춘다.
   dirty `main`을 자동 정리하지 않는다. clean baseline을 만들 수 없으면
   변경 작업을 시작하지 말고 충돌 상태를 보고한다.
3. 각 작업은 저장소 루트의 무시 경로 `.worktrees/<task-id>`에 고유 worktree를
   만들고, branch 이름은 `codex/<phase>-<task-id>-<slug>` 형식을 쓴다.
   새 worktree와 branch의 출발점은 2단계에서 동기화한 local `main`의 기록된
   commit이다.

```sh
git fetch origin
git worktree add -b codex/<phase>-<task-id>-<slug> \
  .worktrees/<task-id> main
```

이미 같은 branch나 worktree가 있으면 `-B`, 강제 교체, 제거를 하지 않는다.
기존 작업을 확인해 이어가거나 새 고유 ID를 사용한다.

### 동시 작업과 통합

- 병렬 writer마다 전용 worktree와 branch, task ID, 기준 commit, 명시적 파일
  소유 범위를 부여한다. 한 worktree에 둘 이상의 writer를 배정하지 않으며,
  동시에 같은 파일을 수정하지 않는다.
- Orchestrator는 병렬 작업 전에 worktree를 순차적으로 만들고, 의존성이 없는
  카드만 동시에 시작한다. 선행 카드 결과에 의존하는 카드는 선행 변경이 통합된
  뒤 시작한다.
- 병렬 task branch는 같은 `main` 기준 commit에서 만든다. 별도의
  `codex/<phase>-integration` worktree/branch도 같은 기준에서 만들고, 완료된
  task branch를 이 통합 branch에 순서대로 merge한다. 충돌 해결과 전체 변경 검토는
  통합 worktree에서 수행한다.
- 작업자는 완료 시 branch/HEAD, 변경 파일, 작업 단위 commit, 실행한 검사와
  미검증 항목을 보고한다. 통합 담당자는 각 변경과 기준선을 대조하고, 다른
  작업자의 파일을 임의로 되돌리거나 덮어쓰지 않는다.

### 검증, main merge, push

- 사용자가 테스트를 한 번에 수행하라고 지정한 경우 task별 전체 테스트를 중복
  실행하지 않는다. 모든 task branch를 통합 branch에 모은 뒤, 그 branch의 정확한
  HEAD에서 정해진 검증 묶음을 한 번 실행한다. Linux/BPF 검증은 guest에서도 같은
  commit과 source 상태인지 확인한다.
- 필수 검증, 독립 검토, diff 확인이 모두 통과하기 전에는 통합 branch를 `main`에
  merge하지 않는다. 실패·skip·환경 차단이 있으면 이를 성공으로 취급하지 않고
  `main`은 그대로 둔다. 수정은 작업 branch 또는 통합 branch에서 하고, 영향받은
  검증을 다시 실행한다.
- 검증이 통과하면 `main`이 여전히 통합 branch의 조상인지 확인한 뒤 main worktree에서
  `git merge --ff-only codex/<phase>-integration`으로 통합한다. 통합 중 main이
  전진해 fast-forward할 수 없으면 최신 main 기준 worktree를 다시 만들고 변경을
  재통합·재검증한다. force push나 무단 rebase로 이력을 덮지 않는다.
- task 단위 커밋은 해당 worktree에서 관련 경로만 stage하고 staged diff를 검토해
  conventional commit으로 남긴다. 전체 테스트가 batch gate로 정해졌으면 테스트
  전 task branch 커밋은 허용하지만, green 검증 전 main merge는 금지한다.
- push는 원격 저장소 변경이다. 사용자가 해당 branch 또는 batch의 push를 명시적으로
  요청한 경우에만 수행한다. 테스트 전에는 작업 branch만 push할 수 있고, `main` push는
  통합 테스트 통과와 main 반영 요청이 모두 확인된 뒤 수행한다. `main`을 직접 작업
  branch로 사용하지 않으며 force push, PR 생성, 원격 merge는 별도 요청 없이 하지 않는다.
- merge 완료 후 clean하고 merge된 작업의 worktree만 일반 `git worktree remove`로
  정리한다. dirty worktree, 미merge branch, 기존 사용자 작업은 정리하지 않는다.

### Codex 권한 요건

`git add`, `git commit`, `git worktree add/remove`는 저장소 파일뿐 아니라 `.git`
metadata를 쓴다. OS 파일 mode가 쓰기 가능이어도 Codex sandbox가 `.git`을 read-only로
지정하면 `index.lock` 생성 등이 `Operation not permitted`로 실패한다. `chmod`,
`sudo`, guest VM의 root 계정은 host Codex sandbox 경계를 바꾸지 않는다.

로컬 Codex 세션에서는 앱의 permission control에서 다음 범위만 갖는 사용자 지정
profile을 선택한다. 자세한 profile 동작은 [Codex permission profiles](https://developers.openai.com/codex/permissions)를
참고한다. `:workspace`의 보호 규칙은 `.git` read-only를 기본으로 할 수
있으므로 저장소 `.git` 및 `.worktrees` 쓰기를 명시적으로 허용해야 한다. 외부 push가
필요한 세션에만 GitHub network allowlist를 추가한다. 예시 설정은 사용자 Codex 설정에
적용하며 저장소에 자격증명이나 사용자 설정 파일을 추가하지 않는다.

```toml
default_permissions = "agenttaint-git"

# 기존 [features] 테이블이 있으면 새 테이블을 중복 선언하지 말고 이 값을 합친다.
[features]
network_proxy = true

[permissions.agenttaint-git]
extends = ":workspace"

[permissions.agenttaint-git.filesystem.":workspace_roots"]
"." = "write"
".git" = "write"
".worktrees" = "write"

[permissions.agenttaint-git.network]
enabled = true

[permissions.agenttaint-git.network.domains]
"github.com" = "allow"
"api.github.com" = "allow"
```

Codex 권한 profile은 legacy `sandbox_mode` 설정과 함께 쓰지 않는다. 사용자/조직의
관리 정책이 더 제한적이면 이를 우회하지 않는다. 승인 요청이 가능한 환경에서는
사용자 검토를 켜고, `git push`는 요청된 branch와 목적지를 확인한 뒤 실행한다.
현재 세션이 `.git` 쓰기나 네트워크를 허용하지 않으면 그 세션 안에서 우회하지 말고
권한 profile을 선택한 새 로컬 세션에서 작업을 이어간다.

Lima guest 실행은 별도 권한 범위다. Git 전용 profile에 `~/.lima` 전체나 모든
Unix socket을 추가하지 않는다. guest acceptance가 필요할 때에만 실제 오류에 나온
정확한 hostagent/SSH socket을 별도 profile에 추가하고, localhost/vsock 경로를
확인한다. VM 재생성이나 guest root 권한 변경을 host sandbox 해결책으로 취급하지 않는다.

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
