# AgentTaint 개발 환경과 구현 작업 명세

작성일: 2026-10-02 KST

최초 분석 기준: `bocopile/AgentTaint`의 `5cc919dadb0a86ea5ec00c794ed07918ccc42d9a`

상태: **2026-10-03 전체 작업을 37개 상위 작업과 실행 카드로 정리하고 순차 실행 준비를 시작했다. Phase 0/1의 5개 작업은 로컬 수용 검증 완료, PRE-01/02와 ENV-01은 일부 진행이다. 후속 “진행” 승인에 따라 G1/G2/G4의 제한된 Phase 2 문서 계약을 반영했다. 제품 기능 구현 완료는 아니다. 기존 Linux VM은 Stopped이며 현재 세션에서 시작에 필요한 경로 쓰기 권한이 없어 guest 진단은 미실행이다. 남은 권한·구현 조건은 12.12절에 기록했다. Linux runtime·원격 CI·외부 스킬 설치·Phase 2 이후 구현은 아직 완료하지 않았다.**

이 문서는 제품 철학 분석, macOS 사용 가능성, `addyosmani/agent-skills` 도입 검토, 아키텍처, 언어, 디렉터리, 작업 목록, 검증 기준과 사용자 결정 사항을 한 곳에 모은다. 다른 AI가 새로운 세션에서 읽고 현재 작업을 식별할 수 있도록 작성했다. 최초 분석 당시에는 제품 코드·Go 모듈이 없었다. **현재 실행 상태는 12.10절 기록이 기준이며, 2절의 환경·저장소 조사는 최초 분석 시점의 snapshot이다.**

핵심 결론은 다음과 같다.

- Mac에서 문서 작성, 스킬 사용, Go 코어·CLI 개발은 가능하다. Linux eBPF 센서와 차단 기능은 Linux VM 안에서 빌드하고 검증해야 한다.
- Mac 호스트에서 실행 중인 AI를 Linux VM의 eBPF로 직접 감시할 수는 없다. 보호하려는 AI와 도구도 VM 안에서 실행해야 한다.
- `addyosmani/agent-skills`는 선택적인 개발 지원 도구다. AgentTaint 실행 의존성이 아니며 별도 MCP 서버가 필수인 것도 아니다.
- 기존 Go + C/CO-RE 스택과 Linux 우선 순서는 유지한다. 초기 IFC 범위를 확대하거나 커널에 동기 라벨 전파를 도입하는 변경은 별도 결정이 필요하다.
- 우선 필요한 것은 기능 수보다 보호 범위, 상태 전이, 장애 의미, Linux 검증 환경의 명확화다.

후속 요청 반영: 사용자가 적절한 안을 AI가 선택해 넣도록 요청했다. 이에 따라 일반적인 제품·도구 선택을 사용자 응답 대기에서 해제했다. 이 문서의 기본 경로는 **기존 Lima 재사용 검토 → Mac 개발·Linux guest 실행 → 제한된 Linux MVP 검증 → 일반 AI 데이터 전달 경로의 보호 검증**이다. 스택은 Go+C/CO-RE를 유지하고 외부 스킬은 프로젝트 단위로 세 개부터 선택 도입한다. 커널 동작은 선택만으로 증명되지 않으므로 구현 전 실험·검증 항목으로 관리한다.

추가 누락 검토 반영: 시작 전 입력·열린 파일의 경계, 이벤트 인과 순서, 실행 종료와 잔류 자식, 권한 재획득, 정책 snapshot, 환경 증거의 유효 범위, 격리 CI, 테스트 양성 대조군과 작업 인계 기준을 보강했다. 기능·플랫폼을 추가한 것이 아니라 기존 ENV/P0~P4/V-01의 완료 조건을 구체화했다. 추가 내용도 실제 구현·규범 수정 완료를 뜻하지 않는다.

유사 프로젝트 후속 조사: **16절에 26개 비교 항목과 BP-01~26 구현 체크리스트를 추가했다.** ActPlane·CamFlow·Flume을 확인하여 기존 “IFC 유사 구현이 사실상 없다”는 설명의 정정 근거를 기록했다. source 확인·공식 문서·설계 권고·실행 미검증을 구분하며, 이번 조사는 기존 Phase 완료 상태나 규범을 변경하지 않는다.

빠른 이동:

