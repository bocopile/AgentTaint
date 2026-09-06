# CLAUDE.md

@AGENTS.md

Claude Code는 프로젝트 루트의 `AGENTS.md`를 자동으로 읽지 않는다 —
위 `@AGENTS.md` import 한 줄이 그 간극을 메운다. AGENTS.md 자체가 명시하듯,
이 프로젝트에서 AI 코딩 에이전트가 항상 지켜야 하는 규칙은 AGENTS.md가
최우선이며, `prompts/`의 Phase별 지시가 AGENTS.md와 충돌하면 AGENTS.md가
이긴다.

## 문서 지도

- `docs/ARCHITECTURE.md` — 기술 스택, 디렉터리 구조, 컴포넌트 경계
- `docs/SECURITY_MODEL.md` — taint 모델, 정책/이벤트 스키마
- `docs/THREAT_MODEL.md` — 보호 대상, 공격자 모델, non-goals
- `docs/ROADMAP.md` — Phase 0~7+ 전체 방향
- `prompts/` — 현재 Phase의 작업 지시 (`prompts/README.md`부터 시작)
- `docs/SUBORCHESTRATOR.md` — SubOrchestrator(외부 멀티에이전트 파이프라인
  도구)로 이 저장소를 구동할 때의 연동 방식. AgentTaint 자체 설계와는
  무관한 툴링 문서다.
- `docs/RELATED_WORK.md` — 유사 철학의 오픈소스 프로젝트 조사 기록(별점,
  구조, 문서 패턴 비교). **규범이 아니다** — 참고용 조사 기록이며 구현이
  이 문서를 따를 의무는 없다.

## 작업 시작 규칙

새 세션에서는 `prompts/README.md`가 지시하는 순서를 따른다: AGENTS.md →
관련 docs/ → 현재 Phase 프롬프트 순으로 확인한 뒤, **현재 Phase만**
수행한다.
