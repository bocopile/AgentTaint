# AgentTaint 전체 실행 작업표

작성: 2026-10-25 KST. 기준: `3b8c9ea8442e181df72d01e83d74bb0af0b0ed51`.

사용자가 요청한 전체 작업 목록과 순차 진행을 위한 실행 큐다. 기존 완료
증거는 [master plan](IMPLEMENTATION_MASTER_PLAN.md), 규범은
[아키텍처](ARCHITECTURE.md)·[보안 모델](SECURITY_MODEL.md)·
[위협 모델](THREAT_MODEL.md)·[로드맵](ROADMAP.md)을 따른다.
이 문서는 규범이나 Phase 순서를 변경하지 않는다.

재설계 입력은 별도 브랜치 `codex/arch-redesign-20261025-sensitive-resources`의
`b9c7368`에 있는 master plan 17~18절이다. 아직 main에 통합되지 않은 제안이며,
`git show b9c7368:docs/IMPLEMENTATION_MASTER_PLAN.md`로 확인할 수 있다.
이 문서의 P02~P10은 그 제안의 작업 ID를 유지한다. GF01~GF04는 기존
TOOL-03의 하위 카드이며 별도 제품 Phase가 아니다.

## 현재 상태와 실행 원칙

- Linux arm64 기술 MVP, INT-02/03의 저장 실패·capture loss 보완, TOOL-01/02 및
  agent handoff 문서는 기존 기록상 완료. 이번 작업에서 커널 검증을 재실행하지 않았다.
- 전체 TOOL-03, 실제 토큰 절감 비교, Linux BPF 원격 CI, 배포, 제품 대시보드는 미완료.
- 사용자 지정 파일 접근 모델은 구현되어 있다. 자동 민감 내용 판별, 임의 DB 조회
  보호, 실행 전체 상태, 일반 IPC 전파는 현재 구현된 보장으로 계산하지 않는다.
- 제품 정의 → 재사용 비교 → 증거·위협 계약 → 규범 변경 확인 → 제한된 실험 →
  최소 기능 구현 순서다. 도구 평가가 제품 정의 작업을 무기한 지연시키지 않도록 한다.
- `no-loss`는 손실을 숨기거나 성공 기준을 완화하지 않는 수용 기준이다.
  과부하를 포함한 모든 환경에서 절대 무손실을 보장한다는 뜻이 아니다.
  손실·불완전·집행 건강을 각각 기록하고 incomplete를 성공으로 바꾸지 않는다.

## A. 개발 도구와 토큰 비용

| ID / 상태 | 세부 작업과 산출물 | 완료 기준 / 검증 | 선행 / Git 단위 |
|---|---|---|---|
| GF01 / 평가 완료, 미채택 | Graphify source·license·설치 부작용 확인, 임시 Python 환경, 고정 main의 코드 사본 분석, 검색·HTML 생성 시험, 전체 실행 큐 작성 | [평가 보고서](GRAPHIFY_EVALUATION.md)와 manifest; 성공·누락·미검증 분리, 기존 AGENTS/hook/MCP 설정 보존 | TOOL-03; `codex/tool-gf01-20261025-evaluation` / 문서·증거 커밋 |
| GF02 / 제한된 비교 완료 | 동일 37개 파일의 대표 심볼·원문 대조와 합성 freshness 실험 | [비교 결과](GRAPHIFY_COMPARISON.md): 10개 질문의 근거와 누락, MCP header 위치 오류와 디렉터리 제외 확인. 전체 semantic accuracy 검증 아님 | GF01; `codex/tool-gf02-20261025-comparison` |
| GF03 / inconclusive | 실제 모델 usage·동일 모델 A/B 미확보로 절감률 산정 불가 | [비교 결과](GRAPHIFY_COMPARISON.md)의 비용 판정. 비용 평가 성공으로 처리하지 않음 | GF02와 같은 문서 카드로 종료 판정; 필요 시 계측 가능한 환경에서 재개 |
| GF04 / 현재 미채택 | 자동 설치·hook·MCP 교체를 하지 않고 기존 원문 확인 경로 유지 | BPF coverage와 비용 이득 미확정. 재평가에는 GF02/03 추가 근거와 구체 도입 결정 필요 | 구현 branch 없음; 미채택은 구현 완료가 아님 |
| TOOL-03 잔여 / 계획 | agent 위임·인계 행동, go-arch-lint/Staticcheck/govulncheck 후보, fuzz 공백, verifier 도구 평가 | 기존 검사와의 중복·고정 버전·라이선스·실제 반례·비용 기록. 후보별로 분리, 새 도구 설치만으로 완료 처리 금지 | 기존 TOOL-03.a~e; 후보당 별도 branch/commit |
| MCP-01 잔여 / 계획 | freshness 확인과 실제 outage fallback, 연결 도구 권한 범위 기록 | graph miss·미등록 오류와 transport outage를 구분. 테스트를 위해 사용 중인 공유 MCP를 중단하지 않음 | 격리 가능한 검증 환경; `codex/tool-mcp01-<date>-evidence` |

