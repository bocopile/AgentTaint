# AgentTaint

AI 에이전트와 그 하위 프로세스의 민감정보 접근·전파를 OS 수준(eBPF)에서
추적하고, 위험한 정보 흐름을 정책에 따라 감사·차단하는 runtime security
system을 목표로 한다. **현재 Linux runtime은 arm64만 지원한다. 구현에는 eBPF
관측, Phase 3 audit-only taint/policy 평가, 제한된 Phase 4 BPF-LSM 사전 집행이
포함되며 검증 범위는 지정 guest와 fixture에 한정된다.**

> **Status: 🚧 제한된 Linux arm64 MVP — observer·taint·policy와 일부 BPF-LSM block 시나리오를 aarch64 guest에서 검증했다. S25 mmap path-capture 및 concurrent-run no-loss fixtures를 포함한 P4-04 acceptance는 Linux 7.0.0-28에서 통과했다. amd64 지원은 후속 작업이며, 다른 kernel의 runtime 동작은 검증되지 않았다.**
> 진행 단계는 [docs/ROADMAP.md](./docs/ROADMAP.md), AI 코딩 에이전트가
> 따르는 규칙은 [AGENTS.md](./AGENTS.md), Phase별 작업 지시는
> [prompts/](./prompts/) 참고. 스택은 **Go + C/CO-RE(cilium/ebpf)**로
> 확정.

## 현재 사용할 수 있는 것

Linux observer는 프로세스 lifecycle, 파일 open, IPv4 connect를 schema v1
JSONL로 기록한다. Go reference/audit taint engine은 성공 완료된 source
`FileOpen`에서 process-level SECRET label을 부여하고 fork 시점 자식에 복사한다.
block mode의 kernel projection은 별도 집행 상태이며 source-open 시도만으로도
후속 sink decision에 상태를 반영할 수 있다. Policy snapshot은 명시적 IPv4
CIDR·port 규칙을 평가한다. Phase 3/reference의 `deny`는
verdict만 뜻한다. 명시적인 `block` mode만 검증된 policy subset을 BPF-LSM
pre-operation hook에서 집행한다. 검증 범위는 직접 file open과 새 IPv4 connect에
한정되며 기존 socket 송신, UDP `sendto`, IPv6 및 다른 미지원 경로를 보장하지 않는다.

민감 source 경로는 사용자가 YAML policy에 지정해야 한다. AgentTaint은 파일
본문이나 프롬프트를 검사해 민감 값을 찾지 않는다. 향후 Source Discovery는
사용자가 고른 root의 path/metadata 후보를 제안하는 별도 정책 작성 보조로
설계됐으며 아직 구현되지 않았다. ProcessExec 관측은 실행 파일 경로를 기록하고
argv를 수집하지 않으므로 명령 인자의 의미를 판별하지 않는다.

Go **1.26 이상**에서 빌드한다. 환경 진단과 일반 커맨드 실행은 macOS/Linux를
대상으로 한다. 현재 Linux runtime은 arm64 전용이다. 실제 차단 검증은
BPF-LSM이 활성화된 Linux `7.0.0-28-generic` aarch64 guest의 지정 fixture에
한정된다. `run --observe`는
Linux와 eBPF 권한이 필요하며 root 실행 시
`--run-as UID:GID`로 target을 비특권 실행해야 한다. `run --observe`는
monitor/audit alias다. `notify`와 `kill`은 post-event 동작이며, `block`은
지원된 BPF-LSM hook에서만 pre-operation enforcement를 제공한다. 전체 OS 효과,
byte-level taint 또는 AI 전체 실행 차단은 보장하지 않는다.

### 커널·아키텍처 지원 근거

| 항목 | 의미와 현재 증거 |
|---|---|
| Linux 5.13+ | 프로젝트에 선언된 최소 커널 기준이다. 이 버전 전반의 observer/block 동작 검증을 뜻하지 않는다. |
| Linux 5.15+ | Phase 2 acceptance prompt가 지정한 Linux 테스트 profile이다. 5.15 전체에서 통과했다는 런타임 증거는 아니다. |
| Linux 7.0.0-28-generic, aarch64 | BPF-LSM이 활성화된 guest에서 지정된 observer·block fixture를 실행한 기록이 있다. 각 기록은 [구현 계획 12.10절](./docs/IMPLEMENTATION_MASTER_PLAN.md)의 scenario별 범위와 한계를 따른다. |
| Linux amd64 | 현재 runtime에서 지원하지 않는다. Go cross-build만 확인했으며 BPF verifier/load/attach/runtime은 검증되지 않았다. 별도 아키텍처 지원 작업으로 이연했다. |
| macOS arm64 | portable Go 개발·검사와 CLI 일부를 확인했다. macOS native observer/enforcement는 구현되지 않았다. |

