# Graphify 격리 평가 — GF01

평가일: 2026-10-25 KST. 상태: **evaluated, not adopted**.
작업 큐: [EXECUTION_QUEUE.md](EXECUTION_QUEUE.md).
기계 판독 근거: [manifest·환경·조회 결과](evidence/graphify-20261025.json).

## 결론

Go 코드 관계 탐색과 HTML artifact 생성은 이번 제한된 corpus에서 동작했다.
그러나 BPF header의 심볼 누락, 매크로로 정의된 함수의 위치 누락, 조회 예산
초과가 확인됐다. 기존 codebase-memory-mcp를 대체하거나 Graphify를 유일한
코드 근거로 채택하지 않는다. 정확한 소스 확인을 유지한 보조 평가 후보로 둔다.

실제 LLM 토큰 절감률은 측정하지 않았다. 이 평가는 설치·AST 추출·심볼 조회의
smoke test이며, 동일 corpus/질문/모델을 사용한 end-to-end A/B가 아니다.
전체 TOOL-03이나 제품 아키텍처 재설계 완료를 의미하지 않는다.

## 범위와 재현 기준

| 항목 | 실제 사용 값 / 제한 |
|---|---|
| AgentTaint source | `3b8c9ea8442e181df72d01e83d74bb0af0b0ed51`의 tracked 파일만 `git show`로 복사 |
| corpus | `cmd`, `internal`, `bpf`의 `.go/.c/.h`와 `go.mod`, 37개 파일 / 186,036 bytes |
| 제외 | 테스트, 생성 bpfel/bpfeb Go 파일·object, `vmlinux.h`, 문서, 정책 파일, worktree/.git/사용자 설정 |
| Graphify 실행 배포본 | PyPI `graphifyy==0.9.95`; `pip check` PASS; 해석된 dependency 버전은 evidence에 기록 |
| 소스 검토 기준 | upstream `92741d162b2094ffc2fed58cfbd8ee823e9da62c`; PyPI wheel과 이 commit의 동일성은 미검증 |
| 실행 환경 | macOS arm64, Python 3.12.5; Linux/BPF runtime 실행 없음 |
| 설치 위치 | `/private/tmp/agenttaint-graphify-eval-20261025-01`의 전용 venv |
| 실행 환경변수 | `env -i PATH=/usr/bin:/bin GRAPHIFY_BACKEND=none`으로 실행, 모델 credential을 전달하지 않음 |
| 설치 경계 | 패키지 다운로드에 network approval 후 임시 venv만 사용. `graphify install`, skill/hook/MCP 등록은 실행하지 않음 |

추출은 다음 명령으로 실행했다. 모든 명령의 cwd는 임시 `corpus`다.

```sh
env -i PATH=/usr/bin:/bin GRAPHIFY_BACKEND=none \
  /private/tmp/agenttaint-graphify-eval-20261025-01/bin/graphify update . --no-cluster
env -i PATH=/usr/bin:/bin GRAPHIFY_BACKEND=none \
  /private/tmp/agenttaint-graphify-eval-20261025-01/bin/graphify query applyFileOpen --budget 1000
env -i PATH=/usr/bin:/bin GRAPHIFY_BACKEND=none \
  /private/tmp/agenttaint-graphify-eval-20261025-01/bin/graphify query applyFileOpen --context call --budget 1000
env -i PATH=/usr/bin:/bin GRAPHIFY_BACKEND=none \
  /private/tmp/agenttaint-graphify-eval-20261025-01/bin/graphify export html
```

저장된 manifest의 path/sha256는 동일 corpus 재구성 확인에 사용한다. 임시 venv와
출력 artifact는 재현 입력을 대신하는 영구 저장소가 아니다. dependency 목록은
실제 해석 결과이며 wheel hash까지 고정된 재현 설치 lock은 아니다.

## 실제 결과

