# 06 测试与覆盖率门禁

题目硬指标：**自编 Go 生产代码整体单元测试语句覆盖率 ≥85%，各核心业务包分别 ≥85%**；集成测试覆盖率不能代替单元测试；必须覆盖正常、边界、非法状态、重复操作、权限和指定异常路径；并发必须有可重复验证；代码必须通过 `gofmt`、`go vet`、`go test -race`。

## 0. 环境前置（2026-11-12 拍板：SQLite + Windows 需 MinGW）

- **数据库**：本项目使用 SQLite。集成测试连**临时文件库**（每个用例独立临时目录，WAL + `busy_timeout`，用 `t.TempDir()`），不得用共享库；并发测试在同一进程内用条件更新验证。
- **`-race` 需要 cgo + gcc**：Windows 本机必须装 MinGW-w64（推荐 WinLibs 便携版，解压后把 `bin` 加入 PATH），验证并归档：

```powershell
gcc --version
go env CGO_ENABLED          # 期望 1
go test -race ./... -count=1
```

- 归档 `docs/evidence/tests/<sha>/race-env.txt`（`gcc --version`、`go env CGO_ENABLED`、`go version` 原始输出）。**没有 race 结果 = 06 不合格**；装不上只能记 BLOCKED 并立即找教师，不能跳过。
- 若剩余时间不足以装 MinGW：至少先跑 `-race` 的单包关键并发测试，并在报告中如实写“未全量 race”；但这是降级，不是合规。

## 1. 分层与覆盖率归属

| 包 | 内容 | 覆盖率要求 |
|---|---|---|
| `internal/domain` | 状态机、取消政策、名额规则、金额计算（纯函数） | ≥85%（必须最高，目标 90%+） |
| `internal/service` | 编排、事务、并发控制、幂等 | ≥85% |
| `internal/repo` | SQL 访问（集成测试 + 单元测试可注入 DB） | ≥85% |
| `internal/api` | handler、鉴权、参数校验、错误码 | ≥85% |
| `cmd/mockpay` | 模拟渠道 | 不要求（显式列入排除并说明） |
| 生成代码/第三方 | 无 | 可排除，需列出 |

排除项必须在 `docs/coverage-report.md` 列明“文件/原因/是否教师允许”，不得为了达标排除自编 DB/HTTP 代码。

**本轮重构决策的影响（2026-11-12）**：先重构数据层、分层暂缓。数据稳定后必须把代码机械迁入 `internal/domain/service/repo/api`（行为不变、测试跟随），覆盖率按目标包统计；**不得以“分层未完成”为由降低 85% 或排除自编代码**。若当日时间不足，至少保证 `domain` 与 `service` 达标，并在 `docs/coverage-report.md` 写明差距、原因与是否经教师确认，由 Acceptance 判定是否可接受。

## 2. 测试分类与存放

- 单元测试：与被测文件同包 `_test.go`；纯逻辑不连 DB；时钟用 fake clock。
- 数据库/接口集成测试：**SQLite 临时库**（`t.TempDir()` 下独立文件，WAL + `busy_timeout`），执行真实事务与并发；同一包内即可运行，不引入 Docker/MySQL。慢用例可加 `//go:build integration` 标签由 CI 额外 job 运行；本机默认 `go test ./...` 也必须能跑通核心用例。
- 并发测试：`-race`，goroutine + `sync.WaitGroup`，循环 ≥10 轮；断言最终状态与记录数，不只断言“有一个成功”。
- 端到端冒烟：HTTP 模拟渠道 + 真实服务，覆盖 S1～S5 各至少一条主路径（详见 11）。

## 3. 命令与证据（原样归档，禁止改写输出）

```bash
gofmt -l .
go vet ./...
go test ./... -count=1 -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out -o coverage.html
go test -race ./... -count=1
go test -tags=integration ./... -count=1
```

证据目录（提交到仓库）：
```
docs/evidence/tests/<sha>/commands.txt      # 实际执行的命令、时间、版本
docs/evidence/tests/<sha>/test-output.txt   # 原始 stdout/stderr 与退出码
docs/evidence/tests/<sha>/coverage.out
docs/evidence/tests/<sha>/coverage.html
docs/evidence/tests/<sha>/coverage-func.txt # go tool cover -func 原始输出
docs/evidence/tests/<sha>/per-package.txt   # 每包覆盖率表 + 门禁判定
docs/evidence/tests/<sha>/race.txt
```

