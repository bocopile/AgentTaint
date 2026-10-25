# Security Model

## 핵심 추상화: Source → Label → Propagation → Sink → Decision

AgentTaint의 모든 기능은 이 다섯 단계로 환원된다. 이후 어떤 기능을 추가할지
판단할 때도 이 모델을 기준으로 삼는다.

```
   SOURCE                 민감정보가 들어오는 지점 (파일, 환경변수 등)
     │
     ▼  read/open
  LABEL 부여              접근한 프로세스에 taint label 부착 (예: SECRET)
     │
     ▼  fork / exec / (향후) write, pipe
 PROPAGATION              label이 프로세스 계보·파생 산출물을 따라 전파
     │
     ▼
   SINK                   label이 붙은 프로세스가 도달하는 출구
     │                    (네트워크 연결, 파일 쓰기 등)
     ▼
  DECISION                정책 평가 결과: ALLOW / AUDIT / DENY
```

예시:

```
.env (SOURCE)
   │ read
   ▼
Claude PID 100  [SECRET]
   │ fork
   ▼
bash PID 101    [SECRET]
   │ fork
   ▼
python PID 102  [SECRET]
   │ connect
   ▼
8.8.8.8:443 (SINK, external network)
   │
   ▼
DECISION: DENY  (policy: prevent-secret-egress)
```

## Taint의 정확한 의미 — 반드시 지켜야 할 정직성

> **Taint label은 "이 프로세스의 모든 출력에 실제 시크릿 바이트가 포함되어
> 있다"는 증명이 아니다.**
>
> 해당 프로세스가 민감정보에 접근했다는 사실 하나만으로, 이후 발생하는
> effect를 **보수적으로(conservatively)** 잠재적 민감 정보로 취급하는
> information-flow policy다.

이 문장을 빼면 AgentTaint는 실제보다 더 강한 보장을 주장하는 것처럼
보이고, 오탐(false positive)이 나왔을 때 "버그"가 아니라 "설계상 당연한
보수적 판단"이라는 것도 이 문장으로 설명된다. `AgentTaint does not prove
that specific secret bytes were exfiltrated` — 이 한계는 README/문서
어디에도 숨기지 않는다.

## Access Control vs Information Flow Control

AgentTaint는 이 둘을 구분하고, 최종적으로 둘 다 제공하는 것을 목표로 한다.

**Access Control** — "이 접근 자체가 허용되는가?"
```
AI Agent → .env open → DENY
```

**Information Flow Control** — "접근을 허용한 뒤, 그 결과 데이터가 어디까지
갈 수 있는가?"
```
AI Agent → .env read → ALLOW (+ SECRET label) → 작업 수행
         → external network connect → DENY
