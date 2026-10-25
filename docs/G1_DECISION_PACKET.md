# G1 — 구현 전 제품 방향 결정 자료

2026-10-25. 상태: **검토용 변경안 준비 / 승인 전 / patch 미적용**.

## 권고안

1. 첫 사용자는 로컬 개발자, 첫 source는 사용자가 지정한 합성 파일 하나로 둔다.
   같은 통제 실행 안의 로컬 AI가 분석하고 본인에게 결과를 표시한다.
2. 현재 process 접근 사실을 유지하면서 별도의 run-wide 보수적 제한 상태를
   설계·비교 실험 후보로 허용한다. 새 연결뿐 아니라 기존 연결 송신도 수용 조건에 넣는다.
3. 격리 제공자의 책임과 AgentTaint의 상태/정책/집행 책임을 분리한다. ActPlane
   엔진과 OpenShell 격리의 재사용 가능성을 고정 시나리오로 비교하며 엔진은 G2에서 고른다.
4. 검증된 로컬 slice와 저장/조회·권한 계약이 준비되면 읽기 전용 대시보드를
   Kubernetes·Feedback보다 먼저 구현할 수 있도록 순서를 조정한다.
5. DB broker, 일반 민감 내용 자동 탐지, payload/LLM 분류, amd64/Kubernetes
   확장은 이번 첫 범위에 포함하지 않는다.

권고안의 이득은 같은 실행의 부모/형제와 기존 연결 공백을 명시적으로 다루고,
사용자가 근거와 한계를 화면에서 확인할 수 있게 만드는 것이다. 비용은 run-wide
과도 제한, 격리 제공자 의존성, 상태·송신 동기화와 장애 수용 조건의 증가다.
이 비용은 아직 측정하지 않았으며 실험 실패 시 보장을 좁히는 재결정이 필요하다.

## 검토할 산출물

- [P02 제품·민감정보·위험 명령·범위](PRODUCT_SCOPE_PROPOSAL.md)
- [P03 고정 후보와 비교 실험](REUSE_COMPARISON_PLAN.md)
- [P04 위협·T01~T15·증거 저장·대시보드 설계](EVIDENCE_AND_DASHBOARD_PROPOSAL.md)
- [적용 전 규범 변경 patch](proposals/g1-normative.patch)
- [전체 실행 큐](EXECUTION_QUEUE.md)

## Patch가 바꾸는 것

| 파일 | 구체 변경 |
|---|---|
| AGENTS.md | 기존 process 의미를 유지하면서 후속 run-scoped 실험 계약 추가. 엔진 변경은 G2 이후. Stop Conditions에도 제한된 UI 순서 예외 연결 |
| SECURITY_MODEL.md | 새 profile의 보수적 상태·송신 순서·범위·실패 요구를 구현 전 설계 계약으로 추가 |
| THREAT_MODEL.md | 격리 제공자 책임, 초기 입력/alias/IPC/기존 연결/supervisor 실패 수용 조건 |
| ARCHITECTURE.md | 상태/정책 소유·실험 경계와 읽기 화면 선행조건. 새 제품 패키지는 아직 허용하지 않음 |
| ROADMAP.md | 로컬 읽기 화면의 제한된 순서 예외. Linux 우선·로컬 IFC 전 Kubernetes 금지는 유지 |
| prompts/09-local-web-dashboard.md | Phase 0~8 일괄 완료 대신 검증된 slice·저장/query/권한·상세 prompt를 UI 선행조건으로 명시 |
| IMPLEMENTATION_MASTER_PLAN.md | 활성 F9-DESIGN/F9 카드의 F8·Phase 0~8 선행조건 5곳을 같은 예외에 맞춤. 기존 완료 상태는 변경하지 않음 |

현재 파일들은 변경하지 않았다. patch 자체만 문서 artifact로 보관한다.
`git apply --check docs/proposals/g1-normative.patch`로 적용 가능성만 검사한다.
이 검사는 정책/커널 correctness나 사용자 승인을 대신하지 않는다.

승인하면 이 patch를 P05에서 적용·정합성 검토한 뒤 P06의 구체 실험 카드를
확정한다. 후보 설치·guest 특권 접근은 지정 카드의 실제 권한 범위에서만 수행한다.
외부 엔진 채택·제품 구현·보장 확대는 P06 증거와 G2 결정 이후다.

## 승인이 필요한 이유

[AGENTS.md](../AGENTS.md)의 “문서는 규범이다” 절은 “그 변경을 적용하기
**전에 멈추고** 사람의 확인을 받는다”고 정한다. 이번 patch의 실행 제한 단위와
UI 순서는 현행 규범을 바꾸므로 일반적인 작업 재개 요청만으로 채택됐다고
간주하지 않았다. 필요한 변경을 모두 검토 가능한 patch로 준비했다.

검토에 사용한 [docs-contract-check skill](../.agents/skills/agenttaint-docs-contract-check/SKILL.md)도
“Explain the conflict and propose the smallest change; do not apply it without user confirmation”을
요구한다. 설계 문서 작성·도구 비교는 진행했고, 실제 규범 적용만 확인 전 단계에 둔다.

## 남은 작업

G1 미채택 → P05 규범 적용 → P06 지정 환경 비교 → G2 엔진 선택 → P07 최소 기능
→ P08 저장·대시보드 → CI/배포 순서다. TOOL-03의 다른 후보, 실제 모델 비용 A/B,
원격 CI/배포는 별도 잔여 항목이며 이번 문서 완료로 성공 처리하지 않는다.

byte-level taint tracking을 주장하지 않는다. post-event detection을 prevention으로
부르지 않는다. 이번 변경안 준비로 새 감지·차단 기능이 생기지 않는다.