커널 버전만으로 BPF 사용 가능 여부를 판정할 수 없다. BTF 접근, BPF 권한,
BPF-LSM 활성 상태와 요청한 각 hook의 load/attach 가능 여부가 필요하며,
`doctor`는 정적 환경 정보만 표시한다. 실제 `block` 요청은 필요한 집행 hook을
준비할 수 없으면 target을 실행하기 전에 실패한다.

## 설치

Go 1.26 이상이 있는 개발 체크아웃에서 다음 명령으로 사용자 계정에 설치한다.
기본 경로는 `~/.local/bin/agenttaint`이며 관리자 권한이 필요 없다.
`~/.local/bin`이 `PATH`에 없다면 해당 디렉터리를 `PATH`에 추가한다.

```sh
make install
```

사용자 prefix에 설치한 바이너리만 제거하려면 다음을 실행한다. 정책·감사 파일은
삭제하지 않는다.

```sh
make uninstall
```

시스템 전체 설치가 필요하면 먼저 사용자 권한으로 staging 디렉터리에 빌드한 뒤,
바이너리만 관리자 권한으로 복사한다. `DESTDIR`는 작업 트리 외부의 임시 경로를
사용해도 된다.

```sh
make install PREFIX=/usr/local DESTDIR=/tmp/agenttaint-stage
sudo install -m 0755 /tmp/agenttaint-stage/usr/local/bin/agenttaint /usr/local/bin/agenttaint
```

설치 후 `agenttaint doctor --json`으로 환경 정보를 확인한다. `make install`은
대상 경로의 기존 `agenttaint` 바이너리를 교체하며, `make uninstall`은 그 바이너리만
제거한다. 현재 머신용 CLI 바이너리만 빌드·복사하고 정책·감사 데이터를 건드리지 않는다.
Linux BPF 권한이나 BPF-LSM을
활성화하지 않고, 정책 파일·systemd 서비스·커널 설정도 설치하거나 변경하지
않는다. 정책 모드와 `block`은 Linux 및 해당 hook이 지원되는 환경에서만 쓸 수
있다.

```sh
go build -o ./agenttaint ./cmd/agenttaint
./agenttaint doctor --json
./agenttaint run -- /usr/bin/printf '%s\n' hello
./agenttaint run --observe --policy ./policies/example.yaml --audit-file ./audit.jsonl --run-as 65534:65534 -- /usr/bin/printf '%s\n' hello
```

정책 mode의 **명령행 문법 예시**는 다음과 같다. `/path/to/command`는 자리표시자다.
실제 동작을 검증하는 fixture나 보장된 실행 예가 아니다. 모든 mode는 `--policy`와
새 audit 파일을 지정해야 하며, `--run-as`는 명시적인 policy mode에서만 쓸 수 있다.

```sh
./agenttaint run --mode block --policy ./policies/example.yaml --audit-file ./block-audit.jsonl --run-as 65534:65534 -- /path/to/command
```

| Mode | 동작 |
|---|---|
| `monitor` (`--observe`) | 이벤트와 policy verdict를 audit한다. operation을 차단하지 않는다. |
| `notify` | policy 위반을 관측한 뒤 알린다. 사전 차단은 아니다. |
| `kill` | `deny` verdict를 기록한 뒤 run scope를 종료한다. 이미 발생한 효과를 되돌리지 않는다. |
| `block` | 지원되는 policy subset과 run scope를 준비하고 BPF-LSM 사전 hook에서 직접 file-open과 새 IPv4 connect를 집행한다. policy projection, run scope, hook 준비 중 하나라도 실패하면 target을 시작하지 않는다. |

`block`도 기존 socket 송신, UDP `sendto`, IPv6 등 미지원 경로를 통제하지 않는다.
감사 누락이 있으면 run은 incomplete로 보고되므로 effect 결과만으로 audit이
완전했다고 판단하지 않는다.

root로 실행하는 예에서는 `--run-as`의 UID/GID를 target에 사용할 비특권 값으로
바꾼다. 비-root observer는 이 옵션을 생략한다. `policies/example.yaml`의
`/workspace/fixtures/secret.txt`는 예시 placeholder이므로 실제 테스트 경로로
수정해야 한다. source는 관측 event의 raw absolute path와 정확히 비교한다.
audit 파일은 새 파일로 생성되며 기존 파일을 덮어쓰지 않는다. 부모 경로는
신뢰할 수 있는 비-symlink 디렉터리여야 하고, 파일은 effective user 소유의
regular file로 mode `0600`에 생성된다.

