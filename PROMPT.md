# Phase 0 부트스트랩 프롬프트 — AgentTaint

아래 프롬프트를 Claude Code / Codex CLI에 그대로 입력해서 Phase 0(환경 점검 +
프로젝트 뼈대)를 시작한다.

---

## 프롬프트

```
# AgentTaint — Phase 0: 환경 점검(doctor) + 프로젝트 뼈대

## 배경
AgentTaint는 AI 에이전트(Claude Code, Codex CLI 등)가 민감정보에 접근하거나
유출하는 것을 커널 레벨(eBPF)에서 감지·차단하는 도구다. 이 도구가 감시할
대상이 바로 로컬에 설치된 AI 에이전트 CLI들이므로, 가장 먼저 "이 머신에
어떤 에이전트 CLI가 실제로 호출 가능한 상태인지"부터 정확히 파악해야 한다.

## 작업 1: 에이전트 CLI 탐지 + PATH 보정
다음 요구사항으로 탐지 로직을 구현하라.

- 대상 바이너리 목록은 하드코딩하지 말고 설정으로 분리한다 (최소: claude, codex,
  향후 gemini, cursor-agent 등을 추가할 수 있게).
- 1차 시도: 현재 PATH에서 `command -v <bin>` 으로 탐지.
- 실패 시: 아래 후보 경로들을 macOS/Linux 공통으로 스캔한다.
  - `~/.local/bin`
  - `~/go/bin`
  - `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`
  - nvm: `~/.nvm/versions/node/*/bin` (버전 디렉터리는 와일드카드로, 특정
    버전을 하드코딩하지 말 것 — 버전은 머신마다 다르다)
  - volta: `~/.volta/bin`
  - fnm: `~/.fnm/aliases/default/bin` 또는 `fnm env`로 노출되는 경로
  - asdf shims: `~/.asdf/shims`
  - npm 전역: `npm` 이 PATH에 있으면 `npm config get prefix`/bin 경로도 확인
  - pipx: `~/.local/pipx/venvs/*/bin` (python 기반 CLI 대비)
- 후보 경로에서 발견되면:
  - 그 디렉터리를 현재 세션 PATH 맨 앞에 prepend (export)
  - "어디서 찾았는지"를 로그로 남긴다 (경로 + 어떤 후보 규칙에 매칭됐는지)
- 끝까지 못 찾으면: 사람이 읽을 수 있는 이유와 설치 확인 방법을 출력하고
  0이 아닌 종료 코드를 반환한다. (예: "codex를 찾지 못했습니다. `npm install -g
  @openai/codex` 또는 nvm/volta로 설치했다면 해당 shell integration이
  로드됐는지 확인하세요.")
- 출력은 사람이 읽는 텍스트와 JSON(`--json`) 두 가지 모드를 지원한다. JSON은
  이후 `agenttaint doctor` 전체 출력에 병합할 수 있는 형태로 설계한다.
  예:
  ```json
  {
    "claude":  { "found": true,  "path": "/Users/x/.local/bin/claude",  "version": "2.1.251", "resolved_via": "PATH" },
    "codex":   { "found": true,  "path": "/Users/x/.nvm/versions/node/v24.14.0/bin/codex", "version": "0.149.0", "resolved_via": "nvm-glob" }
  }
  ```

## 작업 2: doctor 명령의 일부로 통합
이 탐지 로직은 향후 `agenttaint doctor` 서브커맨드의 "에이전트 탐지" 섹션이
된다. doctor는 이후 Phase에서 커널 버전/BTF/BPF-LSM 여부도 함께 점검할
예정이므로, 지금은 에이전트 탐지 부분만 독립 실행 가능한 모듈/스크립트로
분리해서 구현하고, 나중에 doctor에 합칠 수 있게 인터페이스를 단순하게 유지하라.

## 검증
- 이 스크립트/모듈을 현재 개발 머신에서 실행해 claude, codex 모두 found:true로
  나오는지 확인한다.
- PATH에서 일부러 해당 디렉터리를 제거한 상태로도 재탐지가 되는지 확인한다
  (widening 로직이 실제로 동작하는지 증명).
- README 또는 doctor 문서에 "왜 PATH 밖에서도 찾아야 하는가" (nvm/volta 등
  버전 매니저는 셸 초기화 스크립트에 의존하는데, 비대화형 세션이나 다른
  에이전트가 실행하는 서브셸에서는 그 초기화가 스킵될 수 있다)를 한 문단으로
  남긴다.

## 작업 3: 다음 단계 계획 갱신
탐지 결과(어떤 에이전트가 실제로 이 머신에서 사용 가능한지)를 바탕으로,
Phase 1(SSL_write uprobe 관찰) 테스트 시나리오를 어떤 CLI로 먼저 검증할지
결정하고 PLAN.md에 기록하라. 참고로 Claude Code/Codex 모두 TLS 라이브러리를
정적 링크하는 경우가 많아 uprobe 후킹이 바로 안 통할 수 있다 — 이 경우 Phase 1
1차 검증은 동적 링크 대상(curl, python requests)으로 먼저 하고, Claude/Codex
정적 링크 대응은 별도 이슈로 분리해서 기록한다.

전부 구현하지 말고, 작업 1 → 검증 → 내 확인 → 작업 2 → 작업 3 순서로 진행하라.
```
