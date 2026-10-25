# P03 — 재사용·조합·자체 구현 비교 계획

2026-10-25. 상태: **문서 조사·비교 계획 완료, 실행 실험 not_tested, 엔진 미선정**.
입력: [P02 제품 범위 제안](PRODUCT_SCOPE_PROPOSAL.md).
현재 Go+C 엔진을 교체하거나 외부 런타임을 설치하지 않았다.

## 후보와 고정 근거

| 후보 | 고정 기준 / license | 확인한 범위 |
|---|---|---|
| 현 AgentTaint | source `3b8c9ea8442e181df72d01e83d74bb0af0b0ed51`; 기존 저장소 배포/license 결정 유지 | 현재 규범·기존 arm64 acceptance를 회귀 기준으로 사용. 새 profile의 증거가 아님 |
| ActPlane | `bb89c77bcb29cfa81ba1fb212b5634c353c7352b`; CLI/runtime manifest 0.1.8; MIT | pinned README, support-matrix, root/CLI/runtime Cargo.toml, LICENSE 읽기 |
| OpenShell | `c4688aa8715ea75d6c39fe468361dbbc16c691fa`; Apache-2.0 | pinned README, security/best-practices, root Cargo.toml, LICENSE 읽기. workspace version 0.0.0을 배포 release로 해석하지 않음 |

각 후보의 runtime build와 transitive dependency/lockfile 해석은 미실행이다.
P06 실행 전에 source archive, lockfile, 실제 resolved version, build toolchain,
binary checksum, guest/kernel/arch를 기록한다. commit pin만으로 재현 설치·공급망
검증이 끝났다고 하지 않는다. AgentTaint의 pinned Go/cilium 버전은 변경하지 않는다.

## 재사용 판단

| 선택지 | 재사용하려는 부분 | 직접 확인하거나 만들어야 하는 부분 | 현재 판단 |
|---|---|---|---|
| 자체 엔진 유지 | Go policy/taint, 현재 BPF projection, runner·audit·기존 테스트 | run-wide 동기 상태, 기존 연결 송신 gate, 필요한 격리 계약과 증거 화면 | 회귀 기준과 비교 후보. 구현 비용은 아직 미측정 |
| ActPlane 엔진 활용 | source/label/흐름 정책과 지원 pre-op hook | P02 run 경계·송신 시나리오의 실제 coverage, 단일 정책 권위, 이벤트 의미 변환, 버전별 지원 | 가장 직접적인 엔진 비교 후보. 즉시 대체 확정 금지 |
| OpenShell 격리와 선택한 IFC 엔진 조합 | 실행 환경·filesystem/process/network 경계와 credential 경로 | source 이력에 따른 제한의 동기 적용, 기존 연결 처리, scope identity 결합, 장애 생애 | 격리 제공자 후보. source 기반 IFC가 이미 된다고 간주하지 않음 |

ActPlane 지원표는 pre-op block과 post-event kill/notify를 구분하고, argv-token
규칙의 pre-op block을 지원하지 않는다고 설명한다. runtime delta는 시작 때
예약한 hook/matcher 범위에 제약이 있다. 문서의 flow 지원 설명을 P02의
run-wide·기존 연결·모든 송신 보장으로 확대하지 않는다.

OpenShell 보안 문서는 namespace와 proxy, filesystem/process 제한 및 credential
경계를 설명한다. 이는 source 접근 후 상태가 바뀌는 IFC가 구현됐다는 근거가
아니다. 허용 endpoint가 데이터를 받을 수 있다는 한계와 telemetry 기본 동작도
실험 계획에 반영한다. 이 문서 작성에서는 gateway/skill/agent hook을 설치하지 않았다.

## 계약 변환에서 확인할 차이

1. AgentTaint `deny > audit > allow`와 ActPlane의 effect 우선순위는 다른 계약이다.
   문자열만 치환하지 않는다. source·verdict·실제 action·시점·실패 동작을 표로 맞추고,
   의미 보존이 안 되면 adapter가 해당 정책을 거부해야 한다.
2. ActPlane post-hoc attach는 이전 이력을 복원하지 않는다. 첫 실험은 명시 launch
   경로부터 비교한다. launch/attach/delta 결과를 섞지 않는다.
3. OpenShell의 연결/요청 정책 갱신은 AgentTaint source 상태와 원자적으로 연결됐다는
   증거가 아니다. 기존 keep-alive/queued send와 update acknowledgment를 별도 검사한다.
4. 외부 Rust 엔진 채택은 Go+C 내부 엔진의 단순 helper 추가가 아니다. 책임·프로세스·
   배포 의존성·단일 정책 권위가 달라지므로 G2 이후 AGENTS/아키텍처 변경이 필요하다.
5. provider credential을 숨기는 것과 그 권한으로 DB를 조회/수정하는 것을 구별한다.
   첫 파일 slice에 DB broker를 부수적으로 추가하지 않는다.

## P06 실험 카드 초안

G1/P05에서 범위와 권한을 확인한 뒤 지정 Linux arm64 guest 하나를 단일 검증자가
사용한다. 실제 customer/credential 대신 합성 source와 로컬 시험 수신기를 쓴다.
외부 서비스 계정·remote mutation·privileged CI·VM 재생성은 실험의 기본값이 아니다.