| 검사 | 판정 | 관측 근거와 해석 |
|---|---|---|
| 고정 버전 설치 / dependency 검사 | PASS | venv 내부 설치, `pip check`: No broken requirements found |
| AST 추출 | 제한적 PASS | CLI exit=0, 372 nodes / 1,044 edges. 구문 경고와 심볼 누락이 있으므로 전체 정확도 PASS가 아님 |
| BPF map 정의 | FAIL: coverage | `bpf/maps.h` line 5 syntax error, 심볼 추출 없음. 원문에는 libbpf `__uint/__type`와 `SEC` map 선언이 있음 |
| BPF raw event header | FAIL: coverage | `bpf/events.h`는 file node만 존재. 실제 `agenttaint_raw_file_open`은 line 103에 있으나 해당 심볼 node 없음 |
| 매크로 함수 | FAIL: source attribution | 원문의 `BPF_PROG(on_process_fork, ...)`는 process.bpf.c line 85. graph에는 이름만 있고 source_file/source_location이 빈 node가 있음 |
| 주요 Go 심볼 | PASS: 위치 탐색 | Apply/applyFileOpen/applyFork/Evaluate/DecodeEvent의 소스 파일·위치를 반환. 전체 의미/모든 관계 검증은 아님 |
| Go 호출 관계 표본 | PASS: 3개 표본 | Apply→applyFileOpen, applyFileOpen→add, applyFork→SourcePaths를 고정 소스와 대조. 전체 1,044 edges의 precision으로 일반화하지 않음 |
| 동명 Run | 명시적 모호성 | `query Run` exit=3, main.run과 runner.Run 선택 요구. 실제 node id `internal_runner_runner_run`으로 seed 지정 시 성공 |
| 존재하지 않는 심볼 | 주의 | `agenttaint_raw_event`는 원문에 없는 음성 대조군인데 keyword 후보를 반환. exit=0/관련 노드 반환은 정확한 심볼 존재의 증거가 아님 |
| 조회 예산 | hard cap 아님 | Apply/applyFileOpen/applyFork/Evaluate에서 `--budget 1000`에도 각각 약 3718/2205/2219/1877 tokens라는 CLI 자체 경고 |
| 범위 축소 | PASS: 해당 사례 | applyFileOpen에 `--context call` 적용 시 7 nodes, stdout 1,593 bytes. 기본 query 7,080 bytes보다 작음. 실제 LLM 토큰 절감률은 아님 |
| HTML export | 생성 PASS / 화면 not_tested | graph.html 397,819 bytes 생성. 브라우저 표시·상호작용 검증은 수행하지 않음 |
| HTML 오프라인성 | 제한 발견 | 외부 script `https://unpkg.com/vis-network@9.1.6/standalone/umd/vis-network.min.js` 참조. 로컬 HTML 파일이라는 이유만으로 완전 offline이라 표현하지 않음 |
| 개발 설정 보존 | PASS: 작업 범위 | main tracked diff 없음, 제품 파일 수정 없음. 통합 installer/hook 명령을 실행하지 않음. 사용자 전체 home 변경 감사를 한 것은 아님 |

`--budget` 경고의 숫자는 Graphify 자체 추정치이며 모델 tokenizer나 청구 usage가 아니다.
DecodeEvent와 일부 BPF 조회는 별도로 `TRUNCATED`를 표시했다. 이 상태를 완전한
답변으로 간주하지 않는다. 수정 seed로 실행한 Run 조회도 예산 초과 경고를 냈다.

최초 MCP 대조에서 잘못된 `file_pattern` 표현으로 빈 결과가 나왔으며 범위
필터를 제거하고 정확한 이름으로 재조회했다. 초기 빈 결과는 MCP 실패율에
포함하지 않는다. seed ID도 처음 대소문자를 잘못 지정해 exit=4가 났고 실제
graph ID로 수정했다. 사용 오류와 추출 결함을 구분한다.

## 기존 MCP와의 비교 범위

