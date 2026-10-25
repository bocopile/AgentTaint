# CI-01 — 원격 portable 검증 완료

2026-10-25. 상태: **verified**, 대상 commit은
`5777fcb4516183adfec33b57291c93977b396410`이다.

[GitHub Actions 실행](https://github.com/bocopile/AgentTaint/actions/runs/40201268334)의
head_sha와 완료 상태를 GitHub connector로 확인했고 각 job/step 결과를
[구조화 근거](evidence/ci01-5777fcb.json)에 기록했다.

| Job | job ID | make check | make race |
|---|---|---|---|
| portable (ubuntu-latest) | 121192753625 | success | success |
| portable (macos-latest) | 121192753816 | success | success |

해당 commit의 Makefile에서 `make check`는 gofmt 불일치 검사, `go vet ./...`,
`go build ./...`, `go test ./...`를 실행한다. `make race`는 `go test -race ./...`다.
workflow는 main push/PR에서 실행하며 contents:read, 고정 checkout/setup-go action
SHA, persist-credentials:false, cache:false를 사용한다. 이번 작업은 workflow를
수정하거나 특권 runner를 설치하지 않았다.

이 결과는 위 commit의 두 portable job 결과다. raw logs를 내려받아 개별 테스트의
skip 수를 감사한 것은 아니며, runner의 정확한 kernel/CPU를 별도 검증하지 않았다.
verifier/load/attach/pre-operation effect, 새 run-wide profile, amd64 BPF runtime은
이 결과로 보장하지 않는다. CI-02는 별도 미완료다.

상태 기록용 후속 문서 commit을 위 실행의 head로 바꿔 적지 않는다. 제품 코드가
변경되면 그 변경을 포함한 commit에서 필요한 검증을 다시 수행한다.