| 순서 | 입력·대조군 | 성공 조건 / 실패 해석 |
|---|---|---|
| E0 환경 | pinned source/build, doctor/capability, target 권한, clean fixture | 지원 hook/격리·정리 조건 확인. 불충족이면 unsupported, 실행 성공으로 바꾸지 않음 |
| E1 정상 | source 접근 전 승인된 시험 수신기로 새 연결/송신 | receiver가 예상 marker 수신. 처음부터 전부 막힌 경우 IFC 효용 비교 불가 |
| E2 직접 | source 접근 뒤 같은 process의 같은 수신기 송신 | 지원 gate의 거부 결과와 receiver 비수신, allow 대조군을 함께 확인 |
| E3 계보 | child source 접근, parent/sibling 송신 | 실제 source process 기록 보존, 실행 제한 사유 구분, 금지 송신 부재 |
| E4 기존 연결 | source 접근 전에 연 연결에서 접근 뒤 송신 | connect만 거부하는 후보는 이 조건을 통과하지 못함 |
| E5 경계 | IPv6/UDP/외부 IPC/FD 전달/alias/공유 파일 중 환경에서 접근 가능한 경로 | 필요한 경로를 제어하거나 profile launch 전에 제거·거부함을 증명. 미관측 경로를 무손실과 혼동하지 않음 |
| E6 경쟁·장애 | source/send 경쟁, state 갱신 실패, supervisor 종료, audit loss/저장 실패 | 상태 전이·집행 건강·incomplete 구분, 지원한 fail-closed 효과와 cleanup. sleep으로 race를 숨기지 않음 |
| E7 사용자 효용 | 실제 로컬 AI가 합성 파일 분석을 완료, 동일 사용자 출력 | helper의 보안 성공과 별개로 실제 정상 작업 완료·불필요 제한 기록 |

각 행은 `pass / fail / unsupported / not_tested / inconclusive` 중 하나로 기록한다.
한 후보가 E2만 통과하면 제한된 기존 MVP에 대응하는 증거일 뿐 P02 전체 통과가 아니다.
기존 arm64 acceptance 결과를 E3~E7에 복사하지 않는다.

첫 실험은 E0~E4를 우선하여 명백한 capability gap을 찾는다. 실패 원인이 확정되면
그 후보의 무의미한 반복을 멈추고 E5~E7은 해당 profile이 성립하는 후보만 진행한다.
한꺼번에 전체 OSS 제품군을 설치하지 않는다.

## G2 판단 기준

필수 보장·정상 사용자 효용·권한/정리 계약을 통과한 후보끼리만 유지 비용과
통합 복잡도를 비교한다. LOC, GitHub 인기, README 표현, 언어 선호만으로 결정하지
않는다. 필요한 경계를 만족하는 후보가 없으면 첫 보장을 좁히거나 해당 기능을
보류하는 변경안을 제시한다. 검증 실패를 구현 완료로 처리하지 않는다.

현재 권고는 **현 AgentTaint를 회귀 기준으로 보존하면서 ActPlane 엔진과
OpenShell 격리의 제한된 조합 가능성을 비교**하는 것이다. 선택한 IFC 엔진은
하나의 정책 권위를 가져야 하며 두 엔진을 무조건 중첩하는 구조를 기본안으로 삼지 않는다.

## 공식 근거

- [ActPlane support matrix](https://github.com/eunomia-bpf/ActPlane/blob/bb89c77bcb29cfa81ba1fb212b5634c353c7352b/docs/support-matrix.md)
- [ActPlane README](https://github.com/eunomia-bpf/ActPlane/blob/bb89c77bcb29cfa81ba1fb212b5634c353c7352b/README.md), [CLI manifest](https://github.com/eunomia-bpf/ActPlane/blob/bb89c77bcb29cfa81ba1fb212b5634c353c7352b/crates/actplane-cli/Cargo.toml), [runtime manifest](https://github.com/eunomia-bpf/ActPlane/blob/bb89c77bcb29cfa81ba1fb212b5634c353c7352b/crates/actplane-runtime/Cargo.toml), [LICENSE](https://github.com/eunomia-bpf/ActPlane/blob/bb89c77bcb29cfa81ba1fb212b5634c353c7352b/LICENSE)
- [OpenShell security controls](https://github.com/NVIDIA/OpenShell/blob/c4688aa8715ea75d6c39fe468361dbbc16c691fa/docs/security/best-practices.mdx)
- [OpenShell README](https://github.com/NVIDIA/OpenShell/blob/c4688aa8715ea75d6c39fe468361dbbc16c691fa/README.md), [workspace manifest](https://github.com/NVIDIA/OpenShell/blob/c4688aa8715ea75d6c39fe468361dbbc16c691fa/Cargo.toml), [LICENSE](https://github.com/NVIDIA/OpenShell/blob/c4688aa8715ea75d6c39fe468361dbbc16c691fa/LICENSE)

문서상 기능, 소스 manifest, 실제 실행 증거를 구별한다. 이번 카드는 앞의 두
근거만 확인했으며 후보 설치·실행·네트워크 차단·arm64 지원을 검증하지 않았다.
