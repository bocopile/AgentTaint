# Graphify와 codebase-memory 비교 — GF02/GF03

2026-10-25. GF02: **제한된 동일 입력 비교 완료**. GF03: **실제 모델 비용은
inconclusive**. GF04: **현재 미채택**. 제품 구현이나 보안 보장의 검증이 아니다.

기준 source는 `3b8c9ea8442e181df72d01e83d74bb0af0b0ed51`이다.
[GF01 manifest](evidence/graphify-20261025.json)의 37개 파일을 새 임시 디렉터리에
동일 bytes로 복사했다. MCP fast index를 먼저 생성하고 Graphify AST-only graph를
생성했다. Graphify 출력은 초기 MCP 입력에 포함되지 않았다.
실험 근거는 [구조화 결과](evidence/graphify-comparison-20261025.json)에 보관한다.

## 비교 조건과 한계

- Graphify: PyPI 0.9.95, `GRAPHIFY_BACKEND=none`, `update --no-cluster`,
  query는 `--context call --budget 1000`으로 축소. 372 nodes / 1,044 edges.
- MCP: `mode=fast`, `persistence=false`, 526 nodes / 1,451 edges.
  index 응답은 `internal/env` 디렉터리를 제외했다고 보고했다.
  두 도구에 같은 파일을 제공했지만 추출 coverage는 같지 않다.
- 질문별 대표 심볼을 사람이 정한 뒤 exact symbol/관계 검색과 원문 대조를 했다.
  자연어 질문을 독립 LLM에 주고 답을 채점한 blind benchmark는 아니다.
- 응답 형식은 Graphify 관계 텍스트와 MCP 검색+snippet으로 다르다. 노드 수,
  wall time, raw response bytes만으로 우열·정확도·청구 비용 절감률을 계산하지 않는다.
- MCP snippet 12개 모두 보고된 line slice의 현재 bytes와 일치했다. 그러나
  Q09는 잘못된 선언 위치를 가리켰다. bytes 일치만으로 심볼 의미가 검증되지는 않는다.

## 질문별 정답 근거와 발견

아래 경로와 라인은 고정 source commit 기준이다. 답의 의미는 그래프 이름으로
추론하지 않고 원문에서 확인했다. 한 대표 심볼의 위치를 찾았다고 질문 전체에
답할 충분한 근거를 얻었다고 판정하지 않는다.

| ID / 질문 | 원문 기준 핵심 사실 | Graphify / MCP 결과와 추가 확인 |
|---|---|---|
| Q01 파일 접근 후 라벨 조건 | `internal/taint/engine.go:73`: 유효 process key, completed+success, 지정 경로이면 SECRET과 source path 추가 | 양쪽 위치 확인. graph edge만으로 completed/success 조건을 알 수 없어 원문 필수 |
| Q02 자식 전파 | `engine.go:88`: 같은 scope의 서로 다른 PID인지 확인한 뒤 부모 labels/source paths를 child에 복사 | 양쪽 위치 확인. data bytes가 실제 전달됐다는 증거는 아님 |
| Q03 정책 우선순위 | `internal/policy/evaluator.go:12`: IPv4 검사, no-match allow, rank와 동률 rule-name 처리. 상단 주석은 deny>audit>allow이며 정책 verdict만 반환 | 양쪽 위치 확인. Evaluate 관계 목록만으로 pre-op 차단을 주장하지 않음 |
| Q04 mode와 실제 집행 분리 | `internal/runner/observe.go:53`: requested/active/action/result/coverage를 분리하고 mode·policy·command 검사 후 platform 실행에 위임 | 양쪽 위치 확인. 이 함수만으로 BPF hook의 실제 effect를 증명하지 못함. runtime acceptance는 기존 별도 증거 |
| Q05 decoder | `internal/core/event.go:194`: JSON 검사·필수 metadata·event kind별 payload·process 검증으로 이어짐 | 양쪽 위치 확인. decoder 전체/호출 helper 원문을 함께 확인해야 모든 거부 조건 설명 가능 |
| Q06 CLI에서 runner | `cmd/agenttaint/main.go:22`의 run과 `internal/runner/runner.go:18`의 Run은 다른 함수 | Graphify exit=3으로 선택 요구; MCP 두 후보 반환. 대소문자 이름만으로 자동 선택하지 않음 |
| Q07 worker scope | `bpf/file.bpf.c:62`, `bpf/network.bpf.c:54`: tracked TID miss이고 TGID가 다르면 TGID fallback | 양쪽 위치 확인. Graphify는 context=call 적용 뒤에도 두 query에서 예산 초과 경고(자체 추정 1673/1626) |
| Q08 fork hook | `bpf/process.bpf.c:85`: BPF_PROG 매크로로 정의된 on_process_fork | Graphify 이름 node의 위치가 빈 문자열; MCP exact search 0건. 원문 fallback 필요 |
| Q09 raw file-open schema | `bpf/events.h:103`: header/process/path/access/reserved 필드의 struct | Graphify exact node 없음. MCP 검색은 있지만 snippet line152는 `_Static_assert`이고 end_line202는 파일 밖이다. 실제 정의는 103~109이므로 원문 fallback 필요 |
| Q10 incomplete 전달 | `internal/runner/policy_pipeline.go:29`가 engine으로 전달, `internal/taint/engine.go:178`은 complete=false와 reason 추가 | Graphify exit=3/두 위치, MCP 두 후보. 두 위치와 호출 관계를 구분해야 손실이 숨겨지지 않는 흐름을 설명 가능 |

