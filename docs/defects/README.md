# 缺陷与复测汇总

> 来源：独立 Test 会话 `docs/testing/T02-T11-report.md`（首轮 FAIL）与 `docs/testing/T02-T11-retest.md`（三轮复测）。以下 4 项均已关闭，但**不等于验收整体通过**，BLOCKED 项见 `docs/pr-description.md`。

| 编号 | 现象 | 修复提交 | 回归证据 | 状态 |
|---|---|---|---|---|
| D-T02-01 | `Idempotency-Key` 作用域只按 `(scope,key)`，跨用户复用会读到他人响应并静默丢单 | `4cbaf00`（`idem` 按 `:user:<id>` / `:staff:<id>` 隔离） | `internal/api/t02_retest_dt0201_test.go` 强用例、`docs/testing/T02-T11-retest.md` 第二轮 | 已关闭 |
| D-T02-02 | `scripts/smoke.ps1` 只杀 `go run` 外壳，残留 `server.exe` / 端口 / 临时文件，第二次冒烟可能命中旧实例 | `4cbaf00`、`9a641c8`（改用 `go build` 独立 exe、随机端口、按端口兜底清理） | 连续两次 `before=0 after=0`、`scripts/smoke.ps1` | 已关闭 |
| D-T02-03 | 被测工作区不干净，代码 ≠ 声明 SHA | `d315258`（规则包、迁移嵌入、测试证据入库） | `git status --porcelain` 空、复测报告 | 已关闭 |
| D-T02-04 | 同一用户同 key 跨不同预约路径回放首次响应 | `9a641c8`（把 `method + path` 纳入 `sha256` 请求指纹，第二次返回 409） | `internal/api/api_test.go` `TestIdempotencyScopePerRoute`、第三轮定向复测 | 已关闭 |

## 过程教训（已写入约束）

- 修复 D-T02-04 的初版把 path 直接加进 `scope`，导致 `TestRetestDT0201_IdempotencyKeyScopedPerUser` 失败；最终改为保持用户维度 scope、只把路由纳入请求指纹。此后关键提交前统一先跑 `go test ./... -count=1`。
- 三轮复测中 D-T02-02 曾在第二轮判 FAIL、D-T02-04 为新增登记，均在第三轮关闭；失败与重试均保留在 `docs/testing/T02-T11-retest.md`。
