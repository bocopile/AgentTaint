# Phase 7 — Kubernetes (스텁)

아직 상세화하지 않았다. AGENTS.md의 Scope Order 규칙에 따라, 로컬
Linux에서 information-flow 모델(Source→Label→Propagation→Sink→Decision)
이 완성되기 전에는 시작하지 않는다.

방향성만 기록: 동일한 `internal/sensor/linux` + `internal/taint` +
`internal/policy` 코어를 privileged DaemonSet으로 배포하고, cgroup id로
이벤트를 파드/네임스페이스에 매핑한다. 정책은 ConfigMap으로 시작해서
필요해지면 CRD + 컨트롤러로 승격한다 (`deploy/` 디렉터리 사용).

자세한 순서는 [../docs/ROADMAP.md](../docs/ROADMAP.md) 참고.
