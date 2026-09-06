# Phase 10 — Payload-aware LLM Sink (조건부, 스텁)

아직 상세화하지 않았다. **이 Phase는 실행이 확정된 게 아니라 조건부
스텁이다** — 필요성이 실제로 확인되기 전까지는 시작하지 않는다.

착수하려면 먼저 AGENTS.md의 "문서는 규범이다" 절차(충돌 식별 → 이유
설명 → 최소 변경안 제안 → 사람 확인)를 다시 밟아 사람의 확인을 받는다.
그 확인 없이 이 Phase를 시작하지 않는다.

방향성만 기록: `SSL_write`/`SSL_read` uprobe로 LLM API 요청의 목적지·
모델명 등 메타데이터를 Sink 판정에 사용하는 확장. 지켜야 할 제약은
`docs/SECURITY_MODEL.md`의 "(스텁) 향후 payload-aware LLM sink를
추가한다면" 절에 이미 기록되어 있다 — 가장 중요한 제약: Decision 근거는
여전히 메타데이터(목적지/모델명/크기)여야 하고, prompt/response 본문을
분류해서 그 결과를 Decision 근거로 쓰는 것은 이 Phase에서도 금지된다
(AGENTS.md Product Principle).

자세한 순서는 [../docs/ROADMAP.md](../docs/ROADMAP.md) 참고.
