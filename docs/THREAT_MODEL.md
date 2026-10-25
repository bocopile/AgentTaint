# Threat Model

## 보호 대상 (Protected Assets)

- `.env`, 설정 파일에 박힌 자격증명
- SSH/GPG 개인키
- 클라우드/API 자격증명 (`~/.aws/credentials`, API 토큰 등)
- Kubernetes ServiceAccount 토큰, mounted Secret
- 사용자가 정책으로 직접 지정한 임의의 민감 파일/디렉터리

## 공격자 모델

AgentTaint는 "AI 에이전트 = 악성 코드"를 가정하지 않는다. 실제로 문제가
발생하는 경로는 대부분 다음 중 하나다.

```
User Prompt
Malicious Repository (README, 코드 주석, 데이터 파일)
Prompt Injection
Tool Output (검색 결과, 웹 페이지, MCP 응답)
Compromised MCP Server / Dependency
        │
        ▼
    AI Agent (정상적으로 동작 중)
        │
        ▼
  shell / python / curl / subprocess
        │
        ▼
   Sensitive Resource 접근
        │
        ▼
   External Network 전송
```

즉 공격자는 에이전트 자체가 아니라 **에이전트가 신뢰하는 입력**(프롬프트,
레포 콘텐츠, 툴 출력, MCP 서버)인 경우가 더 흔하다. 정상적으로 동작하는
에이전트가 오염된 입력 때문에 잘못된 행동을 하는 경우까지 막는 것이
목표다.

## 핵심 가정

> **AgentTaint는 AI의 의도나 프롬프트의 안전성을 판단하지 않는다.**
> **실제 OS-level effect(파일 접근, 프로세스 생성, 네트워크 연결)를 기준으로
> 정책을 적용한다.**

프롬프트를 분류하거나 "이 요청이 악의적인가"를 판단하는 것은 확률적이고
우회 가능하다(긴 컨텍스트에서 지시를 잊거나, 프롬프트 인젝션으로 재작성될
수 있음). AgentTaint는 그 판단을 하지 않고, 커널이 관찰 가능한 사실
— 어떤 프로세스가 어떤 파일을 읽었고 어디로 연결했는가 — 만을 근거로
삼는다.

## 막는 것 / 못 막는 것

**막는 것 (v0.1 목표)**
- 지정된 민감 파일에 접근한 프로세스 및 그 자식 프로세스가 외부 네트워크로
  연결을 시도하는 것 (information-flow 기준 차단/감사)
- 지정된 민감 파일에 대한 직접 접근 자체 차단 (access-control 기준)

위 항목은 Phase 4에서 요청한 block profile, 필수 BPF-LSM hook의 실제 attach,
kernel state 초기화 및 해당 scenario의 syscall/effect 검증이 성공한 환경에만
해당한다. 구현 전이나 미검증 환경에서는 AgentTaint가 해당 effect를 차단한다고
주장하지 않는다. Go audit/reference model은 성공한 `FileOpen` 완료 event에서만
`SECRET`을 부여한다. 동기 enforcement projection은 source `file_open` 시도에서
보수적으로 label을 표시할 수 있으므로, 이후 source open이 실패해도 뒤따르는
지원 sink를 거부할 수 있다. 이는 성공한 open이나 파일 byte read의 증거가 아니다.

**못 막는 것 (v0.1 명시적 한계)**
- 커널/eBPF 자체를 우회하는 권한 상승 공격 (예: 컨테이너 탈출, 커널 익스플로잇)
- AgentTaint가 설치되지 않은 별도 머신/프로세스로의 유출
- supervisor가 비정상 종료된 뒤 남은 target descendant의 enforcement 지속성은 아직 보장하지 않는다. kernel link/cgroup lifecycle 검증 전에는 이 장애를 block 보장 범위로 표현하지 않는다.
- 은닉 채널 유출 (DNS 터널링, 타이밍 사이드채널 등) — 향후 과제
- 정적 링크된 TLS 라이브러리를 쓰는 바이너리의 페이로드 내용 검사 (v0.1은
  파일/프로세스/네트워크 연결의 존재 여부만 보며, TLS 페이로드 자체는 보지
  않는다 — 자세한 이유는 [SECURITY_MODEL.md](./SECURITY_MODEL.md) 참고)
- AgentTaint 자신에 대한 공급망 공격 (서명/배포 검증은 별도 과제)
- **명시적 kill mode는 예방이 아니다**: Phase 4에서 사용자가 명시한 kill은
  사후 대응이지 사전 차단이 아니다. block 요청을 kill로 자동 대체하지 않는다.
  요청된 block surface에 필요한 훅을 실제로 load/attach할 수 없으면 target을
  시작하지 않는다.
  예를 들어
  `write()` 시스템콜 도중 SIGKILL을 보내도, 데이터가 이미 커널 버퍼로
  넘어간 뒤라면 파일에 그대로 써질 수 있다 — 이건 추상적 우려가 아니라
  cilium/tetragon이 자사 문서에서 명시하는 것과 동일한 한계다. AGENTS.md
  Security Semantics의 "post-event detection을 prevention이라 부르지
  않는다" 원칙은 명시적 kill mode에도 그대로 적용된다: kill이 성공적으로
  프로세스를 종료시켜도, 그 프로세스가 kill 신호를 받기 직전까지 이미
  만든 effect(부분적으로 쓰인 파일, 이미 나간 네트워크 패킷)를 되돌리지
  못한다.

## Non-goals

AgentTaint v0.x는 다음이 **아니다**:

- full DLP (byte-level content inspection)
- prompt injection detector
- malware scanner
- LLM 출력 안전성 분류기
- TLS decryption / packet DPI 엔진
- 완전한 sandbox (컨테이너/VM 대체재 아님 — 그 위에 얹는 semantic policy layer)
- Kubernetes NetworkPolicy 대체재

## 후속 실행 보호 profile의 추가 수용 경계 — 구현 전

첫 비교 시나리오는 사용자가 지정한 합성 파일을 동일 실행의 로컬 AI가 분석하고
본인에게 표시하는 작업이다. 외부 LLM 전송과 DB broker는 첫 범위에 포함하지 않는다.
실행 전체 제한은 process 관측 사실과 별개이며 단독으로 완전한 sandbox를 제공하지 않는다.

격리 제공자는 대상의 scope 이탈, 외부 same-UID process/IPC/공유 cache, 초기
입력/FD/mapping, source alias와 산출물 재사용 경계를 제한해야 한다. AgentTaint는
필요 capability를 확인하고 미지원 profile을 시작하지 않는다. 기존 연결 송신과
source/send 경쟁은 실제 효과·대조군으로 검증하며 관측 시간 정렬만으로 증명하지 않는다.

supervisor 종료 후 target이 남는 경우 집행 지속 또는 안전한 실행 종료를 확인한다.
증거가 없으면 해당 장애 경로의 보호는 미지원이다. 감사 누락은 incomplete로 남기고
집행 성공으로 오인하지 않는다. host root/kernel compromise는 여전히 범위 밖이다.
이 절은 후속 실험의 요구 조건이며 현재 완료된 runtime 보장이 아니다.
