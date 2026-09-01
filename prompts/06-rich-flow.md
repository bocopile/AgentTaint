# Phase 6 — Rich Flow Propagation (스텁)

아직 상세화하지 않았다. `docs/SECURITY_MODEL.md`의 "v0.1이 다루지 않는
것" 표에 있는 propagation 확장(file write/read, pipe, unix domain
socket)과 다중 label(`CREDENTIAL`, `SOURCE_CODE`, `UNTRUSTED` 등)을
다룰 예정이다. Phase 3(Taint Engine)이 fork/exec 전파만으로 4개
시나리오를 통과한 뒤, 그 구현을 기준으로 상세화한다.

자세한 순서는 [../docs/ROADMAP.md](../docs/ROADMAP.md) 참고.