GF02/03은 결과가 불충분하면 Graphify 채택을 보류하고 제품 정의 작업을 계속할 수 있다.
Graphify는 제품의 민감정보 추적·집행 엔진이 아니다.

## B. 제품 정의·아키텍처 결정

| ID / 상태 | 세부 작업과 산출물 | 완료 기준 / 검증 | 선행 / Git 단위 |
|---|---|---|---|
| P02-a / 제안 작성 완료 | [첫 사용자·정상/금지 시나리오](PRODUCT_SCOPE_PROPOSAL.md): 지정 합성 파일을 로컬 AI로 분석하고 사용자에게 표시 | 로컬/cloud/분리 실행 비교, source 전후 대조 및 helper와 실제 AI 사용성 분리. G1 승인; 새 runtime 미구현 | `codex/design-p02-20261025-product` |
| P02-b / 제안 작성 완료 | [민감정보·자원·근거·권한·집행 상태 용어표](PRODUCT_SCOPE_PROPOSAL.md) | 사용자 지정과 자동 내용 탐지 구분, DB 연결과 결과 전달 구분. 새로운 보장 미구현 | P02-a와 같은 문서 카드 |
| P02-c / 제안 작성 완료 | [위험 명령의 operation/resource/destination 예시](PRODUCT_SCOPE_PROPOSAL.md) | 조회/삭제/전송/credential 권한 구분. argv 관찰을 사전 차단으로 설명하지 않음 | P02-a/b와 같은 문서 카드 |
| P03 / 조사·계획 완료 | [고정 후보·재사용 비교·P06 실험안](REUSE_COMPARISON_PLAN.md) | ActPlane/OpenShell commit·manifest·license 확인, launch/attach·effect 차이 명시. 후보 실행 not_tested, G2 엔진 미선정 | P02 제안; `codex/design-p03-20261025-reuse` |
| P04-a / 설계 초안 완료 | [신뢰 경계·상태·identity 계약](EVIDENCE_AND_DASHBOARD_PROPOSAL.md) | process 사실과 run-wide 제한 분리, 기존 연결/alias/IPC/초기 입력/실패 수용 조건. G1 승인; 새 runtime 미구현 | `codex/design-p04-20261025-evidence-ui` |
| P04-b / 수용 초안 완료 | [T01~T15 fixture·관측·판정 계약](EVIDENCE_AND_DASHBOARD_PROPOSAL.md) | supervisor 종료와 no-loss/장애 PASS 구분 포함. 새 runtime 테스트는 모두 not_tested | 같은 P04 문서 카드 |
| P04-ui / 정보 설계 초안 완료 | [Runs→타임라인→근거 상세·가상 화면](EVIDENCE_AND_DASHBOARD_PROPOSAL.md) | incomplete/unknown/deny와 실제 block 구분, 저장/query·로컬 권한 요구. UI 미구현 | 같은 P04 문서 카드 |
| G1 / 승인 | [결정 자료와 적용 전 규범 patch](G1_DECISION_PACKET.md) | 권고: 로컬 사용자·파일 하나·run-wide 후보·기존 연결 검증·읽기 UI 순서 조정. DB broker 제외, 엔진은 P06 후 G2 선택 | 2026-10-25 구체 patch 제시 뒤 사용자 순차 진행 요청으로 승인 |
| P05 / 규범 반영 | AGENTS·ARCHITECTURE·SECURITY_MODEL·THREAT_MODEL·ROADMAP·Phase prompt·허용 파일을 정합화 | 기존/새 의미 차이, 호환성, 보장/비보장, 이전 완료 증거 적용 범위 명시. 승인 patch 적용 및 독립 검토 | G1 및 구체 규범 변경 확인; `codex/design-p05-<date>-normative` |
| P06 / 실험 카드 준비 | 고정된 arm64 환경에서 후보별 동일 fixture 비교 | compile/verifier/load/attach/runtime 분리; 실제 errno·수신 효과·allow 대조군·cleanup. 언어 표현 가능성과 실제 지원 구분 | [승인 범위의 실험 카드](P06_EXPERIMENT_CARD.md); `codex/experiment-p06-<date>-comparison` |
| G2 / 미도달 | 재사용·조합·자체 구현 최종 선택 | P06 evidence, 실패/미지원 보존, 독립 correctness/security 검토 | P06; 결정 기록 커밋 |

