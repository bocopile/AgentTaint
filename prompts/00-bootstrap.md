# Phase 0 — Repository Bootstrap + Doctor

## Goal

Go 모듈과 `docs/ARCHITECTURE.md`에 정의된 디렉터리 골격을 만들고,
읽기 전용 환경 진단(`doctor`)과 명시적 단일 바이너리 실행(`run`)의
뼈대를 구현한다. eBPF, taint, policy는 이 Phase에 없다.

## Context

AgentTaint의 보안 경계는 `agenttaint run -- <COMMAND>`로 명시된
프로세스 트리다 (`docs/ARCHITECTURE.md`의 "`run`이 보안 경계다" 절).
`doctor`는 그 경계와 무관한 UX 편의 기능이며, 시스템 상태를 절대 바꾸지
않는 순수 진단 도구여야 한다 (같은 문서의 "doctor vs run" 절).

## Architecture

```
cmd/agenttaint/main.go
        │
   ┌────┴────┐
internal/env  internal/runner
(진단, 읽기전용) (명시적 exec)
```

## In Scope

1. `go.mod` 초기화, Go 모듈 구조 생성.
2. `docs/ARCHITECTURE.md`의 디렉터리 트리대로 골격 생성 (빈 패키지라도
   `doc.go`로 자리 확보: `internal/core`, `internal/sensor/linux`,
   `internal/sensor/darwin`, `internal/taint`, `internal/policy`,
   `internal/enforce`, `internal/env`, `internal/runner`).
3. `cmd/agenttaint/main.go` — `doctor`, `run` 서브커맨드 라우팅만.
4. `internal/env` (doctor):
   - OS 판별 (Linux/macOS), Linux면 커널 버전·BTF 존재 여부·BPF-LSM
     활성화 여부 점검. macOS면 "v0.1 미지원, Linux VM에서 실행하세요"
     출력.
   - 알려진 AI 에이전트 CLI 탐지 목록은 하드코딩하지 말고 설정/상수
     슬라이스로 분리 (최소 `claude`, `codex`).
     - PATH에서 `exec.LookPath`로 1차 탐지.
     - 못 찾으면 후보 경로 스캔 (탐지만, 아무것도 수정하지 않음):
       `~/.local/bin`, `~/go/bin`, `/opt/homebrew/bin`, `/usr/local/bin`,
       `/usr/bin`, `~/.nvm/versions/node/*/bin` (버전 와일드카드),
       `~/.volta/bin`, `~/.fnm/aliases/default/bin`, `~/.asdf/shims`,
       `~/.local/pipx/venvs/*/bin`.
   - 각 항목에 `found`, `path`, `version`, `available_in_path`를 채워
     사람이 읽는 텍스트와 `--json` 두 형식으로 출력.
   - `available_in_path: false`면 PATH를 고치지 말고 안내 문구만 출력
     (`docs/ARCHITECTURE.md`의 예시 출력 형식 참고).
5. `internal/runner` (run):
   - `agenttaint run [--bin <path>] -- <command...>`.
   - `--bin` 있으면 그 경로로 직접 exec.
   - 없으면 PATH에서 resolve, 실패 시 doctor와 동일한 후보 경로에서
     **이 한 번의 실행에 한해서만** 탐색 후 resolve. 어디서 찾았는지
     stderr에 로그. 세션 전체 PATH는 건드리지 않는다.
   - 표준입출력을 그대로 연결해서 자식 프로세스를 실행하기만 하면 된다
     (감시 기능 없음 — Phase 2에서 추가).

## Out of Scope

다음은 이 Phase에서 절대 구현하지 않는다.

- eBPF 프로그램, `bpf/` 아래 C 코드
- `internal/core`의 실제 Event 타입 정의 (Phase 1)
- taint propagation (Phase 3)
- policy 평가 (Phase 3)
- enforcement/blocking (Phase 4)
- Kubernetes 관련 어떤 것도 (`deploy/` 내용 채우기 포함)
- claude/codex를 `run`에서 특별 취급하는 코드 (run은 임의의 커맨드를
  동일하게 다뤄야 한다)

## Files

`go.mod`, `cmd/agenttaint/main.go`, `internal/env/*.go`,
`internal/runner/*.go`, 그리고 `docs/ARCHITECTURE.md` 트리의 나머지
패키지 디렉터리(빈 `doc.go`만).

## Technical Requirements

- Go 표준 라이브러리 우선. 이 Phase는 `cilium/ebpf` 의존성이 필요 없다.
- doctor 출력 구조체는 `internal/env` 안에 정의하고, 이후 다른 Phase가
  같은 타입을 재사용할 수 있게 export한다.

## Security Requirements

- `doctor`는 PATH, 환경변수, 파일시스템 어떤 것도 쓰지 않는다 (읽기
  전용). 테스트로 이를 증명한다 (아래 Acceptance Criteria 참고).
- `run`의 후보 경로 탐색은 그 실행 1회의 `exec` 대상 resolve에만
  쓰이고, 프로세스 전역/세션 PATH를 수정하지 않는다.

## Acceptance Criteria

- `go build ./...` 성공.
- `agenttaint doctor`가 개발 머신에서 claude(PATH 안)와 codex(PATH
  밖, nvm 경로)를 정확히 구분해서 보여준다.
- `agenttaint doctor` 실행 전후로 부모 셸의 `$PATH`가 바뀌지 않는다.
- `agenttaint run -- codex --version`이 PATH에 없어도 후보 경로 탐색으로
  성공한다.
- `agenttaint run -- claude`, `agenttaint run -- python3 --version`,
  `agenttaint run -- bash -c "echo ok"`가 모두 동일한 코드 경로로
  동작한다 (claude를 특별 취급하는 분기가 없다).

## Tests

- `internal/env`: PATH 안/밖 케이스를 mock/fixture로 구성한 단위 테스트.
- `internal/runner`: `--bin` 지정 케이스, PATH resolve 케이스, 후보
  경로 fallback 케이스.

## Documentation

- 새 문서를 만들지 않는다. `docs/ARCHITECTURE.md`가 이미 doctor/run
  설계를 담고 있다 — 구현하며 그 문서와 다른 결정을 내렸다면 AGENTS.md의
  "문서는 규범이다" 절차를 따른다.

## Required Final Report

AGENTS.md의 "Required Final Report" 형식을 그대로 사용한다.

## Stop Conditions

- 후보 경로 스캔 목록에 없는 새로운 버전 매니저(예: mise)를 추가하고
  싶어질 때 — 목록에 추가하는 건 괜찮지만 먼저 이유를 보고하고 진행한다.
- Phase 1의 Event 타입을 미리 정의하고 싶어질 때 — 하지 않는다, 멈추고
  보고한다.