```

Resource/operation access control과 IFC는 다른 질문에 답한다. 전자는 현재
주체의 자원 접근을 허용할지 판단하고, IFC는 관측한 민감 자원 접근에서
파생된 process label을 이후 sink 판정에 사용한다. 더 복잡한 access-control
시스템이 조건부 규칙을 표현할 수 없다는 뜻은 아니다. 두 방식은 함께 쓸
수 있지만, AgentTaint의 핵심 모델은 Source→Label→Propagation→Sink→Decision이다.

### 이 구분이 실제로 중요한 이유

경로·syscall 단위 access-control 규칙의 존재만으로는 관측된 민감 자원
접근이 이후 어떤 process sink에 영향을 주는지 표현하지 않는다. 반대로
IFC label은 자원 접근 자체를 허용하거나 거부하는 access control을 대체하지
않는다. AgentTaint의 설계 차이는 특정 생태계 도구가 조건부 access control을
구현할 수 없다는 주장이 아니라, Source→Label→Propagation→Sink→Decision
흐름을 제품의 핵심 모델로 삼는 데 있다.

## Scope (v0.1)

| 구분 | v0.1 범위 |
|---|---|
| Source | 정책에 지정한 민감 파일 |
| Propagation | fork / exec (프로세스 계보) |
| Sink | 외부 IPv4 네트워크 연결 |
| Decision | audit / deny |

**v0.1이 다루지 않는 것** (향후 확장 대상이지 지금 구현 대상이 아님):

| 구분 | 확장 후보 |
|---|---|
| Source | 여러 종류의 credential, Kubernetes Secret |
| Propagation | file write/read, pipe, unix domain socket |
| Sink | 파일, clipboard, stdout |
| Label | 다중 라벨: `SECRET`, `CREDENTIAL`, `SOURCE_CODE`, `UNTRUSTED` |

TLS 페이로드 내용 검사(`SSL_write` uprobe 등 payload-aware observability)는
v0.1 sink 정의에 없다. 이유는 [THREAT_MODEL.md](./THREAT_MODEL.md)와
[ARCHITECTURE.md](./ARCHITECTURE.md)에서: payload inspection은 AgentSight가
이미 다루는 관찰(observability) 영역이고, AgentTaint의 핵심 정체성은
"접근했다는 사실 자체를 근거로 흐름을 통제"하는 IFC이지 "내용을 들여다보는
DLP"가 아니다. 필요해지면 명시적으로 별도 phase에서 추가한다.

### (스텁) 향후 payload-aware LLM sink를 추가한다면

지금 결정하는 것이 아니라, 나중에 이 확장이 실제로 필요하다고 판단되어
AGENTS.md의 "문서는 규범이다" 절차(충돌 식별 → 이유 설명 → 최소 변경안
제안 → 사람 확인)를 다시 밟게 될 경우를 대비해, 그 phase가 반드시 지켜야
할 제약을 지금 기록해둔다.

- **Decision 근거는 여전히 메타데이터여야 한다** — 목적지 host/도메인,
  요청에 선언된 model 필드, 요청 크기 정도. prompt/response 본문을
  분류(PII 판별, 위험도 스코어링 등)해서 그 결과를 Decision 근거로 쓰는
  것은 이 확장에서도 금지된다. 그건 DLP이지 AgentTaint의 IFC 정체성이
  아니다 (AGENTS.md Product Principle).
- **목적지 단위 차단은 이미 v0.1 Sink로 충분하다** — SECRET 라벨이 붙은
  프로세스가 `api.anthropic.com`을 포함한 외부 네트워크로 나가는 것 자체를
  막는 데는 payload를 볼 필요가 없다 (위 "Scope (v0.1)" 참고). 이 확장이
  실제로 필요해지는 경우는 "같은 목적지라도 요청마다 다르게 처리해야
  하는" 케이스로 좁혀서 판단한다.
- eBPF 훅 선택 시 AGENTS.md의 detect≠block 원칙을 코드로 강제한다 — 어떤
  훅(uprobe vs LSM)이 어떤 Decision(audit vs deny)을 낼 수 있는지 타입
  레벨에서 검증하는 게이트를 둔다 (참고 선례:
  eunomia-bpf/ActPlane의 `te_effect_mode()` — 훅 종류가 지원 안 하는
  effect를 rule이 요청하면 무효화하는 패턴).

## Policy Model (v0.1, Phase 3)

v0.1은 `SECRET` 단일 label과 IPv4 network sink를 지원한다. 정책은 YAML
snapshot으로 한 번 읽고 검증하며, run 중 파일이 바뀌어도 재로드하지 않는다.
상대 source path는 policy 파일 디렉터리를 기준으로 해석한다. `~`, 환경변수
치환, command substitution은 지원하지 않는다. resolved source path는 syscall
event의 raw `resource.path`와 정확히 비교한다. relative event path는 absolute
policy path로 자동 확장하지 않으며 dirfd, symlink, hardlink, rename 및 mount
identity를 해석하지 않는다. 따라서 relative/alias path가 같은 파일을 가리킨다고
추정하지 않는다. 성공한 `FileOpen` completed event는 해당 프로세스가 source를 열었다는
근거로 `SECRET`을 부여하지만, 파일 byte를 읽었다는 증거는 아니다.

## Phase 4 kernel enforcement projection

Phase 4에는 두 상태 경로가 있다. `internal/taint`의 Go reference model은 위
성공한 completed `FileOpen` 의미를 유지하고 audit·differential test의 기준으로
남는다. 실제 pre-operation 집행은 `internal/policy`가 검증한 immutable snapshot의
지원 subset을 `internal/enforce`가 kernel에 전달해 수행한다. BPF는 일반 policy
parser/evaluator가 아니며 새 label 종류나 별도 policy precedence를 정의하지 않는다.

Kernel projection은 `file_open`의 pre-operation source match 시 `SECRET` 상태를
보수적으로 표시할 수 있다. 같은 hook 뒤 filesystem open 등이 실패해도 이 상태는
유지될 수 있으며, 따라서 이후 지원 sink가 거부될 수 있다. 이것은 **source open
시도에 대한 집행 상태**이고 successful-open audit 사실이 아니다. Audit event와
Go reference model은 실패한 open을 성공으로 바꾸지 않는다. 어느 경로도 파일 byte를
읽었다는 증거를 제공하지 않는다.

Kernel state는 run/scope와 process birth identity로 분리하고, 지원된 fork 전파와
sink 판단은 해당 pre-operation hook 안에서 동기 처리해야 한다. source state 갱신,
child propagation 또는 sink lookup을 안전하게 처리할 수 없으면 해당 operation을
허용 상태로 간주하지 않는다. 지원 hook, map 초기화, rule subset, scope 또는 권한이
준비되지 않으면 `block` 요청은 target 시작 전에 실패한다. Ring-buffer/audit 손실은
kernel enforcement state와 별개다. Concurrent source/sink의 실제 순서와 map failure
동작은 커널 integration test로 확인하기 전까지 보장하지 않는다.

Fork tracepoint는 관측 이벤트이므로 callback에서 fork syscall 자체를 취소할 수
없다. 지원되는 fork 경로에서 child state 갱신이 실패하면, kernel state에 지속되는
scope-level error가 모든 후속 scoped sink를 거부하도록 해야 한다. 이 error state를
유지할 bounded map 경로가 준비되지 않거나 자체 갱신에 실패할 수 있으면 fork를
포함하는 enforcement profile을 지원하지 않는다. 미등록 process identity는 clean으로
추정하지 않는다. Linux v7.0의 일반 `kernel_clone()`은 fork tracepoint 뒤에
`wake_up_new_task()`를 호출하지만, 이것은 모든 task 생성 경로나 map 갱신 성공의
보장이 아니다. 실제 지원 경로는 고정 kernel source와 integration test로 한정한다.

이 projection의 구현·검증 없는 상태는 차단 기능을 뜻하지 않는다. 실제 block 주장은
지원 Linux kernel에서 hook load/attach 및 syscall 결과, effect 부재와 allow 대조군을
함께 확인한 시나리오에 한정한다.

```yaml
sources:
  - path: /workspace/fixtures/secret.txt
    label: SECRET

