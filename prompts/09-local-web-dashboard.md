# Phase 9 — Local Web Dashboard (스텁)

아직 상세화하지 않았다. Phase 0~8 완료 후, 이 시점에 확정된 이벤트/
Decision 영속화 방식과 `docs/ARCHITECTURE.md`의 `internal/web` 자리를
기준으로 `prompts/README.md`의 템플릿에 맞춰 상세화한다.

방향성만 기록: `agenttaint`가 이미 수집한 이벤트/정책 위반 로그를 로컬
브라우저에서 조회하는 뷰어. AgentSight의 `frontend`+`controller`(별도
SaaS, OAuth/relay)를 그대로 따르지 않고, 클라우드 계층 없이 단일 바이너리
안에 `go:embed`로 내장하는 것을 기본 방향으로 한다 (자세한 내용은
`docs/ARCHITECTURE.md`의 "향후: 로컬 웹 대시보드" 절 참고).

이 Phase를 시작하기 전에 먼저 풀어야 할 선행 질문: 지금 아키텍처에는
이벤트/Decision을 영속화하고 쿼리하는 계층이 정의돼 있지 않다
(`internal/enforce`는 "구조화 로그"만 언급). 이 저장/쿼리 계층부터 먼저
설계해야 하며, 이 결정 없이 코드를 작성하지 않는다.

자세한 순서는 [../docs/ROADMAP.md](../docs/ROADMAP.md) 참고.