Q01~Q10은 기존 평가의 10개 질문을 대표 진입점으로 구체화한 것이다. Q04의
커널 집행 경로 전체, Q05의 모든 decoder helper, Q06의 모든 CLI option,
Q10의 모든 손실 producer를 이번 symbol probe만으로 검증 완료 처리하지 않는다.
없는 심볼에 대한 broad keyword 반환은 GF01의 음성 대조군 결과를 유지한다.

## Freshness 실험

별도 합성 `probe.go`를 인덱싱한 뒤 `GF02Before`를 `GF02After`로 바꿨다.

| 단계 | Graphify | MCP |
|---|---|---|
| 갱신 전 검색 | 예전 GF02Before와 L3 반환 | 예전 GF02Before 검색 결과 반환 |
| 갱신 전 snippet | graph의 이름·라인은 예전 값 | **name=GF02Before인데 source 본문은 GF02After** 반환 |
| 명시 update/index 이후 | GF02After와 L3 반환 | exact search에서 GF02After만 반환 |

MCP 중간 재인덱싱 한 번에는 생성된 graph artifact가 포함되어 node 수가 늘었다.
이를 비교 결과에서 제외하고 graph 출력 디렉터리를 source 밖으로 이동한 뒤
재인덱싱했다. 최종 source 디렉터리에 probe.go 하나뿐임을 확인했다. 이 실험은
명시 refresh 뒤 해당 이름이 갱신되는지만 확인하며 전체 삭제/동시성/freshness를
증명하지 않는다. 자동 watcher를 설치하거나 공유 MCP를 중단하지 않았다.

따라서 `index ready`, 검색 성공, 최신 snippet bytes 각각을 서로 다른 근거로
취급해야 한다. source revision과 검색 메타데이터가 맞지 않으면 갱신 후 재조회하고,
정의 위치·본문을 대조해야 한다. graph에 없는 BPF macro/header는 좁은 원문
fallback을 유지한다. 기존 AGENTS의 원문 보존 원칙을 변경하지 않는다.

## 비용 판정과 도입 결정

실제 input/output/cache token 및 동일 작업의 재시도 비용을 읽는 usage API가
이번 도구 표면에 없으며 독립 동일 모델 A/B session을 실행하지 않았다.
GF03 결과는 **inconclusive**다. bytes/문자 추정으로 절감률을 대신하지 않는다.
이는 실제 비용 비교를 완료했다는 뜻이 아니라 현재 채택 판단에 필요한 비용
근거를 확보하지 못했다는 종료 판정이다.

GF04는 **미채택**으로 둔다. 기존 MCP를 기본으로 유지하고, Graphify의 수동
시각화는 필요할 때만 재평가한다. 자동 hook/전역 skill/두 개의 무조건적인
graph-first 규칙은 추가하지 않았다. BPF 누락과 잘못된 source attribution은
도구의 실제 한계이며 AgentTaint 보안 원문을 손실 압축할 이유가 되지 않는다.

TOOL-03 전체와 실제 transport outage 검증은 미완료다. 제품 정의 P02는 이
도구의 채택에 의존하지 않으므로 다음 순서로 진행할 수 있다.