rules:
  - name: prevent-secret-egress
    when:
      process_has: SECRET
    sink:
      network:
        # Explicit IPv4 CIDRs; there is no built-in public/private classifier.
        external:
          - 203.0.113.0/24
        ports: [443]
    action: deny
```

다음 `PRIVATE_KEY` snippet은 미래의 다중 label 확장 예시다. v0.1/Phase 3의
실행 가능한 policy에 포함하지 않으며, 해당 확장은 Phase 6에서 별도 계약과
검증을 거쳐야 한다.

```yaml
sources:
  - path: /home/alice/.ssh/id_rsa
    label: PRIVATE_KEY

rules:
  - name: block-private-key-access
    when:
      source: PRIVATE_KEY
    action: deny
```

`action`은 `allow` / `audit` / `deny` 세 가지만 지원한다. `external`은
자동 분류가 아니라 정책에 명시한 IPv4 CIDR 집합이다. RFC1918·loopback·기타
주소를 자동으로 신뢰하지 않는다. 여러 rule이 매치하면 `deny > audit > allow`,
동일 action에서는 rule 이름의 사전순으로 하나를 선택한다. no-match 기본값은
`allow`이며 이 기본값은 rule 누락이 허용으로 이어질 수 있음을 뜻한다. Phase 3의
`deny`는 policy verdict/audit일 뿐 실제 연결을 차단하지 않는다.
`ports`가 있으면 나열한 1~65535 포트에만 매치하며, 생략하면 해당 CIDR의 모든
포트에 매치한다.

## Event Schema

`session_id`는 `internal/core`가 정의하는 `SessionID`(POSIX 세션 ID를
감싸는 named type — `prompts/01-core-event-model.md` "식별자 타입" 절
참고)를 문자열로 직렬화한 값이다. 커널이 이벤트를 발생시킨 프로세스에
대해 이미 갖고 있는 세션 식별자이며, 애플리케이션이 별도로 발급하는
run 식별자가 아니다.

Phase 3의 `policy_violation`은 raw connect 이벤트 바로 뒤에 추가하는 파생
audit record다. 공통 schema v1 metadata와 원본 process identity를 보존한다.
`operation_result`는 관측된 connect syscall 결과이며 `decision`은 `audit` 또는
`deny` 정책 verdict다. 둘은 서로 다른 사실이고, `deny`도 syscall을 막지 않는다.
`allow`는 별도 파생 record를 만들지 않으며 원본 이벤트는 계속 기록한다.

```json
{
  "event": "policy_violation",
  "schema_version": 1,
  "event_id": "derived-event-id",
  "run_id": "run-id",
  "timestamp": "2026-10-19T00:00:00Z",
  "session_id": "4021",
  "observation_stage": "completed",
  "operation_result": "success",
  "process": { "pid": 19342, "birth_id": "birth-id", "scope_id": "scope-id", "ppid": 1, "pgid": 19342, "comm": "python" },
  "labels": ["SECRET"],
  "source": { "path": "/workspace/.env" },
  "sink": { "address": "1.2.3.4", "port": 443 },
  "decision": "deny",
  "policy": "prevent-secret-egress"
}
```

`source.path`는 관측된 raw path 중 사전순 첫 provenance이며 파일 객체의
정체성을 보장하지 않는다. operation은 derived event가 만들어지기 전에 이미
시도되거나 완료됐을 수 있다. 이 record는 감사를 위한 것이며 prevention 증거가
아니다.

## 후속 실행 보호 profile — 설계 계약, 구현 전

기존 v0.1 Go reference의 successful completed FileOpen→process SECRET 의미는
그대로 유지한다. 별도의 실험 profile은 source-attempt의 동기 projection에 따라
동일 run 전체에 단조 증가하는 보수적 제한을 적용할 수 있다. 다른 process의
successful-open event나 실제 byte 이동을 만들어내지 않는다. 현재 CLI/policy
schema에 새로운 필드가 구현됐다는 뜻이 아니다.

source 접근을 허용하는 경계 이전에 상태를 반영하고, 이후 지원 송신 gate가 그
상태를 확인해야 한다. 기존 연결, 다른 CPU, 초기 FD/mapping, alias, 외부 IPC와
다음 run 산출물 경로를 profile별로 검사한다. 필요한 경계를 제공하지 못하면
해당 strict profile의 launch를 거부한다. 정확한 hook과 지원표는 실험 증거 후 확정한다.

실행 중 exec/자식 종료/로그 삭제/UI 조작으로 제한을 해제하지 않는다. audit loss와
집행 health는 별도 기록하며, 실패한 source open을 성공으로 고치지 않는다.
Byte-level taint tracking과 일반 유출 방지를 주장하지 않는다. 현재 구현·검증된
v0.1 지원 범위는 이 설계 계약으로 확장되지 않는다.