현재 규범은 OS effect 기반 process label 모델이다. broker를 신뢰된 source로
추가하거나 실행 전체 집행 상태를 도입하려면 의미 변경이 필요하다. 계획 수립이나
Graphify 평가 승인을 그 구체 변경의 채택으로 간주하지 않는다.

## C. 최소 기능과 대시보드

| ID / 상태 | 세부 작업과 산출물 | 완료 기준 / 검증 | 선행 / Git 단위 |
|---|---|---|---|
| P07-a / 미착수 | 선택한 source 1개, identity/state, 정책·정규화 Event 계약 구현 | 단위/replay 검증, 중복·순서·재사용 identity·오류 처리, 기존 arm64 의미 회귀 없음 | G2와 실제 Phase/card; `codex/impl-p07a-<date>-model` |
| P07-b / 미착수 | 선택한 sink 1개와 pre-operation 집행 연결 | 정상 작업 성공, source 접근 뒤 금지 effect 부재, 경쟁·오류·미지원 시작 거부. byte-level 보장 주장 금지 | P07-a; `codex/impl-p07b-<date>-enforcement` |
| P07-c / 미착수 | CLI 실행·진단·예시·지원표·사용자 설명 | source SHA와 guest 일치, portable+Linux acceptance, 독립 리뷰, 자원 정리 | P07-b; `codex/impl-p07c-<date>-acceptance` |
| P08a / 계획 | Event/Decision/집행 결과 저장 schema, 조회·페이지 처리, 보존·삭제·권한·용량 상한 계약 및 승인 후 구현 | 손실 상태 저장/조회 보존, 부분 기록·disk full·삭제·재시작·호환성. payload/전체 argv/환경변수 기본 비수집 | P07 계약과 P04-ui; `codex/dashboard-p08a-<date>-storage` |
| P08b / 계획 | Runs → 실행 타임라인 → 근거 상세, 자원·목적지·정책별 필터 | 화면↔원시 근거 일치, incomplete/미지원/집행 실패 표시, localhost 접근 경계, 대표 run end-to-end | P08a 및 Phase 순서 변경 승인; `codex/dashboard-p08b-<date>-viewer` |
| P09 / 조건부 | 원격 DB source 하나의 adapter/broker 설계, 필요 시 구현 | credential 보호와 DB 작업 권한 분리, 직접 접속 우회·첫 byte 전 상태 설정·부분 실패·재시도·교차 run·결과 재사용 검증 | DB 필요성·신뢰 경계 변경 별도 결정, P07 송신 경계; `codex/source-p09-<date>-db` |

