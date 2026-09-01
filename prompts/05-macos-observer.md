# Phase 5 — macOS Observer (스텁)

아직 상세화하지 않았다. Phase 0~4("Linux MVP") 완료 후, 이 시점의
`internal/core` Event 모델과 `docs/ARCHITECTURE.md`의 `internal/sensor/darwin`
자리를 기준으로 `prompts/README.md`의 템플릿에 맞춰 상세화한다.

방향성만 기록: macOS 네이티브 지원은 Endpoint Security 프레임워크
기반이며, Swift/entitlement/공증이 필요한 별도 규모의 작업이다
(`docs/ARCHITECTURE.md`의 "플랫폼 백엔드" 절 참고). 이 Phase를 시작하기
전에 실제로 네이티브 지원을 할지, v0.1처럼 Linux VM 우회로 계속 남길지를
먼저 사람과 결정한다 — 이 결정 없이 코드를 작성하지 않는다.

자세한 순서는 [../docs/ROADMAP.md](../docs/ROADMAP.md) 참고.