현재 연결된 main 프로젝트 index에서 Apply/applyFileOpen/applyFork/Evaluate/
DecodeEvent/Run/file_current_process/network_current_process를 검색했다.
해당 경로를 찾았으며 applyFileOpen snippet은 현재 main 원문과 일치했다.
`on_process_fork` exact search는 빈 결과였다. 이 결과도 매크로 C 원문 fallback이
필요하다는 근거다.

MCP index는 전체 저장소를 대상으로 하고 이번 Graphify corpus는 생성물·테스트를
제외했다. 전체 index freshness도 검증하지 않았다. 따라서 노드 수, 응답 크기,
조회 횟수를 두 도구의 성능 우열이나 절감률로 비교하지 않는다. MCP에서 문서
section 노드도 관측되므로 문서 검색을 Graphify만의 기능이라고 단정하지 않는다.

## 상류 소스 검토에서 확인한 계약

- [ARCHITECTURE.md](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/ARCHITECTURE.md): 코드 AST와 문서 의미 추출의 구분, 관계 confidence, 그래프 표현과 export.
- [SECURITY.md](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/SECURITY.md): 의미 분석 시 configured backend로 내용 전송 가능. 프롬프트 주입 완전 방어는 보장하지 않음.
- [install.py](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/graphify/install.py)와 [agents-md block](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/graphify/always_on/agents-md.md): Codex 설치 시 AGENTS.md·hooks 설정 및 graph-first 지침 변경. 현재 프로젝트의 MCP 우선 계약과 조정 필요.
- [benchmark.py](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/graphify/benchmark.py): 전체 corpus와 부분 graph의 추정 token 비교. 문자/단어 기반 근사와 미매칭 질문 제외. 실제 end-to-end 모델 비용 비교가 아님.
- [README](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/README.md): 80% 절감은 Cloud 홍보 문구. 이번 OSS pilot의 결과로 사용하지 않음.
- [pyproject.toml](https://github.com/Graphify-Labs/graphify/blob/92741d162b2094ffc2fed58cfbd8ee823e9da62c/pyproject.toml): Python>=3.10, graphifyy 패키지, Apache-2.0 metadata. 이번 단계에서는 재배포·라이선스 통합 판단을 하지 않음.

## 다음 평가와 미검증 항목

GF02에서는 동일 소스 집합과 정답 근거를 고정한 다음 질문을 비교한다.

1. 성공한 FileOpen에서 라벨이 추가되는 조건은 무엇인가?
2. 부모에서 자식으로 어떤 상태가 전파되는가?
3. 정책 우선순위와 no-match 동작은 무엇인가?
4. 정책 verdict와 실제 pre-op enforcement는 어디서 분리되는가?
5. JSON decoder가 거부하는 입력은 무엇인가?
6. CLI에서 실행 runner까지 어떤 경로를 따르는가?
7. worker thread의 file/network scope lookup은 어떻게 동작하는가?
8. BPF fork hook의 소스 위치와 상태 변경은 무엇인가?
9. raw event header와 map 선언의 실제 필드는 무엇인가?
10. 손실이 발생했을 때 incomplete가 어디에 전달되는가?

정답의 중요한 조건을 빠뜨리지 않는지, 없는 심볼과 stale graph를 어떻게 처리하는지
검사한다. source 범위·LLM 모델·예산을 맞출 수 없는 경우 GF03 비용 비교는
`inconclusive`로 남긴다. 과도한 반복·새 계정·전역 usage 수집기는 자동 도입하지 않는다.

문서 의미 분석, graph update freshness, browser interaction, MCP server integration,
network packet capture, 공급망 전수 감사, Linux/BPF verifier·실제 효과, end-to-end
LLM 사용량은 이번 작업에서 `not_tested`다. `GRAPHIFY_BACKEND=none`과 credential
미전달은 실행 설정이며 무통신을 packet 수준에서 증명했다는 뜻이 아니다.

Graphify는 코드 관계를 탐색한다. 민감정보 접근을 새로 감지하거나 어떤 행위를
차단하지 않는다. byte-level taint tracking을 제공한다는 주장을 하지 않으며,
post-event detection을 prevention으로 부르지 않는다.