- [현재 환경과 조사 결과](#2-조사-결과와-현재-상태)
- [제품 철학과 보호 범위](#3-제품-철학과-보호-범위)
- [사용자 결정 목록](#4-사용자-결정-목록)
- [macOS와 Linux VM](#5-macos-지원과-개발-환경-계획)
- [언어와 아키텍처](#6-언어와-구성요소-경계)
- [데이터와 보안 계약](#7-데이터와-보안-계약-초안)
- [agent-skills 설치 판단](#8-addyosmani-agent-skills-도입-판단)
- [스킬과 서브에이전트](#9-프로젝트-전용-스킬과-서브에이전트)
- [MCP 구성](#10-mcp와-개발-도구-경계)
- [디렉터리 구조](#11-목표-디렉터리-구조)
- [전체 작업 목록](#12-전체-작업-목록과-단계별-실행-계약)
- [37개 작업의 실행 카드와 순서](#1211-전체-작업-카드와-순차-실행-순서)
- [현재 실행 게이트와 재개 절차](#1212-현재-실행-게이트와-재개-절차)
- [테스트와 증거](#13-테스트와-완료-증거)
- [다음 AI 실행 템플릿](#14-다음-ai에게-전달할-실행-지시)
- [근거 자료](#15-근거-자료와-사실-확인-범위)
- [유사 프로젝트 비교 조사와 적용 설계](#16-유사-프로젝트-베스트-프랙티스-조사)

## 1 문서 사용 규칙

### 1.1 기존 규범과 이 문서의 관계

`AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`, 관련 위협 모델 및 현재 Phase 프롬프트를 먼저 읽는다. 이 문서는 그 파일들을 자동으로 대체하지 않는다. 권고안이 기존 규범과 다른 곳은 아래 결정 표와 작업 항목에 표시했다.

최초 요청은 **하나의 상세 명세 파일 작성**이었으며 당시에는 이 파일만 새로 작성했다. 후속 실행 요청에 따라 Phase 0용 Go 테스트·패키지 스텁·Makefile·portable CI 허용을 기존 아키텍처에 명시적으로 반영했다. 디렉터리 제한을 일반적으로 해제하거나 아래 제안 디렉터리를 모두 생성해도 된다는 뜻은 아니다.

표기 의미:

| 표기 | 의미 | 실행 가능 여부 |
|---|---|---|
| 확인 | 저장소·공식 자료·로컬 읽기 전용 진단으로 확인한 사실 | 사실의 유효 시점을 확인하고 활용 |
| 기존 확정 | 현재 규범 문서에 이미 정해진 원칙 | 변경하지 않고 준수 |
| 채택 기본안 | 사용자가 위임한 판단에 따라 이 계획에서 선택한 방향 | 다시 선택을 묻지 않음; 해당 구현 작업에서 규범 반영·검증 |
| 권고 | 이번 분석의 설계·운영 제안 | 해당 작업 승인 및 규범 반영 후 적용 |
| 결정 필요 | 제품 범위·보안 보장·권한에 영향을 주는 선택 | 해당 선택에 의존하는 구현은 보류 |
| 미검증 | 실제 실행이나 환경 확인을 하지 못한 사항 | 통과로 기록하지 않음 |

사용자가 방향 선택을 위임했으므로 D01~D08을 다시 질문하지 않는다. 최초 기준안 요청을 실제 설치·전체 로드맵 구현으로 확대 해석하지 않는다. 후속 구현 요청에서는 요청 범위의 정상적인 편집·테스트와 선택한 기본안을 진행한다. 이미 설명하고 선택한 방향을 재승인받지 않고, 검증 과정에서 새로 드러난 규범 충돌·보장 약화·범위 확대만 구체적인 근거와 함께 다룬다.

### 1.2 읽는 순서

1. 이 문서의 2~4절에서 현재 상태, 채택 기본안, 남은 기술 검증을 확인한다.
2. 실행할 작업 ID를 12절에서 하나 선택한다. 전 로드맵 실행을 기본값으로 삼지 않는다.
3. `AGENTS.md` → 관련 규범 → 해당 `prompts/NN-*.md` → 작업 ID의 수용 기준 순으로 읽는다.
4. 13절의 테스트와 14절의 보고 기준을 적용한다.
5. 판단이 바뀌면 결정 표의 상태와 근거를 먼저 갱신한다. 추천을 확정으로 몰래 바꾸지 않는다.

### 1.3 범위

상세 구현 계획은 현재 상세 프롬프트가 존재하는 Phase 0~4와 그 준비 작업에 집중한다. Phase 5~10은 진입 조건과 목표만 기록한다. 아직 오지 않은 단계의 API·프레임워크를 확정하지 않는다.

이 문서 자체의 작성·검토는 제품 Phase 완료가 아니다. 이후 각 Phase 구현이 끝나면 기존 Required Final Report를 사용한다.

## 2 조사 결과와 현재 상태

### 2.1 저장소 상태

아래는 **최초 분석 기준 커밋의 과거 상태**다. 이후 Phase 0/1 작업으로 추가된 코드와 검증 현황은 12.10절에서 별도로 관리한다.

- GitHub `main`과 로컬 기준 커밋이 동일함을 이전 검토에서 GitHub compare로 확인했다.
- `README.md`, `AGENTS.md`, `CLAUDE.md`, `docs/`, `prompts/`가 존재한다.
- `go.mod`, `cmd/`, `internal/`, `bpf/`, 테스트·CI 구현은 아직 없다. 아키텍처 문서의 트리는 목표 구조다.
- 검토 시작 시 worktree는 깨끗했다. 이 작업은 이 명세 파일만 추가한다.
- 코드가 없으므로 코드 지식 그래프 검색·인덱싱은 수행하지 않았다. 코드가 생기면 프로젝트 지침대로 MCP 그래프 탐색을 먼저 사용한다.

### 2.2 Mac 개발 머신에서 확인한 값

아래는 로컬 명령 결과이며, 권장 버전 목록을 새로 정의한 것이 아니다.

| 항목 | 2026-10-02 확인 결과 | 의미 |
|---|---|---|
| OS / CPU | macOS 26.6.2 / arm64 | Apple Silicon 개발 호스트 |
| Go | go1.26.1 darwin/arm64 | 현재 프로젝트 Go 기준 충족 |
| Node.js / npm | v24.14.0 / 11.9.0 | 선택적인 npm 기반 스킬 설치 경로 사용 가능성 있음 |
| Git | 2.50.1 | 저장소 조회 가능 |
| Codex CLI | 0.160.0 | 로컬 help에서 `plugin add`, `plugin marketplace` 확인 |
| Claude Code | 2.1.287 | CLI 실행 파일과 버전 확인; 플러그인 설치는 미수행 |
| Lima | 2.1.4 | VM 관리 CLI 존재 |
| Docker CLI | 29.4.3 | CLI 존재만 확인; daemon·VM·BPF 기능 미검증 |
| Apple clang | 17.0.0 | `--print-targets`에 BPF 타깃 없음 |
| `llvm-strip`, `bpftool` | 현재 Mac PATH에서 미발견 | 모든 디스크·Linux guest에서 미설치라는 뜻은 아님 |
| Colima / OrbStack CLI | 현재 PATH에서 미발견 | 별도 설치 필요성을 뜻하지 않음; Lima가 이미 있음 |

진단 중 Lima는 샌드박스로 인해 Rosetta 관련 sysctl 조회 실패 경고를 냈고, Codex는 PATH alias 생성 불가 경고를 냈다. 버전·도움말·VM 목록은 반환됐다. 이 경고를 제품 기능 실패나 설치 성공으로 해석하지 않는다. 인증·토큰·사용자 설정 파일 전체는 수집하지 않았다.

### 2.3 기존 Linux VM

`limactl list --json`으로 다음 설정을 확인했다.

| 항목 | 확인 결과 |
|---|---|
| VM 이름 | `ebpf-lab` |
| 상태 | Stopped |
| 가상화 / 아키텍처 | VZ / aarch64 |
| 자원 | 4 CPU, 4 GiB RAM, 20 GiB disk |
| 이미지 설정 | Ubuntu 26.04 cloud image 항목 존재; 고정 digest 항목 및 fallback URL 존재 |
| provisioning 내용 | git, python3, bpftrace, 커널 헤더 등 |
| readiness 내용 | `CONFIG_BPF=y`와 bpftrace 등을 확인 |
| 아직 확인하지 않은 것 | 실제 guest 커널, BTF, 활성 BPF-LSM, clang BPF 타깃, bpftool, Go, attach/verifier 결과 |

**권고:** 기존 VM을 우선 진단한 후 적합하면 재사용한다. VM을 새로 만들거나 기존 VM의 커널·부팅 옵션을 바로 바꾸지 않는다. 기존 설정에 적힌 provisioning은 현재 설치 상태의 증거가 아니다. 이번 작업에서는 정지된 VM을 시작하지 않았다.

### 2.4 아직 수행하지 않은 작업

최초 분석 당시의 미수행 목록이다. 이 중 Phase 0/1 코드·Go 검사·관련 규범 수정은 후속 작업에서 수행했으며, 최신 상태는 12.10절을 따른다. 나머지 설치·VM·BPF·실제 AI 실행·Git 배포 작업은 수행하지 않았다.

- 외부 스킬·플러그인·npm 패키지 설치 또는 업데이트.
- VM 시작·생성·삭제, guest 패키지 설치, 커널 설정 변경.
- AgentTaint 코드 구현, Go 빌드·테스트, BPF 생성·로드·attach.
- 실제 AI CLI 인증·실행, 실제 secret을 이용한 유출 테스트.
- 기존 규범 문서 수정, 커밋·push·PR 작성.

## 3 제품 철학과 보호 범위

### 3.1 유지할 원칙

제품의 기준은 `Source → Label → Propagation → Sink → Decision`이다. AI의 의도·프롬프트 안전성·LLM 분류 결과를 보안 판정 근거로 사용하지 않는다. 명시적으로 지정한 실행의 실제 OS 효과와 정책을 이용한다.

`agenttaint run -- <COMMAND>`의 실행 계보가 대상이다. 특정 AI 바이너리 이름은 `doctor`의 편의 기능에만 사용한다. 진단은 환경을 고치거나 임의의 대상 커맨드를 실행하는 기능이 아니다.

Taint는 지정된 민감 자원 접근 이력에 따른 보수적 상태다. Byte-level taint tracking을 주장하지 않는다. post-event detection을 prevention이라고 부르지 않는다. SIGKILL 성공은 이미 발생한 네트워크·파일 효과가 없었다는 증거가 아니다.

### 3.2 초기 보장으로 표현할 수 있는 것

현재 문서 기준 목표는 지정 파일 접근, fork/exec에 따른 라벨 상태 관리, 새 외부 IPv4 연결에서의 정책 평가다. Phase 3은 audit only이고, Phase 4의 block은 지원·검증된 pre-operation 훅에만 해당한다. 현재 Phase 0/1 구현은 진단·일반 실행·데이터 모델에 한정되며 이 보안 목표 중 구현 완료된 기능은 없다.

| 흐름 | 초기 모델의 위치 |
|---|---|
| 민감 파일 접근 → 같은 프로세스의 새 IPv4 연결 | 핵심 수용 테스트 |
| 부모의 민감 파일 접근 → fork → 자식의 새 연결 | 핵심 상속 테스트 |
| taint 프로세스의 exec → 새 연결 | 라벨 유지 계약을 확정하고 테스트 |
| 자식 도구의 민감 파일 접근 → pipe/stdout → 부모 AI → API | 초기 fork 상속만으로 보장 못함; 역방향 데이터 전달 필요 |
| 연결을 먼저 생성 → 민감 파일 접근 → 기존 소켓 송신 | connect 훅만으로 보장 못함 |
| IPv6, connect 없는 UDP 송신, 원격 MCP 내부 동작 | 초기 범위 밖 또는 별도 검증 대상 |
| Mac 호스트 프로세스의 파일·네트워크 동작 | Linux VM 센서의 범위 밖 |

다음은 설계 한계를 설명하는 예다. 실행된 공격 재현 결과가 아니다.

```text
VM 안 AI 부모 ──fork──> 도구 자식 ──open──> secret fixture
     ▲                         │
     └────── pipe/stdout ───────┘
     │
     └── 기존 LLM API 소켓으로 전송
```

자식에서 부모로의 전파와 기존 소켓 송신은 별개의 문제다. 둘 중 하나를 해결했다고 위 전체 흐름이 보호된다고 주장하지 않는다. 전체 실행에 라벨을 붙이는 대안은 오탐과 의미를 바꾸며, 기존 connect 문제도 독립적으로 남는다. 기존 process taint 모델을 조용히 run-wide taint로 바꾸지 않는다.

### 3.3 제품 사용성 계약

민감 파일을 읽은 뒤 모든 외부 연결을 막으면 원격 LLM API 호출도 막힐 수 있다. 반대로 API 목적지를 예외로 허용하면 그 목적지로의 민감정보 전송도 허용될 수 있다. 이 도구는 전송 본문의 안전성을 증명하지 않는다.

권고 초기 사용 사례는 합성 secret을 사용하는 로컬 개발·테스트 작업, 외부 전송이 필요 없는 민감 작업, 좁게 지정한 새 연결 경로의 감사다. 실사용 자격증명과 클라우드 에이전트 전체 흐름을 안전하게 보호한다고 마케팅하기 전에는 해당 경로의 증거가 필요하다.

### 3.4 보호 시작 전 입력과 관측하지 않는 경로

`run`으로 시작했다는 사실은 대상이 모든 민감정보에 대해 처음부터 clean하다는 증명이 아니다. 초기 label 없음은 **지원하는 source 접근이 아직 관측되지 않았음**을 뜻한다.

| 입력·경로 | 초기 계약 | 검증·표시 |
|---|---|---|
| supervisor가 실행 전 열어 target에 넘긴 민감 파일 FD | 실행 후 FileOpen만으로 라벨이 부여됐다고 가정하지 않음 | inherited FD fixture로 한계 재현; loader의 불필요 FD 전달은 별도 금지 |
| shell redirection으로 연결된 stdin, 환경변수, argv, 기존 메모리 | 지정 파일 open 기반 source 모델 밖 | 기본적으로 수집·검사하지 않음; 민감하지 않다는 뜻도 아님 |
| mmap·비동기 I/O·다른 프로세스가 전달한 FD | 지원 hook의 실제 관측·실행 주체를 확인해야 함 | 새로운 지원 주장 전 전용 시나리오 필요; 현재 포괄 지원 금지 |
| 실행 전에 만들어진 소켓과 이미 실행 중인 daemon | 새 connect·계보 모델로 전체 효과를 추적하지 못함 | S22/S26과 연결; daemon을 자동 attach하지 않음 |
| IPv4 TCP와 UDP connect, IPv6·기타 socket family | 주소 종류와 transport는 별개 | 최초 반복 수용 테스트는 IPv4 TCP; 다른 transport는 검증 결과 없이 같은 지원으로 표시하지 않음 |

이 공백을 해결하려고 환경변수·stdin 본문을 검사하거나 전체 run을 무조건 taint시키지 않는다. 이는 새로운 source/전파 모델이며 현재 Phase의 자동 확장이 아니다. 초기 fixture는 guest 파일을 **보호 시작 이후 target이 직접 여는 경로**로 구성하고, 위 경로들은 한계 테스트로 분리한다.

## 4 사용자 결정 목록

2026-10-02 후속 요청에서 사용자가 적절한 선택을 AI에게 위임했다. 아래 D01~D08은 추가 응답을 기다리지 않는 **채택 기본안**이다. 사용자 본인이 각각의 기술안을 직접 승인했다고 기록하지 않으며, 위임에 따라 AI가 선택한 근거를 남긴다. D09만 배포 시점의 소유자 결정으로 남기고 지금 작업을 막지 않는다.

### 4.1 채택한 제품 기본안과 이유

| ID | 항목 | 채택 기본안 | 선택 이유·영향 | 상태 |
|---|---|---|---|---|
| D01 | Mac 지원 | Mac에서 개발하고 AI·도구·AgentTaint는 Linux VM에서 실행. 네이티브 Mac backend는 보류 | 이미 있는 Lima를 활용하고 Linux 의미를 먼저 검증. 기존 플랫폼 순서 유지 | 채택 기본안 |
| D02 | 초기 보호 범위 | Phase 0~4는 기존 좁은 범위의 기술 MVP. 자식→부모 전달·기존 소켓 송신은 일반 AI 유출 방지 주장의 필수 후속 검증 | 초기 구현을 완결하면서 실제 사용 경로의 공백을 제품 완료로 오인하지 않음. 4.3절의 두 완료 기준 적용 | 채택 기본안 |
| D03 | 첫 사용자·사용 사례 | 로컬 개발자, guest 내부 synthetic secret, 지정 파일 접근 후 새 연결 감사·차단 | 실제 자격증명·조직 배포 없이 재현 가능한 가치 검증 가능 | 채택 기본안 |
| D04 | 외부 LLM API | 초기 보호 예시에서 LLM API 목적지에 자동 예외를 주지 않음. taint 이후 평가 대상 연결에는 같은 정책 적용 | 유명 API라는 이유로 보호를 우회시키지 않음. 기존 연결 미지원 한계는 그대로 명시 | 채택 기본안 |
| D05 | block 미지원 | block 요청 시 필수 기능이 없으면 target 시작 전 오류 종료. monitor/notify/kill은 사용자가 그 mode를 명시했을 때만 | 요청한 보장과 실제 mode 일치. 기존 Phase 4의 fallback 문구를 이 계약에 맞춰 정리 | 채택 기본안; 규범 반영 작업 필요 |
| D06 | 외부 스킬 | 프로젝트 단위로 source-driven-development, documentation-and-adrs, code-review-and-quality부터 선택 도입. 원본 SHA와 필요한 reference를 함께 관리 | global 전체 팩·중복 router·hook의 영향 억제. 8.5절의 선택 배포본 방식 사용 | 채택 기본안; 아직 미설치 |
| D07 | CPU 검증 | 현재 arm64 VM으로 개발하고 배포 전 amd64 Linux 검증 추가. 유료 runner를 자동 도입하지 않음 | 로컬 환경 활용과 배포 호환성 증거 분리. 검증 전 amd64 지원 표시는 보류 | 채택 기본안 |
| D08 | 로그 | payload·환경변수·전체 argv를 기본 수집하지 않음. resource 별칭·최소 프로세스 식별자 중심; 상세 경로는 명시적 진단 선택. 로컬 저장·외부 업로드 없음 | 관측 도구가 새로운 민감정보 저장소가 되는 위험을 줄임. 저장 시 사용자 전용 권한·용량 상한 적용 | 채택 기본안 |
| D09 | 라이선스·공개 배포 | 지금 LICENSE나 공개 배포 방침을 바꾸지 않음. 릴리스 준비 때 소유자 선택 | 일반 개발·검증에는 불필요한 결정. 권리·배포 조건 변경은 별도 확인 | 배포 시점에만 확인 |

D01~D08의 일반 제품 선택을 다시 질문하지 않는다. 다만 이 기본안은 규범 충돌의 구체적인 변경 승인이나 실행 환경의 쓰기 권한을 대신하지 않는다. 현재 확인이 필요한 착수 조건은 12.12절을 따른다. 라이선스 변경, 유료 인프라 계약, 실제 자격증명 도입, Mac 네이티브 보호처럼 이후 새로 범위에 들어오는 사항도 해당 시점에 확인한다.

### 4.2 AI가 구체화하고 검증할 설계 항목

아래는 사용자에게 다시 선택지를 물어볼 목록이 아니라 구현 담당자가 구체화할 기술 작업이다. 표의 방향을 기본으로 상세안을 만들고 실험·테스트 증거를 남긴다. 기존 규범과 충돌하는 변경은 먼저 문서에 반영하며, 아직 설명하지 않은 충돌이나 근본적인 보장 변경이 발견될 때만 사용자 판단을 요청한다.

| ID | 결정할 내용 | 권고 방향 | 구체화·검증 담당 작업 |
|---|---|---|---|
| E01 | 새 문서·테스트·스킬·CI 경로를 허용하는 범위 | 제품·툴링·산출물 경로를 명시적으로 분리 | PRE-02, TOOL-01 |
| E02 | ProcessKey와 실행 식별자 | PID만으로 상태를 키잉하지 않음; OS SessionID 의미 유지 | P1-01 |
| E03 | open 시도·성공·읽기의 라벨 부여 의미 | 관측 가능한 사실과 보수적 부여 기준을 구분 | P1-02, P2-02, P3-01 |
| E04 | 동기 커널 집행 상태와 Go 모델의 관계 | 정책 준비·의미 정의는 Go, 최소 동기 집행은 커널이라는 안을 검증 | P4-01 |
| E05 | 경로·파일 객체의 동일성 | 초기 절대 경로 규칙과 한계를 명시; kernel enforcement 객체 식별은 별도 검증 | P3-02, P4-02 |
| E06 | 정책 우선순위·기본값·외부 목적지 | deny > audit > allow, no-match allow. unknown rule은 오류. 목적지 집합을 명시하고 사설 IP를 자동 신뢰하지 않음 | P3-02 |
| E07 | 생성된 BPF Go 코드·object·vmlinux.h 관리 | 고정 Linux toolchain으로 생성한 Go·object와 헤더 provenance를 함께 버전 관리. 일반 Go build와 재생성 CI 분리 | ENV-02 |
| E08 | 이벤트 출력과 대상 stdout의 분리 | 대상 stdout 유지, 감사 스트림 별도 FD/파일 옵션 | P2-03 |
| E09 | 높은 권한의 supervisor와 대상 실행 권한 | 대상에 loader 권한을 상속하지 않는 설계 | P2-04, P4-01 |

E04는 현재 `internal/taint`에 전파를 집중하는 규칙과 실제 커널 동기 상태 관리 사이의 설계 조정이 필요하다. 기본 검토 방향은 Go reference model과 최소 kernel state machine이며, 커널별 가능성과 race-free 여부는 P4-01에서 증명해야 한다. 사용자에게 커널 구현을 선택하도록 요청하기보다 가능한 안과 테스트 결과를 먼저 준비한다. 검증되지 않은 안을 차단 보장으로 표시하지 않는다.

### 4.3 기술 MVP와 일반 AI 보호의 완료 기준

**기술 MVP 완료:** 기존 Phase 0~4의 지정 source, 계보 상속, 새 IPv4 연결 평가·집행이 지원 환경에서 검증된다. 이 단계는 좁게 명시한 기능을 제공하는 개발자용 릴리스로 다룬다.

**일반 AI 도구 결과의 외부 전달 보호 주장:** 자식의 민감정보 접근 결과가 부모에게 전달되는 S23과, 이미 열린 API 연결을 사용하는 S22를 실제 지원 경로로 구현·검증한 뒤에만 해당 주장을 한다. 단순 한계 재현이나 문서 경고는 이 완료 기준을 충족하지 않는다. 전파 확장은 Phase 6 방향을 따르고, 송신 시점 통제는 별도 설계로 연결한다. Phase 순서를 조용히 바꾸지는 않는다.

Mac 네이티브 지원·Kubernetes·대시보드·payload metadata 확장은 이 두 검증의 대체재가 아니다. 초기 사용자에게 보여줄 핵심 결과는 정확한 capability, 재현 가능한 위반 설명, 실제 집행 결과다.

## 5 macOS 지원과 개발 환경 계획

### 5.1 환경별 사용 가능성

| 작업 | Mac 네이티브 | Mac의 Linux VM | Linux 서버 |
|---|---|---|---|
| 문서·스킬·코드 리뷰 | 가능 | 가능하지만 필수 아님 | 가능 |
| Go core·policy·taint 단위 테스트 | 플랫폼 독립 구현이면 가능 | 가능 | 가능 |
| CLI doctor·일반 runner 개발 | 가능; 감시 지원 여부는 별도 | 가능 | 가능 |
| Linux BPF C 빌드 | 별도 LLVM 등으로 가능할 수 있으나 이 환경의 Apple clang은 불가 | 권고 경로 | 가능 |
| Linux BPF 로드·verifier·attach | 불가 | guest 기능·권한 충족 시 가능 | 기능·권한 충족 시 가능 |
| AgentTaint로 대상 AI 보호 | 현재 Linux backend로 불가 | AI도 같은 guest에서 실행해야 함 | 같은 커널 관측 범위에서 가능 |
| Mac 호스트 프로세스 직접 보호 | 별도 네이티브 backend 필요 | 불가 | 불가 |

Linux 바이너리를 cross-build하는 것과 Mac에서 Linux 커널 프로그램을 실행하는 것은 다르다. Docker의 privileged 옵션도 호스트 XNU를 Linux 커널로 바꾸지 않는다. VM 안의 컨테이너를 사용해도 보장은 guest 커널 기능에 의존한다.

### 5.2 권고 실행 구조

```text
Mac arm64
  editor / Git / AI 개발 도구 / 순수 Go 테스트
  선택 설치: agent-skills와 프로젝트 검증 스킬
       │ 작업 소스 동기화와 테스트 호출
       ▼
Lima ebpf-lab 또는 검증된 별도 Linux VM
  Go + clang(BPF target) + LLVM tools + bpftool + BTF
  AgentTaint supervisor / Linux sensor / BPF programs
       │ 명시적 target launch, 원래 사용자 권한
       ▼
  보호 대상 AI CLI / shell / tool / local MCP child
       │
       └── guest 내부 합성 파일과 통제된 테스트 수신 서버
```

Mac에서 개발 AI를 사용하면서 guest에 빌드 명령을 보내는 개발 방식은 가능하다. 다만 그 Mac 개발 AI 자체가 AgentTaint로 보호되는 것은 아니다. 제품 실험에서는 AI와 그 도구 실행을 guest 안으로 옮겨야 한다.

### 5.3 Linux guest 점검 절차

다음은 **나중에 ENV-01 실행 권한을 받은 경우의 점검 예시**다. 이번에는 실행하지 않았다. VM을 시작할 때 기존 provisioning이 재실행될 가능성과 자원 사용을 확인한다.

```sh
# Mac에서: 정지된 기존 VM을 사용하기로 결정한 뒤
limactl start ebpf-lab
limactl shell ebpf-lab

# 아래부터 Linux guest 안에서 실행
uname -a
uname -m
test -r /sys/kernel/btf/vmlinux
cat /sys/kernel/security/lsm
cat /proc/cmdline
go version
clang --version
clang --print-targets
command -v llvm-strip bpftool
bpftool version
```

커널 config는 `/boot/config-$(uname -r)` 또는 `/proc/config.gz` 중 실제 제공되는 경로를 읽는다. 파일이 없다는 것과 커널 기능이 없다는 것을 동일시하지 않는다. 확인할 항목은 `CONFIG_BPF`, `CONFIG_BPF_SYSCALL`, `CONFIG_BPF_LSM`, BTF 관련 설정이다. BPF-LSM은 빌드 지원뿐 아니라 활성 LSM 목록에 `bpf`가 있는지도 확인한다. securityfs 접근 불가·미마운트 상태도 별도 상태로 기록한다.

추가로 `CONFIG_BPF_EVENTS`, `CONFIG_BPF_JIT`, `CONFIG_SECURITY`, 네트워크 집행의 `CONFIG_SECURITY_NETWORK`를 확인한다. 이들을 모두 같은 필수 조건으로 단정하지 말고 사용 program type·hook과의 관계를 기록한다. 후속 작업에서 `lsm=` 변경이 필요하면 기존 LSM 목록을 보존하고 재부팅 후 활성 상태를 확인한다. 기존 목록을 무시하고 `lsm=bpf`로 덮어쓰지 않는다.

버전 문자열만으로 합격시키지 않는다. 실제 프로그램의 로드·attach와 수용 테스트가 최종 증거다. root 또는 필요한 capabilities로 하는 능동 feature probe는 환경 진단의 읽기 전용 구간과 구분해 실행한다. `doctor`가 자동으로 부팅 옵션·LSM 목록·마운트·패키지를 바꾸면 안 된다.

### 5.4 패키지와 재현성

- 기존 기준인 Go 1.26+, cilium/ebpf v0.22.0, clang/LLVM 12+, bpftool, kernel 5.13+를 임의로 변경하지 않는다.
- Phase 0은 표준 라이브러리로 시작한다. cilium/ebpf는 실제 센서 구현 단계에서 도입한다.
- Ubuntu guest에는 BPF 타깃 지원 clang/LLVM, strip 도구, bpftool 및 필요한 BPF 헤더를 준비한다. 정확한 distro 패키지 이름과 guest 커널 대응 버전은 ENV-01에서 확인한다.
- bpftrace가 설치돼 있다는 이유로 CO-RE 빌드 환경이 준비됐다고 판단하지 않는다.
- `bpftool btf dump`는 vmlinux.h 생성용이고, Go loader 연결은 bpf2go를 기준으로 한다. C용 skeleton을 Go 빌드의 필수 단계로 혼합하지 않는다.
- bpf2go 버전은 cilium/ebpf와 맞춘다. 고정 버전을 확인하고 생성한다.
- bpf2go의 Go 생성 코드가 object를 embed하므로 `.go`와 `.o`를 함께 버전 관리하는 E07 기본안을 적용한다. 고정 toolchain·헤더 출처·생성 명령을 남기고 재생성 CI로 diff를 확인한다. 현재 `.gitignore`와 충돌하는 object 경로는 ENV-02에서 조정한다. 일반 사용자의 Go build마다 clang 설치를 요구하지 않는다.
- CO-RE가 모든 hook·helper·CPU·커널 설정 차이를 없애는 것은 아니다. arm64와 amd64의 load/attach 증거를 분리한다.

지원 하한과 검증 baseline도 구분한다. AGENTS의 최소 기준은 5.13+이며, 현재 Phase 2 수용 기준의 실행 환경은 5.15+다. 5.15+ 환경 한 곳에서 통과했다고 5.13 커널에서 검증됐다고 기록하지 않는다. 하한을 바꾸려면 기존 버전 변경 절차를 따른다.

### 5.5 VM 공유와 자격증명

보안 테스트는 guest의 일반 Linux 파일시스템에서 시작한다. virtiofs 공유 경로는 경로·inode·권한 의미가 달라질 수 있어 별도 테스트로 분리한다. 호스트 홈 전체, SSH agent, Docker socket, 실제 클라우드 자격증명을 테스트 VM에 편의상 공유하지 않는다. 현재 VM의 실제 공유·forwarding 상태는 실행 전 확인한다.

빌드 산출물과 Go cache는 guest 전용 경로를 사용해 호스트와 서로 다른 OS·아키텍처 산출물이 섞이지 않게 한다. AI 인증이 필요한 실사용 단계에서는 guest 전용 최소 권한 자격증명과 보호하려는 fixture를 구분한다. 이 설정은 순수 합성 통합 테스트의 필수 조건이 아니다.

### 5.6 Mac 네이티브 지원의 정확한 의미

Apple Endpoint Security는 C API이므로 Swift가 필수 언어는 아니다. 기존 문서의 해당 표현은 정정 후보다. native backend를 선택하면 entitlement, 권한·사용자 승인, 배포 형태, 서명·공증 관련 요건과 지원 이벤트를 별도 조사해야 한다. Xcode나 SDK 설치만으로 배포 가능한 보호 기능이 완성되지 않는다.

Endpoint Security의 AUTH/NOTIFY 분리를 Linux 훅과 일대일로 가정하지 않는다. 특히 동일한 네트워크 sink 제어를 제공하는지는 별도 검증이며, 필요하면 다른 Apple framework와의 경계를 검토한다. Go core 재사용 가능성과 OS 센서·집행 동등성은 다른 문제다. Phase 5 전에 Swift 전환이나 macOS backend 구현을 시작하지 않는다.

Phase 5에 도달했다는 사실 자체도 native 구현 승인은 아니다. D01의 현재 기본안은 VM 실행 유지다. native 보호는 실제 필요성이 생겼을 때 별도 범위로 다룬다.

공식 근거: [Lima 설치](https://lima-vm.io/docs/installation/), [Linux BPF-LSM](https://docs.kernel.org/bpf/prog_lsm.html), [Apple Endpoint Security](https://developer.apple.com/documentation/endpointsecurity).

### 5.7 실행 환경 증거와 재검증 조건

ENV-01/02는 도구 설치 여부 목록만 남기지 않고 다음 내용의 **환경 manifest**를 검증 artifact에 남긴다. 이는 새 제품 서비스나 데이터베이스가 아니며 13.4절 결과와 연결하는 작은 기록이다.

- 환경 식별자·검사 시각·guest boot 식별 정보, OS/CPU/kernel, 실제 활성 LSM과 BTF 확인 결과.
- 실제 사용 image 출처·digest와 toolchain 버전. fallback URL을 썼다면 실제 받은 image digest를 별도로 기록한다. checksum이 없는 최신 image를 이전 고정 image와 같은 환경으로 취급하지 않는다.
- vmlinux.h 생성 입력의 hash·생성 명령, Go module 및 생성 도구 버전, BPF `.c`/생성 `.go`/`.o`의 연결 관계와 hash.
- 실행 사용자·group·capability 요약, 사용 중인 공유 mount·forwarding·관리 socket의 종류와 노출 범위. 환경변수 원문·자격증명·홈 전체 경로 목록은 저장하지 않는다.
- 실제 검증한 hook/program과 scenario, read-only 진단과 능동 load/attach 검증의 구분.

커널·부팅 LSM·VM image·CPU·toolchain·BPF 소스/생성 object·권한 profile·공유 파일시스템 구성이 바뀌면 관련 검증을 다시 수행한다. reboot 이후 예전 attach 성공을 현재 attach 상태로 표시하지 않는다. 정책·제품 코드가 바뀌면 해당 정책·코드에 의존하는 시나리오도 다시 실행한다.

기존 Lima를 재사용하는 기본안은 유지한다. `plain` 모드는 공유 기능 축소의 후보일 뿐 격리 완료 증거가 아니다. 공식 문서상 static port forwarding과 provisioning은 남을 수 있으므로 실제 설정·실행 상태를 따로 점검한다. 이번 문서 검토에서 해당 모드를 적용하거나 VM을 변경하지 않았다. [Lima plain mode](https://lima-vm.io/docs/config/plain/)

## 6 언어와 구성요소 경계

### 6.1 언어 선택

| 구성요소 | 언어·기술 | 상태 |
|---|---|---|
| CLI, core, taint, policy, runner, diagnostics | Go 1.26+ | 기존 확정 |
| Linux 커널 프로그램 | C + libbpf CO-RE headers, cilium/ebpf loader | 기존 확정 |
| BPF 생성 | 고정 cilium/ebpf 버전의 bpf2go + clang/LLVM | 빌드 절차 구체화 필요 |
| 정책 파일 | YAML | 기존 초안; parser 의존성은 Phase 3에서 검토·고정 |
| 감사 출력 | JSON/JSONL | schema 확정 필요 |
| 빌드·검증 진입점 | Make + 작은 shell scripts | 권고; 경로 승인 후 도입 |
| 합성 시나리오 | Go 테스트 helper 우선, 필요한 짧은 shell/Python fixture | 권고; 제품 Python runtime 의존성은 만들지 않음 |
| AI 스킬·에이전트 지시 | Markdown, host별 metadata | 선택적 개발 도구 |
| Node.js | npm 기반 스킬 설치를 선택한 경우 | 제품 의존성 아님 |
| 웹 UI·native macOS·Kubernetes | 해당 Phase에서 결정 | 초기 구현 범위 밖 |

### 6.2 패키지 책임

| 경로 | 책임 | 금지 |
|---|---|---|
| `cmd/agenttaint` | 인자·출력 형식·하위 기능 연결 | 직접 taint/allow/deny 판단 |
| `internal/core` | 플랫폼 독립 event·identity·resource·label value | Linux/Mac 전용 타입·sensor import |
| `internal/env` | 기본 doctor의 정적 진단·AI CLI 비실행 탐지; Phase 2 명시 toolchain probe는 subprocess 실행(미구현) | PATH·커널·패키지 자동 변경 |
| `internal/runner` | 명시 대상 resolve·실행·종료·신호·준비 순서 | 특정 AI CLI 보안 특별 취급 |
| `internal/sensor/linux` | BPF load/attach·raw decode·정규화 | 사용자 정책 해석·전파 의미 정의 |
| `internal/taint` | 라벨 상태 전이의 의미·reference engine | CLI·센서에 같은 전파 의미 복제 |
| `internal/policy` | 정책 parse/validate/evaluate, 미래 kernel representation 준비 | LLM 분류·프롬프트 분석 |
| `internal/enforce` | 모드·기능 적합성·집행 결과 연결 | audit를 block으로 표시 |
| `bpf/` | 정해진 OS 훅·제한된 상태·사전 집행 | 동적 문자열 파싱·정규식·unbounded loop |
| `internal/sensor/darwin`, `internal/web` | 기존 계획의 미래 자리 | 현재 기능 구현 |

### 6.3 audit 경로와 enforcement 경로

Phase 2~3의 audit 경로는 기존 설계와 일치한다.

```text
kernel event → ring buffer → normalized core.Event
             → taint state transition → policy evaluation → audit record
```

Phase 4에서는 이미 발생한 이벤트에 대한 Go 판정으로 동일 operation을 소급 차단할 수 없다. 다음 구조는 E04의 **검토할 권고안**이다.

```text
Go policy parse/validate → 제한된 kernel policy representation
                                  │
source/fork/exec hook → 동기 상태 갱신 → sink LSM hook → allow / -EPERM
                                  │
                                  └── ring buffer → 감사·대조·설명
```

이 안은 source와 fork에 대한 동기 kernel 상태 관리가 필요할 수 있어 기존 전파 패키지 규칙을 수정·구체화해야 한다. Go가 state map을 뒤늦게 갱신하는 것만으로 경합을 해결했다고 주장하지 않는다. 다른 대안을 택하면 다음을 입증한다: source 관찰과 필요한 state 변경이 대상의 후속 보호 operation보다 먼저 유효해지고, 이벤트 전달 지연·손실이 집행 상태를 잘못 허용으로 만들지 않는다.

정책 의미는 하나여야 한다. Go reference model과 kernel state machine을 같은 event vector·규칙으로 대조하고, kernel이 지원하지 않는 정책은 적용 전에 거부한다. 플랫폼별 집행 능력을 플랫폼 독립 capability로 표현하되 core에 커널 훅 문자열을 필수 의존성으로 넣지 않는다.

## 7 데이터와 보안 계약 초안

이 절은 구현 계약과 미래 설계 초안을 구분한다. 7.1/7.2절의 **Phase 1 채택 계약**은 현재 core 구현을 설명하며 검증 완료 여부는 12.10절을 따른다. 나머지 E02~E06의 센서·라벨·정책 설계는 관련 Phase에서 규범 반영·검증을 거쳐 확정한다. 각 필드를 사용자에게 다시 선택하게 하지 않으며 Phase 1에서 라벨 의미를 미리 구현하지 않는다.

### 7.1 프로세스와 실행의 식별

**Phase 1 채택 계약 — 데이터 표현만 구현, OS 수집·상태 전파는 미구현:**

| 타입·필드 | 현재 의미·직렬화 |
|---|---|
| `ProcessID`, `ProcessGroupID`, `SessionID` | 각각 `uint32` 기반 named type. 관측 주체 PID는 0 불가; PPID·PGID의 0은 미상 표현 |
| `Metadata.SessionID` | POSIX SID이며 JSON은 10진 문자열 `session_id`. 현재 raw event 계약에서는 0 불가. `RunID`로 재해석하지 않음 |
| `RunID` | 별도 opaque string `run_id`. core는 값을 발급하거나 실행 membership을 판정하지 않음 |
| `ProcessKey` | 비교 가능한 `{PID, BirthID, ScopeID}`. JSON은 `pid`, `birth_id`, `scope_id`; PID 재사용 및 관측 scope를 구분하며 birth/scope를 PID로 대체하지 않음 |
| `BirthID` / `ScopeID` | 각각 프로세스 생애 / PID와 birth가 의미를 가지는 관측 영역의 opaque string. 실제 값 확보·정규화·안정성 검증은 Phase 2 책임이며 Linux clock/namespace 선택을 core에 넣지 않음 |
| `ProcessContext` | key를 포함하고 `ppid`, `pgid`, `comm`을 추가. `comm`은 관측된 이름이지 전체 command line이 아님. PPID만으로 안정된 부모 identity·run 소속을 증명하지 않음 |

exec는 같은 프로세스 key를 유지한다는 의미를 문서화하되 core가 이를 자동 관리하지 않는다. fork는 parent `process`와 별개 identity의 `child`를 표현한다. exit는 개별 thread가 아니라 프로세스 생애 종료를 뜻한다. argv·환경변수·label 필드는 없다. 어떤 객체가 이 타입을 만족해도 실제 관측 사실·전역 유일성·생애 일관성이 증명되지는 않는다.

**후속 센서·taint 구현에 남은 원칙:** 아래 항목 중 ID의 데이터 표현은 위에서 구체화했고, 실제 수집·상태 동작은 이후 Phase에서 검증한다.

- `ProcessID`, `ProcessGroupID`, `SessionID`는 기존 계획대로 named type으로 유지한다.
- `SessionID`는 POSIX 세션 ID다. 제품의 한 번 실행을 뜻하도록 재해석하지 않는다.
- 권고 `RunID`: AgentTaint 실행 하나를 구분하는 별도 opaque identifier. 동시 run을 혼합하지 않는다.
- 권고 `ProcessKey`: PID와 프로세스 생애를 구분할 birth identity의 조합. Linux에서 확보 가능한 값과 PID namespace 해석을 검증하고 core에는 정규화한 값만 전달한다.
- process와 thread의 라벨 단위를 확정한다. 메모리를 공유하는 thread가 각각 다른 라벨로 처리돼 보호가 달라지지 않게 테스트한다.
- fork는 그 시점의 label snapshot 상속, exec는 같은 보안 주체의 기존 라벨 유지가 기본 권고다. exec를 새 child 생성으로 취급하지 않는다.
- 부모의 나중 접근이 이미 존재하는 자식에게 자동으로 전달됐다고 가정하지 않는다. 실제 데이터 전달과 계보는 구분한다.
- exit 처리·재부모화·setsid 이후에도 원래 run membership을 유지하는 규칙이 필요하다. PPID나 POSIX session만 계속 조회하는 구현으로 보안 경계를 대신하지 않는다.
- 개별 thread exit를 process 전체 종료로 처리하지 않는다. 공유 라벨·membership을 해제하는 생애 종료 조건을 명시한다.

### 7.2 이벤트 envelope

**Phase 1 채택 envelope:** `Event` interface의 `Kind()`와 7개 concrete event가 있고, 각각 공통 `Metadata`를 포함한다. `schema_version`은 정수 `1`, discriminator는 기존 초안과 호환되는 **`event`**다(`kind`라는 별도 JSON 키를 만들지 않음).

| 현재 공통 JSON 필드 | 의미 |
|---|---|
| `schema_version`, `event` | 지원 버전 1과 아래 7종 중 하나 |
| `event_id`, `run_id` | 공급자가 제공하는 비어 있지 않은 식별 문자열; core가 유일성을 발급·검증하지 않음 |
| `timestamp` | `time.Time`의 RFC3339 JSON wall-clock 시각. raw kernel clock이나 전역 정렬 보장이 아님 |
| `session_id` | 위 7.1절의 POSIX SID 10진 문자열 |
| `process` | `pid`, `birth_id`, `scope_id`, `ppid`, `pgid`, `comm` |
| `observation_stage` | `unknown`, `attempt`, `pre_operation`, `completed` |
| `operation_result` | `unknown`, `success`, `failure`. `completed` 이외 stage는 반드시 `unknown`; completed도 결과 미상이면 unknown 허용 |

| event / Go 타입 | 추가 payload·의미 |
|---|---|
| `process_exec` / `ProcessExecEvent` | `executable: {path}`; exec 대상의 관측 경로 |
| `process_fork` / `ProcessForkEvent` | `child: ProcessContext`; `process`는 부모. 라벨 snapshot·membership 전파 없음 |
| `process_exit` / `ProcessExitEvent` | 선택적 `exit_code`(`*int32`). 미수집·signal-only 종료는 생략하며 성공 0으로 추정하지 않음. 명시 null은 거부. raw wait status가 아니며 제공 시 completed 및 비음수 필요. operation_result는 관측 operation 결과이므로 exit 23과 success가 함께 표현될 수 있음 |
| `file_open` / `FileOpenEvent` | `resource: {path}`, `access: unknown/read/write/read_write`. access는 정규화한 접근 의도이지 byte read/write의 증거가 아님 |
| `file_read` / `FileReadEvent` | `resource: {path}`. 타입 존재만으로 read 센서 지원을 주장하지 않음 |
| `file_write` / `FileWriteEvent` | `resource: {path}`. file-flow 전파·정책 구현 없음 |
| `network_connect` / `NetworkConnectEvent` | `destination: {address, port}`; `netip.Addr`의 IPv4만, 포트는 uint16. 시도 데이터를 표현하므로 0도 허용; IPv6·IPv4-mapped IPv6는 제외 |

`FileResource.Path`는 관측한 UTF-8 문자열을 그대로 보존하며 symlink 해석·`Clean`·파일시스템 조회·안정된 파일 identity 판정을 하지 않는다. non-UTF-8 OS 경로를 이 문자열 모델로 전달하는 방법은 future sensor의 명시적인 경계 계약이 필요하며 임의의 치환으로 정확한 경로인 것처럼 만들지 않는다. stage `pre_operation`은 관측 시점을 표현할 뿐 실제 차단 가능 증거가 아니다. `FileOpen`을 성공한 `FileRead`로 바꾸지 않는다.

**JSON 경계:** 정상 concrete event는 `encoding/json.Marshal`로 직렬화하고 외부 입력은 `core.DecodeEvent`로 읽는다. DecodeEvent는 하나의 JSON object를 concrete pointer로 반환하며 미지원 kind/version, 모르는 필드, 필수 필드 누락/null, 중복 object key, 잘못된 타입 및 stage/result 모순을 거부한다. 직접 `json.Unmarshal`로 concrete struct를 채우는 방법은 discriminator·schema 검증이 없는 경로이므로 지원하는 ingress가 아니다. Marshal도 동일한 schema 검증을 사용한다. Go의 nil pointer marshal은 `null`이 될 수 있지만 DecodeEvent는 이를 이벤트로 받지 않는다. 세부 오류 경계는 현재 구현·회귀 테스트를 함께 확인한다.

키는 canonical lowercase ASCII `[a-z_]` 철자만 허용하고 대소문자·Unicode case-fold alias도 거부한다. Marshal 전에 Go 문자열의 UTF-8을 검사하고 DecodeEvent는 JSON 원문 bytes의 UTF-8 및 Unicode surrogate escape 짝을 검사한다. `\ud800`(백슬래시 하나로 시작하는 JSON escape) 같은 단독 surrogate가 U+FFFD로 치환되어 서로 다른 identity가 합쳐지는 입력을 거부한다. 백슬래시 자체가 escape된 literal 문자열은 허용한다. schema 검사 전 token 순회는 `maxJSONDepth = 32`의 명시적 재귀 깊이 제한을 적용한다(루트 깊이 0). 이는 총 입력 byte 크기·처리시간 상한을 보장한다는 뜻은 아니다. fork의 부모·자식이 같은 scope의 같은 PID면 거부하며, producer가 관측 scope의 의미를 일관되게 공급할 책임은 남는다.

이것은 **raw event 모델**이다. 기존 SECURITY_MODEL의 `policy_violation` 파생 예시는 변경하지 않으며 아직 구현하지 않았다. 공통 `event`, `process.pid/comm`, `session_id`, 주소·포트 의미를 보존하고 label·decision·policy는 Phase 3 이후에 다룬다.

**후속 확장 후보(아래 표 전체가 현재 구현됐다는 뜻은 아님):**

| 후보 필드 | 의미 |
|---|---|
| `schema_version` | 소비자가 해석할 버전 |
| `event_id`, `run_id` | 상관관계와 실행 구분 |
| `timestamp`와 순서 정보 | 관측 시점; 다른 clock을 혼합하지 않음 |
| `process` | 안정된 process identity, pid/tgid/parent 관계 |
| `kind`, `resource`, `access` | 접근 종류와 대상 |
| `observation_stage` | 시도·사전 검사·완료 중 어떤 사실인지 |
| `operation_result` | 성공/실패/unknown; 관측 안 했으면 추측하지 않음 |
| `truncated` 또는 quality 표시 | 경로·이벤트 일부 누락 여부 |

raw kernel layout에는 버전·길이·고정 폭 필드와 endian·padding 규칙이 필요하다. Go decoder는 짧은 record, 알 수 없는 kind, 잘린 경로를 정상 이벤트처럼 처리하지 않는다. event interface의 JSON round-trip은 concrete event를 선택하는 discriminator decode가 필요하므로 marshaling 테스트 하나로 충분하다고 보지 않는다.

### 7.3 라벨 상태 전이

초기 label은 `SECRET` 하나다. `PRIVATE_KEY` 예시는 현재 단일 label 계약과 충돌하므로 실행 가능한 예시에서 분리한다.

| 입력 | 권고 전이 | 주의 |
|---|---|---|
| source 조건에 해당하는 FileOpen | 해당 ProcessKey에 SECRET 추가 | 조건은 성공·접근 모드·훅 시점을 명시 |
| ProcessFork | 부모의 해당 시점 라벨을 child에 복사 | child identity 확인 |
| ProcessExec | 같은 주체의 라벨 유지 | 실행 파일 교체로 해제하지 않음 |
| ProcessExit | 해당 생애의 상태 정리 | PID 재사용 상태와 혼동하지 않음 |
| NetworkConnect | 현재 라벨을 policy에 전달 | 연결 시도와 성공 구분 |
| 센서 손실·map 실패 | 관측 건강 상태 변경 | 상태 부재를 clean으로 단정하지 않음 |

초기에는 임의 declassification이나 에이전트의 라벨 초기화 기능을 제공하지 않는 것을 권고한다. 향후 해제가 필요하면 누가 어떤 증거로 해제할 수 있는지 별도 모델이 필요하다.

### 7.4 정책 계약

기존 YAML의 기본 구조를 유지하는 단일 라벨 예시는 다음과 같다. 현재 실행 가능한 parser가 있다는 뜻은 아니다.

```yaml
sources:
  - path: "/workspace/fixtures/secret.txt"
    label: SECRET
rules:
  - name: prevent-secret-egress
    when:
      process_has: SECRET
    sink:
      network: external
    action: deny
```

P3-02에서 다음을 확정한다.

1. 기본안은 상대 source 경로를 policy 파일이 위치한 디렉터리 기준으로 해석하는 것이다. `~`, 환경변수 치환, command substitution은 초기 parser에서 지원하지 않고 오류로 처리한다. 권한이 높은 loader의 홈과 target의 홈을 혼동하지 않는다. 해석 결과는 내부 검증에 사용하고 일반 로그에는 D08에 따라 resource 별칭을 우선한다. 기존 `~` 예시는 PRE-01에서 절대 경로 또는 policy 기준 상대 경로로 정리한다.
2. source path와 실제 file identity의 연결: symlink, hardlink, rename, 교체, 삭제, mount 경계의 지원 범위를 명시한다.
3. `external`은 단순 문자열이 아니라 정해진 주소 분류 또는 명시적 CIDR 집합이다. RFC1918 주소를 무조건 안전하다고 해석하지 않는다. 테스트에서는 통제된 주소를 sink로 지정할 수 있어야 한다.
4. E06에 따라 여러 rule 매치 시 deny > audit > allow, no-match는 allow를 기본 계약으로 삼는다. 따라서 보호할 source/sink 규칙의 누락은 허용으로 이어짐을 문서·테스트로 명시한다. 모호하거나 미지원인 규칙을 no-match로 무시하지 않고 validation error로 처리한다.
5. unknown field·label·action·지원되지 않는 sink는 오류로 처리하는 방향을 권고한다. 무시하고 성공시키지 않는다.
6. access-control rule은 source 접근 시점, egress rule은 sink 시점 평가임을 구분한다. network-only evaluator로 direct file deny를 제공한다고 표시하지 않는다.
7. Phase 3의 deny는 audit record만 만든다. Phase 4는 kernel이 지원 가능한 규칙인지 추가 검증한다.
8. 정책 갱신·hot reload는 초기 요구사항이 아니다. 필요 시 버전·원자적 적용·기존 taint 유지 의미를 별도로 설계한다.

정책 입력은 한 번 제한된 크기로 읽고 검증한 **불변 snapshot**으로 한 run 동안 사용한다. 정책 파일이 실행 중 바뀌어도 자동 재읽기하지 않는다. 원본 snapshot digest·정규화 계약 버전·활성 kernel 표현의 식별자를 연결해 어떤 정책이 실제 적용됐는지 설명한다. hash는 변경 식별용이지 정책 작성자의 신뢰를 증명하는 서명이 아니다.

초기 parser의 구현 기본 상한은 파일 1 MiB, source 1,024개, rule 1,024개로 제안한다. 이는 실측 성능 보장이 아닌 입력 자원 한도이며, 구현 실험에 따라 이유와 회귀 테스트를 남겨 조정할 수 있다. kernel 표현이 더 작은 한도를 갖는다면 block 모드에서는 그 한도를 적용 전에 검사한다. 중복 YAML key·중복 rule 이름·다중 YAML document·alias/merge 확장·unknown field를 거부하고, CIDR/port/경로 오류는 target 시작 전에 보고한다. 단순 decode 성공을 정책 유효성으로 보지 않는다. 이 검증은 policy 계층의 책임이고 sensor에 YAML 해석을 넣지 않는다.

### 7.5 판정과 집행 결과

제안 schema는 다음 개념을 구분해야 한다. 필드 철자·enum은 E02~E06과 함께 확정한다.

```json
{
  "schema_version": "draft",
  "event": "policy_violation",
  "run_id": "example-run",
  "decision": "deny",
  "mode": "monitor",
  "action": "audit",
  "enforcement_result": "not_attempted",
  "operation_result": "unknown",
  "policy": "prevent-secret-egress",
  "labels": ["SECRET"]
}
```

`deny`는 정책 결과, `block`은 검증된 사전 거부, `kill`은 종료 조치, `audit`는 기록이다. 외부 수신 측 데이터가 없다는 단일 테스트 결과로 모든 전송이 항상 차단된다고 일반화하지 않는다.

### 7.6 장애·권한·시작 종료 계약

| 상황 | 권고 또는 확정해야 할 처리 |
|---|---|
| invalid policy | target 시작 전 오류 종료 |
| sensor 일부 attach 실패 | 요청 모드 충족 불가를 보고; 보호 성공으로 표시하지 않음 |
| ring buffer reserve 실패 | 별도 drop counter와 degraded 상태; 기존 state 갱신 실패와 구분 |
| taint/membership map 포화 | silent eviction 금지 권고; 집행 모드 실패 정책 E04/D05에서 결정 |
| target 시작 | 필수 sensor·policy·membership 준비 후 실행하도록 시작 barrier 검증 |
| supervisor 종료 | target·pinned map·link의 잔류 및 해제 순서를 명시 |
| target fork/exec storm | resource 상한·손실·CPU 사용 측정 |
| block 미지원 | D05에 따라 target 시작 전 오류 종료; 다른 mode로 자동 대체하지 않음 |
| audit 저장 실패 | 집행 상태와 증거 손실을 분리 보고 |
| 높은 권한 필요 | loader/supervisor 권한과 target 사용자·group·capability를 분리 |
| 제어·정책·로그 파일 | target의 변경·삭제 권한과 보안 영향 정의 |

trusted computing base에는 host/guest 커널, BPF loader, 승인된 정책 공급 경로와 AgentTaint 자체가 포함된다. 호스트 root·커널 침해를 막는 제품이라고 주장하지 않는다. target이 다른 머신·기존 daemon·원격 MCP에게 작업을 위임하면 그 실행이 자동으로 같은 경계에 들어오지 않는다.

### 7.7 이벤트 순서와 감사의 완전성

초기 audit pipeline의 기본안은 **하나의 공유 ring buffer → 순서를 보존하는 decode → 단일 상태 전이 소비 경로**다. formatter·파일 출력은 분리할 수 있지만, FileOpen/Fork/Connect의 판정을 여러 worker에 무순서로 배분하지 않는다. 최적화를 위한 sharding은 나중에 인과 순서와 동일 결과를 입증한 뒤 검토한다.

Linux ring buffer는 reservation 순서로 record를 소비자에게 제공한다. 다만 이는 필요한 source/fork 상태 갱신을 자동으로 만들거나 실제 operation 완료 순서를 보증하는 기능은 아니다. hook 시점·reservation 시점·state update 시점의 관계는 구현이 정의해야 한다. [Linux ring buffer 설계](https://www.kernel.org/doc/html/latest/bpf/ringbuf.html)

필수 계약:

1. 정규화된 수신 순서와 kernel 관측 timestamp를 구분한다. 사용자 공간에서 붙인 순번만으로 kernel 누락 여부를 판정하지 않는다.
2. timestamp 정렬로 손실 event를 복원했다고 주장하지 않는다. monotonic 시간은 지연 계산에, wall clock은 사람이 읽는 시각에 사용한다.
3. drop·decode 오류·queue 포화가 source/fork 이력을 손상시킬 수 있으면 해당 run의 audit 완전성을 `incomplete`로 유지한다. 이후 drop이 없어졌다는 이유로 이전 이력을 완전하다고 되돌리지 않는다.
4. `policy_verdict=allow`와 관측 품질을 분리한다. state가 없거나 이력이 누락됐을 때 정상 관측으로 확인된 untainted 상태와 구별한다.
5. Phase 4에서 kernel 집행 상태가 계속 유효한지와 감사 출력이 완전한지는 별도 상태다. audit 손실만으로 집행 실패라고 단정하지 않고, kernel state 실패를 단순 로그 문제로 축소하지도 않는다.
6. 같은 입력 열의 replay 결과가 결정적이어야 한다. 순서 교환·중복·누락 fixture를 따로 넣어 오류 감지와 명시한 의미를 검증한다. 동시에 발생한 독립 사건의 임의 전역 순서를 사실로 주장하지 않는다.

구현 소유자는 P2-03(순서·손실), P3-01/03(reference 상태·품질), P4-01/03(kernel 상태와 대조)이다. Phase 2에서 라벨 판정을 미리 구현하지 않는다.

### 7.8 실행 생애와 종료 결과

다음 상태명은 구현 가이드이며 새 CLI 옵션이나 현재 API가 아니다. P2-04에서 관측 lifecycle을, P4-01/03에서 집행 lifecycle을 확정한다.

```text
VALIDATING → ATTACHING → READY → RUNNING → DRAINING → CLOSED
      └────── 실패: target 미실행 ──────┘
RUNNING 중 이상 → DEGRADED 또는 FAILED (모드·실패 종류별)
```

- **READY:** 요청한 기능의 policy·hook·membership 준비를 확인한 뒤에만 target의 보호 대상 동작을 시작하게 한다. 프로세스를 이미 실행시킨 뒤 SIGSTOP을 보내는 방식이 시작 경합을 없앴다고 가정하지 않는다.
- **RUNNING:** root target 종료와 전체 run 종료를 구분한다. 기본안은 추적한 자손이 남아 있으면 감시·집행을 유지하는 것이다. POSIX 세션이 바뀌거나 부모가 종료됐다는 이유로 scope에서 빼지 않는다.
- **DRAINING:** 추적 대상 생애가 끝났음을 확인한 뒤 남은 record와 drop/health 상태를 정리한다. 고정 sleep만으로 event drain 완료를 증명하지 않는다. 최종 timeout·잔류 자손 처리 규칙은 시험 가능한 lifecycle 계약으로 남긴다.
- **CLOSED:** 생성한 자원만 소유권 식별자로 정리한다. 다른 run의 pinned map/link·fixture·파일을 이름 prefix만 보고 지우지 않는다. 초기 기본안은 불필요한 BPF pinning을 사용하지 않는 것이며, pinning이 필요한 설계라면 생성·복구·잔류 위험을 먼저 기록한다.
- **DEGRADED/FAILED:** 로그 저장 실패, 감사 손실, kernel state 실패, 일부 hook 해제, supervisor crash를 별도 분류한다. crash 후에도 보호가 지속된다는 주장은 현재 기본 보장에 없다. 보장하려면 별도 소유·생존성 설계와 장애 시험이 필요하다.

종료 결과에는 `target_exit`, `supervisor_status`, `coverage_status`, 잔류 target 여부를 분리한다. 정상 supervisor 종료에서는 root target 종료 코드를 보존하되, 보호 시작 실패·run 중 보호 실패를 target의 0 종료에 묻어 성공 처리하지 않는 방향으로 계약을 확정한다. 숫자 exit code와 signal 표현은 CLI 테스트와 함께 문서화하며 두 실패 원인을 하나의 숫자로 완벽하게 표현한다고 가정하지 않는다.

Ctrl-C·SIGTERM·timeout에 대한 target 전달, 자손 종료 대기, cleanup을 각각 검증한다. root PID나 process group 하나에 신호를 보냈다는 사실만으로 모든 자손의 종료를 보장하지 않는다. 강제 종료가 필요하면 이 역시 사후 조치이지 이미 발생한 효과의 취소가 아니다.

### 7.9 권한 profile과 제어 경로

기본 검증 profile은 **관리 권한으로 load하는 supervisor와 별도의 비특권 target**이다. `sudo agenttaint run`이라는 호출 형태만으로 target도 root로 실행해도 된다고 해석하지 않는다. 높은 권한으로 시작했는데 원래 target identity를 신뢰할 수 없으면 임의로 UID를 추정하지 않고 명시 identity를 요구하거나 시작을 거부하는 설계를 택한다. 정확한 옵션은 E09/P2-04에서 정한다.

P2-04 및 P4-01/04는 다음을 실제 target 내부 helper로 검사한다.

- real/effective/saved UID·GID, supplementary groups, capabilities, 접근 가능한 관리 socket. 일반 UID라도 sudo·Docker socket 등으로 동등한 관리 권한을 얻을 수 있는 경로를 별도 확인한다.
- target의 exec 이후 setuid/file-capability 재상승 제한. 기본 검토안은 지원 Linux 경로에 `no_new_privs`를 적용하는 것이지만, LSM 전이와 사용자 workload 영향을 검증하고 관련 규범에 반영한 뒤 적용한다.
- loader map/link/control FD가 exec 경계에서 target에 상속되지 않는지 확인한다. target에 허용할 stdin/out/err와 명시된 application FD는 loader 내부 FD와 구분한다.
- target이 supervisor·policy·audit 파일·BPF 제어 경로를 변경하거나 supervisor에 signal/ptrace를 보낼 수 있는지 확인한다. 파일 mode만 확인하고 같은 UID의 전체 권한 문제가 해결됐다고 판단하지 않는다.
- source 작업 디렉터리에서 탐색한 실행 파일이나 환경 설정을 높은 권한으로 실행하지 않는다. 명시 target 실행 권한과 loader의 tool 실행 권한을 분리한다.

`no_new_privs`는 exec로 새 권한을 얻는 경로를 제한하지만 이미 가진 권한을 제거하거나 모든 권한 변경을 막는 장치는 아니다. 이를 완전한 sandbox로 소개하지 않는다. [Linux no_new_privs](https://docs.kernel.org/userspace-api/no_new_privs.html)

초기 릴리스에서 위 경계를 충족하지 못하는 profile은 tamper-resistant 보호 지원으로 표시하지 않는다. 새로운 supervisor daemon·권한 브로커·컨테이너 관리 서비스를 자동 추가하지 않고, 필요한 최소 구조를 E09에서 검증한다.

## 8 addyosmani agent skills 도입 판단

### 8.1 무엇을 추가하는 패키지인가

확인한 upstream은 `addyosmani/agent-skills`의 `9d0c60d406b454a78ccc0a175b19932047aa4dac`이며 manifest 버전은 `0.6.11`이다. 실제 `skills/*/SKILL.md`는 25개로 확인했다. lifecycle 스킬 24개와 `using-agent-skills` meta-skill 1개를 합한 수로, manifest의 24 workflows라는 표현과 구분한다. 실제 설치 목록도 함께 기록한다.

이 패키지는 개발 절차와 체크리스트를 담은 스킬 모음이다. 일반 사용에 전용 서버·DB·MCP·Linux VM은 필수가 아니다. 기존 `claude-mem`과도 별개다. Go/eBPF 실행 환경을 설치하거나 보안 정확성을 보증하는 도구가 아니다.

근거: [검토한 README](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/README.md), [Codex manifest](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/.codex-plugin/plugin.json).

### 8.2 필요한 제반환경

| 항목 | 필요 조건 | 현재 판단 |
|---|---|---|
| 스킬 지원 AI host | Markdown 지시를 로드할 수 있어야 함 | Codex·Claude CLI 존재 |
| Git와 저장소 접근 | clone/marketplace 방식을 사용할 때 | Git 존재; 설치 자체는 미검증 |
| Node/npm/npx | `npx skills` 경로를 선택할 때만 | 현재 Node 24.14.0; native plugin에는 별도 필수 아님 |
| host 플러그인 기능 | native plugin 방식을 선택할 때 | Codex 0.160.0 help에서 명령 확인 |
| 프로젝트 경로 쓰기 권한 | 프로젝트 선택 설치 시 | `.agents`/`.codex`는 현재 세션의 보호 경로일 수 있음; 실제 설치 환경에서 확인 |
| 원본과 reference 보존 | 선택 스킬이 외부 reference를 읽을 때 | 단순 폴더 복사만으로 완결되지 않을 수 있음 |
| 추가 AI 계정 | cross-model CLI 검토를 선택할 때만 | 기본 요구사항 아님; 인증 확인 안 함 |
| Bash/jq 등의 hook 도구 | 선택 hook을 별도로 활성화할 때만 | 초기 도입에 hook 활성화 권고하지 않음 |

Vercel `skills` CLI의 조사 당시 GitHub main package metadata는 `1.7.0`, Node engine `>=22.20.0`였다. npm에 같은 버전이 배포돼 있는지는 확인하지 않았다. 설치 시점에 선택할 CLI 버전의 조건을 다시 확인하고 고정한다. 현재 Mac Node 버전은 이 조사한 하한보다 높다. [설치 도구 소스](https://github.com/vercel-labs/skills), [package metadata](https://github.com/vercel-labs/skills/blob/main/package.json)

### 8.3 선택 도입할 스킬

다음은 실제 upstream 이름이다. 앞선 분석에서 제안한 AgentTaint 전용 스킬과 혼동하지 않는다.

| 스킬 | 권고 시점·역할 | 프로젝트 보완 |
|---|---|---|
| `source-driven-development` | 우선: kernel/cilium API를 실제 문서·소스에서 확인 | 현재 고정 버전의 소스를 우선; 최신 예시에 맞춰 버전 변경 금지 |
| `documentation-and-adrs` | 우선: 규범 충돌과 설계 결정 기록 | 새 ADR 경로는 E01 이후; 현재 문서를 코드에 맞춰 조용히 수정하지 않음 |
| `code-review-and-quality` | 우선: 변경 검토 | 일반 품질 평가보다 보안 불변조건 우선 |
| `test-driven-development` | Phase 1~3: 상태 전이·정책 회귀 | Go 테스트 사용; 단위 테스트와 kernel 증거 구분 |
| `api-and-interface-design` | Phase 1: core Event·Decision 계약 | 웹·TypeScript 예시를 그대로 도입하지 않음 |
| `planning-and-task-breakdown` | 한 Phase 작업 분해 시 선택 | `tasks/` 자동 생성 대신 승인된 경로·산출물 사용 |
| `incremental-implementation` | 승인된 구현 작업에서 선택 | 자동 다음 Phase 진입·임의 commit 정책 적용 금지 |
| `security-and-hardening` | 보조 검토 | 웹 보안 중심 내용만으로 eBPF/IFC 전문 검증을 대체 못함 |
| `doubt-driven-development` | 중요한 미결정 설계에 명시적으로 선택 | 매 interactive cycle의 cross-model 질문·추가 CLI 조건을 이해하고 사용 |

우선 도입할 범위는 앞의 세 스킬을 제안한다. 실제 구현 단계에서 테스트·API 설계 스킬을 추가한다. 한 번에 모든 스킬을 항상 활성화할 필요는 없다.

참고 본문: [source-driven-development](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/source-driven-development/SKILL.md), [documentation-and-adrs](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/documentation-and-adrs/SKILL.md), [code-review-and-quality](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/code-review-and-quality/SKILL.md), [test-driven-development](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/test-driven-development/SKILL.md), [api-and-interface-design](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/api-and-interface-design/SKILL.md).

초기 제외 대상은 frontend/browser/web-performance/shipping 자동화다. 현재 CLI·Go·eBPF 작업 때문에 Chrome DevTools MCP, Lighthouse, Playwright, Node 웹앱 도구를 설치하지 않는다. `constraint-driven-development`는 도구·hook·CONSTRAINTS.md를 추가하는 절차를 포함하므로 단순 검토에서 실행하지 않는다.

### 8.4 설치 경로별 차이

아래 명령은 upstream에서 확인한 **향후 설치 예시**이며 이번에 실행하지 않았다. 원격 main을 이용하는 명령은 검토 SHA에 고정된 설치가 아니다. 재현 가능한 설치를 원하면 먼저 검토 SHA의 외부 clone을 확보하고 그 경로를 source로 사용한다.

설치 범위(project/local/user)와 설치 분량(선택 스킬/전체 팩)은 서로 다른 선택이다. 전체 plugin도 해당 프로젝트에서 시험할 수 있고, 선택 스킬도 사용자 전역에 설치할 수 있다. D06은 두 축을 모두 기록한다.

**Codex native plugin 경로**

```sh
codex plugin marketplace add addyosmani/agent-skills
codex plugin add agent-skills@agent-skills
```

upstream 안내의 최소 CLI는 0.122이고 현재 로컬 0.160.0에 관련 명령이 있다. 설치·검색 성공은 미검증이다. 이 경로는 전체 팩을 다루며, Claude 전용 slash command와 persona가 Codex subagent로 자동 등록되는 것은 아니다. 출처: [고정 Codex 설치 안내](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/docs/codex-setup.md).

**Claude Code plugin 경로**

```text
/plugin marketplace add https://github.com/addyosmani/agent-skills.git
/plugin install agent-skills@addy-agent-skills
```

설치 전 local/project/user 적용 범위를 명시한다. 설치 편의를 위해 전역 Git URL rewrite나 광범위한 permission bypass를 기본 적용하지 않는다. 원본 clone을 테스트할 때의 `claude --plugin-dir /path/to/agent-skills` 방식도 upstream에 있지만, 검토한 경로를 구체적으로 선택한 후 사용한다. [Claude plugin 공식 문서](https://code.claude.com/docs/en/plugins)

**선택 스킬 설치 경로**

```sh
npx skills add addyosmani/agent-skills --list
npx skills add addyosmani/agent-skills --agent codex \
  --skill source-driven-development \
  --skill documentation-and-adrs \
  --skill code-review-and-quality
```

`--list`도 npx 패키지를 내려받아 실행할 수 있으므로 완전한 오프라인 읽기 전용 조회가 아니다. 현재는 실행하지 않았다. 설치 시 CLI 버전을 확정하고 출력 경로·symlink·생성 설정을 확인한다.

Codex 공식 문서는 프로젝트 `.agents/skills`의 local discovery를 설명한다. 사용자 경로에 대해서는 외부 installer의 설명과 host 공식 설명이 다를 수 있으므로 둘을 동일시하지 않는다. 현재 세션에서 사용하는 `~/.codex/skills`가 존재한다는 사실만으로 모든 배포·버전의 탐색 경로를 일반화하지 않는다. [OpenAI 공식 스킬 문서](https://learn.chatgpt.com/docs/build-skills)

### 8.5 선택 설치의 reference 누락 문제

upstream은 개별 스킬 설치 시 저장소 최상위 `references/`가 누락될 수 있음을 명시한다. 예를 들어 code review·TDD·planning 스킬이 참조하는 공통 체크리스트가 끊길 수 있다. [upstream 이슈 361](https://github.com/addyosmani/agent-skills/issues/361)

선택지는 다음과 같다.

1. 검토한 전체 clone/plugin 구조를 유지해 상대 경로를 보존한다. 전체 팩의 자동 트리거 범위는 별도 관리한다.
2. 선택 스킬과 필요한 reference만 프로젝트 배포본으로 가져오고 링크를 조정한다. 원본 SHA·라이선스·변경 기록과 업데이트 검토 절차가 필요하다.
3. 설치하지 않고 프로젝트 전용 검증 절차를 작성할 때 해당 스킬을 참고한다.

D06의 기본안은 2번이다. 초기 세 스킬과 실제 참조하는 파일만 함께 관리하며 원본 SHA·라이선스·로컬 수정 기록을 남긴다. 세 폴더 복사만으로 완료됐다고 보고하지 않는다. 원본 루트의 `AGENTS.md`·`CLAUDE.md`를 AgentTaint의 같은 이름 파일에 덮어쓰지 않는다. upstream 업데이트 시 이 작은 배포본의 변경과 reference를 다시 확인한다.

### 8.6 자동화 충돌을 방지하는 도입 계약

- 외부 스킬은 현재 Phase의 작업을 보조한다. `Source → Label → Propagation → Sink → Decision`을 바꾸지 않는다.
- 외부 스킬의 기본 디렉터리·언어·commit·설치 절차보다 이번 작업의 사용자 범위와 프로젝트 규범을 우선한다.
- `using-agent-skills` 전체를 AGENTS의 항상 켜진 지시로 복사하지 않는다. native skill routing과 중복될 수 있다.
- upstream의 SessionStart helper는 확인한 두 plugin에 연결돼 있지 않다. 자동 hook 설치가 필수라고 가정하지 않는다.
- 선택 simplify hook 등 작업 파일을 임시 변경하는 도구는 초기 도입에서 제외한다.
- upstream `doubt-driven-development`는 interactive cycle마다 cross-model 선택을 묻는 절차가 있다. 자동 상시 보안 리뷰로 사용하지 않고 필요 시 명시적으로 선택한다. [해당 스킬](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/skills/doubt-driven-development/SKILL.md)
- `/build auto`는 upstream에서 spec 위치와 `tasks/` 산출물을 기대하며, 승인된 계획의 작업을 연속 수행하는 흐름이다. 이 파일을 자동으로 발견하거나 전체 로드맵을 안전하게 실행한다고 가정하지 않는다. AgentTaint에서는 **승인된 한 Phase만** 명시적으로 전달하고 경로·commit 정책을 조정해야 한다. [실제 build command](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/.claude/commands/build.md)

### 8.7 도입 수용 기준

- [ ] 원본 저장소·SHA·선택 스킬·원본 라이선스와 수정 내용을 기록했다.
- [ ] 모든 필요한 로컬 reference가 실제 설치본에서 열린다.
- [ ] host의 skill discovery 결과를 확인했다.
- [ ] Go 과제에 npm test나 웹 도구를 임의로 제안하지 않는지 smoke test했다.
- [ ] 규범 충돌 사례를 넣었을 때 자동 수정·다음 Phase 진입을 하지 않는지 확인했다.
- [ ] 불필요한 hook·MCP·worker가 추가되지 않았다.
- [ ] 파일 이동·권한 변경·commit·외부 호출의 실제 범위를 확인했다.
- [ ] 제거할 때의 정확한 대상과 복구 방법을 기록했다. 검증 없이 공유 스킬 폴더를 삭제하지 않는다.

## 9 프로젝트 전용 스킬과 서브에이전트

### 9.1 프로젝트 스킬

외부 팩만으로 eBPF·taint 보안 검증을 충족하지 못한다. 다음은 **새로 작성할 후보**이며 아직 설치된 스킬 이름이 아니다. 실제 스킬을 작성할 때는 해당 host의 skill-creator 지침을 확인한다.

| 이름 제안 | 입력 | 수행·출력 | 구현 시점 |
|---|---|---|---|
| `agenttaint-phase-preflight` | Phase ID·작업 ID·기준 commit | 규범·선행 완료·변경 허용 경로·검증 환경 목록 | PRE/TOOL |
| `agenttaint-docs-contract-check` | docs/prompts diff | Phase·버전·예시·규범 참조 충돌과 위치 | PRE/TOOL |
| `agenttaint-security-semantics-review` | diff·주장하는 보장·test evidence | 주장→훅→상태→테스트 대응표 | TOOL 및 매 보안 변경 |
| `agenttaint-ebpf-linux-verify` | commit·guest profile·scenario IDs | build/verifier/attach/행동 결과·환경·artifact | Phase 2부터 |
| `agenttaint-taint-policy-replay` | event vector·policy | reference state transition·Decision 비교 | Phase 3부터 |
| `agenttaint-adversarial-flow-test` | capability·지원 범위 | 합성 fixture 기반 경계·손실·우회 테스트 | Phase 2~4 |
| `agenttaint-phase-closeout` | 변경·검증 결과 | Required Final Report·미검증·다음 단계 | Phase 완료 때 |

각 스킬은 trigger, 입력, 읽을 규범, 허용 도구·경로, 수행 순서, 출력 schema, 중단 조건을 갖는다. 기계적으로 검증 가능한 작업은 script/test에 두고 스킬은 그것을 호출·해석한다. 기존 테스트와 같은 로직을 복제해 무조건 통과하는 스킬을 만들지 않는다.

### 9.2 서브에이전트 역할

기본 작업 인원은 구현 담당·보안 의미 리뷰어·검증 담당의 세 역할이다. 작업 크기가 작으면 하나의 에이전트가 순서대로 수행해도 된다. 많은 에이전트의 합의가 실제 증거를 대신하지 않는다.

| 역할 | 책임과 산출물 | 경계 |
|---|---|---|
| phase implementer | 한 Phase의 승인된 코드·단위 테스트 | 지정 경로만 수정; 규범 변경 임의 적용 금지 |
| security semantics reviewer | 모델·범위·판정·실제 조치의 모순 찾기 | 기본 읽기 전용; 작성자의 결론보다 원문 계약·diff 확인 |
| Linux verifier | guest에서 실제 load/attach·시나리오 검증 | 지정 guest·합성 데이터·검증 명령만 |
| eBPF reviewer | 필요한 경우 hook·state·경합·verifier 검토 | 확인 안 된 helper/signature 추정 금지 |
| adversarial test designer | 필요한 경우 경계·failure 시나리오 설계 | 실제 자격증명·외부 유출 서버 금지 |
| docs reviewer | Phase·경로·버전·schema·용어 정합성 | 의미 변경과 오기를 구분 |

upstream `agent-skills`에는 code reviewer, test engineer, security auditor, web performance auditor persona가 있다. 일반 리뷰용 참고이며 AgentTaint 역할 정의를 대체하지 않는다. Claude persona가 Codex native agent로 자동 변환된다고 가정하지 않는다. [upstream agents 안내](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/docs/agents.md)

공통 위임 계약:

```text
Task ID / Phase / baseline commit:
User authorization and scope:
Required documents:
Allowed write paths:
Read-only or mutation authority:
Expected evidence and acceptance criteria:
Known pending decisions:
Report: sources, exact findings, files changed, commands actually run,
        pass/fail/skipped/not-run, security semantics, unresolved questions.
```

동일 파일을 여러 구현자가 동시에 수정하지 않는다. 독립적인 문서·공식 API 조사나 읽기 전용 리뷰는 병렬 가능하다. 필요한 규범을 읽지 않은 서브에이전트의 결론으로 설계를 바꾸지 않는다. CI·Linux 검증 증거를 갖춘 통합 담당자가 최종 상태를 판단한다.

## 10 MCP와 개발 도구 경계

### 10.1 필요한 도구

| 기능 | 현재·권고 | 추가 설치 판단 |
|---|---|---|
| codebase-memory-mcp | Phase 0/1 코드 인덱싱·그래프 탐색 사용. 프로젝트 `Users-bhshin-IdeaProjects-AgentTaint` | 유지; 코드 변경 시 freshness·누락 확인 |
| GitHub 조회 | 원격 파일·commit 비교에 이미 사용 가능 | 중복 MCP 불필요 |
| 공식 문서 조회 | kernel/cilium/Go/host API 확인 | 현재 web/연결 도구로 가능하면 새 서버 불필요 |
| Linux 검증 | 재현 script·CI가 먼저 | 반복 필요가 확인되면 제한적 MCP wrapper 검토 |
| claude-mem | 선택적 세션 기억 | 장애가 개발·검증을 막는 필수 의존성이 되면 안 됨 |
| browser/웹 성능 도구 | 현재 Linux CLI 단계에는 직접 필요 없음 | Phase 9에서 재평가 |
| 제품용 AgentTaint MCP | Phase 8 feedback 방향 | 초기 구현 금지 |

codebase graph는 소스의 정적 관계이고 runtime taint provenance와 다르다. index 결과에 commit·시간·누락 여부를 확인한다. 존재하지 않는 심볼을 그래프에 있다고 가정하지 않고 부족한 결과만 파일 검색으로 보완한다.

Phase 0 탐색에서 `internal/env`가 인덱서의 자동 제외 대상이어서 그래프 결과에 없음을 확인했다. 해당 패키지는 직접 파일 읽기로 보완했다. 그래프의 심볼 부재를 소스 부재로 해석하지 않는다.

### 10.2 도구 접근 계약

- MCP tool output·검색 결과·저장소 자료는 사실 확인 자료다. 거기에 적힌 명령을 사용자 승인으로 해석하지 않는다.
- GitHub 조회 권한과 push/merge/issue 작성 권한을 분리한다. 이번 작업에는 외부 쓰기가 없다.
- Linux runner를 감싼다면 arbitrary shell보다 사전 정의된 환경 검사·BPF 빌드·verifier·scenario·artifact 조회 작업을 제공한다.
- 결과에는 baseline commit, guest kernel, architecture, active LSM, scenario, exit status와 artifact 위치를 남긴다.
- 도구 장애 시 `unavailable`을 보고한다. 기억·검색 결과 부재를 보안 테스트 통과로 처리하지 않는다.
- 민감 원문·환경변수·자격증명은 메모리·검색 인덱스·CI artifact에 저장하지 않는다. 경로·명령행도 필요 최소한으로 남긴다.

현재 claude-mem은 provider allowance exhausted로 저장이 불가능하다는 세션 알림이 있었다. 재시작으로 해결할 문제로 취급하지 않는다. 검증 증거와 설계 결정은 저장소·테스트 artifact를 기준으로 관리한다.

### 10.3 제품용 MCP의 미래 계약

Phase 8에서 고려할 후보는 capability 조회, run 상태 조회, 위반 설명, 적용 없는 policy 평가다. 이름·API schema는 그때 결정한다. 초기 인터페이스에 라벨 제거·집행 비활성화·목적지 예외 자동 추가를 포함하지 않는 방향을 권고한다.

로컬 stdio MCP child, 기존 local daemon, remote HTTP MCP는 서로 다른 관측 범위다. 서버가 target 계보 밖에서 실행되거나 원격에서 효과를 만들면 로컬 Linux sensor가 그 효과를 직접 본다고 주장하지 않는다. [MCP 보안 지침](https://modelcontextprotocol.io/docs/draft/tutorials/security/security_best_practices)

## 11 목표 디렉터리 구조

이 트리는 **제안된 전체 작업 지도**다. 현재 존재하는 파일과 미래 파일을 구분한다. 모든 디렉터리를 지금 생성하지 않는다. E01로 새 경로가 승인되고 해당 Phase가 시작될 때 생성한다. 기존 구조의 package 이름을 유지한다.

```text
AgentTaint/
├── AGENTS.md                         [현재] 공통 규범
├── CLAUDE.md                         [현재] 규범 import와 문서 지도
├── README.md                         [현재] 제품·지원 상태
├── .gitignore                        [현재] 산출물·민감 파일 패턴
├── go.mod / go.sum                   [현재 P0 / go.sum은 외부 의존성 도입 때]
├── Makefile                          [현재 P0] 공통 검증 진입점
├── cmd/agenttaint/main.go             [현재 P0] CLI wiring + 테스트
├── internal/
│   ├── core/                         [현재 P1] event/process/resource + codec/tests; label은 P3
│   ├── env/                          [현재 P0] doctor, read-only probe + 테스트
│   ├── runner/                       [현재 P0] resolve, launch + 테스트; lifecycle P2+
│   ├── sensor/
│   │   ├── linux/                    [현재 doc.go만] P2 load/attach/decode
│   │   └── darwin/                   [현재 doc.go만] native 결정 전 구현 없음
│   ├── taint/                        [현재 doc.go만] P3 reference transition engine
│   ├── policy/                       [현재 doc.go만] P3 parser/validation/evaluation
│   ├── enforce/                      [현재 doc.go만] P4 modes/capabilities/result
│   └── web/                          [현재 doc.go만] P9까지 기능 없음
├── bpf/                              [P2]
│   ├── process.bpf.c
│   ├── file.bpf.c
│   ├── network.bpf.c
│   ├── maps.h / events.h
│   └── vmlinux.h                     [생성] 출처·재생성 정책 E07
├── policies/example.yaml             [P3] 단일 SECRET 예시
├── examples/                         [P3] normal / sensitive-read / subprocess-exfil
├── test/
│   ├── fixtures/                     [P1~] synthetic events / files / raw records
│   └── integration/                  [P2~] Linux 및 enforcement scenario
├── scripts/                          [제안] 환경 점검·검증·문서 계약 검사
├── dev/lima/                         [제안] 재현 VM 설정 또는 기존 VM 구성 참고
├── .github/workflows/ci.yml          [현재 P0] portable Go CI; Linux BPF CI는 미래
├── .agents/skills/                   [제안] 프로젝트 전용·선택 외부 skills
│   └── agenttaint-*/SKILL.md
├── .claude/                          [선택 제안] Claude host용 adapter/agent definitions
├── .codex/                           [선택 제안] Codex host용 adapter/agent definitions
├── docs/
│   ├── IMPLEMENTATION_MASTER_PLAN.md [이 파일] 단일 종합 검토·작업 명세
│   ├── ARCHITECTURE.md               [현재] 규범
│   ├── SECURITY_MODEL.md             [현재] 규범
│   ├── THREAT_MODEL.md               [현재] 보장·공격자·한계
│   ├── ROADMAP.md                    [현재] Phase 방향
│   ├── RELATED_WORK.md               [현재] 비규범 조사 기록
│   ├── SUBORCHESTRATOR.md            [현재] 선택적 외부 pipeline 안내
│   ├── POLICY.md / EVENT_SCHEMA.md   [미래] 구현으로 계약이 확정될 때 분리
│   └── decisions/                   [제안] 승인된 ADR만; 지금 파일 생성하지 않음
├── prompts/                          [현재] 기존 Phase 번호 유지
└── deploy/                           [미래 P7] 초기 Kubernetes 내용 생성 금지
```

새 파일명·host adapter 포맷은 실제 생성 시 해당 도구 공식 문서와 대조한다. 루트에 별도의 거대한 `CONSTRAINTS.md`, `tasks/plan.md`, 중복 보안 모델을 자동으로 만들지 않는다. 사용자는 이번 결과물 하나를 요청했으므로 필요한 계약의 초안은 우선 이 파일에 모았다.

테스트 결과는 저장소 소스와 분리된 run별 임시 디렉터리 또는 CI artifact로 보관하는 것을 권고한다. 경로는 검증 시작 시 생성하고 정확한 경로를 기록한다. 실제 비밀이 들어갈 수 있는 로그 전체를 commit하지 않는다.

## 12 전체 작업 목록과 단계별 실행 계약

### 12.1 전체 체크리스트

이 표의 체크는 수용 기준 검증 완료를 뜻한다. 구현 중인 항목의 상세 상태는 12.10절을 따른다. 문서 작성 완료를 제품 작업 완료 체크로 바꾸지 않는다. PRE/ENV/TOOL은 준비 작업 ID이며 기존 제품 Phase 번호를 바꾸지 않는다.

| 완료 | ID | 작업 | 선행 조건 | 주 산출물 |
|---|---|---|---|---|
| [ ] | PRE-01 | 문서·Phase·예시 불일치 정리안 적용 | 문서 수정 요청 | 일관된 기존 문서 |
| [ ] | PRE-02 | 채택 기본안·허용 경로를 구체화해 규범에 반영 | 4절 기본안, 관련 E 설계 | 보장 표·정합적인 구조·현재 작업 범위 |
| [ ] | TOOL-01 | 선택한 외부 스킬 세 개와 reference 도입 | D06 기본안, E01 | source SHA·reference 검증·discovery 증거 |
| [ ] | TOOL-02 | 전용 preflight·docs·semantics 스킬 | E01 | 세 개의 좁은 검증 절차 |
| [ ] | TOOL-03 | 서브에이전트 역할·인계·후속 검증 절차 | TOOL-02, 해당 Phase 진입 | 역할별 변경 경계·검증 증거·세션 인계 |
| [ ] | MCP-01 | 기존 MCP·도구 접근과 장애 경계 점검 | 10절, 실제 사용 환경 | 그래프 freshness·최소 권한·장애 대체 절차 |
| [ ] | ENV-01 | 기존 ebpf-lab guest readiness 진단 | VM 사용 작업 승인 | 기능 표·부족 도구·재사용 판단 |
| [ ] | ENV-02 | 재현 BPF toolchain·생성물 정책 | ENV-01, E07 | 고정 생성 경로·빌드 재현 |
| [x] | P0-01 | Go 모듈·패키지 골격·CLI routing | Phase 0 착수 | 빌드 가능한 최소 구조 |
| [x] | P0-02 | doctor와 단일 커맨드 runner | P0-01 | 읽기 전용 진단·명시 실행 |
| [x] | P0-03 | portable CI 작성·검토 및 Phase 0 로컬 검증 | P0-02, CI 경로 승인 | 로컬 build/vet/test·runner 증거; 원격 CI not_run |
| [x] | P1-01 | identity·resource·event 모델 | Phase 0 완료, E02 | 플랫폼 독립 core; OS identity 수집은 별도 |
| [x] | P1-02 | event 의미·JSON codec·계약 테스트 | P1-01, E03 | 안정된 직렬화·오류 처리; 라벨 부여 기준 미구현 |
| [ ] | CI-01 | 기존 portable CI의 원격 실행 검증 | P0-03/P1-02, 별도 GitHub 쓰기 승인 | 정확한 commit의 Linux/macOS 실행 증거 |
| [ ] | P2-01 | process observer·decoder | Phase 1 완료, ENV-02 | fork/exec/exit 관찰 |
| [ ] | P2-02 | file·IPv4 connect observer | P2-01, E03 | source/sink 원시 관찰 |
| [ ] | P2-03 | run scope·출력·손실 상태 | P2-02, E08 | 계보 필터·별도 audit 스트림 |
| [ ] | P2-04 | start/stop·권한·Linux 실제 검증 | P2-03, E09 | 시작 경합·cleanup·실제 verifier 증거 |
| [ ] | CI-02 | 격리된 Linux BPF 검증 job | ENV-02, P2-04, 13.7절 | verifier·필수 scenario·잔류 자원 정리 증거 |
| [ ] | P3-01 | SECRET 상태 전이·replay | Phase 2 완료 | taint reference model |
| [ ] | P3-02 | YAML policy·평가·명시적 external | P3-01, E05/E06, D04 | 단일 라벨 policy와 검증 |
| [ ] | P3-03 | audit-only 통합·예시·증거 | P3-02, D08 | 네 가지 기본 시나리오와 확장 회귀 |
| [ ] | P4-01 | 동기 집행 아키텍처 결정·검증 설계 | Phase 3 완료, E04/E09, D05 | 승인된 kernel/Go 계약 |
| [ ] | P4-02 | 지원 source·network pre-op 집행 | P4-01 | 실제 EPERM·연결 거부 |
| [ ] | P4-03 | modes·지원 불가 처리·health·로그 | P4-02 | 실제 action과 상태를 표시 |
| [ ] | P4-04 | adversarial·장애·아키텍처별 검증 | P4-03, D07 | 지원 범위에 대한 증거 |
| [ ] | V-01 | Linux MVP 최종 보장 감사 | P4-04 | claim→test 표·Required Final Report |
| [ ] | DOC-01 | 사용자 안내·운영·문제 해결 문서 | 해당 구현 완료, 최종판은 V-01 | 설치·모드·지원 범위·진단·복구 안내 |
| [ ] | REL-01 | 재현 배포 산출물·의존성 검토 | V-01, 지원 환경의 CI 증거 | binary/BPF/source 대응·checksum·SBOM·라이선스 기록 |
| [ ] | REL-02 | 배포·설치·업데이트·제거·복구 | REL-01, D09 및 공개 배포 승인 | 승인된 배포 방식과 실제 설치/롤백 검증 |
| [ ] | REL-03 | 제한된 시범 사용·피드백·유지보수 | V-01, F5, DOC-01, 필요 시 별도 자격증명 승인 | 검증된 사용 사례·문제 접수·지원 범위 |
| [ ] | F5 | Mac에서 Linux VM을 사용하는 경험 정리 | V-01, D01 | VM 실행·진단·소스 동기화 UX; native는 별도 |
| [ ] | F6 | rich flow·다중 라벨 상세화 | 선행 Phase 완료 | pipe/file/IPC 전파 설계 |
| [ ] | F7 | Kubernetes 범위 상세화 | 로컬 IFC 완성·선행 Phase 완료 | cgroup/pod attribution 계획 |
| [ ] | F8 | 설명·피드백 인터페이스 상세화 | 선행 Phase 완료 | 읽기 중심 MCP/hook 계약 |
| [ ] | F9 | 로컬 웹 뷰어 상세화 | 이벤트 저장·조회 계약 및 선행 완료 | 단일 바이너리 뷰어 계획 |
| [ ] | F10 | payload metadata 확장 필요성 판단 | 실제 필요성·별도 승인 | 구현 여부 결정; 자동 착수 안 함 |

**설계 입력과 선행 조건의 구분:** E01~E09는 독립적인 실행 작업이 아니라 담당 작업에서 구체화할 설계 계약이다. E07을 확정하기 위해 ENV-02를 수행하고, E04를 검증하기 위해 P4-01을 수행한다. 표의 E 참조를 “이미 구현 완료돼야 설계 착수 가능”으로 해석하지 않는다. 다만 그 설계를 사용하는 후속 코드 작업에는 규범 정합성과 검증된 계약이 필요하다.

**최소 진행 경로:** 관련 PRE 정합성 정리 → P0 → P1 → P2 → P3 → P4 → V-01. ENV-01/02는 P2 전에 준비하고, TOOL-01/02는 개발 보조 경로다. 외부 스킬·메모리 서버의 설치 실패가 portable P0/P1의 필수 의존성이 되지 않게 한다. 단계 사이의 기존 완료 보고·범위 확인은 생략하지 않는다.

### 12.2 문서 정합성 준비 작업

**대상:** PRE-01, PRE-02. 제품 코드 구현 전 또는 관련 Phase 착수 전에 필요한 계약을 정리한다.

기존 문서 위치와 수정 후보(이미 고친 항목을 다시 작업하지 않도록 상태를 구분):

| 위치 | 최초 검토 항목 / 현재 남은 문제 | 최소 변경안 | 상태 |
|---|---|---|---|
| `docs/ARCHITECTURE.md` 플랫폼 표 | Kubernetes Phase 번호 | ROADMAP의 Phase 7과 일치 | Phase 0에서 완료 |
| `prompts/03-taint-engine.md`, `04-linux-enforcement.md` | 다중 라벨·Kubernetes 등 Phase 참조 오류 | ROADMAP의 Phase 6/7과 일치 | 미완료 |
| `SECURITY_MODEL.md` 정책 예시 | PRIVATE_KEY와 단일 SECRET Phase 충돌 | v0.1 예시와 미래 예시 구분 | 의미 정리 확인 필요 |
| `prompts/README.md` | 규범 문서 집합 | 규범 파일을 명시적으로 나열 | Phase 0에서 완료 |
| `ARCHITECTURE.md` 트리 제한 | Phase 2 생성물 범위 허용; 정확한 generator basename/suffix는 ENV-02에서 생성 전 기록 필요 | 기존 sensor 패키지의 생성 Go/object 및 해당 fixture·portable 검사만 허용 | G2 문서 반영 완료; 스킬·privileged CI는 별도 |
| `SUBORCHESTRATOR.md` | 오래된 .gitignore·untracked 서술, broad permission 권고 | 현재 상태 확인 절차와 최소 권한 안내로 교체 | 미완료 |
| `ARCHITECTURE.md` 및 `05-macos-observer.md` | ARCH의 Swift 필수 표현은 수정됐으나 Phase 5 프롬프트는 남음 | C API·언어 선택·VM 우선·native 별도 결정 구분 | 일부 완료 |
| `SECURITY_MODEL.md` access control 비교 | 조건부 접근 제어 자체가 불가능하다는 일반화 | 차별점을 동적 접근 이력·라벨 전파로 표현 | 미완료 |
| `03-taint-engine.md` TEST 4 | 단일 정상 사례를 오탐 없음 검증으로 표현 | 특정 정상 흐름 회귀 검증으로 명명 | 미완료 |
| `00-bootstrap.md` 수용 기준 | 개인 머신 PATH 상태 고정 | fixture로 PATH 안/밖 통제 | Phase 0에서 완료 |
| `00-bootstrap.md` doctor 검증 | 부모 PATH 불변만으로 읽기 전용 입증 | subprocess 환경·쓰기·임의 실행 여부 별도 검증 | Phase 0에서 완료 |
| `02-linux-observer.md` 출력 | audit JSON stdout과 target stdout 보존 충돌 | 별도 필수 감사 파일·안전한 생성·실패 경계 | G1 문서 반영 완료; 옵션 미구현 |
| AGENTS clang 검사 / ARCH doctor 계약 | Phase 2 toolchain 버전 검사와 외부 명령 비실행 계약의 접점 | 기본 비실행 유지, 신뢰된 절대 경로의 명시 probe만 제한 실행 | G4 문서 반영 완료; probe 미구현 |
| `03-taint-engine.md` 상태·exec | PID-only map 예시, exec를 부모→자식 전파처럼 표현 | ProcessKey 기반 상태와 동일 생애 exec 유지로 구체화 | P3 착수 전 |
| `04-linux-enforcement.md` fallback | 자동 kill과 D05의 block 미지원 시 시작 전 실패 충돌 | 명시 mode와 capability 계약 정리 | P4 착수 전 확인 |

참조: 기존 `AGENTS.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY_MODEL.md`, `docs/ROADMAP.md`, `prompts/00~04`.

수용 기준: 규범 충돌 목록이 없어졌거나 의도적으로 미결정으로 남겨졌고, 현재 구현 가능한 scope가 하나로 읽혀야 한다. 형식 오류·링크 오류를 확인한다. 보안 모델 변경은 추천안을 구체화한 뒤 필요한 검토를 받아 적용한다. 조사·경쟁 프로젝트 문장을 보안 보장의 증거로 사용하지 않는다.

### 12.3 환경과 툴링 준비 작업

**ENV-01:** 5절의 확인 명령을 guest에서 수행하고 `supported / unsupported / unknown / permission_denied / not_tested`를 구분한 표를 만든다. 실제 mount·forwarding과 synthetic workspace, 7.9절의 target 실효 권한 전제를 확인한다. 5.7절의 환경 manifest를 남긴다. 기존 VM이 부적합하면 이유와 최소 변경안을 제시한다. 무조건 새 VM·Docker·Colima를 설치하지 않는다.

**ENV-02:** `bpf2go@v0.22.0`, clang/LLVM, vmlinux.h 생성 입력을 정한다. E07의 `.go`·`.o` 함께 보존 기본안을 구체화하고 생성 파일 위치를 확정한다. BPF 빌드의 macOS host 의존성을 제거하고 Linux에서 재현한다. 생성 source와 object의 대응·provenance를 5.7절 manifest에 남기며 재생성 job은 차이를 보고하고 자동 commit하지 않는다. 실제 product BPF 프로그램은 Phase 2에서 구현한다. 준비 단계의 작은 toolchain probe는 제품 센서 완료로 세지 않는다.

**TOOL-01:** D06에 따라 8절의 세 스킬과 필요한 reference를 프로젝트 범위에 도입한다. 설치 환경의 쓰기 권한을 확인하고, scope·reference·자동 트리거·원본 SHA를 기록한다. 이미 선택한 설치 범위를 다시 질문하지 않는다. global 설정 변경은 project 도입과 묶어서 수행하지 않는다.

**TOOL-02:** 전용 세 스킬의 입력·출력·범위 제한을 작성한다. 테스트 프롬프트에 Phase 0에서 eBPF 구현을 요청하는 사례, audit를 block으로 부르는 사례, 미검증 Linux 상태를 완료로 부르는 사례를 넣어 올바른 대응을 확인한다. 스킬은 kernel correctness를 증명하지 않으며 그 증거를 요구하는 절차다.

허용 API·참고: Lima의 `limactl list/start/shell`, Linux BTF/LSM 파일, cilium/ebpf의 고정 버전 bpf2go, 선택한 host의 공식 plugin/skill 명령. 존재하지 않는 MCP 도구명이나 plugin 옵션을 추정해 실행하지 않는다.

### 12.4 Phase 0 Bootstrap

**입력:** `prompts/00-bootstrap.md`, 기존 ARCHITECTURE doctor/run 절, 필요한 문서 정합성 수정.

**파일:** `go.mod`, `cmd/agenttaint/main.go`, `internal/env/*.go`, `internal/runner/*.go`, 계획된 빈 package의 `doc.go`. 승인한 경우 Makefile·portable CI만 추가한다.

**작업:**

1. module path는 저장소와 일치시키고 Go directive `1.26`을 유지한다. 아직 필요 없는 cilium/YAML 의존성은 넣지 않는다.
2. CLI는 doctor/run으로 라우팅한다. core 보안 판단을 넣지 않는다.
3. doctor는 OS·Linux 기본 기능·CLI resolution 상태를 출력한다. Mac에서는 Linux backend가 동작하지 않음을 명시한다.
4. version 필드 수집 때문에 임의로 발견한 실행 파일을 무조건 실행하지 않는다. 읽기 전용 보장과 버전 수집의 관계를 설계하고 필요하면 unknown으로 보고하는 방안을 검토한다.
5. runner는 argv를 shell 문자열로 합치지 않고 유지한다. `--bin`, PATH, 지정 fallback 순서를 테스트한다. 다수 fallback 후보의 선택 규칙도 명시한다.
6. target stdin/stdout/stderr·종료 코드를 처리한다. 보안 감시는 아직 없다는 상태를 표시한다.

**검증:** gofmt, `go vet ./...`, `go build ./...`, `go test ./...`. fixture PATH를 이용한 발견·미발견·fallback·명시 경로 테스트, argv 보존·exit code 테스트. doctor가 부모 PATH를 바꿀 수 없는 일반 프로세스 특성만으로 안전성을 입증하지 말고 코드 경로·파일쓰기·추가 실행을 검증한다.

P0-03의 portable CI는 13.7절의 최소 권한·고정 action·비밀정보 없는 실행 기준을 따른다. 이후 BPF job 추가와 섞어 지금 privileged runner를 구축하지 않는다. 7.9절의 보호 launch 검증은 P2 이후 작업이며 Phase 0의 무보호 runner를 완전한 sandbox로 만드는 요구가 아니다.

**금지:** BPF, Event 실제 구현, policy, taint, enforcement, Kubernetes, 특정 AI에 대한 보안 분기. Phase 0이 끝났다고 보호 기능이 있다고 표시하지 않는다.

**완료:** Mac/Linux portable build 범위가 명확하고 테스트가 통과하며 기존 Required Final Report를 작성한다. 다음 단계 자동 실행은 하지 않는다.

### 12.5 Phase 1 Core Event Model

**입력:** Phase 0 완료, `prompts/01-core-event-model.md`, E02/E03.

**파일:** `internal/core/event.go`, `process.go`, `resource.go`, 관련 `_test.go`. label은 Phase 3 자리만 둔다.

**작업:**

1. 기존 요구 variant ProcessExec/Fork/Exit, FileOpen/Read/Write, NetworkConnect를 표현한다. 타입 존재와 실제 센서 지원을 구분한다.
2. identity·resource·access·관측 시점·result unknown 표현을 확정한다.
3. concrete event JSON discriminator와 decode 오류 처리를 구현한다. interface 자체를 바로 unmarshal할 수 있다고 가정하지 않는다.
4. schema와 synthetic fixture를 맞추고 외부 패키지 의존을 최소화한다.

**검증:** serialization/round-trip, unknown kind/version, 누락·잘못된 타입, PID 생애 구분. `internal/core`가 다른 internal 패키지나 OS 센서 타입을 import하지 않는지 검사한다.

**금지:** raw Linux 구조체, BPF 상수, 실제 taint 판정·네트워크 차단. 새 식별자가 기존 SessionID 의미를 덮어쓰지 않는다.

### 12.6 Phase 2 Linux Observer

**입력:** Phase 1 완료, `prompts/02-linux-observer.md`, ENV-01/02, E03/E08/E09.

**파일:** `bpf/process.bpf.c`, `file.bpf.c`, `network.bpf.c`, `maps.h`, `events.h`, 생성 vmlinux.h, `internal/sensor/linux/`, runner lifecycle, raw fixture·Linux integration tests.

**작업:**

1. 실제 커널 tracepoint/LSM 정의와 고정 라이브러리 예제로 프로그램 signature를 확인한다. 추측하지 않는다.
2. process observer부터 load/attach/cleanup을 검증하고 file open·IPv4 connect를 추가한다.
3. raw layout 길이·padding·endian을 명시해 Go decoder로 정규화한다.
4. run membership·fork·exit·재부모화 의미를 적용해 대상 외 event를 노출하지 않는다.
5. target 시작 준비·권한 분리를 설계하고 시작 직후 동작하는 프로그램으로 놓친 event를 검증한다.
6. 승인·반영된 G1 계약대로 대상 stdin/out/err를 보존하고 `--observe`에 필수 `--audit-file PATH`를 구현한다. 파일의 안전한 생성·크기 상한·실패 lifecycle을 먼저 확정하고 stdout/stderr fallback을 금지한다.
7. ring buffer reserve 실패·decode 실패·attach 실패를 계측한다. 경로 truncation을 정상 정확 경로로 취급하지 않는다.

P2-03은 7.7절의 순서·queue·손실 계약을, P2-04는 7.8~7.9절의 lifecycle·권한·내부 FD 계약을 적용한다. P2에서 보호 시작 전 입력(S31)의 한계를 기록하되 새로운 source 추적 기능을 구현하지 않는다. privileged CI는 13.7절을 따른다.

**검증:** raw decode 단위 테스트, 실제 guest verifier/load/attach, 빠른 child·fork/exec/exit, 통제된 file/connect fixture, scope 밖 noise, cleanup. Linux 필수 job에서는 권한 없음·skip을 green으로 취급하지 않는다.

**금지:** policy·taint·kill·deny 구현, TLS/payload 관찰. tracepoint 관찰 성공을 차단 성공이라고 표현하지 않는다.

### 12.7 Phase 3 Taint와 Policy Audit

**입력:** Phase 2 완료, `prompts/03-taint-engine.md`, E05/E06, D04/D08.

**파일:** `internal/taint`, `internal/policy`, 필요 label value, `policies/example.yaml`, 기존 examples 세 종류, replay·integration tests.

**작업:**

1. 하나의 SECRET label에 대해 7.3절 상태 전이를 구현한다.
2. 안정된 process identity로 상태를 관리하고 fork·exec·exit·PID 재사용을 테스트한다.
3. 승인된 YAML schema와 외부 목적지 정의를 구현한다. path·action·label·rule 충돌을 검증한다.
4. allow/audit/deny Decision을 만들되 어떤 것도 실제 차단하지 않는다.
5. Decision·mode·action·result의 구분을 audit output에 적용한다.
6. 정책과 동일 event vector를 반복 실행하면 동일 결과가 나오게 replay harness를 만든다.
7. 문서의 네 가지 시나리오와 경계 테스트를 자동화한다. real secret과 공개 인터넷 수신처를 쓰지 않는다.

P3-02에는 정책 snapshot·경로 기준·parser 상한·중복 key 거부를 포함한다. P3-03은 감사 불완전 상태를 출력하고 S32/S35/S37을 검증한다. 손실이 있는 audit에서 “violation 없음”을 안전 판정으로 바꾸지 않는다.

**검증:** Go state/precedence tests, parser invalid cases, replay, Linux end-to-end 네 가지 시나리오. child→parent pipe·기존 connection 사례는 unsupported limitation으로 분리해 재현하고 지원 성공으로 기록하지 않는다.

**금지:** 실제 kill/block, 다중 label 확대, payload 분류. 논리 deny를 실제 거부로 표시하지 않는다.

### 12.8 Phase 4 Linux Enforcement

**입력:** Phase 3 완료, `prompts/04-linux-enforcement.md`, P4-01 설계 검토, E04/E09·D05.

**P4-01의 필수 산출물:**

- kernel map과 Go reference state의 소유·업데이트 시점·동기성·lifecycle 계약.
- source 접근→label 부착→fork 상속→sink 검사 사이 경합 분석.
- kernel policy representation이 지원하는 rule subset과 불일치 거부 규칙.
- target 권한 분리·제어 경로 보호·시작 barrier·shutdown 처리.
- unsupported hook, map 포화, telemetry 손실, supervisor 장애의 동작 표.
- 기존 패키지 경계와 달라지는 지점, 최소 변경안, 승인 기록.

**구현 파일:** 승인된 `bpf/*.bpf.c`, maps, `internal/enforce`, policy 준비 경로, runner·env integration, Linux integration tests.

**작업:**

1. file_open과 socket_connect의 실제 pre-op 지원 및 반환 의미를 해당 guest에서 확인한다. LSM의 앞선 거부 결과를 덮어 허용하지 않는다.
2. 지원 source access와 새 IPv4 connect의 거부 경로를 구현한다. kernel 상태가 준비되기 전에 target을 실행하지 않는다.
3. 기본 monitor, notify, 명시 block/kill의 실제 동작과 capability를 구분한다. notify 전달 방식은 명시하고 원격 알림을 임의 도입하지 않는다.
4. kill은 post-operation 한계를 기록한다. signal 성공만으로 부작용이 없었다고 판단하지 않는다.
5. 요청한 mode와 활성 mode, 지원하지 않는 규칙, 실제 조치 결과를 보고한다.
6. 빠른 source→connect, fork, 여러 CPU, thread, 상태 용량·event 손실을 검증한다.

**검증:** syscall 반환값·상태·수신 측 결과·kernel audit를 함께 확인한다. 정책상 allow인 정상 흐름도 통과해야 한다. BPF-LSM 미지원 환경은 실제 별도 profile 또는 정확히 범위를 밝힌 simulation으로 테스트한다. 시뮬레이션이 실제 커널 검증을 대신하지 않는다.

direct file-deny와 source-read-allow→taint→network-deny는 서로 다른 정책 fixture로 테스트한다. 모든 source open을 거부하면 IFC egress 경로를 실제로 검증할 수 없다. 각 fixture의 기대 라벨·이벤트·errno·수신 측 결과를 따로 기록한다.

P4-03/04는 7.7~7.9절의 관측 품질·종료·권한 계약과 13.6절의 양성 대조 기준을 함께 적용한다. 감사 유실과 kernel 집행 상태 오류의 결과를 구분하고, source·fork 이력을 잃은 상태를 자동으로 clean으로 복구하지 않는다.

**금지:** 기존 연결·IPv6·pipe까지 자동 보장 확대, kernel race를 임의 sleep으로 숨기는 테스트, payload 검사·inline redaction. kernel 구현이 기존 규범을 위반해야 한다면 코드 변경 전에 설계 결정을 완료한다.

### 12.9 최종 검증과 이후 Phase

**V-01:** 현재 commit에서 claim→test→환경→결과를 대조한다. portable tests, Linux verifier·attach, mode별 actual effect, 지원 아키텍처, 알려진 한계가 README·doctor·로그·보고서에서 일치해야 한다. 성능 측정에는 workload·CPU·커널·event rate·drop count를 같이 기록하고 측정하지 않은 오버헤드 수치를 만들지 않는다.

F5~F10은 현재 구현하지 않는다. F5는 D01에 따라 VM 실행 경험을 정리한다. D02의 후속 보호 기준에 따라 F6에서는 자식→부모 전달을 핵심 시나리오로 다루고 기존 소켓 송신 통제와 함께 검증할 작업을 구체화한다. 이때 필요한 범위·Phase 프롬프트를 갱신하며, 기술 MVP 완료를 일반 AI 흐름 보호 완료로 기록하지 않는다. Kubernetes 배포로 로컬 정보 흐름의 빈틈을 해결하려고 하지 않는다.

### 12.10 작업 상태와 세션 인계

**현재 실행 기록 — 2026-10-03, 전체 목록 정의 및 순차 실행 준비**

- 사용자 요청: 전체 작업 목록 정의 후 순차 진행. 계획 작성은 완료했지만 아래 작업 전체의 구현 완료를 뜻하지 않는다.
- 후속 승인 범위: 직전 제시한 G1/G2/G4 최소 문서 변경안에 대한 사용자 “진행”에 따라 `docs/ARCHITECTURE.md`와 `prompts/02-linux-observer.md`를 수정했다. 감사 파일 분리, 기존 경로 안의 Phase 2 생성물 허용, 기본 비실행 doctor와 별도 명시 toolchain probe 경계를 반영했다. 새로운 제품 API·VM 작업·설치·권한 변경까지 승인된 것으로 확대하지 않는다.
- 전체 37개 상위 ID: `verified` 5개(P0/P1), 일부 진행 3개(PRE-01/02, ENV-01), 나머지 29개 미완료. 항목별 크기가 달라 개수 비율을 제품 완성도로 환산하지 않는다.
- 수행: 기존 docs/prompts·실제 core/CLI API 조사, 작업 카드/의존성/완료 조건 정리, 읽기 전용 Lima host preflight, 현재 `make check` 재검증.
- ENV-01 host preflight: Lima 2.1.4, `ebpf-lab` Stopped, VZ/aarch64, 4 CPU/4 GiB/20 GiB. 저장 설정의 `mounts: []` 및 유효 설정의 SSH agent/X11 forwarding 비활성 확인. 이는 runtime 격리 검증이 아니다.
- ENV-01 guest 진단: `not_tested`. `/Users/bhshin/.lima/ebpf-lab`가 현재 허용 쓰기 경로 밖이고 권한 상향도 불가하다. VM 시작은 시도하지 않았으므로 실행 실패·자동 승인 거부가 발생했다고 기록하지 않는다.
- PRE-02: G1/G2/G4 문서 계약은 반영했다. 감사 파일 크기/실행 중 실패 처리, probe 인자·시간/출력 제한·신뢰 기준, generator의 정확한 생성 이름은 각 구현 전에 기록·검증해야 한다. 다른 Phase의 규범 정합성 작업이 남아 PRE-01/02 전체는 계속 일부 진행이다.
- 제품 코드·설정·스킬 설치·VM·커밋·push 변경 없음. Phase 0/1 검증 이력과 소스 식별자는 아래에 보존한다.
- 다음 실행은 12.12절 G3의 허용된 Linux 검증 환경 확보부터다. G1/G2/G4를 다시 포괄 질문하지 않으며 새 충돌만 확인한다. 선행 미완료를 피해 Phase 3/4 코드를 먼저 작성하지 않는다.

**보존 이력 — 2026-10-03, Phase 1 로컬 수용 검증 완료**

| 항목 | 상태·증거·남은 작업 |
|---|---|
| 범위·선행 | Phase 0 완료 이후 `prompts/01-core-event-model.md`, 7.1/7.2, P1-01/02만 수행. 아래 Phase 0 이력은 당시 검증으로 보존 |
| P1-01 | `verified` — 플랫폼 독립 named IDs·process key/context·resource·7개 raw event 및 데이터 모델 검증 완료. 센서·정책·label 없음 |
| P1-02 | `verified` — schema 1 discriminator·stage/result·strict codec 및 독립 리뷰·수정·최종 회귀 검증 완료 |
| PRE-01/02 | `in_progress` 유지 — Phase 0 관련 일부 수정 외에 다른 Phase·정책 예시 등의 정합성 작업은 남아 있음 |
| 규범 호환 | Phase 1 프롬프트가 허용한 필드 구체화. POSIX 문자열 SID와 기존 공통 JSON 의미 유지; SECURITY_MODEL의 파생 policy_violation 예시는 미변경·미구현 |
| 미검증 | 안정된 birth/scope의 실제 OS 수집, Linux runtime·BPF·verifier, 원격 CI. 타입만으로 실제 관측·차단·전파를 보장하지 않음 |
| 다음 동작 | Phase 1 완료 보고 후 정지. 후속 요청 시 관련 PRE와 ENV-01/02부터 확인. 현재 새 사용자 선택이 필요한 사항은 없으며 Phase 2 자동 착수·VM 변경·설치·commit/push 없음 |

**Phase 1 Final Report — 최종 검증 완료**

#### Changed

- `internal/core/event.go`, `process.go`, `resource.go`, `doc.go`, `event_test.go`: 7종 raw event와 typed IDs·리소스·검증 codec·회귀 테스트.
- README·ROADMAP의 Phase 1 상태와 이 파일의 데이터 모델·작업 상태·검증 기록을 갱신했다. 센서·CLI의 보안 기능은 추가하지 않았다.

#### Architecture Decisions

- `SessionID`는 POSIX SID 문자열 직렬화를 유지하고 `RunID`·`ProcessKey`를 별도로 표현한다. E02는 모델 표현까지 구체화했으며 OS 생애 식별자 수집은 Phase 2 미검증 항목이다.
- E03은 관측 stage/result/unknown 표현까지만 확정했다. open/read의 라벨 부여 기준은 Phase 2/3에서 정하며 현재 라벨·정책 판단은 없다.
- 기존 `policy_violation` 예시를 변경하지 않았다. raw ingress는 `DecodeEvent`이고 직접 concrete `json.Unmarshal`은 검증 경로가 아니다.
- 다음 7개는 `go test ./internal/core -run TestAllEventsJSONRoundTrip -count=1 -v`가 실제 직렬화한 전체 객체다. 편집상 하나의 배열로 모았지만 codec이 이벤트 배열을 받는다는 뜻은 아니다. 각 예시는 독립 synthetic fixture여서 같은 `event_id`를 재사용하며 실제 관측 순서·수집 결과를 뜻하지 않는다.

```json
[
  {"event":"process_exec","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"unknown","operation_result":"unknown","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"executable":{"path":"/usr/bin/python"}},
  {"event":"process_fork","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"completed","operation_result":"success","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"child":{"pid":19343,"birth_id":"birth-b","scope_id":"scope-a","ppid":19342,"pgid":19300,"comm":"python"}},
  {"event":"process_exit","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"completed","operation_result":"success","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"exit_code":23},
  {"event":"file_open","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"unknown","operation_result":"unknown","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"resource":{"path":"/workspace/.env"},"access":"read"},
  {"event":"file_read","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"unknown","operation_result":"unknown","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"resource":{"path":"/workspace/.env"}},
  {"event":"file_write","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"unknown","operation_result":"unknown","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"resource":{"path":"./link/../result $HOME *"}},
  {"event":"network_connect","schema_version":1,"event_id":"event-1","run_id":"run-a","timestamp":"2026-10-03T00:00:00Z","session_id":"4021","observation_stage":"unknown","operation_result":"unknown","process":{"pid":19342,"birth_id":"birth-a","scope_id":"scope-a","ppid":19300,"pgid":19300,"comm":"python"},"destination":{"address":"1.2.3.4","port":443}}
]
```

#### Tests

- gofmt: `make check`의 형식 검사 통과.
- go vet: `make check`의 `go vet ./...` 통과.
- go test: 독립 fresh 실행에서 최상위 Test 27개 + fuzz target 1개, 일반 subtest 31개 + seed case 8개 통과, skip 0개. 6개 미래 stub 패키지는 테스트 없음.
- `make check`의 build/test 및 fresh race 검사 통과. 통합 담당자의 별도 재검사도 통과.
- 독립 리뷰에서 단독 surrogate의 치환, Unicode case-fold 키 alias, token 순회의 깊이 무제한 문제를 찾아 수정했고 해당 회귀·7종 round-trip을 다시 통과했다.
- fuzz 10초·병렬 2개 실행: 117,526회 실행, baseline 72개와 새 interesting 입력 37개. 이는 해당 실행의 검증 기록이지 성능 수치나 모든 입력에 대한 안전성 증명이 아니다.
- core의 직접 의존성은 표준 라이브러리 10개이며 전이 의존성의 non-standard package는 core 자신뿐임을 확인했다.
- native macOS 실행·CLI smoke 통과. Linux amd64/arm64 전체 패키지 cross-build와 CLI 바이너리 빌드 통과. Linux runtime과 원격 CI는 `not_run`이다.

검증 환경은 macOS arm64·Go 1.26.1이며, 명령은 저장소 루트에서 실행했다. 독립 검증 artifact 디렉터리는 `/private/tmp/agenttaint-phase1.JMROUp`이다. `make-check.log`, `test.jsonl`, `race.log`, `fuzz.log`, `core-deps.json`, 7개 전체 객체의 `events.jsonl`, `source-bundle.txt`와 native/Linux 바이너리를 남겼다. 임시 artifact는 영구 보관을 보장하지 않는다.

최종 소스 묶음 SHA-256: `851228228dcd999d4fa8567dce66186dfe1f8ad48d3f2630122ef1bfe99fe3a5`.
`go.mod`, `Makefile`, `.github/workflows/ci.yml`, `cmd/`·`internal/` 아래 모든 `.go` 총 20개 상대경로를 사전순 정렬하여 각 `UTF-8 경로 + NUL + 원본 bytes + NUL`을 연결한 SHA-256이다. `doc.go`는 포함하고 Markdown은 제외한다. native CLI 해시는 아래 Phase 0 native 해시와 동일했다(core 라이브러리는 CLI에 아직 연결되지 않음). hash는 검증 대상 동일성 표시이지 공급자 인증이 아니다. 기준 commit 위 dirty 소스에 대한 결과이며 이후 변경 시 관련 검사를 재실행한다.

#### Security Semantics

- what is detected: 아직 OS 동작을 감지하지 않는다. synthetic event 표현·입력 schema 유효성만 검사한다.
- what is blocked: OS 파일·프로세스·네트워크 동작을 차단하지 않는다. codec의 잘못된 입력 거부는 보안 집행이 아니다.
- what is NOT guaranteed: Byte-level taint tracking을 주장하지 않는다. post-event detection을 prevention이라고 부르지 않는다. 센서·라벨·정책·전파·실제 Linux 차단은 구현하지 않았다.

#### Limitations

- birth/scope의 OS 수집·실제 PID 재사용 처리·관측 완전성·순서·식별자 유일성은 미검증이다.
- observed UTF-8 경로는 stable file identity가 아니다. codec은 depth를 제한하지만 입력 총 크기·처리시간 제한 API는 제공하지 않는다.
- Linux runtime·BPF·원격 CI를 실행하지 않았다. 코드 데이터 모델의 portable 검사와 실제 OS 지원을 구분한다.

#### Next Phase

Do not implement it. Only describe the next expected step.

Phase 2 전에 남은 관련 PRE와 ENV-01/02를 확인하고 Linux guest readiness·재현 toolchain·process identity 수집 계약을 검증한다. 센서·VM 변경은 이번 Phase 1 완료 작업에 포함하지 않는다.

**보존 이력 — 2026-10-02, Phase 0 로컬 수용 검증 완료**

| 항목 | 상태·증거·남은 작업 |
|---|---|
| 요청 범위·기준 | 후속 “일단 진행해” 요청에 따른 Phase 0 및 관련 PRE 일부. 기준 commit은 문서 상단과 같고 변경은 현재 worktree에 있으며 commit/push하지 않음 |
| PRE-01/02 | `in_progress` — ARCHITECTURE의 테스트/portable CI 허용·doctor/run 계약, 프롬프트 수용 기준·규범 관계, Kubernetes Phase·macOS 언어 표현만 정리. 12.2절의 다른 Phase·정책 예시·SUBORCHESTRATOR 수정은 남음; 전체 완료 아님 |
| P0-01 | `verified` — Go 1.26 모듈, CLI routing, 미래 패키지의 빈 doc.go 및 portable build 확인 |
| P0-02 | `verified` — 읽기 전용 doctor·결정적 resolver·단일 커맨드 runner와 fixture 테스트. 독립 리뷰의 symlink/`..` 경로 오선택, 실효 실행 권한(0641) 판정, shell 복사 안내 인용 3건 수정·회귀 검증 완료 |
| P0-03 | `verified` (로컬 수용 범위) — Makefile·Linux/macOS portable CI 작성·검토 및 로컬 최종 검사 통과. 원격 GitHub Actions 실행은 `not_run`이며 CI green을 뜻하지 않음 |
| 도구·실환경 | 외부 스킬·AI CLI 설치, 사용자 설정 변경, VM 시작·변경, BPF 로드·attach는 수행하지 않음. Linux 실행 검증은 cross-build와 구분하며 완료 주장하지 않음 |
| 보안 의미 | Phase 0은 바이너리 경로·정적 환경 정보만 진단하고 지정한 커맨드를 실행함. 감시·taint·정책·차단·프로세스 트리 supervision 없음 |
| 다음 동작 | Phase 0 완료 보고 후 정지. 후속 요청 시 Phase 1 관련 남은 PRE·식별자 계약부터 확인하며 자동 착수하지 않음 |

최종 검증 환경은 **macOS arm64, Go 1.26.1**이다. 통합 담당자의 수정 후 검사와 독립 검증에서 다음 결과를 확인했다.

| 검증 | 결과 |
|---|---|
| `make check` | 형식·vet·build·test 통과 |
| `go test -count=1 -v ./...` | 최상위 17개 + subtest 24개 통과, skip 0개; 별도 7개 stub 패키지는 테스트 없음 |
| `go test -race -count=1 ./...` | 통과 |
| 최종 바이너리 빌드 | native macOS arm64 및 `CGO_ENABLED=0` Linux amd64/arm64 빌드 통과. Linux 실행 검증 아님 |
| native 바이너리 smoke | doctor JSON·version unknown·Mac 한계, literal argv·binary stdio·exit 23·잘못된 --bin 거부 통과 |
| 원격 CI·Linux runtime·BPF | 모두 `not_run`; VM 시작·설치·설정 변경·commit/push 없음 |

재실행에 사용한 Go 환경(저장소 루트 `/Users/bhshin/IdeaProjects/AgentTaint`에서 실행, 캐시 외 실제 사용자 설정 변경 없음):

```sh
export GOTOOLCHAIN=local
export GOPROXY=off
export GOCACHE=/private/tmp/agenttaint-phase0.5JNgRU/go-cache
export GOMODCACHE=/private/tmp/agenttaint-phase0.5JNgRU/go-mod-cache
make check
go test -count=1 -v ./...
go test -race -count=1 ./...
```

검증 명령은 저장소 루트에서 실행했다. 캐시·빌드 산출물 디렉터리는 `/private/tmp/agenttaint-phase0.5JNgRU`다. 임시 경로는 지속 보관을 보장하지 않는다. 기준 commit 위의 dirty 소스를 검증한 결과이므로 후속 세션은 현재 diff와 환경을 다시 확인하고 소스 변경 후 관련 검사를 재실행해야 한다. 이 기록이 미래 worktree의 통과를 보장하지 않는다.

검증 대상 식별자(문서 변경은 제외):

- 소스 묶음 SHA-256: `f6570788a66244d468ca03a360bc35782f9f4c2082063582b14fc2c8bac6db77`.
  `go.mod`, `Makefile`, `.github/workflows/ci.yml`, `cmd/`·`internal/` 아래 모든 `.go`의 총 16개 상대경로를 사전순 정렬하여, 각 `UTF-8 경로 + NUL + 원본 파일 내용 + NUL`을 연결한 값의 SHA-256이다.
- native `/private/tmp/agenttaint-phase0.5JNgRU/agenttaint-review` SHA-256:
  `84b2b7508293d6b9756884802539430196ea8093e1e6ac7c17101b413f97ba87`.
  이 해시는 검증한 대상의 동일성을 확인하기 위한 것이며 공급자 인증·코드 안전성 증명은 아니다.

다음 기록은 이 문서의 해당 작업 또는 작업 완료 보고에 남긴다. 새 이슈 관리 서비스·별도 계획 파일을 필수로 추가하지 않는다.

| 필드 | 기록 내용 |
|---|---|
| `task_id / phase` | 12.1절의 실제 작업과 Phase |
| `status` | `planned / ready / in_progress / blocked / verified` |
| `baseline` | commit, 관련 dirty 변경 식별값 |
| `scope / owner` | 허용 변경 경로, 작업 담당 역할, 사용자 요청 범위 |
| `inputs` | 읽은 규범, 채택 D/E, 선행 작업의 실제 증거 |
| `verification` | 실제 명령·환경·scenario·결과 artifact |
| `remaining` | 실패·미실행·가정·규범 변경 필요 사항 |
| `next_action` | 이어받는 AI가 수행할 한 작업과 필요한 재검증 |

`ready`는 그 작업을 시작할 입력·권한이 있다는 뜻이고 `verified`는 필수 수용 기준의 실제 증거가 있다는 뜻이다. Linux 필수 검증이 없으면 portable test만 성공해도 Phase 2/4 전체를 `verified`로 바꾸지 않는다. 체크박스 완료는 `verified`에만 대응한다. 이는 프로젝트 문서상의 상태 표기이며 외부 에이전트 시스템의 goal 상태를 자동 변경하는 지시가 아니다.

세션 중단·담당 교체 때에는 변경 파일, 마지막 성공·실패 명령, 아직 실행하지 않은 검사와 남은 자원(실행 프로세스·VM 상태·attach 등)을 기록한다. 다른 AI의 “완료” 요약만으로 체크하지 않고 현재 worktree와 artifact를 확인한다. 새 commit이나 관련 diff가 생기면 영향받는 증거를 다시 만든다. 사용자 변경을 되돌리거나 남은 프로세스를 무단 종료하는 지시로 해석하지 않는다.

### 12.11 전체 작업 카드와 순차 실행 순서

이 절은 12.1절의 **37개 상위 작업을 실행 가능한 범위로 풀어 쓴 목록**이다. 같은 ID는 같은 작업이며 이중 집계하지 않는다. 제품 기능뿐 아니라 문서·환경·AI 도구·테스트·운영·배포까지 포함한다. 미래 작업의 목록화는 지금 구현하거나 새로운 파일 경로를 자동 허용한다는 뜻이 아니다.

**이번 계획의 준비 조사(제품 Phase 0과 별개):** AGENTS, ARCHITECTURE, SECURITY_MODEL, THREAT_MODEL, ROADMAP, 기존 Phase 프롬프트, 이 문서 5/7~10/13절을 기준으로 삼았다. 코드 그래프로 실제 core/runner/CLI 심볼을 확인했고, 환경 담당자는 설치된 Lima의 help/list와 해당 VM 설정을 확인했다. 설계·목록 작성과 구현·독립 검증 역할을 분리했다.

현재 재사용할 수 있는 계약은 `core.Event.Kind() EventKind`, `core.DecodeEvent([]byte) (Event, error)`, concrete event의 `encoding/json.Marshal`, CLI의 `doctor`/`run`, `make check`/`make race`다. `internal/runner/runner.go`의 `Run`은 무보호 커맨드 실행이지 observer supervisor가 아니다. `--observe`, `--audit-file`, `doctor --probe-toolchain`, `make generate-bpf`, policy evaluator, enforcement API는 아직 존재하지 않는다. 아래의 그런 이름은 **구현 제안**이며 실행 가능한 명령으로 복사하지 않는다. Linux 수집 helper·hook signature는 ENV-02/P2에서 실제 고정 버전 소스와 guest BTF로 확인한다.

각 카드를 실행할 때는 **① 참조 원문 읽기 → ② 범위·허용 파일 확인 → ③ 구현 → ④ 독립 리뷰·검증 → ⑤ 12.1 체크 및 12.10 증거 갱신**을 따른다. 미래 API를 추측하거나 문서만 작성하고 기능 완료로 체크하지 않는다. Phase 완료 보고는 매번 남기고, 다음 Phase는 이전 수용 기준과 그 Phase의 범위·권한이 확인된 경우에만 시작한다. 12.12절처럼 실제 게이트가 있으면 순차 진행 요청만으로 이를 생략하지 않는다.

#### A. 규범·도구·환경 준비

| ID / 현재 상태 | 실행할 일·주요 파일 또는 대상 | 선행·완료 기준·금지 |
|---|---|---|
| PRE-01 / 일부 진행 | `docs/`, `prompts/`의 Phase 번호·예시·현황 오류를 12.2 표와 대조. 단일 SECRET과 PRIVATE_KEY 미래 예시 분리, SUBORCHESTRATOR의 오래된 현황·과도한 권한 안내 수정안 작성 | 기존 Phase 0 수정은 보존. 의미 변경은 PRE-02로 분리. 링크·버전·Phase 참조 검증 후 완료; 경쟁 제품에 대한 단정으로 보안을 증명하지 않음 |
| PRE-02 / 일부 진행 | E01/E08/E09와 D05에 따른 문서 충돌별 최소 변경안 작성. 첫 범위는 감사 출력 분리·생성 파일 정확한 위치·Linux 실행 권한 계약 | 관련 사용자 확인 뒤 ARCHITECTURE/현재 prompt부터 갱신. 변경 전 충돌 위치·이유 기록. 후속 모든 Phase를 한 번에 포괄 승인한 것으로 취급하지 않음 |
| TOOL-01 / 미착수·선택 | 8절의 고정 upstream 세 스킬과 필요한 references를 프로젝트 범위에 도입. provenance·원본 SHA·변경점·host discovery·제거 대상 기록 | E01/설치 경로 쓰기 권한 필요. 스킬 설치 실패는 제품 개발을 차단하지 않음. 전역 설정·전체 팩·hook을 함께 설치하지 않음 |
| TOOL-02 / 미착수·선택 | 9.1절의 preflight/docs-contract/security-semantics 세 절차부터 skill-creator로 작성. phase-closeout, Linux verifier, replay, adversarial 절차는 해당 Phase에 도달할 때 추가 | 승인된 경로만 사용. 정상/범위 위반/증거 없음 프롬프트로 동작 검증. 스킬의 답변을 실제 kernel 시험으로 대체하지 않음 |
| TOOL-03 / 미착수·운영 보조 | 9.2절의 implementer/reviewer/verifier 역할, 파일 소유권, 인계 필드, 중단·재개 절차 정리. 필요할 때만 eBPF/adversarial 담당 추가 | 서로 다른 에이전트가 같은 파일을 동시 수정하지 않음. 호스트별 agent 설정 API는 확인 후 사용. 별도 orchestration 서버가 필수는 아님 |
| MCP-01 / 미완료·기존 도구 사용 중 | 10절의 그래프 freshness/제외 경로, GitHub 읽기 대 쓰기, tool output의 신뢰 경계, 메모리 장애 fallback 점검 | 실제 도구 목록과 권한·실패 사례를 기록. graph는 runtime taint가 아님. 새 MCP 서버·DB·원격 업로드를 기본 의존성으로 추가하지 않음 |
| ENV-01 / 일부 진행·guest 대기 | 기존 `ebpf-lab`의 host 설정 점검 다음 guest kernel/config/BTF/LSM/도구/권한/공유/forwarding 확인. 결과는 5.7 manifest에 기록 | 현재 막힘은 12.12 참조. `supported/unsupported/unknown/permission_denied/not_tested` 구분. 설정 파일만으로 실제 guest 상태를 합격 처리하지 않음 |
| ENV-02 / 미착수 | Linux guest에서 pinned Go/cilium/clang/LLVM/bpftool 경로 확인·준비. BTF 기반 `vmlinux.h`, bpf2go 생성 `.go`/`.o`, endian/architecture·생성 명령·입력 해시 결정 | ENV-01 및 정확한 생성 경로 승인 필요. 도구 설치·커널/부팅 설정 변경 전 범위 확인. 작은 빌드 probe와 실제 제품 sensor 검증을 구분 |

#### B. 완료된 기반 작업과 portable CI

| ID / 현재 상태 | 산출물·재사용 위치 | 완료 증거·남은 경계 |
|---|---|---|
| P0-01 / 완료 | `go.mod`, `cmd/agenttaint`, 패키지 골격 | Phase 0 로컬 build/vet/test 증거. 미래 stub은 기능 구현이 아님 |
| P0-02 / 완료 | `internal/env/doctor.go`, `internal/runner/runner.go`, CLI·fixture tests | 읽기 전용 진단·literal argv·스트림·exit 보존 검증. 감시·프로세스 트리 supervisor 미구현 |
| P0-03 / 완료·로컬 범위 | `Makefile`, `.github/workflows/ci.yml` | portable workflow 작성·검토 및 로컬 검사 완료. 원격 CI 실행은 별도 CI-01 |
| P1-01 / 완료 | `internal/core/event.go`, `process.go`, `resource.go` | 7종 event, named ID, 생애 key, IPv4 모델. 실제 birth/scope 수집은 P2 |
| P1-02 / 완료 | `internal/core/event_test.go`, strict codec | 7종 JSON·unknown·잘못된 입력·Unicode·깊이 제한·race/fuzz 증거. 새 sensor가 보낸 이벤트도 이 계약에 맞춰야 함 |
| CI-01 / 미실행 | 기존 portable GitHub Actions를 검토한 commit에서 실행하고 Linux/macOS 결과 수집 | commit/push/원격 실행은 별도 허용 범위. 로컬 성공이나 workflow 파일 존재만으로 원격 green 표시 금지 |

#### C. Phase 2 — Linux 관찰, 아직 차단 없음

필수 참조: `prompts/02-linux-observer.md`, ARCHITECTURE Event Pipeline, 7.1/7.2/7.7~7.9, 12.6. raw layout·BPF 생성 코드·integration fixture를 추가할 **정확한 경로는 PRE-02/ENV-02에서 먼저 확정**한다.

| ID | 세부 구현·담당 경로 | 수용 기준·선행 |
|---|---|---|
| P2-01 | `bpf/process.bpf.c`, `maps.h`, `events.h`, `internal/sensor/linux/sensor.go`: fork/exec/exit, stable ProcessKey 정규화, 버전·길이·padding/endian raw decoder, bpf2go load/attach/close | ENV-02 후. guest verifier·load·attach·빠른 fork/exec·PID 재사용·thread exit 구분 증거. process가 시작되기 전에 관측 준비가 됐는지 P2-04에서 최종 통합 |
| P2-02 | `bpf/file.bpf.c`, `network.bpf.c`: 파일 open과 IPv4 connect, 관측 단계·결과·접근 모드·경로 품질 정규화 | P2-01 후. 정상/실패 operation 및 truncation·비UTF-8 처리 테스트. 실제 read 성공을 open 시도로부터 추정하지 않음. FileRead/Write 타입 존재를 센서 지원으로 확대하지 않음 |
| P2-03 | runner/sensor/CLI: 명시 run과 자손 scope, run 간 격리, 감사 스트림 분리, queue·drop·decode 오류·불완전 이력 | P2-02 및 출력 계약 승인 후. 대상 JSON/binary stdout 보존, scope 밖 noise 제외, S25/S27/S32/S37. 데이터 손실 뒤 clean 상태로 자동 표시 금지 |
| P2-04 | runner/env/CLI/Linux integration: attach-before-exec, target 비특권 실행, 내부 FD 비상속, 취소·root 종료·잔류 child·drain·cleanup | P2-03 후. S10/S11/S13/S29/S31/S33/S34/S38 및 실제 guest 수용 시험. kernel test skip을 완료로 세지 않음. taint/policy/kill/deny 미구현 유지 |
| CI-02 | 격리·일회성 Linux 검증 job과 environment/artifact manifest. BPF 재생성·verifier·observer scenario·정리 검증 | P2-04 후 검증 자동화. 13.7 최소 권한·cache 격리·필수 scenario 수 확인. 외부 PR을 개발자 VM이나 재사용 root runner에서 자동 실행하지 않음. P4 도달 후 enforcement job 추가 |

#### D. Phase 3 — Taint·정책·감사

필수 참조: `prompts/03-taint-engine.md`, SECURITY_MODEL, 7.3/7.4, 12.7. **P2 실제 Linux 수용 완료 전 착수하지 않는다.**

| ID | 세부 구현·담당 경로 | 수용 기준 |
|---|---|---|
| P3-01 | `internal/core/label.go`, `internal/taint/engine.go`: SECRET 상태, source→label, fork 시점 snapshot, exec 유지, exit 정리, PID 생애 분리, replay | 수집된 사실에 맞는 라벨 부여 기준을 먼저 문서화. 전파 로직을 sensor/CLI에 중복 구현하지 않음. 동일 event vector의 동일 상태 전이, 순서 교란·재부모화·thread·손실 한계 테스트 |
| P3-02 | `internal/policy/model.go/parser.go/evaluator.go`, `policies/example.yaml`: YAML 검증, source 경로 계약, rule 우선순위, allow/audit/deny, 명시적인 external 집합, run 시작 policy snapshot | 오타·미지원 action/label·중복 key·크기/다중 문서·경로 모호성 거부. 사설 IP나 유명 LLM API 자동 예외 없음. snapshot 변경·정상/거부/감사 규칙 경계 검증 |
| P3-03 | runner/policy/taint audit 통합, `examples/normal`, `sensitive-read`, `subprocess-exfil`, replay/integration tests | 정상 흐름·직접 source→sink·자식 sink·audit-only 네 기본 시나리오. Decision deny와 실제 operation 성공을 구분. payload/전체 argv/env 수집 없음. child→parent·기존 소켓은 미지원 한계로 재현 |

#### E. Phase 4 — 실제 집행과 Linux MVP 감사

필수 참조: `prompts/04-linux-enforcement.md`, AGENTS 보안 의미, 7.5~7.9, 12.8, 13.6. P4-01의 승인된 kernel/Go 책임 계약 없이 구현하지 않는다.

| ID | 세부 구현·담당 경로 | 수용 기준 |
|---|---|---|
| P4-01 | 동기 커널 상태와 Go reference model의 소유·갱신·수명·policy snapshot·race 계약. hook capability와 장애 행동표·권한 profile 설계 | Phase 3 완료 후 실제 kernel 가능성 검증 및 규범 충돌 제안/사용자 확인. 비동기 Go 이벤트 처리를 pre-op 동기 판정처럼 설명하지 않음 |
| P4-02 | 승인된 `bpf/*.bpf.c`, maps, `internal/enforce/enforce.go`: 지원 source file-open·새 IPv4 connect의 pre-op 거부, 앞선 LSM 거부 존중 | direct file deny와 read-allow→taint→network-deny를 별개 fixture로 검증. syscall errno·상태·수신 측·allow 대조군이 함께 증명돼야 PASS |
| P4-03 | CLI/enforce/env: monitor/notify/block/명시 kill, 요청/활성 mode·실제 action/result, unsupported 처리·health·감사 로그·종료 의미 | D05 수정안 확인 후 block 미지원이면 target 시작 전 실패. kill은 사용자가 명시한 경우만. 로그 손실과 커널 집행 상태 손실을 구분 |
| P4-04 | Linux integration/adversarial: source→connect race, 여러 CPU/thread, map 포화, attach/loader 장애, supervisor 방해·FD·권한 재상승, timeout cleanup | S17~S38의 해당 사례와 arm64/amd64 실제 검증. bounded synchronization 사용, race 숨기는 sleep 금지. 기존 소켓/IPv6/pipe 미지원은 따로 표시 |
| V-01 | 현재 source/object/policy/environment를 묶은 claim→test 감사, 지원표·성능 측정 조건·README/doctor/log 일치 | P4-04 후. 실제 Linux 부작용까지 확인한 범위만 보장. 제한된 기술 MVP와 일반 AI 전체 유출 방지는 다름. 필수 검증 누락 시 완료 불가 |

#### F. 사용자 문서·배포·운영

이 그룹은 제품 기능을 조용히 늘리는 작업이 아니라 이미 구현한 기능을 재현·사용·유지하기 위한 작업이다. CI-01/02와 함께 13.7절에 암묵적으로 있던 일을 명시적인 ID로 분리했다. 공개 릴리스·계정·서명 키·라이선스 선택은 별도 확인 전 실행하지 않는다.

| ID | 작업·산출물 | 선행·완료 기준 |
|---|---|---|
| DOC-01 | 기존 README/docs에 설치/실행/진단, 지원 kernel/CPU/mode, 감사·차단 차이, 권한 오류·BTF/LSM 문제, 정책 예시·한계·자원 정리 안내 | 해당 기능 검증 때마다 갱신, V-01 후 최종 검토. 명령 예제를 실제 synthetic 환경에서 재현. 새 문서 경로가 필요하면 먼저 허용 |
| REL-01 | 배포 후보 binary/BPF object/source/toolchain 대응, architecture별 재현 빌드, checksum, dependency/license inventory 및 SBOM 형식 선정 | V-01과 배포 대상별 CI 증거 필요. 생성물·의존성을 누락하지 않는지 검증. checksum을 서명 또는 안전성 증명으로 부르지 않음 |
| REL-02 | 선택한 배포 방식의 설치·업데이트·제거·이전 버전 복구 절차, 서명/배포 provenance, 릴리스 노트·지원 범위 | REL-01, 소유자 D09와 외부 공개 권한 확인 후. 로컬 설치 검증과 실제 GitHub Release 게시를 분리. 기존 사용자 데이터/정책을 무단 삭제하지 않음 |
| REL-03 | 제한된 시범 사용의 workload·지원 약속·성공/실패 기준, 문제 접수·보안 제보 경로·의존성 업데이트 및 회귀 절차 | V-01/F5/DOC-01 후. 먼저 합성 데이터; 실제 AI 계정/자격증명이 필요하면 별도 승인. 실제 워크로드의 미지원 데이터 경로를 숨기지 않음 |

#### G. MVP 이후 기능 목록 — 상세 API는 해당 Phase에서 확정

아래는 **향후 실행 카드**다. 기존 순서 Linux → Mac VM → richer flow → Kubernetes를 지킨다. 특히 F10은 전체 목록에 포함돼 있어도 자동 구현 대상이 아니다.

| ID / 분해할 하위 작업 | 목표·관련 경로/참조 | 진입·완료 조건 |
|---|---|---|
| F5 / 환경 profile → 소스/fixture 전달 → 실행 UX → 설치·문제 해결 검증 | 5절, `prompts/05-macos-observer.md`의 VM 방향 정합화. Mac host 편집과 Linux guest 실행을 안내하고 감사 산출물 회수·자격증명/공유 최소화 | V-01 후. AI/도구/AgentTaint가 guest에 있을 때의 지원만 검증. source 전달은 공유 home 없이 수행. native Endpoint Security 구현은 별도 결정 없이는 제외 |
| F6 / 전달 경로 위협 모델 → file/pipe/IPC 객체 identity → child→parent 전파 → 다중 label → 기존 연결/전송 제어 검증 | `prompts/06-rich-flow.md`, taint/core/sensor/policy 확장. inherited FD/stdin/env/mmap 등은 지원/미지원 목록을 먼저 작성 | F5 및 상세 규범 확인 후. 새로운 source·propagation·sink마다 hook·상태·실제 effect 증거. 기존 연결 송신 통제는 별도 확장 설계가 필요하며 단순 fork 상속으로 해결됐다고 표시 금지 |
| F7 / cgroup↔pod 귀속 → 종료/재사용 → 최소 권한 배포 → 정책 배포 → 다중 tenant 검증 | `prompts/07-kubernetes.md`, `deploy/daemonset.yaml/configmap.yaml/rbac.yaml`, Linux sensor 재사용 | 로컬 IFC와 선행 Phase 완료 후. hostPID/privilege·업데이트·노드 차이·pod attribution 증거. 첫 배포에 CRD/operator를 자동 추가하지 않음 |
| F8 / 피드백 계약 → 읽기 중심 MCP/hook → 인증·권한·rate/size 제한 → host 통합 검증 | ROADMAP Phase 8, 10.3절. 아직 없는 상세 프롬프트부터 작성·확인 | F7 및 저장/조회에 필요한 데이터 계약 확인 후. capability/run 상태/위반 설명 중심. remote/기존 daemon의 scope 경계 유지, 자동 라벨 제거·정책 완화 API 금지 |
| F9 / 로컬 저장·보존/삭제 → query → localhost HTTP → 내장 UI → 브라우저 보안·접근성 검증 | `prompts/09-local-web-dashboard.md`, `internal/web`, ARCHITECTURE의 dashboard stub | F8 및 영속화 계약 승인 후. loopback 접근 제어·CSRF/웹 입력·민감 경로 표시·로그 용량 검증. SaaS/OAuth relay/외부 업로드 기본 추가 금지 |
| F10 / 필요성 검토 → 명시적 범위 승인 → metadata hook 가능성 → 별도 수용 시험 | `prompts/10-payload-aware-llm-sink.md`, SECURITY_MODEL의 조건부 확장 절 | 실제 요구와 별도 승인 전 보류. prompt/response 분류·DLP 전환 금지. 정적 TLS·hook 부재·관찰 대 집행의 차이를 검증. 불필요 판정이면 구현하지 않는 것이 정상 결과 |

**실행 순서:** 완료 P0/P1 재확인 → 현재 PRE-02/ENV-01 게이트 해결 → ENV-02 → P2-01→02→03→04 → Phase 2 보고 → P3-01→02→03 → Phase 3 보고 → P4-01 설계 확인→02→03→04 → V-01 → F5 → F6 → F7 → F8 → F9. F10은 조건부다. TOOL/MCP는 선택 보조 작업, CI/DOC/REL은 해당 선행 완료와 별도 권한이 있을 때 끼워 넣는 작업이며 다음 제품 Phase의 검증을 생략하는 우회 경로가 아니다.

현재 G1/G2/G4 문서 정합성은 반영했으며, 다음 실행을 막는 것은 스킬 개수가 아니라 **허용된 Linux 검증 환경(G3)**이다. 상위 작업 간 달력·공수는 아직 추정하지 않는다. ENV-01/02에서 실제 kernel/toolchain 상태를 확인하기 전 eBPF 작업 기간을 확정할 근거가 없기 때문이다.

### 12.12 현재 실행 게이트와 재개 절차

#### 지금 필요한 확인

| 게이트 | 확인한 근거·이유 | 최소 제안 / 재개 조건 | 현재 상태 |
|---|---|---|---|
| G1 감사 출력 계약 | 기존 prompt의 audit stdout 요구가 대상 stdout 보존과 충돌 | 승인 후 ARCHITECTURE/prompt에 target stdin/out/err 보존과 `--observe`의 필수 `--audit-file PATH` 계약 반영. 새 일반 파일 `0600` 배타 생성·기존 파일/symlink/FIFO/장치 거부·소유자/권한/경합 검증. 크기 상한·쓰기 실패 lifecycle은 P2-03/04에서 구현 전 확정; stdout/stderr 감사 fallback·자동 보안 kill 금지 | **문서 반영 완료**, 옵션·제품 기능 미구현 |
| G2 생성물 경로 | 기존 tree에 bpf2go 생성 쌍의 정확한 이름이 없음 | 승인 후 기존 `internal/sensor/linux`의 sensor.go 및 실제 생성 `.go`/`.o`, 기존 `bpf/` C/header·생성 `vmlinux.h`, 기존 fixture/integration·Makefile·portable CI 수정만 허용. 정확한 basename/arch/endian suffix는 ENV-02에서 생성 전에 확인·기록. 새 package·skill 경로·privileged CI는 제외 | **문서 반영 완료**, 생성물 미생성·ENV-02 대기 |
| G3 Linux 실행 권한 | VM Stopped. 상태 경로 `/Users/bhshin/.lima/ebpf-lab`는 이 세션 writable roots 밖이며 approval escalation 불가 | 허용된 Linux 실행 환경이 연결된 세션에서 이어가거나, 사용자가 VM을 검토·시작한 뒤 허용된 guest 명령 실행 경로를 제공. 단순히 Mac에서 시작만 해도 현재 세션의 SSH/control 접근이 자동 허용된다고 보장하지 않음 | **환경 권한 필요**, 시작 미시도 |
| G4 doctor toolchain 검사 | AGENTS의 Phase 2 clang 검사와 기본 doctor 비실행 계약의 접점 | 승인 후 기본 doctor/AI CLI 탐지는 계속 비실행, 신뢰된 명시적 절대 경로의 compiler/LLVM/bpftool만 별도 opt-in probe로 실행하도록 반영. 후보 `doctor --probe-toolchain`은 미구현. 고정 버전 인자·timeout·출력 상한·신뢰/권한 기준은 구현 전에 기록. PATH/프로젝트 fallback·shell·승격·설치·mount·BPF load 금지. 절대 경로/실행 권한은 신뢰 증거가 아니고 버전 실행의 무부작용이나 BPF capability를 보장하지 않음 | **문서 반영 완료**, probe 미구현 |

G1/G2/G4는 사용자 후속 “진행”으로 승인된 최소 문서 정리이며 기존 보안 모델·고정 버전·Phase 0 이력은 변경하지 않았다. 이 승인은 G3의 세션 권한을 늘리거나 미래 Phase의 규범 변경까지 허용하지 않는다. `.lima`를 임시 경로로 복제하거나 LIMA_HOME을 바꿔 제한을 우회하지 않는다.

**G3의 시작 부작용:** 현재 VM 설정 `lima.yaml:7`은 `mounts: []`다. `:10` 이후 provisioning에는 `apt-get`, `modprobe`, tracefs mount가 있다. 유효 설정은 user containerd·host resolver·proxy 환경 전파를 활성화하고 plain mode가 아니다. 실제 재시작에서 어떤 provisioning이 실행될지는 아직 확인하지 않았으므로 시작을 단순 읽기 전용 진단이라고 부르지 않는다. 기존 readiness의 CONFIG_BPF/bpftrace 성공만으로 Go·clang BPF target·bpftool·BTF·활성 BPF-LSM을 갖췄다고 판단하지 않는다.

**허용된 환경에서의 다음 한 작업:** ENV-01을 완료한다. 현재 목록과 설정을 재확인하고, 선택한 VM의 시작·진단 범위가 허용된 경우에만 guest에서 5.3절의 읽기 전용 검사들을 실행한다. 실제 출력으로 5.7 manifest를 채운 뒤 부족한 패키지·권한·커널 기능에 대한 최소 수정안을 제시한다. 바로 `sudo apt install`, 커널 재부팅, BPF attach 또는 실제 AI 실행을 묶어서 수행하지 않는다.

#### 나중에 필요한 확인 — 지금 질문을 늘리지 않음

- P3: PRIVATE_KEY 예시와 단일 SECRET, 정책 경로·external 집합·우선순위의 규범 정합성. 실제 라벨 부여 조건은 선택 hook 증거를 보고 정한다.
- P2 정규화: 필수 birth/scope/SID/time을 얻지 못하거나 경로가 truncated/non-UTF-8이면 정상 core event로 꾸며 보내지 않는다. 현재 schema 1에는 quality 필드가 없으므로 별도 진단·drop/health 경로로 표현할지 먼저 구체화한다. core schema 변경이 필요하면 변경 전에 다시 확인한다.
- P4: `prompts/04-linux-enforcement.md`의 자동 kill fallback과 D05의 시작 전 실패가 충돌한다. 요청 block 미지원 시 실패하고 kill은 명시 선택만 허용하는 안을 권고하며 P4 전에 확인한다. Go 전파 모델과 동기 kernel state 소유 계약도 P4-01에서 별도 확인한다.
- TOOL: `.agents`/`.codex` 같은 보호 경로와 정확한 host discovery 규약·라이선스. 현재 개발에 추가 MCP·스킬 설치가 필수는 아니다.
- CI/REL: commit/push·원격 실행·유료 runner·서명 키·공개 릴리스·라이선스(D09)는 별도 권한·소유자 결정이 필요하다.
- F5/F10: native macOS 보호나 payload 관련 범위는 기존 non-goal/조건부 경계를 유지한다. 전체 목록에 적혔다는 이유로 자동 착수하지 않는다.

## 13 테스트와 완료 증거

### 13.1 테스트 계층

| 계층 | 실행 위치 | 확인하는 것 | 확인하지 못하는 것 |
|---|---|---|---|
| portable unit | Mac / Linux | parser·identity·decoder·reference state·CLI | 실제 kernel 관찰·집행 |
| deterministic replay | Mac / Linux | 주어진 event 열에 대한 상태·판정 | event가 빠짐없이 수집됐는지 |
| BPF build/verifier | Linux guest / CI | 생성·로드·verifier 수용 | 실제 훅의 모든 동작·정책 정확성 |
| observer integration | Linux | 실제 source/process/sink 관찰·scope | pre-op block |
| enforcement integration | BPF-LSM 활성 Linux | operation 거부·실제 side effect | 미지원 경로의 완전한 유출 방지 |
| adversarial/failure | 격리 Linux | 경합·손실·경계 사례 | 모든 공격에 대한 일반적 안전성 증명 |

Go `-race`는 Go 코드의 race 검출이고 kernel/Go 간 논리적 race를 모두 검출하지 않는다. BPF verifier 통과 역시 의도한 보안 정책이 맞다는 증명이 아니다.

### 13.2 시나리오 목록

`PASS`는 명시한 기대 결과에 대해서만 사용한다. 아래 unsupported 시나리오는 우회가 재현됐다는 사실을 제품 지원 통과로 바꾸지 않는다. 향후 지원한다고 주장할 때 기대 결과와 Phase를 함께 바꾼다.

| ID | 시나리오 | 기대·증거 | 단계 |
|---|---|---|---|
| S01 | 비민감 파일 → 새 연결 | taint 없음, allow, 불필요한 violation 없음 | P3 |
| S02 | 민감 파일 → 새 연결 | SECRET, policy deny; P3에서는 action audit | P3 |
| S03 | 부모가 민감 파일 접근 → fork → child connect | 정확한 상속·Decision | P3 |
| S04 | 부모가 정상 파일 접근 → fork → child connect | 정상 회귀 | P3 |
| S05 | SECRET → exec → connect | exec 이후 라벨 유지 | P3 |
| S06 | process exit → PID 재사용 | 이전 생애 상태 누수 없음 | P3 |
| S07 | failed open / write-only open / O_PATH | 확정한 source 의미와 일치 | P2~3 |
| S08 | thread A source 접근 → thread B connect | process/thread 단위 계약 준수 | P2~4 |
| S09 | source 직후 지연 없는 connect | E04의 동기 집행 보장; sleep 의존 없음 | P4 |
| S10 | 빠른 fork/exec·target 즉시 동작 | 시작 barrier와 membership 검증 | P2~4 |
| S11 | reparent / setsid / double fork | 원래 run scope 유지 또는 한계 명시 | P2~4 |
| S12 | 상대 경로·symlink·hardlink·rename·교체 | E05 지원 범위와 일치 | P3~4 |
| S13 | ring buffer 부하·drop | drop 보고·health 변화, false healthy 없음 | P2~4 |
| S14 | membership/taint map 포화 | 결정한 오류 정책·명시 결과 | P3~4 |
| S15 | invalid policy / 미지원 rule | target 시작 전 실패 또는 정한 계약 | P3~4 |
| S16 | monitor + deny rule | 로그는 deny, operation은 막지 않음 | P3~4 |
| S17 | file-open block | syscall 거부, 실제 접근 제한, audit 근거 | P4 |
| S18 | 새 network connect block | connect 거부와 통제된 수신 측 결과 | P4 |
| S19 | kill mode | 대상 종료; 이미 일어난 효과는 별도 기록 | P4 |
| S20 | BPF-LSM 없음 + block 요청 | D05 결정과 일치, silent downgrade 없음 | P4 |
| S21 | supervisor crash·detach·로그 저장 실패 | 정한 lifecycle/health 의미와 일치 | P4 |
| S22 | 먼저 connect → source → 기존 소켓 send | 초기 connect 모델의 알려진 한계로 기록 | 문서화·P4 한계 검증 |
| S23 | child source → pipe → parent API | 초기 전파 한계로 기록 | 문서화·미래 F6 |
| S24 | IPv6 / UDP without connect | 미지원 범위의 구체화, 지원 주장 금지 | 한계 검증 |
| S25 | 같은 파일을 다른 run이 접근 | run 간 상태·로그 혼합 방지 | P2~4 |
| S26 | 기존 daemon·remote MCP가 작업 수행 | run scope 밖 효과를 보호했다고 표시하지 않음 | 한계 검증 |
| S27 | target stdin/stdout에 JSON·binary 출력 | audit와 대상 데이터가 섞이지 않음 | P0/P2 |
| S28 | target가 policy/control/log를 수정 시도 | 정의한 권한 경계와 threat model 일치 | P4 |
| S29 | guest 내부 FS와 host 공유 FS | 파일 동일성 차이를 분리 기록 | ENV/P2~4 |
| S30 | arm64와 amd64에서 동일 지원 시나리오 | 각각의 kernel·verifier·실행 증거 | D07에 따라 |
| S31 | 보호 전에 열린 파일 FD·stdin redirection·env 입력 | 초기 source 범위 밖임을 명시; 라벨 없음 ≠ 비밀 없음 | P2~4 한계 검증 |
| S32 | 여러 CPU/worker의 source·fork·connect 순서 및 replay 교란 | 정한 인과·소비 순서 준수, 누락·중복 의미 명시 | P2~4 |
| S33 | root target 종료 뒤 살아 있는 자식과 취소/timeout | 남은 scope의 감시 유지, 종료·drain·잔류 상태 보고 | P2~4 |
| S34 | target의 exec 재상승·group/capability·관리 socket | 지원하는 비특권 profile 준수; privilege 누출 없음 | ENV/P2~4 |
| S35 | 정책 파일 변경·중복 key·다중 문서·크기 상한 | run snapshot 불변, invalid 입력은 시작 전 실패 | P3~4 |
| S36 | 수신 서버 미준비·양성 대조 실패·다른 원인의 거부 | AgentTaint 차단 PASS로 오인하지 않음 | P4 테스트 harness |
| S37 | 손실 후 추가 drop 없이 실행 계속 | audit 불완전 이력 유지, 집행 건강 상태와 분리 | P2~4 |
| S38 | target의 supervisor signal/ptrace·내부 FD 접근 | 제어 권한 경계 증거, 미지원 profile은 지원으로 표시 금지 | P2~4 |

### 13.3 검증 진입점 제안

Phase 0에서 `make check`와 `make race`를 구현했다. 나머지는 아직 존재하지 않으며 E01 승인과 해당 Phase 착수 후 구현할 인터페이스 제안이다.

| target | 상태 | 의미 |
|---|---|---|
| `make check` | 현재 구현 | 포맷 확인·vet·portable test·build |
| `make race` | 현재 구현 | 해당 host에서 가능한 Go race tests |
| `make generate-bpf` | 미래 제안 | 고정 Linux toolchain으로 생성 |
| `make verify-bpf` | 미래 제안 | 실제 guest 로드·verifier 증거 |
| `make test-linux` | 미래 제안 | observer integration; 필수 환경 없으면 명시 실패 |
| `make test-enforce` | 미래 제안 | BPF-LSM·mode별 집행 검증 |
| `make check-docs` | 미래 제안 | 링크·Phase·schema 예시·규범 목록 검사 |

일반 `go test ./...`에서 Linux 전용 테스트를 build tag 또는 runtime skip으로 분리할 수 있지만, 전용 Linux job에는 반드시 실행된 scenario 수를 기록한다. `go test`가 성공했어도 모든 kernel 테스트가 skip이면 그 단계는 미검증이다. Go fuzzing은 decoder/parser의 의미 있는 경계 입력에 사용한다. 테스트를 늘리는 목적은 불변조건 검증이며 단순 구조 복제 테스트를 만들지 않는다.

### 13.4 결과 artifact

테스트 실행마다 아래 정보를 남긴다. 필드명은 구현 때 확정할 수 있다.

```json
{
  "source_commit": "<tested commit>",
  "dirty_worktree": false,
  "environment": {
    "os": "linux",
    "arch": "arm64",
    "kernel": "<measured>",
    "active_lsm": ["<measured>"],
    "btf": "<measured>",
    "toolchain": "<pinned versions>"
  },
  "scenario_id": "S18",
  "requested_mode": "block",
  "effective_mode": "<measured>",
  "result": "pass|fail|skipped|not_run",
  "skip_reason": null,
  "verifier_log": "<artifact reference>",
  "operation_result": "<observed>",
  "receiver_result": "<observed>",
  "drops": "<measured>"
}
```

dirty worktree에서 테스트했다면 commit만으로 결과를 재현할 수 없으므로 변경 diff 또는 내용 hash를 함께 기록한다. 테스트 output·명령행·ELF debug 정보의 절대 경로에 민감 데이터가 섞이지 않도록 확인한다. artifacts에는 synthetic fixture만 사용한다.

추가 manifest에는 `verification_run_id`, task/Phase, UTC 시작·종료 시각, 5.7절 환경 manifest 참조, **실제 실행 바이너리·BPF object·policy snapshot·fixture의 내용 hash**를 묶는다. 명령별 cwd·exit status·timeout·stdout/stderr artifact를 구분하며 command 기록은 자격증명·민감 argv를 제거한다. 실행 파일과 다른 commit의 테스트 로그를 하나의 결과로 묶지 않는다. 검증되지 않은 자기보고 문자열보다 관측 가능한 syscall·receiver 결과를 함께 사용한다.

### 13.5 지원·성능 판정 기준

초기 합격 조건은 기능과 의미에 대한 조건이다. 성능 목표 수치는 사용자 workload가 정의된 뒤 정한다. 임의의 “5% 이하 오버헤드” 같은 수치를 근거 없이 완료 기준에 넣지 않는다.

필요한 측정은 target wall time, CPU/RSS, event rate, drop count, map 사용량, source→판정 latency, 지원 mode다. 정상 workload·짧은 프로세스 다수·연속 파일 접근·연속 연결을 분리한다. 측정 대상에 없는 플랫폼·커널은 `not tested`로 표시한다.

### 13.6 오판을 막는 통합 테스트 harness

통제된 IPv4 TCP receiver를 먼저 시작하고 명시적인 준비 완료 신호를 받은 뒤 target을 실행한다. 외부 사이트·실제 LLM API·실제 secret은 기본 수용 테스트에 필요하지 않다. 임시 fixture와 receiver는 검증용 자원이며 제품 runtime 의존성으로 추가하지 않는다.

1. **양성 대조:** 동일 endpoint·주소 계열·network 경로에서 비민감/allow 또는 monitor 실행이 실제 연결·합성 marker 수신에 성공해야 한다.
2. **거부 검증:** source 접근·taint 상태·적용 policy와 mode·실제 syscall 결과·receiver 결과를 연결한다. timeout, 수신 데이터 없음, 프로세스 crash만으로 PASS를 주지 않는다.
3. **원인 분리:** 외부 방화벽·라우팅 문제·다른 LSM의 거부와 AgentTaint의 거부를 구분한다. 원인이 불명확하면 `inconclusive`로 설명하고 시나리오 결과를 성공 처리하지 않는다.
4. **접근 제어 분리:** direct file-deny에는 파일이 실제 존재하고 대조 실행이 열 수 있었다는 증거를, IFC에는 파일 읽기가 허용된 뒤 egress가 거부됐다는 증거를 남긴다.
5. **동기화:** 준비 조건과 결과 확인에 bounded timeout을 두되, race를 숨기기 위해 source와 sink 사이 sleep을 끼워 넣지 않는다. 경합 테스트에는 반복 횟수·실패율·CPU 조건을 기록한다.
6. **정리:** 성공·실패·취소 모두 자기 실행의 process·listener·임시 파일·BPF 자원만 정리한다. receiver 준비 실패·대조 실패·cleanup 실패를 artifact에 별도로 남긴다.

추가 필드는 `receiver_ready`, `positive_control_result`, `target_syscall_result`, `failure_class`, `cleanup_result`를 기본 후보로 삼는다. “테스트 환경 때문에 block이 안 됨”과 “테스트 환경 때문에 원래 연결이 안 됨”을 모두 구분할 수 있어야 한다.

### 13.7 CI와 릴리스 검증의 권한 경계

portable test와 실제 BPF load/attach job을 분리한다. 외부 PR·검토하지 않은 코드를 개발자의 Mac/기존 VM 또는 재사용하는 높은 권한 runner에서 자동 실행하지 않는다. privileged 검증은 검토한 정확한 commit을 격리된 일회용 Linux 환경에서 실행하는 것이 기본안이다. 해당 환경이 없으면 portable 결과만 보고하고 Linux 검증을 `not_run`으로 남긴다. 이를 해결하려고 유료 runner를 자동 구입하거나 로컬 개발 머신을 공개 CI에 등록하지 않는다.

CI 구현 기준:

- repository token은 읽기 최소 권한, 배포 키·실제 자격증명·SSH agent·호스트 홈·관리 socket은 주입하지 않음.
- 외부 action은 검토한 full commit SHA로 고정. PR 문자열을 shell 명령으로 직접 삽입하지 않음.
- `pull_request_target`/`workflow_run`의 높은 권한 context로 미신뢰 PR 코드를 checkout·실행하는 구성 금지. 단순 수동 승인 버튼을 격리의 대체재로 사용하지 않음.
- privileged job과 미신뢰 job의 실행 공간·쓰기 가능한 cache·artifact 실행 경로를 분리. 받은 artifact를 검증 없이 높은 권한 shell로 실행하지 않음.
- timeout·취소에서도 잔류 process/attach/임시 network 자원과 정리 결과를 확인. 실행된 필수 scenario 수가 0이면 합격 금지.

이 기준은 제품의 자체 공급망 방어 기능을 구현한다는 뜻이 아니라 **개발·테스트 인프라의 최소 운영 조건**이다. [GitHub Actions 보안 지침](https://docs.github.com/en/actions/reference/security/secure-use)

V-01 및 공개 배포 준비에서는 binary·BPF object·source commit·checksum·지원 환경의 대응, 외부 의존성/재배포 자료의 라이선스 기록, 알려진 한계를 확인한다. checksum을 공급자 신원 인증이나 코드 안전성 증명으로 표현하지 않는다. 서명 키·배포 계정·공개 릴리스 작업은 별도 배포 요청 범위이며 이번 작업에서 생성하지 않는다.

### 13.8 추가 보완의 작업 대응표

| 추가 계약 | 구현·검증 책임 | 연결 시나리오·증거 |
|---|---|---|
| 시작 전 입력의 보호 범위 | P2-04/P3-03/V-01 | S31 및 README의 source 제한 |
| 순서·손실·품질 상태 | P2-03/P3-01/P3-03/P4-03 | S13/S32/S37 |
| root 종료·자손·drain | P2-04/P4-03 | S10/S11/S21/S33 |
| 실제 권한·exec 재상승·내부 FD | ENV-01/P2-04/P4-01/P4-04 | S28/S34/S38 |
| policy snapshot·parser 자원 한도 | P3-02/P4-01 | S15/S35, policy/object 식별자 |
| 환경 provenance·증거 유효 범위 | ENV-01/02/V-01 | 환경·실행 manifest와 hash |
| 양성 대조·실패 원인 구분 | P3-03/P4-04 | S17/S18/S36 |
| CI 신뢰 경계·정리 | P0-03/P2-04/P4-04 | workflow review와 실제 isolated job 결과 |
| 작업 입력·인계 상태 | 각 작업 담당 및 통합 담당 | 12.10절 작업 보고 |

이 표는 기존 작업의 수용 조건을 보강한다. 새 Phase를 만들거나 현재 체크박스를 완료로 바꾸지 않는다.

## 14 다음 AI에게 전달할 실행 지시

### 14.1 한 작업 착수용 템플릿

```text
Read:
- AGENTS.md
- docs/ARCHITECTURE.md
- docs/SECURITY_MODEL.md
- docs/THREAT_MODEL.md
- docs/IMPLEMENTATION_MASTER_PLAN.md의 관련 절
- prompts/<현재 Phase 파일>

Task:
- 현재 Phase: <번호>
- 실행할 작업 ID: <예: P0-01>
- 사용자에게 승인된 범위: <구체적 범위>
- 채택 기본안: <4절 D/E ID와 내용; 동일한 선택을 다시 묻지 않음>
- 남은 기술 검증 또는 새 범위: <내용; 없으면 없음>

Instructions:
1. 현재 commit과 작업 중 변경을 확인하고 사용자 변경을 보존한다.
2. 기존 구현이 생겼다면 codebase-memory-mcp로 먼저 탐색한다.
3. 이 작업에 필요한 source/API만 실제 문서에서 확인한다.
4. 승인된 경로와 Phase 안의 가장 작은 변경을 구현한다.
5. 관련 테스트와 Linux 검증을 실행한다. 실행 못한 것은 미검증으로 적는다.
6. 채택 기본안을 관련 규범과 맞추고 구현한다. 새로 발견한 규범 충돌·보장 약화·범위 확대는 구체안을 준비해 보고한다.
7. Required Final Report로 보고한다. 다음 Phase는 설명만 한다.
```

이 템플릿은 호스트 공통 지시다. `/build auto`용 spec 위치나 특정 plugin 명령으로 자동 해석된다고 가정하지 않는다. 어떤 외부 workflow를 사용해도 이 작업 범위와 기존 규범의 관계를 먼저 맞춘다.

### 14.2 Phase 완료 보고

아래는 기존 AGENTS.md의 구조를 유지한 템플릿이다. 각 항목에 실제 결과를 채운다.

```markdown
## Changed
- 변경 파일과 사용자에게 보이는 동작

## Architecture Decisions
- 적용한 결정 ID, 기존 계약과의 관계

## Tests
- gofmt: 실행 결과 또는 미실행 이유
- go vet: 실행 결과 또는 미실행 이유
- go test: 실행 결과, kernel 테스트 skip 여부
- build: 실행 결과
- Linux verifier / integration: 환경과 결과 또는 미검증 이유

## Security Semantics
- what is detected: 지원·검증된 관측 경로
- what is blocked: 실제 pre-op 거부가 검증된 경로만
- what is NOT guaranteed: byte-level taint tracking이 아니며,
  post-event detection/kill은 prevention이 아님; 현재 미지원 경로

## Limitations
- 미검증 환경, 지원하지 않는 경로, 남은 결정

## Next Phase
Do not implement it. Only describe the next expected step.
```

### 14.3 작업 중단·진행 기준

중단해야 하는 것은 관련 구현이다. 다른 독립적인 읽기·문서 정리·테스트 설계까지 무조건 멈추지 않는다.

- 현재 Phase의 의미를 바꾸어야 하는 경우: 충돌 위치·이유·최소 변경안을 작성하고 적용 전 결정한다.
- target 보호 범위를 확장하는 경우: D01/D02와 로드맵을 다시 확인한다.
- verifier를 통과하려고 보안 의미를 약화해야 하는 경우: 해당 구현을 중단하고 다른 방법을 검토한다.
- kernel 기능이 없을 때: 숨은 fallback으로 통과시키지 않는다. 환경 개선·지원 범위 제한을 명시한다.
- 도구 설치·설정 경로 쓰기가 현재 권한으로 불가능할 때: 자동 승인 검토·sandbox 결과를 그대로 보고하고 다른 안전한 범위의 작업을 수행한다. 설정 우회는 하지 않는다.
- 아직 구현 요청을 받지 않은 계획 단계: 실제 설치·제품 코드 구현·VM 변경을 시작하지 않는다.

## 15 근거 자료와 사실 확인 범위

### 15.1 저장소 내부 근거

| 문서 | 사용하는 근거 |
|---|---|
| [AGENTS.md](../AGENTS.md) | 제품 철학·버전·패키지 경계·Phase 순서·보고 형식 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | CLI 보안 범위·패키지·OS backend·event pipeline |
| [SECURITY_MODEL.md](SECURITY_MODEL.md) | source/label/propagation/sink·초기 범위·초안 schema |
| [THREAT_MODEL.md](THREAT_MODEL.md) | 공격 입력·자산·막는 것/못 막는 것 |
| [ROADMAP.md](ROADMAP.md) | Phase 번호와 확장 순서 |
| [SUBORCHESTRATOR.md](SUBORCHESTRATOR.md) | 외부 pipeline의 기존 매핑·검증 환경 gap |
| [Phase 안내](../prompts/README.md) | 단계별 실행 계약과 stop gate |
| [Phase 0](../prompts/00-bootstrap.md) | doctor/run과 초기 파일 범위 |
| [Phase 1](../prompts/01-core-event-model.md) | core 타입과 직렬화 |
| [Phase 2](../prompts/02-linux-observer.md) | 실제 Linux 관찰 |
| [Phase 3](../prompts/03-taint-engine.md) | 단일 label·네 가지 audit 시나리오 |
| [Phase 4](../prompts/04-linux-enforcement.md) | modes·pre-op·fallback |

### 15.2 외부 일차 자료

외부 문서 main/latest 링크는 변경될 수 있다. 구현 시 프로젝트 고정 버전·검토 SHA와 대조한다. 아래 문서 확인은 현재 guest에서 기능이 실행됐다는 증거가 아니다.

| 출처 | 이 명세에 사용한 사실 |
|---|---|
| [agent-skills 검토 커밋](https://github.com/addyosmani/agent-skills/tree/9d0c60d406b454a78ccc0a175b19932047aa4dac) | 스킬·plugin·reference 구조, 실제 스킬 이름 |
| [upstream Codex setup](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/docs/codex-setup.md) | native plugin 명령·Claude 전용 기능과 차이 |
| [upstream adoption](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/docs/adoption-guide.md) | 기존 프로젝트에 선택 도입하는 맥락 |
| [upstream build command](https://github.com/addyosmani/agent-skills/blob/9d0c60d406b454a78ccc0a175b19932047aa4dac/.claude/commands/build.md) | spec 위치·계획 실행 흐름의 차이 |
| [OpenAI skills](https://learn.chatgpt.com/docs/build-skills) | local discovery·스킬 구조·선택적 metadata |
| [Claude plugins](https://code.claude.com/docs/en/plugins) | host plugin 설치·범위 |
| [Lima](https://lima-vm.io/docs/) | Linux VM과 아키텍처 지원 |
| [Lima plain](https://lima-vm.io/docs/config/plain/) | 공유·forwarding 축소 선택지; 기존 VM에 적용하지 않음 |
| [Linux LSM](https://docs.kernel.org/admin-guide/LSM/index.html) | 실행 중 활성 LSM 확인 |
| [Linux BPF-LSM](https://docs.kernel.org/bpf/prog_lsm.html) | LSM 프로그램 구조·반환·BTF |
| [Linux ring buffer](https://www.kernel.org/doc/html/latest/bpf/ringbuf.html) | reservation 실패와 사용자 공간 전달 의미 |
| [Linux no_new_privs](https://docs.kernel.org/userspace-api/no_new_privs.html) | exec 권한 취득 제한과 한계; 적용은 E09 검증 대상 |
| [GitHub Actions secure use](https://docs.github.com/en/actions/reference/security/secure-use) | CI 최소 권한·미신뢰 checkout·action pinning·self-hosted 위험 |
| [Linux socket source v6.12](https://github.com/torvalds/linux/blob/v6.12/net/socket.c) | connect와 send 검사 경로의 차이; 실제 guest 버전은 별도 대조 |
| [cilium bpf2go v0.22.0](https://github.com/cilium/ebpf/tree/v0.22.0/cmd/bpf2go) | 고정 버전의 생성 도구 확인 경로 |
| [ebpf-go 시작 안내](https://ebpf-go.dev/guides/getting-started/) | C 빌드·Go scaffolding 생성 |
| [ebpf-go portability](https://ebpf-go.dev/guides/portable-ebpf/) | 생성 object embed·고정 toolchain·배포 구분 |
| [Tetragon enforcement](https://tetragon.io/docs/concepts/enforcement/) | signal과 실제 operation 거부의 차이 |
| [Apple Endpoint Security](https://developer.apple.com/documentation/endpointsecurity?language=objc) | C API와 native framework |
| [Apple system extensions](https://developer.apple.com/system-extensions/) | entitlement·배포 검토 출발점 |
| [MCP 보안](https://modelcontextprotocol.io/docs/draft/tutorials/security/security_best_practices) | local server·권한 위임·도구 경계 |
| [SELinux type enforcement](https://github.com/SELinuxProject/selinux-notebook/blob/main/src/type_enforcement.md) | 조건부 access control 불가능이라는 기존 일반화 정정 |

### 15.3 작성 완료와 미검증의 구분

**최초 계획 작성 시점:** 원격 저장소·공식 문서 조사, Mac 도구·정지 VM 설정 조회, 단일 계획 파일 작성과 문서 자체의 검토를 완료했다. 당시에는 제품 코드가 없어 gofmt/go vet/go test/build/verifier를 수행하지 않았다. 이후 Phase 0/1 코드와 Go 검증의 상태는 12.10절에 기록한다. VM은 시작하지 않았고 Linux enforcement 가능 여부는 미검증이다. 스킬 설치·호출 결과도 미검증이다.

이 문서의 단계별 계약·외부 자료 조사 방식에는 `claude-mem:make-plan` 절차를 사용했다. Codex 설치 정보는 `OpenAI Docs`로 대조했고, 메모리·도구의 민감정보 최소화에는 기존 `task-observer` 지침을 반영했다. 외부 `addyosmani/agent-skills`는 조사만 했으며 현재 작업을 수행하는 지시로 설치·활성화하지 않았다.

후속 Phase 0/1 구현에서는 `claude-mem:do` 실행 절차에 따라 범위를 나누어 구현하고 독립 리뷰·수정·재검증을 거쳤다. 이 절차가 후속 Phase 자동 실행이나 commit/push 권한을 추가하지는 않았으며 실제 commit/push도 하지 않았다.

추가 누락 검토는 보안 계약과 실행 환경/도구 계약을 나누어 읽기 전용으로 대조한 뒤 본문과 기존 작업에 통합했다. 새 MCP·DB·worker·플랫폼을 추가해야 할 근거는 이번 검토에서 찾지 못했다. 형식 검증은 링크·코드 fence·JSON 예제·작업/시나리오 ID의 중복과 참조를 대상으로 하며, 문서 검증 통과는 제품 보안 검증 통과가 아니다.

후속 구현 요청에 따라 **4절의 채택 기본안 적용 → PRE-01/02의 Phase 0 관련 계약 정리 → Phase 0 완료 → 후속 요청의 Phase 1 완료**까지 진행했다. D01~D08의 답변을 다시 기다리지 않는다. 이번 진행을 전체 로드맵 자동 실행이나 실제 설치 완료로 해석하지 않는다.

## 16 유사 프로젝트 베스트 프랙티스 조사

조사일: **2026-10-03 KST**. 이번 요청은 비교 조사다. 제품 코드, 규범 아키텍처, VM, 설치 환경을 변경하지 않는다. 아래의 권고는 구현 완료나 새로운 보장 승인이 아니다. 기존 37개 작업의 완료 상태도 바꾸지 않는다.

### 16.1 조사 범위와 증거 수준

“전수 조사”를 세상의 모든 보안 프로젝트를 검증했다는 뜻으로 사용하지 않는다. 기존 `RELATED_WORK.md`의 후보와 직접 관련된 런타임·샌드박스·IFC 선행 연구를 범주별로 조사한다. 비교 목적은 순위 선정이 아니라 **다음 코드에서 어떤 경계·실패 처리·시험을 구현해야 하는지 결정하는 것**이다.

| 증거 표시 | 실제 수행한 일 | 뜻하지 않는 것 |
|---|---|---|
| D: 공식 문서 | 프로젝트 공식 문서·README·저자 논문 확인 | 구현 전체 감사, 현재 환경 동작 보장 |
| S: 선택 소스 | 아래에 명시한 파일·함수의 제어 흐름 확인 | 저장소 전체 코드 리뷰, 취약점 부재 증명 |
| R: 실행 | 해당 버전 설치 후 정해진 시험 수행 | 이번 외부 프로젝트 조사에는 **없음** |
| A: 적용 권고 | D/S와 AgentTaint 규범을 대조한 자체 설계 판단 | upstream이 그대로 구현한 사실 |

선정 축은 ① OS 효과 관찰, ② 동기 거부, ③ 실행 경계/권한, ④ provenance/IFC, ⑤ AI 도구와 macOS 운영, ⑥ 생성물·테스트다. 별점·벤치마크 홍보 수치는 평가 기준에서 제외한다. `main`/`master` 링크는 조사 시점의 가변 자료이며 release pin이 아니다. 보안 코드를 가져오기 전에는 해당 파일의 commit·license·하위 의존성을 다시 고정한다. 논문 연도와 검색엔진의 최근 수집일을 혼동하지 않는다.

**비교하지 않은 범위:** 상용 EDR의 비공개 커널 코드, 모든 sandbox SaaS의 내부 구현, 전 Linux LSM·전 provenance 논문, 각 프로젝트 CVE/과거 release의 완전한 이력, 동일 하드웨어 성능 실험, 법률적 license 적합성 판단. 따라서 “유일”, “무조건 안전”, “경쟁 제품 전부” 같은 결론을 내리지 않는다.

### 16.2 결론 — 무엇을 가져오고 무엇을 가져오지 않는가

1. **스택을 바꿀 이유는 발견하지 못했다.** Go와 C/CO-RE를 유지한다. 고정된 `cilium/ebpf`를 이용해 load/attach/read/close를 구현하고, AgentTaint의 데이터 의미는 기존 core/taint/policy에 둔다. 다른 제품의 daemon·CRD·웹 서버를 함께 가져오지 않는다.
2. **관찰과 차단은 다른 경로다.** 비동기 이벤트를 Go에서 받은 뒤 내리는 판정은 동일 operation의 사전 거부가 아니다. Phase 2의 observer를 잘 만드는 것과 Phase 4의 동기 집행을 증명하는 것을 별도 완료 조건으로 둔다.
3. **다음에 필요한 것은 패키지 트리 추가보다 실행 계약이다.** 필수 hook 준비, 신뢰된 launch barrier, target 권한, scope 등록, 손실 상태, 종료/자원 해제의 순서를 테스트 가능한 계약으로 확정해야 한다.
4. **샘플 코드는 기능 예제이지 보안 운영 기준이 아니다.** ring reserve 실패를 그냥 무시하거나, 동기화 FD의 EOF를 통과시키거나, startup fatal이 cleanup을 생략하는 예제를 제품 계약으로 복사하지 않는다. 아래 소스별로 취할 부분과 보완할 부분을 구분한다.
5. **“IFC가 처음”이라는 차별화는 채택하지 않는다.** Flume은 2007년에 프로세스·파일 디스크립터·pipe/socket을 다루는 DIFC를 발표했다. AgentTaint가 증명해야 할 가치는 AI 실행 도구에 적용되는 좁고 설명 가능한 경계, 사용성, 배포 가능성과 실제 차단 정확성이다. [Flume 원 논문](https://pdos.csail.mit.edu/~yipal/papers/flume-sosp07.pdf)
6. **macOS 개발과 macOS 호스트 보호를 구분한다.** 현재 선택은 Mac에서 portable 코드를 개발하고 Linux guest에서 실제 BPF를 실행하는 것이다. Mac 네이티브 sandbox 제품이 있다는 사실은 AgentTaint Linux backend의 호스트 보호 능력을 만들지 않는다.
7. **새 skill/MCP/DB가 우선 과제가 아니다.** 조사 근거를 기존 ENV/P2/P3/P4 작업의 테스트로 연결한다. 외부 agent-skills는 개발 보조이며 runtime 보안 의존성이 아니다. 수집 도구에는 읽기 전용 권한만 주고 실제 민감정보를 fixture로 쓰지 않는다.

요약 권고 구조는 **작은 launcher/supervisor + Linux observer + 순서 있는 audit 경로**, 이후 **검증된 별도 동기 enforcement 경로**다. 이 중 현재 구현된 것은 Phase 0의 단순 runner와 Phase 1의 portable event 모델뿐이다.

### 16.3 소스까지 확인한 핵심 설계 사례

#### A. 시작 동기화 — “기다렸다”와 “준비 승인을 받았다”는 다르다

nsjail의 `newProc`는 부모의 완료 메시지를 정확한 길이로 읽고 지정된 완료 값인지 검사한 뒤 다음 준비 단계로 진행한다. 부모 쪽 준비와 자식 쪽 confinement 설치도 별도 단계다. 반면 bubblewrap의 `opt_block_fd` 경로는 `read` 반환값을 버린다. 따라서 bubblewrap의 대기 옵션 자체를 EOF까지 거부하는 AgentTaint 시작 허가 프로토콜로 간주하면 안 된다. **이 차이는 upstream 취약점 판정이 아니라 계약 차이**다. [nsjail 고정 소스](https://github.com/google/nsjail/blob/4ff54a6e0d5b65a0e1633d6e2fd1425ba6c882ce/subproc.cc), [bubblewrap 고정 소스](https://github.com/containers/bubblewrap/blob/0f8073dd9b547e15e8ec9451371f9729830b083d/bubblewrap.c)

**AgentTaint 권고:** `READY`는 필수 hook attach·reader·run scope·감사 파일 준비 완료를 의미해야 한다. trusted launcher는 정확한 run에 대한 명시적 허가만 받아 target으로 exec한다. EOF, 잘못된 값, 부분 메시지, timeout, 부모 실패를 성공으로 해석하지 않는다. target을 먼저 실행하고 SIGSTOP으로 붙잡는 방식은 첫 syscall 경합의 해법이 아니다. 단순 sleep도 준비 증거가 아니다.

gVisor는 `Loader.WaitForStartSignal`, `containerManager.StartRoot`, `onStart`에서 생성 상태·시작 신호·시작 결과를 나눈다. daemon의 “ready” 알림과 특정 target의 첫 동작 전 gate는 별개이므로, readiness라는 같은 이름만 보고 의미를 같게 취급하지 않는다. [gVisor loader](https://github.com/google/gvisor/blob/master/runsc/boot/loader.go), [controller](https://github.com/google/gvisor/blob/master/runsc/boot/controller.go)

#### B. 감사 실패와 거부 실패 — 하나의 성공/실패로 합치지 않는다

KubeArmor `enforce_proc`의 확인한 경로는 거부값을 결정한 뒤 ring buffer 공간 예약에 실패해도 그 `retval`을 반환한다. 로그를 못 남겼다는 이유로 이미 결정한 거부를 허용으로 바꾸지 않는 구체적 예다. **이 부분의 확인이 모든 오류 분기·모든 hook의 fail-closed 검증은 아니다.** [KubeArmor 고정 BPF 소스](https://github.com/kubearmor/KubeArmor/blob/6e13207d7dfa9500221db43e586ca881ea5e5242/KubeArmor/BPF/enforcer.bpf.c)

**AgentTaint 권고:** 최소한 `telemetry_incomplete`, `scope_state_invalid`, `enforcement_unavailable`을 다른 내부 상태로 다룬다. 감사 손실을 집행 성공의 증거로 쓰지 않고, 반대로 로그 하나가 없다는 이유만으로 거부가 실패했다고 단정하지 않는다. Phase 2에는 집행 경로가 없으므로 감사 실패 시 자동 보안 kill을 추가하지 않는다. Phase 4의 state/map 실패 정책은 별도 설계 대상이다.

#### C. ActPlane — 직접 비교할 taint/IFC 구현이 이미 있다

조사한 commit의 `taint_engine.bpf.h`에는 `te_fork`의 자식 라벨 상속, `te_read_domain` 계열의 파일→프로세스 흐름, 파일/endpoint 상태와 라벨 조건 평가가 존재한다. `process.bpf.c`는 전파 함수를 호출하고 effect에 따라 `-EPERM` 또는 signal 경로를 선택한다. `te_effect_mode`는 notify/kill/block과 backend 지원 범위를 구별한다. 따라서 **ActPlane을 단순 정적 access-control 도구로만 분류하는 것은 부정확하다.** [고정 taint engine](https://github.com/eunomia-bpf/ActPlane/blob/3a94d645ef1896f3ae3fe682edd056c8f6bd3c94/bpf/taint_engine.bpf.h), [고정 BPF hook·effect 구현](https://github.com/eunomia-bpf/ActPlane/blob/3a94d645ef1896f3ae3fe682edd056c8f6bd3c94/bpf/process.bpf.c)

**AgentTaint 권고:** 가장 가까운 소스 비교 대상으로 올린다. 특히 hook별 허용 effect, label state의 위치, fork/read/connect 사이 순서, map 실패 분기, identity 생애, 기존 FD/연결, 감사와 집행의 분리를 비교한다. 다만 ActPlane의 커널 내 전파·정책 구현을 그대로 도입하면 현재 AgentTaint의 `internal/taint`·`internal/policy` 집중 규칙과 충돌한다. 이번에는 비교만 하며 E04/P4-01의 최소 규범 변경안을 검증 후 제시한다. “상대 프로젝트에 코드가 있다”는 것은 그 프로젝트의 모든 race·우회가 해결됐거나 AgentTaint가 안전하다는 증거가 아니다.

#### D. ring buffer — 순서는 있지만 완전성은 자동으로 생기지 않는다

Linux ring buffer는 예약 순서로 record를 소비자에게 제공하며, 공간 부족 시 예약에 실패한다. NMI에서는 예약 lock 획득 실패도 가능하다. 완료되지 않은 앞 record가 뒤 record의 가시성을 늦출 수 있다. 이는 syscall 성공 순서나 소실 없는 감사의 보장이 아니다. [Linux ring buffer](https://docs.kernel.org/bpf/ringbuf.html)

`cilium/ebpf` v0.22.0의 ringbuffer C 예제는 reserve 실패 시 그냥 반환한다. 사용법 시연에는 적절하지만 AgentTaint에서는 이 경로에 별도 손실 계수가 필요하다. Go 예제의 attach/read/close 흐름은 참고하되 `log.Fatal`을 깊은 라이브러리 코드에 복사해 cleanup을 건너뛰지 않는다. [C 예제](https://github.com/cilium/ebpf/blob/e55144e17360b60cc4583229c35c2dbf0935b308/examples/ringbuffer/ringbuffer.c), [Go 예제](https://github.com/cilium/ebpf/blob/e55144e17360b60cc4583229c35c2dbf0935b308/examples/ringbuffer/main.go)

**AgentTaint 권고:** 하나의 공유 ring buffer와 순서 보존 소비 경로로 시작한다. reserve 실패, raw decode 실패, 사용자 공간 queue 포화, audit write 실패를 각각 계수한다. 손실 통지를 손실 가능한 같은 이벤트 스트림에만 의존시키지 않는다. drop counter도 읽지 못하면 0이 아니라 unknown이다. run의 이력이 손상되면 이후 이벤트가 정상이어도 그 run의 완전성을 복구됐다고 표시하지 않는다.

#### E. capability — 커널 버전, static doctor, load, attach는 다른 증거다

`cilium/ebpf/features`는 지원·미지원·판정 불가를 구분한다. 권한 오류나 probe 오류를 미지원으로 합치지 않는다. 일부 helper/program 조합은 probe만으로 결론내리기 어렵다. 고정 버전 소스는 probe 결과가 process 환경·capability 변경 후에도 cache될 수 있음을 명시한다. [공식 feature 안내](https://ebpf-go.dev/concepts/features/), [고정 features/doc.go](https://github.com/cilium/ebpf/blob/e55144e17360b60cc4583229c35c2dbf0935b308/features/doc.go)

**AgentTaint 권고:** 증거를 `static_detected → object_loaded → hook_attached → fixture_observed → enforcement_verified`로 나눈다. BPF 기능 probe는 자원을 만들거나 프로그램을 load할 수 있으므로 **기본 doctor나 버전 조회 전용 probe에 몰래 넣지 않는다.** run 초기화 또는 별도 승인된 통합 시험에서만 수행한다. 이 체계는 AGENTS의 기존 버전 임계값을 바꾸지 않는다.

Landrun은 기본 strict 동작과 명시적 `--best-effort`를 구별한다. 참고할 것은 명시적인 capability 계약이지 AgentTaint `block`의 자동 약화가 아니다. Landlock의 최신 ABI와 특정 wrapper가 실제 요청하는 권한은 다르다. 무버전으로 네트워크 지원 범위를 단정하지 않는다. [landrun](https://github.com/Zouuup/landrun), [Landlock 공식 ABI·FD 계약](https://docs.kernel.org/userspace-api/landlock.html)

#### F. 권한과 종료 — descriptor와 링크도 수명이 있는 보안 자원이다

`cilium/ebpf`의 고정 `ringbuf.Reader`에는 `Close`, `Flush`, `SetDeadline`이 있다. `Close`는 blocked read를 중단하고, `Flush`는 그 시점의 pending record를 읽게 하는 동작이지 앞으로 생성될 모든 이벤트까지 완료됐다는 뜻이 아니다. [고정 Reader 소스](https://github.com/cilium/ebpf/blob/e55144e17360b60cc4583229c35c2dbf0935b308/ringbuf/reader.go)

**AgentTaint 권고:** producer가 끝나는 조건 → detach/입력 중단 → bounded drain → 최종 counter → reader/link/map 해제 순서를 정의하고 실패 주입으로 검증한다. 실제 순서는 선택한 hook과 scope 생애에 맞춰 P2-04에서 확정한다. FD finalizer나 프로세스 종료만 cleanup 설계로 삼지 않는다. pinning은 기본적으로 도입하지 않으며 crash 후 지속 보호도 주장하지 않는다.

`no_new_privs`는 fork/exec에 상속되는 exec 권한 취득 제한이지, 이미 가진 UID·capability·관리 socket 권한을 없애는 장치가 아니다. target identity, supplementary groups, capabilities, saved IDs, FD 허용 목록과 함께 검증해야 한다. [Linux no_new_privs](https://docs.kernel.org/userspace-api/no_new_privs.html)

#### G. 생성물·CI — 크로스 빌드와 커널 시험을 분리한다

ebpf-go portability 문서는 고정 LLVM 환경과 생성 `.go`/`.o`의 버전 관리를 권고한다. 고정 v0.22.0 CI 소스는 생성물 diff, 크로스 빌드, arm64 시험, 여러 커널의 Linux 시험을 별도 job으로 다룬다. **그 프로젝트의 kernel matrix는 AgentTaint의 지원 matrix가 아니다.** [portability 안내](https://ebpf-go.dev/guides/portable-ebpf/), [고정 CI 소스](https://github.com/cilium/ebpf/blob/e55144e17360b60cc4583229c35c2dbf0935b308/.github/workflows/ci.yml)

**AgentTaint 권고:** portable 테스트·생성 재현·실제 kernel 시험의 결과를 따로 기록한다. upstream CI의 `latest`, 광범위 sudo, runner 설정까지 복사하지 않는다. AgentTaint용 tool/image/action은 검토한 값으로 고정하고 실제 지원 guest에서 verifier·attach·fixture를 확인한다. 현재 허용된 portable CI만으로 privileged 시험 승인을 추론하지 않는다. 외부 PR을 자격증명이 있는 상시 privileged runner에서 실행하지 않는다. [GitHub self-hosted runner 경고](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/add-runners)

### 16.4 AgentTaint 런타임 아키텍처 권고안

이 절은 **A: 설계 제안**이다. 기존 트리와 책임 분리를 사용하되 아직 확정되지 않은 launch/권한 계약을 명시한다. 기존 코드에 아래 API가 있다고 가정하거나 그대로 호출하지 않는다. 변경이 규범과 충돌하는 부분은 PRE-02에서 먼저 결정한다.

#### 16.4.1 모듈과 신뢰 경계

```text
사용자: run --observe --audit-file PATH -- COMMAND  [Phase 2 예정 인터페이스]
          │
          ▼
cmd/agenttaint: 입력·출력·의존성 연결, 보안 판정 없음
          │
          ▼
internal/runner: run 수명·target identity·시작 gate·종료 결과
          ├── Linux sensor: 준비 / scope 등록 / 정규화 Event 수신 / 해제
          │       └── BPF hooks + membership + ring buffer + loss counters
          ├── 감사 writer: 별도 소유 FD → core Event JSONL
          └── 신뢰된 시작 경로 → 권한·FD 제한 → 명시 허가 → target exec

Phase 2: Event → 별도 감사 파일                         [관찰만]
Phase 3: Event → taint reference model → policy → audit [비동기 판정]
Phase 4: 승인된 최소 동기 상태 → pre-op hook 반환        [별도 검증 필요]
```

**runner가 관리하는 scope membership과 taint label은 다르다.** Phase 2에 자손 포함 여부를 추적하는 것은 필요하지만 SECRET 전파·정책을 미리 구현하지 않는다. core는 계속 OS-neutral이며 BPF FD·kernel struct·Linux capability를 넣지 않는다. sensor는 정규화 Event와 운영상 오류/준비 결과를 제공하되 보안 verdict를 내리지 않는다.

감사 writer는 이번 단계에서 별도 daemon·DB·`internal/audit` 패키지를 만들 이유가 없다. 기존 runner 책임 내의 작은 구성요소와 package 테스트를 우선 검토한다. exact 파일 분리는 트리 규칙을 확인한 뒤 결정한다. 새로운 privileged helper 실행 모드도 자동 승인된 것으로 보지 않는다.

#### 16.4.2 구현 전에 확정할 인터페이스 계약

아래 이름은 개념명이며 public Go API 시그니처가 아니다. code review에서 기존 코드와 맞춰 최소 인터페이스로 정한다.

| 경계 | 입력 | 출력·성공 조건 | 실패·소유권 |
|---|---|---|---|
| 실행 요청 validation | target argv, 선택 identity, audit 경로, observe 요청 | 불변 run 설정·식별자 | target 실행 없이 오류; shell 문자열로 재조합 금지 |
| Audit 준비 | 검증된 경로·상한 | 새 regular file의 소유 FD | 초기 실패 시 실행 금지; 기존 파일 유지 |
| Sensor prepare | 요청 hook 집합·capacity·run 설정 | load/attach/reader 준비 결과 | 부분 생성한 자원만 역순 해제 |
| Scope 등록 | 신뢰된 target-launch 생애 identity | membership 설치 확인 | PID 숫자만 등록하고 성공 처리 금지 |
| Launch gate | 해당 run의 준비 결과·전용 control channel | 단 한 번의 명시적 실행 허가 | EOF/timeout/다른 run 허가/중복 허가는 오류 |
| Target exec | 원래 argv/stdio, 확정 UID/GID/groups, 허용 FD | exec 성공 또는 구체적 실패 | helper 시작 성공과 target exec 성공 분리 |
| Event 소비 | raw record와 ABI 식별 정보 | 검증된 `core.Event` | 길이·버전·범위·문자열 문제를 정상 값으로 위조하지 않음 |
| Health 조회 | run과 해당 자원의 counter | 손실·상태 오류 snapshot | 조회 실패는 unknown; 로그 파일 성공과 별개 |
| Run 완료 | root 및 추적 자손 상태 | target 결과 + supervisor 결과 + 감사 품질 | root 종료만으로 scope 완료 처리 금지 |
| Close | 해당 run이 소유한 자원 | 반복 호출에도 안전한 해제 | unrelated map/link/file 삭제 금지 |

**readiness 알림만 추가하고 실행 race가 해결됐다고 하지 않는다.** 센서 API, 신뢰된 시작 경로, membership 등록 방식이 함께 정해져야 한다. Go의 일반 `exec.Cmd.Start()` 다음에 PID를 BPF map에 넣는 구조는 target의 첫 동작이 앞설 수 있다. 대안으로 pre-exec 신뢰된 launcher gate 또는 동등한 초기 등록 메커니즘을 평가한다. Go 런타임에서 임의의 raw fork 후 복잡한 Go 코드를 실행하는 방식은 선택하지 않는다.

#### 16.4.3 권고 상태 전이와 실패 정책

```text
VALIDATED
  → AUDIT_OPEN
  → SENSOR_READY
  → TRUSTED_LAUNCHER_WAITING + TARGET_IDENTITY_VERIFIED
  → SCOPE_REGISTERED
  → RELEASE_AUTHORIZED
  → TARGET_EXEC_CONFIRMED
  → RUNNING
  → SCOPE_FINISHED
  → DRAINING
  → CLOSED

허가 전 실패: target 미실행 + 부분 자원 해제
허가 후 exec 실패: exec failure 기록 + 부분 scope 정리
실행 중 손실: 감사 incomplete가 지속됨; 소급 취소/차단으로 표현하지 않음
```

이 순서는 검증할 후보다. 신뢰된 launcher 준비 과정의 이벤트와 실제 target 이벤트의 귀속, 준비 중 privilege 변경, fork/exec 식별자의 연속성도 함께 시험한다. “프로세스를 만들기 전 센서가 ready”만으로 membership race가 해결되는 것은 아니다.

| 오류 | Phase 2 권고 | Phase 4에서 추가 결정할 일 |
|---|---|---|
| 필수 hook load/attach 실패 | 실행 허가 안 함, 진단·cleanup | block 모드도 시작 거부; monitor 자동 대체 금지 |
| 선택 기능 없음 | 요청 범위에 필요하지 않을 때만 제한을 보고 | 정책에 필요한 효과면 필수로 승격 |
| scope 등록/identity 불명확 | target 실행 안 함 | 동일; 추정 identity로 집행 금지 |
| kernel reserve/queue/decode 손실 | incomplete 상태·위치별 counter | audit 손실과 동기 state 유효성 분리 |
| 감사 쓰기 실패/상한 | 짧은 진단, run 품질 실패 유지; stdout/stderr에 JSON fallback 없음 | 감사 유지 요구와 집행 지속 여부의 운영 계약 |
| 상태 map update 실패 | 관측 scope 불완전; 정상 run으로 보고 금지 | untainted/no-match로 간주 금지; 동기 실패 정책 |
| root target 종료·자손 생존 | scope 수명 유지 기본안 검증 | 자손 포함 집행 유지 가능성 검증 |
| drain timeout | 미처리/판정 불가를 남기고 bounded 종료 | 이미 집행한 결과와 미기록 결과 구분 |
| supervisor crash | 보호 지속·완전한 최종 로그 보장 없음 | pinning만으로 crash-safe라고 주장 금지 |

감사 실패 후 target은 자동 보안 kill하지 않는 현재 규범을 유지한다. 자연 종료까지 관찰을 얼마나 계속할지, 취소/시간 제한 시 사용자의 실행 관리 요청을 어떻게 처리할지, supervisor 오류와 target exit를 어떤 CLI 결과로 표현할지는 **P2-03/04에서 코드 전에 확정할 계약**이다. 이 보고서가 새 종료 코드를 임의로 예약하지 않는다.

#### 16.4.4 식별·시간·파일·네트워크에서 놓치면 안 되는 것

- **Process identity:** PID 재사용, TGID/TID, PID namespace, boot/run 범위, task birth time을 검증한다. `comm`·경로·SessionID로 같은 생애를 추정하지 않는다. exec 전후 identity와 별도 exec identity를 혼동하지 않는다.
- **스레드:** 스레드 하나가 source에 접근한 경우 process 단위 모델에서 다른 스레드의 sink를 어떻게 볼지 P3/P4에서 정의한다. 모든 clone을 새로운 독립 프로세스로, 모든 thread exit를 process 종료로 처리하지 않는다.
- **시간:** kernel monotonic timestamp와 RFC3339 표시 시간을 분리한다. wall-clock 변동·정밀도 손실이 identity나 순서 판정에 영향을 주지 않게 한다. birth ID를 단순 포맷 변경으로 충돌시키지 않는다.
- **파일 사실:** syscall 진입의 path 문자열, LSM에서 본 객체, syscall 성공, 실제 read는 다른 사실이다. `openat` dirfd·상대 경로·rename·hardlink·symlink·잘림을 지원 범위대로 표현한다. 사용자 공간의 나중 `realpath`로 과거 kernel 객체를 증명하지 않는다.
- **raw→JSON:** Linux 경로의 임의 바이트·NUL 종료·고정 버퍼 truncation과 현재 core의 UTF-8/필수 필드 계약은 자동 호환되지 않는다. 표현 불가를 빈 정상 경로로 바꾸지 않는다. 품질 정보가 schema 변경을 요구하면 P1 계약을 조용히 넓히지 말고 최소 변경안으로 정지한다.
- **네트워크:** IPv4 connect 관찰은 모든 송신 관찰이 아니다. 기존 연결 재사용, 연결 전 source 접근 순서, UDP send, IPv6, Unix socket, proxy, 다른 프로세스가 가진 FD는 별도 범위다. “외부 IP 연결 거부”를 “모든 유출 방지”라고 하지 않는다.
- **AI 현실 경로:** 자식 도구가 비밀을 읽고 stdout/pipe로 부모 AI에 돌려주며 부모가 기존 LLM 연결로 보내는 경로는 단순 부모→자식 fork 전파 + 새 connect만으로 완성되지 않는다. 초기 MVP의 제한으로 시험하고 일반 AI 보호 주장 전 별도 확장 gate로 둔다.
- **제어 경로:** target이 policy·audit 파일·supervisor·map/link FD를 바꾸거나 원격/기존 daemon으로 작업을 위임하는 경계를 시험한다. 같은 UID에서 mode `0600`만으로 변조 방지가 완료된다고 보지 않는다.

위 항목은 다른 프로젝트의 안전성 주장 대신 AgentTaint에 요구하는 자체 검증 목록이다. 현재 Phase에 없는 file/pipe 전파·IPv6·native Mac 구현을 즉시 추가하라는 뜻은 아니다.

#### 16.4.5 동기 집행의 최소 검토안 — Phase 4 전용

**현재 설계상의 접점:** `Event → Go taint → Go policy → enforce`만으로는 이미 진행 중인 operation을 사전 거부할 수 없다. kernel 쪽 label update/조회가 필요해지면 “전파는 internal/taint에만” 규칙과 책임을 재정의해야 한다.

권고 검증 순서는 다음과 같다.

1. Go reference model로 source·inheritance·sink 의미와 replay 기대값을 먼저 정의한다.
2. 커널에 필요한 최소 상태와 전이를 분리해 명세한다. 복잡한 문자열/정책 처리를 커널로 옮기지 않는다.
3. source 직후 같은 thread 또는 다른 thread의 지연 없는 sink, fork 직후 sink를 synthetic fixture로 시험한다.
4. user-space reader를 늦추거나 중단해도 동일한 pre-op 거부가 유지되는지 별도 시험한다.
5. map 포화·update 실패·identity 재사용·policy snapshot 교체의 결과를 정의하고 differential test로 reference와 대조한다.
6. hook 반환은 이전 거부 결과를 보존해야 한다. 선택한 hook의 반환 의미를 공식 정의와 실제 guest source/BTF로 확인한다. [BPF-LSM 반환 예제](https://docs.kernel.org/bpf/prog_lsm.html)
7. 실제 상대 수신 fixture에 효과가 없는지와 호출자의 errno를 함께 확인한다. 네트워크 자체가 끊긴 상태에서 “차단 성공”으로 판정하지 않는다.

ActPlane/KubeArmor에서 아이디어를 얻어도 그 내부 구현 전체를 AgentTaint 센서에 넣지 않는다. 이 검토안을 규범에 적용하는 것은 **P4-01의 별도 승인 대상**이며 Phase 2 코딩의 범위를 넓히지 않는다.

### 16.5 조사 결과를 코드 작업으로 바꾸는 체크리스트

아래 BP 항목은 **기존 37개 상위 작업의 세부 검증 항목**이다. 새로운 26개 Phase를 만드는 것이 아니다. 전부 미구현/미검증이며 이번 조사 완료와 구별한다. 이미 7/12/13절에 있는 조건은 출처와 구체적 실패 시험을 보강한 것이다.

| ID | 순서·기존 담당 | 수행할 작업 | 완료 증거·실패 시험 |
|---|---|---|---|
| BP-01 | PRE-01/02 | 16.6의 비교 사실 정정안을 규범 변경과 비규범 수정으로 분리 | 잘못된 독창성·조건부 access-control 불가 문구를 근거와 함께 처리; 제품 모델은 유지 |
| BP-02 | PRE-02/P2-04 설계 | trusted launch gate, identity, 권한, 감사 실패/종료 계약을 좁게 확정 | 현재 tree/CLI/core와 충돌 여부 명시. READY/EOF/취소 결과 표, 과도한 새 daemon 없음 |
| BP-03 | ENV-01 | 허용된 Linux guest 실행 경로 확보 후 읽기 전용 상태 확인 | kernel/config/BTF/LSM/UID/capability/아키텍처 manifest; Mac 조회만으로 완료 금지 |
| BP-04 | ENV-02 | v0.22.0 generator, compiler/LLVM/BTF 입력, 정확한 생성 파일 고정 | source→object→generated Go 대응·hash·재생성 diff. 설치·toolchain 실행 권한 별도 준수 |
| BP-05 | P2-01 | raw ABI의 version/length/endian/layout과 decoder 작성 | short/oversize/unknown version/padding/잘못된 enum fixture. C struct와 Go decode 대응 |
| BP-06 | P2-01 | BPF object·maps·links·reader 소유권 구현 | 단계마다 실패 주입해 FD/link/map 누수 없음. 부분 attach rollback·중복 Close |
| BP-07 | P2-01 | process/thread 생애와 stable ProcessKey 정규화 | exec 연속성, PID reuse, thread exit, namespace 식별 테스트 |
| BP-08 | P2-02 | open/connect hook 관측 stage/result 정의 | 시도·실패·성공을 구별; open을 read 완료로 기록하지 않음 |
| BP-09 | P2-02 | 경로·주소 raw 값의 표현 범위와 오류 처리 | 상대 경로/dirfd/비UTF-8/잘림/IPv4 길이 오류. core 변경 필요 시 먼저 정지 |
| BP-10 | P2-03 | run 및 자손 membership과 커널 scope 관리 | 즉시 fork/exec, reparent/setsid, 동시에 실행한 두 run과 noise 분리 |
| BP-11 | P2-03 | 감사 파일 안전 생성과 target stdio 분리 | 기존 파일 보존, symlink/부모 교체/FIFO/장치/권한, binary stdin/out/err 회귀 |
| BP-12 | P2-03 | bounded queue와 위치별 손실 계수 | ring reserve·decode·queue·writer 실패를 독립 유발. 손실 기록도 소실되면 counter로 식별 |
| BP-13 | P2-03 | 실행 품질을 event payload와 별개로 관리 | drop 후 incomplete 지속; health 조회 실패 unknown; target exit 0에 supervisor 실패 숨기지 않음 |
| BP-14 | P2-04 | 센서 준비·scope 등록 후 명시 launch 허가 | 잘못된 ACK/EOF/짧은 메시지/timeout/부모 종료 시 target marker가 생기지 않음 |
| BP-15 | P2-04 | target identity와 권한·FD 제한 | 실제 target helper에서 real/effective/saved IDs·groups·caps·높은 번호 FD 검사 |
| BP-16 | P2-04 | target exec 결과·signal·root/child 수명 구현 | 존재하지만 exec 실패하는 파일, root만 종료, 잔류 자손, Ctrl-C/SIGTERM 각각 시험 |
| BP-17 | P2-04 | producer 종료와 bounded drain/cleanup | backlog 중 종료, drain timeout, reader unblock, 자손 생존 시 조기 detach 안 함 |
| BP-18 | P2-04 | 실제 Linux observer 통합 시험 | fixture 첫 open/connect 및 자손 event가 core DecodeEvent 통과; 환경·verifier 증거 |
| BP-19 | CI-02 | portable/generated/runtime 검증 분리 | 필수 Linux test skip은 PASS가 아님. 격리된 승인 runner·artifact·소스 SHA 연결 |
| BP-20 | P3-01/03 | deterministic taint reference/replay와 불완전 이력 처리 | 동일 입력 동일 결과; 중복/순서 변경/손실/thread/PID reuse vector |
| BP-21 | P3-02 | immutable policy snapshot·엄격한 validation | 오타/미지원/unknown을 no-match allow로 버리지 않음. 실행 중 파일 교체가 정책 몰래 변경 못 함 |
| BP-22 | P4-01 | ActPlane/KubeArmor와 최소 동기 상태 비교·규범 변경안 | Go 의미 소유와 kernel 전이 관계·loss/state 실패·허용 hook/effect 표 승인 |
| BP-23 | P4-02/04 | 지연 없는 source→sink와 LSM 결과 검증 | 같은 thread/다른 thread/fork 직후, user-space 지연, map 포화, 앞선 LSM deny 존중 |
| BP-24 | P4-03 | requested mode / active capability / actual action 분리 | unsupported block 시작 거부; 명시 kill과 pre-op 거부 분리; audit drop에도 상태 확인 |
| BP-25 | P4-04/V-01 | 공격 경로와 양성 대조군 | source 없는 allow, source→sink deny, 수신 측 확인, supervisor tampering; 기존 socket/pipe 한계 재현 |
| BP-26 | V-01/DOC-01/REL-01 | claim→test 지원표와 배포 재현성 | arm64/amd64 실제 실행 범위, Mac guest/host 구분, source/object/license/SBOM 대응 |

**첫 코드 묶음의 권고:** 선행 게이트를 충족한 다음 P2-01의 raw decoder·resource lifecycle과 실패 주입 테스트부터 시작한다. P2-02 hook, P2-03 audit/scope, P2-04 launch/권한/통합으로 이어간다. Linux에서 만들고 확인한 raw ABI fixture를 이용한 portable 테스트는 Mac에서도 작성할 수 있다. 그러나 현재 ENV 선행 조건을 없애거나 미검증 BPF layout을 발명해 진행해도 된다는 뜻은 아니다.

**문서 작업 종료 기준:** BP-02의 실행 계약을 한 번 확정하고, 검증할 최초 source/fixture 범위가 정해지면 해당 구현으로 전환한다. 전체 제품의 모든 미래 설계를 먼저 완성하려고 Phase 2를 계속 미루지 않는다. 동시에 Linux 실행 권한이 없는 것을 “아키텍처가 부족해서”라는 말로 바꾸지 않는다. 현재 실제 환경 게이트는 12.12의 G3다.

### 16.6 기존 비교 문구의 정정 제안

이번에 `RELATED_WORK.md`와 `SECURITY_MODEL.md`의 아래 문구를 **그대로 신뢰하면 안 되는 근거**를 찾았다. 사용자가 요청한 단일 파일에 정정안을 기록하며, 규범 파일을 몰래 고치지 않는다. 아래는 문헌상의 사실 정정과 제품 보장 변경을 분리한 PRE-01/02 입력이다.

| 기존 표현·위치 | 확인한 반례/한계 | 최소 정정안 |
|---|---|---|
| SECURITY_MODEL: access control은 조건부 접근을 표현할 수 없음 | SELinux TE는 domain/object type과 조건·transition을 사용함 | “정적 접근 정책과 접근 이력에 따른 동적 흐름 정책은 초점이 다르다. access control도 풍부한 조건을 표현할 수 있다.”로 수정 제안 |
| RELATED_WORK/SECURITY_MODEL: 사실상 IFC 프로젝트 없음·SPADE만 근접 | ActPlane의 fork/file/network label 전파와 동기 거부, CamFlow 전파/정책 hook, Flume 선행 연구 | “직접 비교 대상이 존재하며 AgentTaint는 AI 실행 단위의 범위·일관된 event 모델·검증 가능한 보장을 지향한다.” |
| RELATED_WORK: `te_effect_mode`가 타입 수준에서 강제. SECURITY_MODEL: AgentTaint 타입 수준 게이트 목표에 ActPlane를 선례로 연결 | 확인한 ActPlane 함수는 C 런타임 분기와 unsupported 값 반환 | upstream 선례는 “effect/backend 호환성을 명시적으로 검사하는 패턴”으로 한정. 이 사실 정정만으로 AgentTaint의 타입 수준 게이트 목표를 폐기·변경하지 않음 |
| RELATED_WORK: SPADE는 사후 포렌식 질의 전용 | reporter/filter pipeline과 `AttributeLabel.putEdge`의 라벨 전파 존재 | “provenance 수집·처리·질의와 라벨 처리. 조사한 코드에서 OS pre-op 거부는 확인하지 않음.” |
| RELATED_WORK: CamFlow 사실상 유지보수 중단 | 낮은 활동과 공식 종료 선언은 다름; 확인한 저장소는 archived false | 활동 일자·조회일을 기재하되 “현대 커널 지원·운영 유지 상태 미확인”으로 한정 |
| RELATED_WORK: Greywall은 민감 파일 deny-list | 다층 sandbox와 별도 watch 모드 설명, Landlock optional fallback 경로 존재 | 계층·모드별 제한을 설명. 특정 계층 fallback을 전체 무보호로도 단정하지 않음 |
| RELATED_WORK: AgentSight 차단 코드 전무 | 이번에 읽은 TLS probes는 관찰용; 전체 부재는 증명하지 않음 | “확인한 경로는 observability이며 IFC 사전 거부 근거로 쓰지 않는다.” |
| RELATED_WORK: E2B보다 AgentTaint 문서가 우수 | README 일부의 누락은 전체 docs/위협 모델의 부재 증거가 아님 | 우열 판단 삭제 제안; 각 프로젝트의 확인한 문서 범위만 기록 |
| RELATED_WORK: Daytona 공개 저장소 2026-06 유지보수 종료 | 이번 조회의 공식 README에 같은 공지가 있음 | 이 항목은 **현행 공지로 재확인**. 특정 공개 저장소의 상태이지 Daytona 전체 제품 중단이라고 확대하지 않음 |
| RELATED_WORK: Tetragon override/signal만 | 현재 개념 문서가 그 두 방식을 설명함; 모든 hook·확장 지원을 완전 조사하지 않음 | “공식 enforcement 문서에서 설명한 두 방식”으로 출처 범위를 한정. kill≠block 근거는 유지 |

조건부 접근의 반례는 [SELinux TE·constraints](https://github.com/SELinuxProject/selinux-notebook/blob/main/src/type_enforcement.md)와 [label/domain transition](https://github.com/SELinuxProject/selinux-notebook/blob/main/src/objects.md)에 있다. CamFlow에는 [taint 병합](https://github.com/CamFlow/camflow-dev/blob/0948c30f78c2ca69304d1441dd58581864f4bee1/security/provenance/propagate.c), [PREVENT_FLOW→EPERM 변환](https://github.com/CamFlow/camflow-dev/blob/0948c30f78c2ca69304d1441dd58581864f4bee1/security/provenance/include/provenance_query.h), [LSM permission 반환 경로](https://github.com/CamFlow/camflow-dev/blob/0948c30f78c2ca69304d1441dd58581864f4bee1/security/provenance/hooks.c)가 존재한다. 이는 정책 확장용 기반 코드의 확인이며 기본 설치가 완성된 AgentTaint 동등 IFC를 제공한다는 판정은 아니다.

**제안하는 제품 설명:** “AgentTaint는 AI 의도를 분류하지 않고, 명시적으로 실행한 프로세스 범위에서 민감 source 접근과 지원되는 전파·sink를 연결해 정책을 감사하고, 검증된 OS hook에서만 실제 효과를 거부하는 도구를 지향한다. 기존 IFC·provenance·sandbox 연구를 활용하며, byte-level 비밀 유출 증명이나 모든 채널의 차단을 주장하지 않는다.” 이 설명도 현재 구현 상태에서는 **목표**다.

### 16.7 프로젝트별 조사 목록과 적용 판단

총 **26개 비교 항목**이다. 독립 제품뿐 아니라 Linux 기능(Landlock), 선행 연구(Flume), 라이브러리와 개발 환경(cilium/ebpf, Lima)을 포함한다. 모두 같은 깊이의 감사가 아니며 D/S 표시와 구체적으로 읽은 경로가 조사 범위다. 외부 프로젝트에 대한 R 검증은 0건이다.

#### 16.7.1 OS 관찰·집행

| 대상·증거 | 확인한 처리 방식·근거 | AgentTaint 적용 / 비적용 |
|---|---|---|
| 01 Tetragon — D/S | `RunEvents`: reader 후 ready callback, bounded queue 포화 별도 counter. [고정 observer](https://github.com/cilium/tetragon/blob/4ba4a825413eff57e44823285e6f4c3c6676e2f2/pkg/observer/observer_linux.go). `GetExecID`는 PID와 Ktime 사용, TID 구분. [process 소스](https://github.com/cilium/tetragon/blob/main/pkg/process/process.go) | 손실 위치·identity·kill 의미 참고. daemon ready를 target gate로, ExecID를 생애 key로 그대로 복사하지 않음 |
| 02 Falco + libs — D/S | `scap_modern_bpf__init`의 socket calibration은 실제 syscall 이벤트를 확인. [고정 init](https://github.com/falcosecurity/libs/blob/8510814d3e8dd8b3582411aa0a2023aa9a1ba10e/userspace/libscap/engine/modern_bpf/scap_modern_bpf.c). 읽을 수 있는 여러 ring head의 timestamp merge. [ringbuffer 소스](https://github.com/falcosecurity/libs/blob/master/userspace/libpman/src/ringbuffer.c) | capture 경로 확인과 손실의 상태 영향 참고. static doctor에 calibration 추가 금지, timestamp 정렬을 인과 완전성으로 해석 금지 |
| 03 Tracee — D/S | pipelineReady 후 running/callback, 종료 drain 30초 한도. [고정 Run 구현](https://github.com/aquasecurity/tracee/blob/2f9dc40c20b17c2ba27f6d92b25e62790bd48a62/pkg/ebpf/tracee.go). [process/thread tree](https://github.com/aquasecurity/tracee/blob/main/pkg/datastores/process/proctree.go) | 단계별 준비·bounded drain 참고. 30초를 AgentTaint 요구값으로 무검증 복사하지 않음. payload/artifact 수집 확대 안 함 |
| 04 KubeArmor — D/S | 필수 LSM attach 실패는 오류, 선택 path hooks는 경고 가능. [loader 소스](https://github.com/kubearmor/KubeArmor/blob/main/KubeArmor/enforcer/bpflsm/enforcer.go). 16.3.B의 반환값/감사 분리 | hook별 required/optional 표와 사전 거부 경로 참고. K8s 정책·CRD·backend 다중화를 지금 추가하지 않음 |

Falco가 설명하는 [event drop](https://falco.org/docs/concepts/event-sources/kernel/dropped-events/)과 [drop 원인 구분](https://falco.org/docs/troubleshooting/dropping/)은 로그 수뿐 아니라 재구성한 상태의 신뢰도를 다뤄야 하는 근거다. AgentTaint는 버퍼를 크게 했다는 이유만으로 손실 문제가 해결됐다고 하지 않는다.

#### 16.7.2 Sandbox·실행 경계

| 대상·증거 | 확인한 처리 방식·근거 | AgentTaint 적용 / 비적용 |
|---|---|---|
| 05 gVisor — D/S | 생성·start signal·result 분리. Gofer와 Directfs의 파일 접근 설계. [공식 filesystem](https://gvisor.dev/docs/user_guide/filesystem/), 16.3.A의 loader/controller | 신뢰 경계·수명 분리 참고. Sentry/Gofer/OCI runtime 재구현은 제외. 모든 파일 동작이 항상 RPC라는 단순화 금지 |
| 06 bubblewrap — D/S | namespace/mount 기반 구성 도구, 정책 안전성은 설정에 의존. [security 설명](https://github.com/containers/bubblewrap/blob/main/README.md#sandbox-security), 16.3.A의 block FD | 시작 동기화·관리 socket 노출 참고. 범용 confinement 기능을 AgentTaint 필수 의존성으로 만들지 않음 |
| 07 nsjail — D/S | 명시 부모 완료 byte와 child confinement 성공 후 exec. [고정 subproc](https://github.com/google/nsjail/blob/4ff54a6e0d5b65a0e1633d6e2fd1425ba6c882ce/subproc.cc), [FD/권한 처리](https://github.com/google/nsjail/blob/master/contain.cc) | gate 실패 검사·FD allowlist·권한 순서 참고. 확인한 단순 FD fallback 범위만으로 모든 FD 차단 주장 금지 |
| 08 Landlock — D | ABI별 권한·기존 FD 의미를 명시. [공식 문서](https://docs.kernel.org/userspace-api/landlock.html) | capability-aware 계약 참고. 커널 최소 버전과 기능 활성화를 같은 것으로 보지 않음; 동적 taint의 대체재로 취급하지 않음 |
| 09 landrun — D/S | strict와 명시 best-effort, `sandbox.Apply` 실패 반환. [main](https://github.com/Zouuup/landrun/blob/main/cmd/landrun/main.go), [sandbox](https://github.com/Zouuup/landrun/blob/main/internal/sandbox/sandbox.go) | 요청 보장/활성 권한 비교. 확인한 wrapper의 TCP 포트 권한을 임의 IP/domain 정책으로 확대하지 않음 |
| 10 Anthropic sandbox-runtime — D/S | Linux bubblewrap/namespace와 Mac Seatbelt, proxy를 backend별로 사용. [README](https://github.com/anthropics/sandbox-runtime), [Linux 준비 경로](https://github.com/anthropics/sandbox-runtime/blob/main/src/sandbox/linux-sandbox-utils.ts), [Mac profile/wrapper](https://github.com/anthropics/sandbox-runtime/blob/main/src/sandbox/macos-sandbox-utils.ts) | backend별 보장·준비 확인 참고. shell-string wrapper·Seatbelt·proxy를 현재 run 계약에 도입하지 않음 |
| 11 Firejail — D만 | SUID 실행기, namespace/seccomp/capability와 profile을 설명. [공식 README](https://github.com/netblue30/firejail) | 명시 profile·위협 경계 참고 대상으로 분류. SUID 설치/권한 설계를 그대로 도입하지 않음; 소스 실패 분기 미조사 |
| 12 OpenSnitch — D만 | GNU/Linux interactive application firewall. [공식 저장소](https://github.com/evilsocket/opensnitch) | 사용자에게 연결 판단을 설명하는 UX 참고 후보. 대화형 GUI·상시 daemon을 비대화식 AgentTaint MVP 필수로 추가하지 않음 |

Landlock 최신 문서에는 ABI 10 UDP 권한도 있으므로 “Landlock은 UDP를 전혀 지원하지 않는다” 같은 버전 없는 표현은 피한다. 이것이 현재 landrun wrapper나 AgentTaint guest에서 그 권한을 쓴다는 뜻은 아니다. 또한 sandbox-runtime의 저수준 wrapper에 기능별 경고/fallback 경로가 있다는 사실만으로 전체 제품이 언제나 무보호로 실행된다고 단정하지 않는다.

#### 16.7.3 AI 전용 도구·provenance·IFC

| 대상·증거 | 확인한 처리 방식·근거 | AgentTaint 적용 / 비적용 |
|---|---|---|
| 13 ActPlane — D/S | 16.3.C의 고정 source에서 프로세스/파일/endpoint labels, 전파, effect 판정과 LSM 거부 | **직접 비교 우선순위 최상**. 범위·race·동기 상태 시험을 참고하되 kernel 정책 엔진을 규범 승인 없이 복사 금지 |
| 14 AgentSight — D/S | `probe_SSL_read_exit`, `probe_SSL_write_exit`, rustls probe와 ring 전송. [고정 sslsniff](https://github.com/eunomia-bpf/agentsight/blob/bb99b66f8f98e4b9f8b1769a3da0a8fbbe26b6c3/bpf/sslsniff.bpf.c) | 이벤트 상관관계 참고. TLS plaintext 수집은 불필요한 민감 데이터 노출이며 현재 제외 |
| 15 Sandlock — D/S | ABI 확인·권한 mask 구성·restrict 경로. [landlock.rs](https://github.com/multikernel/sandlock/blob/main/crates/sandlock-core/src/landlock.rs), [README](https://github.com/multikernel/sandlock) | 필요한 보호 기능을 제공하는지 확인하는 방식 참고. 확인한 confinement을 IFC 구현으로 재명명하지 않음; 성능 수치 미재현 |
| 16 Greywall — D/S | 다층 sandbox와 watch 모드, Landlock 실패 일부에서 optional fallback. [README](https://github.com/GreyhavenHQ/greywall), [linux_landlock.go](https://github.com/GreyhavenHQ/greywall/blob/main/internal/sandbox/linux_landlock.go) | 계층별 활성 상태·관찰/집행 구분. fallback을 AgentTaint block 성공으로 취급하지 않음 |
| 17 SPADE — S | reporter→buffer, filter chain, `AttributeLabel.putEdge` 라벨 전파. [고정 reporter](https://github.com/ashish-gehani/SPADE/blob/1ff51190f5f0fb67f5d1a82a7fa2679145f1a4fb/pkg/java/src/main/java/spade/core/AbstractReporter.java), [고정 label filter](https://github.com/ashish-gehani/SPADE/blob/1ff51190f5f0fb67f5d1a82a7fa2679145f1a4fb/pkg/java/src/main/java/spade/filter/AttributeLabel.java) | 관찰 사실·graph 관계·derived label 분리 참고. Java graph DB/질의 계층을 Go MVP에 도입하지 않음 |
| 18 CamFlow — D/S | LSM provenance, taint 병합과 prevent-flow 반환 기반. [공식 설명](https://camflow.org/), 16.6 고정 소스 | 관계별 전파·정책 extension을 참고. kernel 패치/LSM을 현재 CO-RE 전략의 대체로 도입하지 않음 |
| 19 Flume — D: 저자 논문 | 프로세스 단위 DIFC와 endpoint/reference monitor. [SOSP 2007 논문](https://pdos.csail.mit.edu/~yipal/papers/flume-sosp07.pdf) | 기존 FD/pipe/socket과 declassification을 함께 모델링해야 한다는 선행 연구. 전체 DIFC·애플리케이션 포팅을 v0.1에 요구하지 않음 |
| 20 SELinux — D | type/domain/transition/constraints·label inheritance. [공식 notebook](https://github.com/SELinuxProject/selinux-notebook/blob/main/src/type_enforcement.md) | access-control의 표현력을 정확히 설명하는 기준. SELinux 전체 정책 모델로 AgentTaint 모델을 바꾸지 않음 |

CamFlow 저장소의 활동 판단은 [공개 metadata](https://api.github.com/repos/CamFlow/camflow-dev)의 조회 시점 값일 뿐이다. 마지막 push 일자만으로 보안 지원 종료를 확정하지 않는다. SPADE의 라벨 처리도 kernel pre-op 집행을 확인한 근거는 아니다. 각 구현의 정확성·보안 지원과 실제 kernel 통과 여부는 미검증이다.

#### 16.7.4 격리 인프라·개발 환경 — 보완 전략

| 대상·증거 | 확인한 설명·근거 | AgentTaint 적용 / 비적용 |
|---|---|---|
| 21 Daytona — D만 | AI 코드 실행 인프라, 해당 공개 repo의 2026-06 이후 유지보수 종료 공지. [README](https://github.com/daytonaio/daytona) | 의존 후보의 실제 유지·배포 상태 확인. 제품 전체 중단으로 확대하지 않으며 runtime 도입 안 함 |
| 22 E2B — D만 | SDK로 원격 sandbox 생성·실행, self-hosting 안내. [공식 저장소](https://github.com/e2b-dev/E2B) | 생성→실행→종료 UX 참고. 클라우드 sandbox API를 local OS IFC의 대체 증거로 사용하지 않음 |
| 23 Coder Agents — D만 | 해당 Agents architecture는 control plane의 agent loop와 provider credential 관리로 workspace 직접 노출을 피함. [공식 Models 문서](https://coder.com/docs/ai-coder/agents/models) | credential을 실행 경계 밖에 두는 보완 전략. 모든 Coder 실행 방식에 일반화하거나 정책 우회를 허용하는 LLM 예외로 쓰지 않음 |
| 24 microsandbox — D만 | microVM·local-first·credential 비노출 기능을 공식 README에서 설명. [저장소](https://github.com/superradcompany/microsandbox) | VM 경계와 secret 미노출의 보완성 참고. “유출 불가”·성능 홍보는 이번 검증 결과가 아님; 추가 VM runtime 도입 안 함 |
| 25 cilium/ebpf — D/S | v0.22.0의 generator/examples/features/Reader/CI 확인. 고정 commit `e55144e17360b60cc4583229c35c2dbf0935b308` | 현재 기술 스택의 직접 구현 참고. generator와 library 버전 일치·resource lifecycle·재현 테스트 채택 |
| 26 Lima — D | plain mode는 mount/dynamic forwarding/containerd 등 편의 기능을 축소하나 provisioning과 static forwarding은 남을 수 있음. [공식 plain 문서](https://lima-vm.io/docs/config/plain/) | 현재 VM 설정의 신뢰 경계 재검토. `plain`을 완전 네트워크 차단이나 무부작용 시작으로 해석하지 않음 |

이 표는 도입 목록이 아니다. 초기 runtime 의존성에 26개 도구를 추가하지 않는다. 직접 필요한 것은 기존 고정 Go/BPF toolchain과 승인된 Linux 실행 환경이고, 나머지는 설계와 시험의 참고 자료다.

### 16.8 macOS·문서·skill·subagent·MCP에 미치는 영향

#### macOS는 기능 단위로 지원 여부를 표시한다

| 작업/보호 대상 | 현재 선택 | 이번 조사로 바뀌는가 |
|---|---|---|
| Go core·CLI·parser·replay·portable 테스트 개발 | Mac에서 가능; Phase 0/1 로컬 증거는 12.10 | 바뀌지 않음 |
| BPF object 생성과 실제 load/attach/verifier | Linux toolchain·kernel 필요 | 생성물 재현·커널 시험 분리를 더 명확히 함 |
| Mac 안 Linux guest에서 실행한 agent와 도구 | Linux observer/enforcement 구현 후 검증 대상 | 현재 guest runtime은 미실행. 구현 완료로 승격하지 않음 |
| Mac host에서 실행 중인 agent·IDE·도구 | Linux guest 센서로 직접 보호 못 함 | 네이티브 sandbox 제품 존재와 별개 |
| native Seatbelt backend | sandbox-runtime/Greywall 사례는 있음 | 현재 non-goal 유지; Linux eBPF와 동등한 taint 보장 아님 |
| native Endpoint Security backend | 별도 entitlement·배포·기능 검토 필요 | 이번 조사에서 구현/실험하지 않음 |
| Apple Silicon·Intel Linux 지원 | arch별 object/ABI/실제 kernel 결과 필요 | arm64 cross-build를 amd64 runtime 검증으로 세지 않음 |

VM을 쓰더라도 host home, SSH agent, Docker socket, 공유 디렉터리, proxy 환경변수, 포트 forwarding이 실제 신뢰 경계를 바꾼다. **Lima plain 문서가 있다는 이유로 정지된 기존 VM 설정을 수정하거나 시작하지 않았다.** 기존 G3 실행 권한 게이트는 그대로다. Native Mac을 즉시 추가하면 별도 센서/권한/테스트 행렬이 필요하므로 지금 일정 단축 수단으로 추천하지 않는다.

#### 문서: 수보다 정확한 계약이 우선

- 이번 산출물은 이 통합 파일 하나다. 비교 보고서를 별도 Pages/슬라이드/새 ADR 폴더로 늘리지 않는다.
- PRE-01/02에서 16.6의 사실 정정과 Phase 2 runtime 계약을 해당 기존 문서에 반영한다. 규범 정정에는 사용자 확인 절차를 지킨다.
- 코드 작성 시 해당 작업 카드에 source SHA·테스트 명령·PASS/FAIL/SKIP·남은 한계를 업데이트한다. master가 실제 코드와 독립된 또 다른 API 명세로 굳어지지 않게 한다.
- 장기적으로 문서 분리가 필요하더라도 현재 한 파일 요구와 경로 규칙을 우선한다. “documentation-and-adrs 스킬이 권한다”는 이유로 새 경로를 자동 생성하지 않는다.

#### Skill: 선택적 개발 절차, 제품 의존성 아님

8절의 `source-driven-development`, `documentation-and-adrs`, `code-review-and-quality` 선택 방향을 유지한다. 이번 연구는 **설명만 읽고 API/보장을 추측하지 말 것**, **규범과 구현 충돌을 기록할 것**, **실패 경로를 독립 검토할 것**이라는 사용 목적을 강화한다. 추가 설치나 전체 skill pack이 있어야 Phase 2를 개발할 수 있는 것은 아니다.

프로젝트 전용 스킬을 향후 만든다면 새 기능 생성기보다 다음 세 개의 좁은 검사 절차가 유용하다. 현재는 제안이며 설치하지 않았다.

| 전용 절차 후보 | 입력 | 출력·금지 사항 |
|---|---|---|
| Linux preflight evidence | 승인된 환경의 static/probe/runtime 결과 | 증거 단계와 미검증 구분; 자동 sudo/install/VM start 금지 |
| Security semantics review | 변경 diff·hook·policy·claim→test 표 | detect/block/kill, source/propagation/sink 범위 확인; LLM 판단을 runtime policy로 삽입 금지 |
| Generated artifact review | source/object/header/toolchain manifest | 재생성 diff·버전·license 대응; 미신뢰 generator 자동 실행/자동 commit 금지 |

#### Subagent: 전문 역할보다 산출물 경계가 중요하다

이번 조사에서는 범주별 읽기 전용 근거 수집을 병렬로 진행하고, 주 에이전트가 소스 재확인·설계 선택·단일 문서 작성을 맡았다. 특히 ActPlane·CamFlow의 반례, nsjail/bubblewrap의 gate 차이, KubeArmor의 반환값 경로를 주 에이전트도 확인했다.

후속 구현에서 유용한 분담은 ① core/decoder portable 테스트, ② 승인된 Linux BPF 구현·통합, ③ 보안 의미/실패 경로 독립 리뷰다. 서로 같은 파일을 동시에 수정하지 않고, 리뷰어에게 변경 권한을 자동으로 주지 않는다. 각 보고에는 **확인한 source, 구체 함수/명령, 적용 판단, 미검증 항목**을 요구한다. 역할 이름을 늘리는 것보다 책임과 acceptance의 분리가 중요하다.

#### MCP: 필요한 읽기 경로만 유지

- codebase-memory-mcp: 현재 코드 구조·호출 관계 탐색 우선. 그래프 결과는 실제 소스·버전으로 확인한다.
- GitHub/web: 공식 source·문서·고정 ref 비교에 사용. source 조회 권한을 repo write/PR/issue 게시 권한으로 확대하지 않는다.
- Linux 실행 도구: 현재 필요한 실제 연결 공백이다. 별도의 MCP 서버를 새로 만든다고 해결된 것으로 보지 않는다. 허용된 guest 명령 경로와 권한·자원 정리 계약이 먼저다.
- provenance DB·vector DB·LLM classifier·runtime MCP gateway: 이번 결과만으로 추가할 이유 없음. 제품 보안 판단의 필수 경로에 넣지 않는다.
- 메모리: claude-mem의 저장 allowance 장애는 계속 별개이며, 이번 자료는 저장소 문서로 남긴다. worker 재시작·provider 설정 변경·전역 skill 기록 쓰기는 수행하지 않는다.

### 16.9 남은 결정과 연구 완료의 의미

#### 다시 사용자 선택을 기다리지 않아도 되는 것

Go+C/CO-RE 유지, Linux 우선, Mac 개발+Linux guest 실행, 핵심 모델 유지, payload 수집 제외, 별도 audit 파일, strict unsupported 처리 방향, 선택 skill 최소 도입, 새 daemon/DB/MCP 제품 기능 보류는 기존 방향과 이번 근거가 일치한다. 세부 API 이름·queue 크기·timeout은 구현자가 workload와 실패 시험으로 정할 기술 문제이지 사용자에게 수십 개 옵션을 고르게 할 문제가 아니다.

#### 확인이 필요한 것은 구체적으로 세 가지다

| 항목 | 권고 | 지금 상태 |
|---|---|---|
| 규범의 비교 사실 정정 | 16.6의 access-control/IFC 독창성/effect 타입 보장 표현을 최소 수정. 제품 핵심 모델은 그대로 | **정정 제안만 기록**, SECURITY_MODEL 등 미수정. 적용 전 확인 필요 |
| Phase 2 최소 runtime 계약 | 16.4를 바탕으로 gate·target identity·권한·FD·감사 실패/종료 결과 확정. 필요한 좁은 규범 변경만 제시 | **설계 후보**, 새 helper/옵션/보장 자동 승인 아님 |
| 실제 Linux 실행 환경 | 허용된 guest 명령 경로·권한 확보 후 ENV 검증 | 기존 G3 유지. VM 시작·설치·권한 우회 미실행 |

E04의 동기 커널 전파 변경, native Mac, Kubernetes, 공개 배포·license 선택은 해당 후속 단계의 확인 사항이다. 이번 조사 때문에 지금 모두 결정할 필요는 없다. 특히 비교 제품에 기능이 있다는 이유만으로 scope 순서를 건너뛰지 않는다.

#### 증거 추적과 재확인 규칙

- release 기준으로 확인한 라이브러리: `cilium/ebpf v0.22.0` tag가 가리키는 commit은 GitHub API에서 `e55144e17360b60cc4583229c35c2dbf0935b308`으로 조회했다. 라이브러리·예제·Reader·CI의 해당 ref 소스를 읽었다.
- 주요 비교 구현은 16.3/16.7의 commit 고정 링크를 근거로 삼는다. Tetragon observer, Tracee Run, Falco init, KubeArmor 반환값, nsjail start, bubblewrap block-fd, ActPlane taint/effect, CamFlow 전파/permission을 고정 ref로 재확인했다.
- branch 링크만 있는 나머지 함수는 **선택 소스 확인·가변 snapshot**이다. 조회한 HEAD 숫자만 나중에 붙여 같은 내용을 읽었다고 꾸미지 않는다. 재사용할 때 exact ref로 다시 읽는다.
- 문서가 기능을 설명한 것과 실제 구현 분기를 확인한 것, 그 코드를 실행한 것은 다르다. 공식 문서의 플랫폼/버전 지원 주장도 AgentTaint의 지원 증거로 전이되지 않는다.
- 코드 재사용 전 license/SPDX/NOTICE와 생성물·vendored header 조건을 확인한다. 이번 보고서는 코드 복사나 license 적합성 판정을 수행하지 않았다.

**연구 산출물 완료:** 비교 범위, 출처, 반례, 적용/비적용 관행, runtime 권고, 기존 작업에 연결한 BP-01~26, Mac·도구 영향과 결정 목록을 이 파일에 정리했다. `make-plan`의 근거 수집/주 에이전트 종합 절차와 `task-observer`의 지침 확인을 사용했으며 외부 agent-skills는 설치하지 않았다.

**제품 검증과 분리:** 이번 변경은 문서만이다. 외부 프로젝트 빌드·실행, Linux verifier/load/attach, 보안 우회·성능 시험, VM 시작, 새 code 구현, commit/push는 하지 않았다. Phase 0/1의 기존 Go 검증 결과는 12.10에 남아 있으며 이번 조사 결과로 재실행/추가 PASS를 만들지 않는다. **Byte-level taint를 주장하지 않으며, post-event detection이나 kill을 prevention이라고 부르지 않는다.** 현재 제품에 실제 OS 감지·taint·차단이 구현됐다고도 주장하지 않는다.

문서 검증 기록: 독립 리뷰에서 sandbox/READY 계약 및 IFC/provenance 비교 주장의 차단성 오류는 발견되지 않았다. ActPlane 선례 설명과 AgentTaint의 타입 수준 게이트 목표를 혼동하지 않도록 정정안을 좁혔다. 코드 fence 균형, JSON 예제 3개 파싱, 로컬 파일 링크 12개, 비교 항목 01~26과 BP-01~26의 개수·중복 검사를 통과했다. `git diff --check`도 통과했다. Go source·go.mod·Makefile·기존 CI를 합친 20개 파일의 bundle SHA-256은 `851228228dcd999d4fa8567dce66186dfe1f8ad48d3f2630122ef1bfe99fe3a5`로 이전 검증과 같으며 제품 코드는 변경되지 않았다. 문서 전용 작업이므로 이번에 gofmt/go vet/go test/build를 다시 실행하지 않았다.