G1/P05의 제한된 UI 순서 예외를 적용한다. 검증된 로컬 slice, 저장/query/권한
계약, 상세 Phase 9 prompt와 독립 검토를 충족하면 Kubernetes·Feedback 이전에
읽기 UI를 구현할 수 있다. 현재 이 선행조건은 모두 충족된 상태가 아니다.

대시보드는 다음 질문에 답하도록 설계한다.

| 화면 | 사용자 질문 | 반드시 구분할 필드 |
|---|---|---|
| 실행 목록 | 어느 실행을 확인해야 하는가? | run ID, 시작/종료, 요청/활성 mode, 사건 수, incomplete, 집행 health |
| 실행 상세 | 어떤 자원 접근 이후 무슨 일이 발생했는가? | 시간/순서 근거, process identity, resource 별칭, label/state 변화, 연결 목적지 |
| 판정·근거 상세 | 왜 거부됐으며 실제로 막혔는가? | source evidence, policy rule/version, verdict, enforcement result, syscall 결과, 관측 한계 |
| 수집 상태 | 기록을 어느 범위까지 믿을 수 있는가? | drop/loss reason, 수집 구간, 미지원 경로, 저장 실패, source/build/kernel/arch |

Graphify의 코드 관계 그림은 이 실행 증거 화면을 대체하지 않는다.

## D. 기존 잔여 작업과 후속 지원

| 기존 ID | 세부 작업 / 완료 기준 | 착수 조건 / Git 단위 |
|---|---|---|
| CI-01 / verified | [5777fcb의 Ubuntu/macOS 원격 make check·make race 성공](CI_PORTABLE_EVIDENCE.md) | 원격 run 40201268334; portable 범위, Linux/BPF runtime은 CI-02 |
| CI-02 | 격리된 Linux BPF runner, verifier/attach/runtime와 정리 증거 | 전용 runner 권한·환경 결정; `codex/ci-ci02-<date>-bpf` |
| REL-01 | binary/BPF/source 대응, checksum·SBOM·license·재현 빌드 | 검증된 제품 commit과 지원 환경; `codex/rel-rel01-<date>-artifacts` |
| F5 | macOS에서 Linux VM 실행·진단·소스 동기화 UX | 기존 플랫폼 순서 유지, native Endpoint Security 제외; `codex/f5-<task>-vm` |
| ARCH-01 | amd64 ABI/object/verifier/attach/runtime 및 arm64 회귀 | arm64 우선 유지, 승인된 amd64 환경; `codex/arch-arch01-<date>-amd64` |
| F6 | 다중 label·file/pipe/IPC 전파 | P05 재설계 결과와 중복 제거 후 실제 Phase 상세화; `codex/f6-<task>-flow` |
| F7 | Kubernetes attribution·배포·권한 | 로컬 IFC 완료, 필요한 사용자 시나리오; `codex/f7-<task>-kubernetes` |
| F8 | 읽기 중심 설명·피드백 인터페이스 | 선행 Phase와 권한·데이터 계약; `codex/f8-<task>-feedback` |
| F9-DESIGN/PERSIST/F9 | 기존 dashboard 카드와 P04-ui/P08 대응 정리 | P05에서 순서·중복·허용 범위 결정. 두 계획을 각각 구현하지 않음 |
| DOC-01 | 설치·실행·진단·지원표·감사/차단 차이·실제 예시·프로젝트 소개와 그림 | 해당 검증 증거에 맞춰 지속 갱신; 기능별 docs commit |
| REL-02/03 | 설치·업데이트·제거·복구, 제한된 시범 사용·피드백 | 재현 산출물, 배포/공개 정책 결정; 각각 별도 rel branch |
| F10 | payload/LLM sink metadata 필요성 재평가 | 구체 사용 사례와 별도 승인. 자동 내용 탐지/LLM 판단으로 핵심 모델 대체 금지 |