- `doctor`: Linux의 커널 버전·BTF 파일·활성 LSM 목록을 정적으로 점검하고
  알려진 CLI의 경로를 조회한다. 발견한 CLI를 실행하지 않으므로 버전은
  `unknown`, 이유는 `not_executed_read_only`다. BPF load/attach는 하지 않는다.
- `run`: `--bin`의 literal 파일 경로 → PATH → 문서화된 후보 경로 순서로
  실행 파일을 선택한다. 잘못된 명시 경로나 안전하지 않은 상대 PATH 결과는
  fallback하지 않는다. argv와 stdin/stdout/stderr를 그대로 연결하고, 자체
  안내는 stderr로 보낸다. shell 보간·PATH 변경을 하지 않는다.
- 종료 코드: target의 정상 종료 코드를 보존한다. target이 signal N으로
  종료되면 POSIX shell 관례대로 `128+N`(예: SIGTERM `143`)이며 signal
  이름을 stderr에 남긴다. 실행/조회 실패는 `1`, CLI 사용법 오류는 `2`다.
  `128+N`은 target이 같은 값으로 정상 종료한 경우와 숫자만으로 구분되지 않는다. `doctor`는 진단 보고서를
  출력하면 `0`이며, 이 값은 보호 가능 판정이 아니다. 보고서 출력 실패는 `1`이다.
- `run --observe`는 monitor mode alias로 sensor·정책 준비 후 target을 시작하고
  프로세스 계보를 관찰해 별도 audit file에 기록한다. observer/audit 손실은
  불완전 run으로 표시한다. `notify`/`kill`은 post-event 동작이며 prevention이
  아니다. `block` mode는 지원된 direct file-open과 새 IPv4 connect를 BPF-LSM
  hook에서 사전 차단할 수 있지만, 전체 operation·정보 흐름을 보호하지 않는다.

### 문제 해결

- **Linux amd64에서 observer가 지원되지 않는다고 표시됨:** 현재 observer runtime은 arm64 전용이다. amd64 교차 빌드나 BPFEL/BPFEB 생성물은 amd64 verifier/load/attach/runtime 지원을 의미하지 않는다. amd64 지원은 ARCH-01 완료 후에만 추가한다.
- **observer가 시작되지 않거나 BTF 오류가 발생함:** Linux guest 안에서
  `./agenttaint doctor --json`을 확인하고 `/sys/kernel/btf/vmlinux`의 존재와
  선택한 mode에 필요한 eBPF 권한을 점검한다. 진단 출력은 hook load/attach 성공을
  증명하지 않는다.
- **`block`이 target 실행 전에 거부됨:** Linux 활성 LSM 목록에서 `bpf`가
  포함되는지 확인하고 오류에 표시된 요청 hook의 준비 상태를 확인한다.
  지원되지 않는 hook을 `kill`이나 `notify`로 자동 대체하지 않는다.
- **root supervisor에서 `--run-as` 오류가 발생함:** `--run-as UID:GID`에는
  target에 사용할 0이 아닌 UID와 GID를 지정한다. root가 아닌 supervisor는
  자기 effective UID/GID로만 실행한다.
- **audit coverage가 incomplete로 반환됨:** stderr의 `audit_coverage`와 손실
  사유를 확인한다. 불완전 로그를 clean run이나 누락 없는 audit으로 판정하지
  않는다. 이미 수행된 target effect는 되돌릴 수 없다.

안내의 명령은 `./agenttaint`, `./audit.jsonl`, `./block-audit.jsonl`을 만들 수
있다. 아래 정리 예시는 이 안내를 따라 직접 만든 파일임을 확인한 뒤에만 실행한다.
audit 파일은 기존 파일을 덮어쓰지 않으므로, 같은 이름으로 다시 실행하기 전에는
직접 생성한 파일을 옮기거나 제거해야 한다.

```sh
rm -f ./agenttaint ./audit.jsonl ./block-audit.jsonl
```

```sh
make check
make race
```

이 명령은 portable 형식·정적 검사·빌드·테스트를 수행한다. Linux 실제 BPF
작동이나 차단을 검증하지 않는다. AI CLI·스킬을 설치하거나 사용자 설정을
변경하지 않는다. 개발·검증의 현재 상태와 남은 작업은
[통합 구현 계획서](./docs/IMPLEMENTATION_MASTER_PLAN.md)의 12.10절을 참고한다.