门禁脚本（可放 CI 或 `scripts/coverage-gate.sh`）：解析 `go tool cover -func` 的 total 与包级输出，任一项 <85% 退出非 0；不得只打印不失败。

## 4. 测试矩阵（必须全部有对应用例，命名引用规则号）

| 规则 | 用例要点 | 类型 |
|---|---|---|
| R1.1 | 非法起止时间、缺字段 | 单测 |
| R1.2 | 串行/并发同宠物同时段；取消后可再约；完成后不可再约 | 单测 + 并发 |
| R1.3 | 容量=3 的第 4 条；释放后新用户可约 | 单测 + 并发 |
| R1.4 | 时钟=start_at 时预约失败 | 单测 |
| R2.1 | 创建后 payExpireAt=+5m，名额+1 | 单测 |
| R2.2 | 4m59s 仍有效、5m 过期；重复扫描只释放一次 | 单测 + 并发 |
| R2.3 | 未付款取消释放名额、无退款 | 单测 |
| R2.4 | 支付成功确认、金额与快照一致 | 单测 |
| R2.5 | 同流水回调 ×10 幂等；不同载荷同 key → 冲突 | 单测 |
| R2.6 | 过期/取消后成功回调 → 异常+退款、状态不变 | 单测 + 集成 |
| R2.7 | 失败后重试成功（新流水、同预约仅一条 SUCCESS）；并发重复支付只有一条收款 | 单测 + 并发 |
| R3.1 | 2h 边界两侧（=2h 自动受理、2h-1s 转审核） | 单测 |
| R3.2 | 无原因拒绝；有原因转审核 | 单测 |
| R3.3 | 待审核期间核销拒绝、名额不减 | 单测 + 集成 |
| R4.1 | 批准后核销拒绝、无核销记录 | 集成 |
| R4.2 | 重复批准/重复取消只释放一次 | 并发 |
| R4.3 | 退款状态可查（成功/失败/重试次数） | 单测 |
| R4.4 | 失败后可重试成功；失败期间预约不可用 | 单测 + 集成 |
| R5.1 | 跨店/未付款/已取消/待审取消四种核销拒绝 | 单测 |
| R5.2 | 未核销不能开始；开始后不能完成越级 | 单测 |
| R5.3 | 重复核销/完成幂等，收入只记一次 | 并发 |
| R6.1 | 核销 vs 取消并发 ×10 轮唯一结果 | 并发 |
| R6.2 | 已核销/服务中/已完成取消均拒绝、无退款单 | 单测 |
| R7.1 | 时段未结束/无原因拒绝；结束后成功 | 单测 |
| R7.2 | 未到店不产生退款、金额保留 | 单测 |
| R7.3 | 用户可见未到店与原因 | 单测/接口 |
| R8.1–8.3 | 日清五类计数、三金额、下钻；已付款≠已完成 | 集成 + 接口 |
| P1–P7 | 越权矩阵（04） | 单测/接口 |
| I1–I12 | 不变量对账（02） | 集成 |

## 5. 缺陷闭环

- Test Agent 发现缺陷 → `.agent/development/defects/<编号>.md`（模板见 templates/defect-record.md）→ Dev 修复 → 记录修复提交 → Test 复测 → 关闭。
- 修复后必须重跑**受影响包 + 全量**，并给出修复前后输出对比；不得只复测单条用例就宣布通过。
- 覆盖率下降或新增未覆盖分支的修复，必须补测试。

## 6. 覆盖率常见作弊识别（Test Agent/验收 Agent 必查）

- 大量 `_ = resp` / 只断状态码不看业务字段；断言被注释；`if err != nil { t.Skip() }`。
- 用 `httptest` 打桩替代核心逻辑，导致 domain 覆盖虚高但业务未验证。
- 并发测试只起 goroutine 不校验最终一致性；或固定 sleep 代替同步。
- 排除整个 `internal/repo` 或 `internal/api` 达成 85%。
- 测试文件与生产逻辑同源复制断言值（自证）。

发现以上任何一条，记为 P0/P1 缺陷，修复前不得合并。