P10은 CI/릴리스의 상위 묶음이다. 기존 CI/REL 카드를 그대로 연결하며 완료 건수를
이중 계산하지 않는다. F6/P07, F9/P08의 중복도 P05에서 해소한다.

## 팀·모델·동시성·도구

- 기본은 main 단독 순차 수행. 병렬 작업이 명시된 경우 조사/설계/보안 검토를
  독립 카드로 배정하고 기본 main 포함 4명, 최대 6명, 재귀 위임 없음.
- writer 기본 1명. writer별 worktree/branch/소유 파일을 명시하고 공통 schema를
  여러 writer가 동시에 바꾸지 않는다. 공유 Linux guest는 검증자 1명이 소유한다.
- 구현 작성자와 독립 reviewer/security reviewer를 분리한다. 모델은 실제 호출
  가능한 모델을 사용하고 모델 ID만으로 가격·품질·절감률을 주장하지 않는다.
  좁은 사실 추출과 보안 판단을 같은 비용 수준으로 일괄 실행하지 않는다.
- 필요한 skill은 preflight, source-driven-development, security-semantics-review,
  code-review-and-quality, documentation-and-adrs, docs-contract-check다.
- 코드 발견은 기존 codebase-memory MCP 우선, 필요한 심볼/원문만 조회한다.
  인덱스 상태를 source freshness 증거로 대체하지 않는다. Graphify 채택 전
  무조건적인 graph-first 지침·전역 skill·자동 hook을 추가하지 않는다.
- 인계는 task/status/HEAD/files/checks/evidence/blocker/next 요약과 원문 경로로
  제공한다. 전체 대화·전체 저장소·긴 로그를 모든 agent에게 복사하지 않는다.

## Git·검증·완료 판정

1. 작업마다 clean main 동기화와 기준 SHA 기록, `.worktrees/<task-id>` 및 위 표의
   고유 branch를 만든다. 기존 dirty/미통합 worktree는 보존한다.
2. 한 카드의 관련 경로만 stage하고 staged diff 확인 후 conventional commit을
   만든다. 큰 카드는 표에 적은 하위 단위로 나누되 의존 변경을 먼저 검토한다.
3. 문서: 링크/상태/규범 정합성·diff 검사. Go: gofmt/vet/build 및 영향 테스트.
   BPF: pinned 도구와 지정 guest의 verifier/load/attach/실제 효과 검증을 추가한다.
4. 카드별 관련 검증은 수행하고 전체 suite는 합의된 통합 batch의 정확한 HEAD에서
   한 번 수행한다. 실패 수정 뒤 영향받은 검증은 재실행한다. 전체 미래 작업이
   끝날 때까지 선행 Phase 검증을 미루지 않는다.
5. integration에서 독립 검토·필수 gate 통과 후 main fast-forward. 해당 branch/batch의
   명시 요청 범위에서 push한다. force push/무단 rebase/자동 원격 merge 없음.
6. 상태는 `planned / proposed / implemented / verified / not_tested / inconclusive`
   로 구분한다. 계획 문서 수·설치 성공·소스 검색 성공을 제품 완성률로 환산하지 않는다.

G1 구체 변경안은 사용자 후속 진행 요청으로 승인했고 P05에서 적용했다.
다음은 [P06 실험](P06_EXPERIMENT_CARD.md)이다. run-wide·기존 연결 송신·격리의
설계 요구와 현재 구현 지원은 구별한다. DB broker는 첫 범위에서 제외하며,
외부 엔진 선정은 P06 증거와 G2 결정 이후다.
