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

- [ ] `git status`에 커밋 안 된 변경이 없는지 확인 (현재 `README.md` 수정,
      `AGENTS.md`/`docs/`/`prompts/` untracked 상태 — 커밋 권장)
- [ ] `.gitignore`에 `.env`, `.orchestrator/logs/` 패턴 추가 (현재 파일
      자체가 없음)
- [ ] `orchestrator init --project-path .` 실행 시 permission-mode를
      `auto` 또는 `bypassPermissions`로 선택 — 기본 allowlist는 npm 계열만
      있고 `go build`/`go vet`/`go test`/`gofmt`가 없어, 그대로 두면 Go
      명령마다 승인 프롬프트가 뜬다.
- [ ] Gemini 어댑터는 2026-08-29 무료 티어 정책 변경으로 인증이 막혀
      있다 — 3-AI consensus가 Codex+Claude 2-AI로 자동 축소된다
      (SubOrchestrator `cross-validate-policy.md`의 "어댑터 unavailable"
      규칙에 따른 정상 동작).

## Phase 2 이후: eBPF/Linux 검증 인프라 (아직 미결정)

Phase 0~1은 순수 Go라 로컬 macOS에서 `go build`/`go test`로 그대로
검증된다. Phase 2(Linux Observer)부터는 `cilium/ebpf` + libbpf CO-RE가
실제 Linux 커널을 요구한다 — macOS 호스트에서는 eBPF 프로그램을 로드하는
테스트를 네이티브로 돌릴 수 없다 (`docs/ARCHITECTURE.md`의 "macOS는 Linux
VM에서 실행" 방침과 동일한 제약).

SubOrchestrator의 언어별 검증 체인(`go vet → golangci-lint → go test →
go build`)은 Go 코드만 다루고 `bpf/*.bpf.c`(C, CO-RE) 컴파일·verifier 통과
여부는 검증 대상에 포함하지 않는다. Phase 2 SPEC을 시작하기 전에 다음을
먼저 정해야 한다:

1. Linux VM(Lima/Docker) 안에서 `go test`를 어떻게 실행시킬지 — SPEC의
   Acceptance Criteria에 "VM 안에서 실행"을 명시적 단계로 넣을지, 아니면
   별도 수동 검증으로 분리할지.
2. `bpf/*.bpf.c` 빌드(clang -target bpf, bpftool gen skeleton)를 CI/파이프라인
   어디에 넣을지 — 현재 로컬에는 `clang`은 있지만 `llvm-strip`/`bpftool`이
   없다.

지금은 결론 없이 갭으로만 기록해 둔다 — Phase 0/1 진행에는 영향 없다.
