# P06 — Linux arm64 제한 실험 카드

2026-10-25. G1 승인 후 P05 규범 적용에 따른 실험이며 제품 구현 카드는 아니다.
작성자는 main agent 한 명이고 공유 guest `ebpf-lab`도 같은 검증자만 사용한다.
제품 기준은 P05 통합 HEAD다. 실제 실행 source는 그 기준에 helper만 추가한
P06 commit의 archive이며 두 SHA와 archive hash를 manifest에 구분해 기록한다.

## 범위와 허용 파일

- 호스트: `test/fixtures/p06-flow-probe/`의 표준 라이브러리 Go helper·단위 검사·README,
  `docs/P06_*`, `docs/evidence/p06-*`, 실행 큐 상태만 변경한다.
- guest: 테스트 전용 `/tmp/agenttaint-p06-*` source/build/evidence, synthetic source,
  loopback receiver와 해당 실험이 만든 process/cgroup만 사용한다.
- 제품 `cmd/`, `internal/`, BPF/생성 object, 정책 schema 및 일반 집행 의미는 바꾸지 않는다.
- 기존 supervisor/loader는 root 권한을 유지하며 target만 `--run-as 65534:65534`로 실행한다.
  source는 테스트용 공개 합성 데이터다. 홈·실제 credential·SSH agent를 제공하지 않는다.
- guest에 전역 도구나 daemon을 설치하지 않는다. 후보 다운로드/build는 전용 경로에
  고정 commit으로만 수행한다. 공유 guest의 다른 process나 BPF program을 제거하지 않는다.

## 최초 실험 묶음

| ID | 실행 | 확인할 사실 |
|---|---|---|
| E0 | kernel/arch/Go/clang/BPF LSM/cgroup, source SHA·archive hash, build·load 준비 | 환경·컴파일·load·attach를 개별 기록. 설치 성공을 runtime 지원으로 취급하지 않음 |
| E1 | source 접근 없이 동일 receiver로 새 TCP 연결·marker 전달 | allow 대조군 실제 수신 및 ACK |
| E2 | 같은 process가 source를 읽고 동일 receiver에 새 연결 | syscall 오류·수신 부재·audit를 따로 기록; 기존 모델의 차단 대조 |
| E3 | 자식이 source를 읽고 종료한 뒤 부모가 새 연결·전송 | 자식 접근 근거와 부모 효과 분리. 현재 process 모델의 공백 여부 |
| E4 | TCP 연결·첫 marker ACK 후 source를 읽고 같은 FD로 두 번째 marker 전송 | 기존 연결 송신 공백. 새 연결 실패로 대체하지 않음 |

실험별 별도 run/audit/receiver 로그를 보존한다. source read/child wait/TCP ACK로
순서를 고정한다. timeout은 실험 상한이지 차단의 단독 증거가 아니다. receiver는
scope 밖 비특권 helper이며 loopback만 listen한다. E1/E2의 목적지는 동일하게 유지한다.
정책은 실험 디렉터리의 synthetic source 절대 경로와 `SECRET`, receiver의
`127.0.0.1/32` 및 고정 시험 port에 대한 `process_has: SECRET` deny rule을
명시한다. loopback을 자동으로 외부 목적지로 분류한다고 가정하지 않는다.
정책 원문·SHA256과 실제 bound 주소를 기록하고 E1~E4에서 동일하게 유지한다.
각 실험은 scope 밖 대조 또는 monitor mode와 block mode를 구분한다.
제품 acceptance와 helper 정확성 검사는 별개이며 기존 acceptance의 미지원/skip을 숨기지 않는다.

## 후보와 후속 범위

- ActPlane: `eunomia-bpf/ActPlane@bb89c77bcb29cfa81ba1fb212b5634c353c7352b`.
- OpenShell: `NVIDIA/OpenShell@c4688aa8715ea75d6c39fe468361dbbc16c691fa`.
- 각 후보는 고정 source의 build prerequisites/lock/toolchain·지원 hook·launch 방식부터
  확인한다. 현재 guest에서 실험 가능한 것만 E1~E4에 연결하며 불가 사유를 보존한다.
- 전체 T01~T15, race/fault/containment/supervisor 종료, 실제 로컬 모델 사용성은 이
  최초 묶음 뒤의 P06 잔여다. E1~E4만으로 G2 채택이나 strict profile 검증 완료를 선언하지 않는다.

## 수용·정리·통합

실제 반환값·marker 수신·ACK·audit completeness·enforcement health를 각각 기록한다.
예상된 baseline gap은 재설계 근거로 남기며 새 profile의 PASS로 바꾸지 않는다.
정상 수용에서 loss는 실패다. 의도적 장애 검사의 incomplete 전달 성공은 무손실 성공이 아니다.

helper gofmt/vet/build/단위 검사 후 exact commit을 guest에 전달한다. 기존 Linux
acceptance는 동일 archive에서 한 번 수행하고 필요 수정이 있을 때만 영향 검사를 반복한다.
receiver/target의 종료와 테스트 소유 cgroup·BPF attachment 정리를 확인한다. evidence는
source SHA, archive/binary hash, 명령, exit status, raw log 경로와 hash, 시나리오 판정을 포함한다.
독립 correctness/security 검토 뒤 commit·integration·main 반영한다. runtime 장애를
문서 수정으로 PASS 처리하지 않으며 제품 변경은 별도 P07/G2 계약을 따른다.

Byte-level taint tracking을 주장하지 않는다. post-event detection을 prevention으로
부르지 않는다. 이 helper와 실험 카드는 제품 보호 기능을 추가하지 않는다.
