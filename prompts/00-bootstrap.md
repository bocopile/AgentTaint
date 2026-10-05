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
     - 커널 버전 판정 임계값(AGENTS.md "버전 고정" 절과 동일): 5.13
       미만이면 "BPF-LSM 불안정/미지원 가능성" 경고, 5.8 미만이면
       ring buffer 자체가 없어 "미지원"으로 표시한다.
     - 커널 버전은 `/proc/sys/kernel/osrelease`에서 읽고, BTF는
       `/sys/kernel/btf/vmlinux`의 존재·읽기 가능 여부를 확인한다.
       BPF-LSM 활성화는 `/sys/kernel/security/lsm`의 쉼표 구분 목록에
       정확한 `bpf` 항목이 있는지로 확인한다. 경로 부재(`missing`),
       접근 거부(`permission_denied`), 파싱 실패 등 판정 불가(`unknown`)를
       구분한다. 읽지 못한 LSM 목록을 "BPF-LSM 비활성"으로 단정하지 않는다.
     - BPF load/attach, 모듈 로드, mount, `uname` 등 외부 명령은 실행하지
       않는다. 정적 진단 통과를 실제 BPF 작동·보호 준비 완료로 표현하지 않는다.
   - 알려진 AI 에이전트 CLI 탐지 목록은 하드코딩하지 말고 설정/상수
     슬라이스로 분리 (최소 `claude`, `codex`).
     - PATH에서 `exec.LookPath`로 1차 탐지.
     - 단순 미발견이면 아래 순서로 후보 경로 스캔 (탐지만, 아무것도
       수정하지 않음):
       `~/.local/bin`, `~/go/bin`, `/opt/homebrew/bin`, `/usr/local/bin`,
       `/usr/bin`, `~/.nvm/versions/node/*/bin` (버전 와일드카드),
       `~/.volta/bin`, `~/.fnm/aliases/default/bin`, `~/.asdf/shims`,
       `~/.local/pipx/venvs/*/bin`.
     - 각 glob 계열 내부는 사전식 경로 순서로 검사하고 첫 실행 가능한
       파일을 선택한다. 최신 Node 등의 의미론적 버전 선택을 하지 않는다.
       디렉터리·실행 권한 없는 파일은 선택하지 않는다. `exec.ErrDot`은
       안전하지 않은 현재 디렉터리 탐지로 보고하고 fallback하지 않는다.
   - 각 항목에 `found`, `path`, `version`, `available_in_path`를 채워
     사람이 읽는 텍스트와 `--json` 두 형식으로 출력.
     - 발견한 바이너리는 `--version`을 포함해 **어떤 인자로도 실행하지
       않는다**. `version: unknown`,
       `version_reason: not_executed_read_only`를 출력한다. 발견 여부와
       버전 미수집은 별개이며, 파일 발견을 그 파일의 신뢰성으로 표현하지 않는다.
   - `available_in_path: false`면 PATH를 고치지 말고 안내 문구만 출력
     (`docs/ARCHITECTURE.md`의 예시 출력 형식 참고).
5. `internal/runner` (run):
   - `agenttaint run [--bin <path>] -- <command...>`.
   - `--bin` 있으면 그 명시적 파일 경로를 검사하고 직접 실행한다. 잘못된
     경로·디렉터리·실행 불가 파일이면 오류로 끝내고 PATH로 대체하지 않는다.
   - 없으면 PATH에서 resolve, 단순 미발견 시 doctor와 동일한 후보 경로에서
     **이 한 번의 실행에 한해서만** 탐색 후 resolve. 어디서 찾았는지
     stderr에 로그. 세션 전체 PATH는 건드리지 않는다.
   - 커맨드가 slash를 포함하면 명시적 경로로 취급하고 실패 시 fallback하지
     않는다. PATH 조회의 `exec.ErrDot`도 오류로 끝내며 후보 탐색으로 우회하지
     않는다. `--bin` → PATH → 후보 목록의 우선순위를 테스트한다.
   - argv를 shell 문자열로 합치지 않는다. target 인자의 환경변수·틸드·
     와일드카드 보간을 하지 않는다. 후보 목록의 홈·glob 처리만 Go 코드로 한다.
   - 표준입출력을 그대로 연결하고 target의 정상 종료 코드를 보존한다.
     실행 전 stderr에 "Phase 0: 감시·차단 없음" 안내를 출력하며 AgentTaint
     자체 진단을 target stdout에 섞지 않는다 (감시는 Phase 2 이후).
