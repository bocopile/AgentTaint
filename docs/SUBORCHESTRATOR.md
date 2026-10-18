# SubOrchestrator 연동

> 이 문서는 AgentTaint의 설계 문서(`ARCHITECTURE.md`/`SECURITY_MODEL.md`/
> `THREAT_MODEL.md`/`ROADMAP.md`)와 달리 **규범(normative)이 아니다.**
> AgentTaint를 [SubOrchestrator](https://github.com/bocopile/SubOrchestrator)
> (외부 멀티에이전트 SPEC 파이프라인 도구)로 구동할 때의 운영 방식만
> 다룬다. AGENTS.md/prompts/의 워크플로우와 무관하게 이 저장소를 손으로
> 작업할 수도 있다 — 이 문서는 그 대안(자동화 경로) 하나를 기록해 둔 것이다.

## 왜 별도 문서인가

`docs/ARCHITECTURE.md`는 이 저장소의 `docs/` 트리를
`ARCHITECTURE.md`/`THREAT_MODEL.md`/`SECURITY_MODEL.md`/`ROADMAP.md` 4개로
못박아 뒀다. 이 파일은 그 목록 밖의 추가다 — 설계가 아니라 툴링 절차이기
때문에 별도 파일로 분리했다. 이 위치/파일명이 마음에 들지 않으면
`docs/tooling/`처럼 하위 디렉터리로 옮겨도 내용엔 영향 없다.

## 두 워크플로우 모델은 다르다

`prompts/README.md`가 정의하는 AgentTaint 자체 워크플로우는 **사람이 세션마다
직접 트리거**하는 모델이다: AGENTS.md + 관련 docs + 현재 Phase 프롬프트를
읽고, 그 Phase 하나만 수행하고, 정해진 형식으로 보고한 뒤 멈춘다.

SubOrchestrator는 그 반대로 **SPEC → Research → (TDD 또는 DDD 방법론) →
Consensus Review → Plan → Run**까지 자동으로 흘러가는 파이프라인이다.
둘을 억지로 동일시하지 않는다 — 아래는 "Phase 프롬프트 하나를 SubOrchestrator의
SPEC 사이클 한 번에 태운다"는 매핑 규칙이다.

## 매핑 규칙: Phase = SPEC 1회

```
orchestrator plan --context-file prompts/0N-xxx.md --project-path .
```

- Phase 하나 = `plan` 호출 하나. 다음 Phase로 넘어가기 전에 반드시 현재
  Phase의 SPEC이 승인·병합됐는지 확인한다 (`docs/ROADMAP.md`의 "이전
  Phase가 완료된 뒤에만 시작" 규칙과 동일).
- **`--context-file`의 자동 문서 첨부는 `docs/[\w-]+\.md` 패턴만 인식한다.**
  즉 context 파일(예: `prompts/00-bootstrap.md`) 본문에 `docs/ARCHITECTURE.md`
  같은 문자열이 그대로 등장하면 그 파일 내용이 자동으로 덧붙지만,
  `AGENTS.md`(루트, docs/ 밖)나 `prompts/` 아래 다른 파일은 이 메커니즘으로는
  안 붙는다. `AGENTS.md`는 위 `CLAUDE.md`의 `@AGENTS.md` import로 별도
  커버된다 — claude 서브프로세스가 뜨는 시점에 자동 로드됨. Codex CLI는
  `AGENTS.md`를 애초에 네이티브로 읽으므로 이 문제가 없다.
- Phase 프롬프트의 **"Out of Scope" 섹션은 SPEC 작성 시 그대로
  "이번 SPEC 범위 밖"으로 옮겨 적는다.** SubOrchestrator의 conductor는 빈
  공간을 보면 기능을 채우려는 경향이 있으므로(AGENTS.md도 동일하게 지적),
  Out of Scope를 SPEC에 명시하지 않으면 다음 Phase 영역을 침범할 수 있다.

## 방법론 자동 판정

Phase 0(코드 0줄, 그린필드)은 SubOrchestrator가 TDD 방법론으로 자동
분류한다. Phase 2 이후(기존 코드 존재)부터는 DDD(Analyze→Preserve→Improve)로
전환된다 — 이 전환은 자동이며 별도 설정이 필요 없다.

## 실행 전 준비 체크리스트

- [ ] 실행 직전에 `git status --short --branch`와 staged diff를 확인한다.
      저장소 상태는 세션마다 달라지므로 이 문서에 특정 파일 상태를
      고정하지 않는다. 기존 사용자 변경은 별도 승인 없이 stage하지 않는다.
- [x] 확인일 2026-10-19 기준 `.gitignore`에 `.env`와
      `.orchestrator/logs/`가 포함돼 있다. ignore 설정은 비밀정보가 안전하게
      보관된다는 증거가 아니므로 실제 staged 파일도 확인한다.
- [ ] 권한 모드는 편의 때문에 `auto` 또는 `bypassPermissions`로 넓히지
      않는다. read-only 기본값과 작업별 최소 권한을 유지하고, 필요한 명령만
      task-scoped sandbox/allowlist 또는 개별 승인으로 실행한다. 저장소·전역
      설정을 바꾸기 전에 실제 적용 권한과 변경 경로를 확인한다.
- [ ] 모델·어댑터 가용성은 시점·계정·설정에 따라 달라진다. 실행 직전에
      활성 provider와 실제 응답을 확인하고, consensus 인원이나 축소 동작을
      사전 사실로 가정하지 않는다. 사용할 수 없는 provider는 `not_tested` 또는
      해당 도구의 명시된 unavailable 상태로 기록한다.

## Phase 2 이후: eBPF/Linux 검증 인프라

Phase 2(Linux Observer)부터는 `cilium/ebpf` + libbpf CO-RE의 실제 Linux
커널 검증이 필요하다. macOS 호스트의 Go build/test는 guest의 BTF, verifier,
load, attach, 실제 event 동작을 증명하지 않는다 (`docs/ARCHITECTURE.md`의
Linux VM 실행 방향 참조).

SubOrchestrator의 언어별 검증 체인(`go vet → golangci-lint → go test →
go build`)은 Go 코드를 다룬다. 이 체인만으로 `bpf/*.bpf.c`(C, CO-RE)의
컴파일·verifier·load·attach 여부를 검증했다고 간주하지 않는다. 다음은 현재
환경 사실과 이후 gate다.

- 2026-10-19 host 확인: macOS arm64, Go 1.26.1, Apple clang 17.0.6. `limactl`
  목록에는 기존 `ebpf-lab` Lima VM이 `Stopped`, VZ/aarch64, 4 CPU/4 GiB/20 GiB로
  나타났다. host PATH에서 `llvm-strip`과 `bpftool`은 찾지 못했다. 이 값은
  Linux guest 상태가 아니다.
- 이 세션에서는 guest 설정·kernel·BTF·LSM·도구·권한을 읽지 않았고 VM을
  시작하지 않았다. G3/ENV-01은 허용된 Linux guest 실행 경로가 제공될 때
  수행한다. 설정 파일이나 host 도구만으로 guest 준비 완료를 판정하지 않는다.
- guest에서 `go test`와 Linux 통합 시나리오를 어디서 실행할지는 ENV-01/02 및
  Phase 2 검증 계약에서 정한다. portable CI는 kernel 검증을 대신하지 않으며,
  Linux privileged CI 구성은 해당 작업의 선행조건·권한이 승인되기 전에는
  추가하지 않는다.

Go toolchain 검사와 eBPF runtime 검증을 구분한다. doctor의 기본 동작은 계속
비실행 진단이며, 임의 설치·VM 시작·권한 변경은 이 운영 문서만으로 허용되지
않는다.
