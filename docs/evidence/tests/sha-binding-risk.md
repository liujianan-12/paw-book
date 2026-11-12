# 发布 SHA 绑定风险记录（合并前）

- 记录时间：2026-11-12
- 记录会话：Dev（Git 收尾）
- 起草时 dev 头：`bd614e04673280f44a01f2a60440102bcba53d20`（本文件所属提交之前的头，以 `git log dev/02-pet-booking` 为准）
- `main` / `master` 基线：`a92e5cd79d1d43c761c3abbd0365e6ebe9d6576b`（`Initial commit`）
- 独立验收报告绑定 SHA：`9680d2c32f1473635d0d442b58088caf88ef6f20`
- PR 状态：未创建、未合并（`main` 默认分支切换与保护由学生在 Gitee 网页操作）

## 结论：验收 SHA ≠ 发布 SHA

`main` 上的合并提交 SHA **尚不存在**，因此：

1. 现有验收报告与门禁原始输出绑定的是验收时的工作树 `9680d2c...`，不是发布提交。
2. 无法在「合并提交」上重跑 `gofmt` / `go vet` / `go test` / 覆盖率，也无法让验收报告绑定一个不存在的 SHA。
3. 按 `AGENTS.md §4.7`（主分支实际合并提交重新跑门禁并自动部署）与 §4 门禁清单，**合并门禁当前不成立**；此项结论为 **BLOCKED**，不是 PASS。

## 本轮明确未执行（不伪造证据）

- 未创建 `docs/evidence/tests/<新SHA>/` 目录（新 SHA 不存在，不占位）。
- 未生成合并提交的门禁输出、覆盖率文件或 `coverage.out`。
- 未修改 `docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md` 的结论或 SHA 绑定。
- 未执行 `go test -race ./...`（本机无 gcc/MinGW、CGO_ENABLED=0，继续 BLOCKED；原始报错见 `docs/evidence/acceptance/9680d2c32f1473635d0d442b58088caf88ef6f20/race.txt`）。

## 合并后待执行清单（需学生确认 PR 已合并）

1. `git fetch origin` → `git checkout main` → `git pull --ff-only`，取合并提交 SHA `<新SHA>`。
2. 在 `main` 的合并提交上重跑并归档到 `docs/evidence/tests/<新SHA>/`：
   - `gofmt -l .`（要求无输出）
   - `go vet ./...`
   - `go test ./... -count=1`
   - `go test ./internal/... -coverprofile=<新SHA>/coverage.out` + `go tool cover -func`（整体与 `domain` / `repo` / `service` / `api` 四包各 ≥85%）
   - `go test -race ./...`（若仍无 gcc 则继续 BLOCKED，不得写成 PASS）
3. 由独立验收会话出具绑定 `<新SHA>` 的复验附录，或重新出具 `docs/acceptance/acceptance-report-<新SHA>.md`；在此之前保留「验收 SHA ≠ 发布 SHA」标记。
4. 自动部署、健康检查、业务冒烟、部署版本与回滚说明（教师环境未提供，仍 BLOCKED）。

## 其它 BLOCKED 项

GitHub MCP 建仓、Meegle 编号（无权限 / 无应用，`#1001` 为占位编号）、教师部署环境、`-race` 无 gcc、S1–S5 录屏、AI 会话导出 / `events.jsonl` / 视频：见 `docs/pr-description.md`「BLOCKED / 未满足项」。
