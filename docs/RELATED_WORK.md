# Related Work

이 문서는 규범(normative)이 아니다 — [ARCHITECTURE.md](./ARCHITECTURE.md),
[SECURITY_MODEL.md](./SECURITY_MODEL.md), [THREAT_MODEL.md](./THREAT_MODEL.md),
[ROADMAP.md](./ROADMAP.md)와 달리 구현이 이 문서를 따라야 할 의무는 없다.
목적은 하나: 유사 철학의 오픈소스 프로젝트를 조사한 기록을 한 곳에
모아둬서, 다음에 같은 질문("이미 이런 게 있지 않나?")이 나올 때마다
매번 재조사하지 않게 하는 것이다. 별점은 조사 시점(2026-09) 스냅샷이며
시간이 지나면 갱신이 필요하다.

## AI 에이전트 관측/집행 전용

| 저장소 | Stars | 분류 | 비고 |
|---|---|---|---|
| eunomia-bpf/agentsight | - | Observability | TLS uprobe(SSL_write/read)로 평문 관찰. 차단 코드 전무. `skills/*.md`로 해석을 코어 밖에 분리하는 패턴 참고할 만함 |
| eunomia-bpf/ActPlane | - | LSM Enforcement | `bprm_check_security`/`file_*`/`socket_connect` LSM 훅. payload 인식 0. `te_effect_mode()`가 훅 타입→effect를 타입 레벨로 강제 |
| multikernel/sandlock | 398 | Access Control (AI 전용) | Landlock+seccomp-bpf. "confinement 자체가 prompt injection 대응"이라는 프레이밍 — AgentTaint AGENTS.md 논지와 수렴. Docker 대비 시작 시간(~5ms vs ~200ms) 등 정직한 트레이드오프 수치화 |
| GreyhavenHQ/greywall | 291 | Access Control (AI 전용) | Claude Code/Cursor/Codex가 SSH키/`.env`/GPG 접근 못 하게 하는 deny-list. `greywall`(강제)/`greywatch`(관찰) 모드 분리 — AgentTaint의 audit/deny 구분과 유사. 보호 자산 목록이 AgentTaint THREAT_MODEL.md와 거의 동일 |

(agentsight/ActPlane 별점은 이전 세션에서 기록 안 함 — 필요 시 갱신)

## 범용 eBPF 보안 관찰/집행

| 저장소 | Stars | Decision 정직성 | 비고 |
|---|---|---|---|
| falcosecurity/falco | 9,343 | 순수 detect | "monitoring and detection agent"로 자기규정, 차단 주장 없음 |
| cilium/tetragon | 4,986 | Override+Signal(SIGKILL)만 | **SIGKILL이 `write()` 도중 발생해도 데이터가 이미 써졌을 수 있다고 명시** — AgentTaint kill≠block 근거로 THREAT_MODEL.md에 인용됨 |
| aquasecurity/tracee | 4,610 | 순수 detect | Falco와 포지셔닝 유사 |
| kubearmor/KubeArmor | 2,610 | LSM(BPF-LSM/AppArmor/SELinux) 기반 실제 사전 차단 | `action: Allow/Audit/Block` — AgentTaint의 allow/audit/deny와 어휘 수렴. `fromSource`는 정적 부모 제약(AgentTaint의 동적 propagation과 대비) |

## LSM 샌드박싱 / Access Control 전용

| 저장소 | Stars | 메커니즘 | 비고 |
|---|---|---|---|
| google/gvisor | 19,238 | syscall 인터셉션(application kernel) | "What isn't X" 식 Non-goals 서술 톤 참고할 만 |
| evilsocket/opensnitch | 14,039 | 프로세스 단위 아웃바운드 방화벽 | 사용자 대화형 승인 모델 — AgentTaint와 UX 결이 다름 |
| containers/bubblewrap | 8,609 | namespace 기반 | — |
| netblue30/firejail | 7,626 | namespace+seccomp | — |
| Zouuup/landrun | 2,284 | Landlock(LSM) | `--best-effort` 커널 버전별 ABI degrade 처리 — AgentTaint 커널 5.13/5.8 임계값 문서화와 같은 문제의식 |

## Provenance / Information Flow Control (희소)

| 저장소 | Stars | 비고 |
|---|---|---|
| ashish-gehani/SPADE | 196 | 유일하게 근접한 실제 provenance 그래프(프로세스/파일/소켓 vertex, read/write/fork edge). **그러나 사후 포렌식 질의 도구이지 실시간 정책 집행이 아님** — AgentTaint처럼 "라벨이 붙는 순간 Decision"을 내리는 구조가 아니다. taint의 한계를 밝히는 정직성 문장도 없음(SECURITY_MODEL.md "Taint의 정확한 의미" 절이 더 명료) |
| CamFlow (Linux Provenance Module 계열) | ≤29 | 사실상 유지보수 중단. 각주로만 인용 |

**결론**: 조사 범위에서 실시간으로 라벨을 붙이고 프로세스 계보를 따라
전파해서 판정하는(AgentTaint의 IFC 모델과 동형인) 고스타 오픈소스는
없었다. Access control은 이미 성숙한 카테고리, AgentTaint의 IFC
추상화가 실제 차별점이라는 근거로 [SECURITY_MODEL.md](./SECURITY_MODEL.md)
"Access Control vs Information Flow Control" 절에서 인용한다.

## 대형 AI 샌드박스 (다른 완화 전략 — credential 격리/사전 노출 차단)

이 카테고리는 "OS effect를 관찰해서 사후 판정"하는 AgentTaint와 전략
자체가 다르다 — credential을 애초에 에이전트 실행 경계 안에 노출시키지
않는 예방적 아키텍처다. 직접 비교 대상은 아니지만 보완 전략으로 참고.

| 저장소 | Stars | 비고 |
|---|---|---|
| daytonaio/daytona | 71,782 | 별점 최상위지만 **2026-06부터 유지보수 중단, 개발이 private로 이전** — 별점과 지속가능성이 무관하다는 반례 |
| coder/coder | 14,381 | LLM API 키가 워크스페이스에 아예 안 들어가게 하는 중앙화 아키텍처 |
| e2b-dev/E2B | 13,684 | 클라우드 sandbox. **README에 threat model/non-goals가 사실상 없음** — AgentTaint 문서화 관행이 상대적으로 우수함을 보여주는 대조 사례 |
| superradcompany/microsandbox | 8,095 | microVM, "secrets that never enter the VM" |

## 종합

- **kill≠block**: 원칙 선언(AGENTS.md)에 그치지 않고 Tetragon의 구체적
  실패 사례로 뒷받침 가능해짐 → THREAT_MODEL.md에 반영됨.
- **IFC 차별점**: 생태계 전체가 access control에 편중되어 있다는 게
  실증됨 → SECURITY_MODEL.md에 반영됨.
- **문서화 관행**: e2b/daytona 대비 AgentTaint의 THREAT_MODEL.md
  Non-goals/막는 것-못 막는 것 구조가 이미 상대적으로 우수 — 별도 변경
  불필요, 현재 관행 유지가 권장됨.
- **어휘 수렴**: KubeArmor의 `Allow/Audit/Block`이 AgentTaint의
  `allow/audit/deny`와 사실상 같음 — 업계 표준에 부합한다는 확인일 뿐,
  변경 불필요.
