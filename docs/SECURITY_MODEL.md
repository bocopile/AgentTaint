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

Access Control만으로는 "파일 A는 스크립트 A를 통해서만 접근 가능"처럼
조건부 규칙을 표현할 수 없고, 전부-허용/전부-차단의 이분법에 갇힌다.
IFC는 접근은 허용하되 그 이후 파생되는 흐름에 규칙을 건다. AgentTaint는
두 계층을 함께 쓴다: 일부 자산은 애초에 접근 자체를 막고(access control),
접근이 필요한 자산은 접근을 허용하되 흐름을 추적한다(IFC).

### 이 구분이 실제로 중요한 이유 (생태계 조사 근거)

이 구분은 추상적 이분법이 아니다. 고스타 오픈소스 보안 도구를 조사해보면
(2026-09 기준, 전체 목록은 [RELATED_WORK.md](./RELATED_WORK.md) 참고)
google/gvisor, evilsocket/opensnitch, netblue30/firejail,
containers/bubblewrap, Zouuup/landrun처럼 범용 샌드박싱 도구는 물론,
AI 에이전트를 명시적으로 타깃하는 multikernel/sandlock,
GreyhavenHQ/greywall까지 **전부 access control**(경로·syscall 단위
allow/deny 리스트)이다. 실시간으로 프로세스에 라벨을 붙이고 그 라벨을
계보를 따라 전파해서 판정하는 프로젝트는 조사 범위에서 사실상 없었다 —
유일하게 근접한 ashish-gehani/SPADE(provenance 그래프)조차 실시간 정책
집행이 아니라 사후 포렌식 질의 도구다.

즉 access control 계층은 이미 성숙한 도구가 많아 AgentTaint가 새로
발명할 이유가 없고, AgentTaint의 taint/IFC 핵심 추상화
(Source→Label→Propagation→Sink→Decision)가 실제로 값을 더하는
지점이라는 게 이 조사로 뒷받침된다.

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

## Policy Model (초안)

구현 전이지만, BPF map/이벤트 스키마를 역설계할 수 있도록 형태를 먼저
고정한다.

```yaml
sources:
  - path: ".env"
    label: SECRET
  - path: "~/.ssh/id_rsa"
    label: PRIVATE_KEY

rules:
  - name: block-private-key-access
    when:
      source: PRIVATE_KEY
    action: deny

  - name: prevent-secret-egress
    when:
      process_has: SECRET
    sink:
      network: external
    action: deny
```

`action`은 초기엔 `allow` / `audit` / `deny` 세 가지만 지원한다.

## Event Schema (초안)

`session_id`는 `internal/core`가 정의하는 `SessionID`(POSIX 세션 ID를
감싸는 named type — `prompts/01-core-event-model.md` "식별자 타입" 절
참고)를 문자열로 직렬화한 값이다. 커널이 이벤트를 발생시킨 프로세스에
대해 이미 갖고 있는 세션 식별자이며, 애플리케이션이 별도로 발급하는
run 식별자가 아니다.

```json
{
  "event": "policy_violation",
  "session_id": "4021",
  "process": { "pid": 19342, "comm": "python" },
  "labels": ["SECRET"],
  "source": { "type": "file", "path": "/workspace/.env" },
  "sink": { "type": "network", "address": "1.2.3.4", "port": 443 },
  "decision": "deny",
  "policy": "prevent-secret-egress"
}
```

이 스키마를 먼저 고정해두면 Linux(eBPF) 백엔드든 향후 다른 플랫폼
백엔드든 동일한 이벤트 모델로 수렴시킬 수 있다. 실제 구현이 이 스키마를
사용하기 시작하면 이 섹션은 별도 `POLICY.md`/`EVENT_SCHEMA.md`로 분리한다.