6. `Makefile`, `.github/workflows/ci.yml`: portable Go build/vet/test만.
   CI는 읽기 최소 권한, 검토한 full commit SHA 고정 action, 비밀정보 없는
   비특권 환경을 사용한다. Linux BPF job·배포·self-hosted runner 구축은 제외한다.

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
패키지 디렉터리(빈 `doc.go`만). 해당 Go 패키지의 `*_test.go`,
`Makefile`, `.github/workflows/ci.yml`은 이 Phase의 검증 파일로 허용한다.

## Technical Requirements

- Go 표준 라이브러리 우선. 이 Phase는 `cilium/ebpf` 의존성이 필요 없다.
- doctor 출력 구조체는 `internal/env` 안에 정의하고, 이후 다른 Phase가
  같은 타입을 재사용할 수 있게 export한다.

## Security Requirements

- `doctor`는 PATH, 환경변수, 파일시스템 어떤 것도 쓰지 않는다 (읽기
  전용). 발견한 프로그램·shell을 실행하지 않는다. 테스트와 코드 검토로
  이를 확인한다 (아래 Acceptance Criteria 참고).
- `run`의 후보 경로 탐색은 그 실행 1회의 `exec` 대상 resolve에만
  쓰이고, 프로세스 전역/세션 PATH를 수정하지 않는다.

## Acceptance Criteria

- `gofmt`, `go vet ./...`, `go build ./...`, `go test ./...` 성공.
- 임시 fixture로 PATH·홈·후보 경로를 통제하여 PATH 안/밖과 미발견을 정확히
  구분한다. 개인 머신에 설치된 claude/codex의 위치에 합격 여부를 의존하지 않는다.
- 실행되면 marker 파일을 쓰는 가짜 바이너리를 발견해도 `doctor`가 marker를
  생성하지 않으며 버전 미수집 이유를 출력한다. 부모 셸 PATH 불변만을
  읽기 전용의 증거로 삼지 않고, 프로세스 환경 불변·fixture 파일 불변·추가
  실행 경로 부재를 함께 확인한다.
- Linux 정적 진단은 파일 내용·부재·접근 거부·잘못된 버전을 fixture 또는
  읽기 함수 대역으로 재현해 구분한다. 실제 Linux에서 실행하지 않았으면
  Linux 실환경 검증 완료라고 보고하지 않는다.
- runner의 `--bin`, PATH, 후보 경로, 다수 glob 후보 선택이 결정적이고,
  명시적 잘못된 경로와 `exec.ErrDot`은 target을 실행하지 않고 실패한다.
- 임의 이름의 fixture 커맨드로 공백·shell 특수문자를 포함한 argv, stdin,
  stdout/stderr, 정상 종료 코드(0 및 비영 값)의 보존을 검증한다.
  `claude`/`codex`를 runner에서 특별 취급하는 분기가 없어야 한다.
- runner의 무보호 안내와 진단은 stderr에만 출력되며 target stdout은
  정확히 보존된다. macOS에서는 Linux 보호 기능 미지원을 명시한다.

## Tests

- `internal/env`: PATH 안/밖 케이스를 mock/fixture로 구성한 단위 테스트.
- `internal/env`: 실행 금지·버전 unknown·환경/파일 불변·Linux 정적 진단 테스트.
- `internal/runner`: `--bin` 지정 케이스, PATH resolve 케이스, 후보
  경로 fallback·glob 순서·명시 경로 오류·`exec.ErrDot` 거부 케이스.
- CLI/runner: argv·표준입출력·정상 종료 코드·무보호 안내의 통합 테스트.

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