Linux guest에서 생성부터 필수 acceptance까지 순서대로 실행하려면 현재 checkout에서
`make linux-acceptance`를 사용한다. BPF 생성물 재생성 후 `vet`, build, 전체 테스트를
실행하고, root 권한 전체 suite와 exit-delete 및 (arm64 guest에서는) S25 outer
fixture를 required mode로 실행한다. 필요한 시점에 `sudo` 암호 입력이 필요하다.
S25 경로 캡처 손실이 남아 있으면 이 acceptance는 실패해야 하며,
이를 skip이나 통과로 처리하지 않는다. 생성된 `.go`/`.o` diff는 결과와 함께 검토한다.

## 핵심 모델

```
Sensitive Source → AI Agent → Process Taint → Propagation → Sink → Decision
```

현재 모델은 지정한 민감 파일을 연 프로세스에 taint label을 부여하고, fork/exec을 통해
그 라벨을 자식 프로세스까지 전파하며, 라벨이 붙은 프로세스가 명시된 IPv4
network sink에 도달하면 policy verdict를 audit한다. Phase 3/reference의 `deny`는
verdict만 뜻한다. 명시적인 Phase 4 `block` mode는 검증된 subset을 BPF-LSM
pre-operation hook으로 집행하며, 현재 증거는 제한된 guest fixture에 한정된다.
자세한 정의는 [docs/SECURITY_MODEL.md](./docs/SECURITY_MODEL.md),
막는 것/못 막는 것의 경계는 [docs/THREAT_MODEL.md](./docs/THREAT_MODEL.md)
참고.

## 제품 철학

AI를 신뢰하는 대신, 민감정보가 안전하게 사용되도록 실행 환경에서 제한하고
실제 정보 흐름을 추적·통제한다.

## 관련 프로젝트

- **AgentSight** — AI 에이전트용 `strace` + `top` + APM. AI 에이전트가 실제
  시스템에서 어떤 프로세스를 생성하고, 어떤 파일과 네트워크에 접근하며, 어떤
  시스템 자원을 사용하는지를 관찰하는 시스템 수준의 Observability 프로젝트다.
- **ActPlane** — AI 에이전트용 policy engine + sandbox/guardrail. AI
  에이전트와 그 하위 프로세스가 수행하는 실제 시스템 동작을 정책으로 정의하고,
  이를 OS 수준에서 허용·차단하는 Runtime Policy Enforcement 프로젝트다.
  eBPF로 fork/exec·file I/O를 따라 레이블을 전파하고 kill/block/notify로
  강제한다는 점에서 커널 메커니즘은 AgentTaint와 유사하지만, ActPlane은
  민감정보(secret/credential) 보호를 스스로 범위 밖으로 명시하고 "테스트 후
  커밋" 같은 에이전트 워크플로우 규칙 강제에 초점을 맞춘다.

AgentTaint는 이 둘과 달리 **민감정보 보호**에 초점을 맞춘 AI 실행 보안
환경을 목표로 한다. AI와 그 하위 프로세스가 민감정보에 접근하는 것을
제한하고, 허용된 민감정보가 프로세스·파일·네트워크를 통해 어떻게 이동하는지
추적하며, 위험한 정보 흐름이 발생할 경우 이를 탐지하고 차단하는 것이 목표다.
Byte-level taint tracking을 주장하지 않으며, 사후 감지·kill을 사전 차단이라고
부르지 않는다.

## Docs

- [AGENTS.md](./AGENTS.md) — AI 코딩 에이전트가 항상 지켜야 하는 규칙 (프로젝트 헌법)
- [docs/PROJECT_OVERVIEW.md](./docs/PROJECT_OVERVIEW.md) — 처음 읽는 사람을 위한 프로젝트 개요와 그림
- [docs/THREAT_MODEL.md](./docs/THREAT_MODEL.md) — 보호 대상, 공격자 모델, non-goals
- [docs/SECURITY_MODEL.md](./docs/SECURITY_MODEL.md) — taint 모델, 정책/event schema v1
- [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md) — 기술 스택, 디렉터리 구조, doctor/run 역할 분리
- [docs/ROADMAP.md](./docs/ROADMAP.md) — Phase 0~7+ 전체 로드맵
- [docs/IMPLEMENTATION_MASTER_PLAN.md](./docs/IMPLEMENTATION_MASTER_PLAN.md) — 상세 결정·작업 계약·검증·현재 인계 상태
- [prompts/](./prompts/) — Phase별 AI 작업 지시 (implementation contract)
