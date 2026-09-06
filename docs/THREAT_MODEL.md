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

**못 막는 것 (v0.1 명시적 한계)**
- 커널/eBPF 자체를 우회하는 권한 상승 공격 (예: 컨테이너 탈출, 커널 익스플로잇)
- AgentTaint가 설치되지 않은 별도 머신/프로세스로의 유출
- 은닉 채널 유출 (DNS 터널링, 타이밍 사이드채널 등) — 향후 과제
- 정적 링크된 TLS 라이브러리를 쓰는 바이너리의 페이로드 내용 검사 (v0.1은
  파일/프로세스/네트워크 연결의 존재 여부만 보며, TLS 페이로드 자체는 보지
  않는다 — 자세한 이유는 [SECURITY_MODEL.md](./SECURITY_MODEL.md) 참고)
- AgentTaint 자신에 대한 공급망 공격 (서명/배포 검증은 별도 과제)
- **BPF-LSM이 없는 환경의 kill 폴백은 예방이 아니다**: Phase 4가 계획하는
  kill 폴백(ROADMAP.md)은 사후 대응이지 사전 차단이 아니다. 예를 들어
  `write()` 시스템콜 도중 SIGKILL을 보내도, 데이터가 이미 커널 버퍼로
  넘어간 뒤라면 파일에 그대로 써질 수 있다 — 이건 추상적 우려가 아니라
  cilium/tetragon이 자사 문서에서 명시하는 것과 동일한 한계다. AGENTS.md
  Security Semantics의 "post-event detection을 prevention이라 부르지
  않는다" 원칙은 kill 폴백에도 그대로 적용된다: kill이 성공적으로
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
