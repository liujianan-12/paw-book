# 13 验收 Agent 执行手册（限时、可复现、不拖慢开发）

> 适用：派发「独立验收会话」时，把本文件作为唯一执行依据。默认时间盒 **10 分钟**；只做门禁与只读核对，**不启动服务、不建库、不跑 HTTP 实测**（运行时冒烟属于开发阶段的 E2E 任务）。
> 依据：`.agent/11-acceptance-checklist.md`（逐项表）、`AGENTS.md`（门禁）。验收对象必须是**固定提交 SHA**，不是工作区。

## 0. 基本原则

- 只读：唯一允许新建的文件是 `docs/acceptance/acceptance-report-<sha>.md`（证据输出可放 `docs/evidence/acceptance/<sha>/`）。
- 缺证据 = `FAIL` 或 `BLOCKED`，**不得写 PASS**；不写“整体看起来没问题/建议优化”。
- 同一命令最多重试 1 次；单条命令超时 180 秒即终止并记 BLOCKED，不死等。
- 总时限到点先落报告骨架（统计 + 已知结论 + 待验证清单），再补细节。
- 完成后确认 `git status` 只有报告/证据新增，无任何代码改动。

## 1. 分两段执行，不要一条长命令链

### 验收 A：门禁（5–8 分钟，自己跑、贴真实输出）

```powershell
$sha = git rev-parse HEAD
git status --porcelain                      # 期望为空（报告/证据目录除外）
gofmt -l .                                  # 期望无输出
go vet ./...                                # 期望 exit 0
go test ./... -count=1                      # 期望 exit 0
go test ./... -count=1 -coverprofile=coverage.out
go tool cover -func=coverage.out | Select-String 'total:'
# 逐包 ≥85%（按拆分后的目标包；未拆分则按实际核心包并在备注说明）
go test ./internal/domain/... ./internal/service/... ./internal/repo/... ./internal/api/... -cover
# race：只尝试一次；无 gcc 环境直接记 BLOCKED，不要修环境
go test -race ./... -count=1
```

归档到 `docs/evidence/acceptance/<sha>/`：

```
commands.txt        # 实际命令 + 时间 + 退出码
test-output.txt     # go test 原始输出
coverage.out / coverage-func.txt / per-package.txt
race.txt / race-env.txt   # 失败输出与环境（gcc 不存在即 BLOCKED）
```

### 验收 B：规则与证据（5–8 分钟，只读）

1. 读 `docs/requirements.md`、`docs/assumptions.md`、`.agent/01`、`.agent/11`、`docs/acceptance/test-report-<sha>.md`（若有）。
2. 用**测试名**对照规则编号（R/P/C/I/S）：`Select-String -Path ./**/*_test.go -Pattern 'func Test'`，找不到对应用例 → 记 FAIL（缺证据）。
3. 抽读关键分支代码：状态迁移条件、名额一次性释放、部分唯一索引、幂等判断、权限归属校验。
4. 页面/录屏/视频只核对文件存在与索引，**不启动浏览器**；JSONL/会话导出/`skill-usage` 只核对文件与字段。
5. 不做 HTTP 实测；确需运行时验证时另开任务，且不计入验收时限。

## 2. 验收 Agent 禁止事项

- 不启动服务、不执行迁移建库、不写业务数据（临时库也不行）。
- 不重复开发已做过的命令：先看 `docs/evidence/tests/<sha>/`，缺什么补什么。
- 不修改代码、测试、需求、证据；不代开发修复。
- 不把 `BLOCKED` 写成 `PASS`；不把“单元测试通过”扩展成“部署上线通过”。
- 不把“AGENTS.md 写了要求”当作“实现了要求”。

## 3. 卡住 / 超时 / 残留处理

| 现象 | 处理 |
|---|---|
| 单条命令 >180 秒无输出 | 终止；记 BLOCKED（命令、等待时长、最后输出） |
| 总时限到 | 立即写报告骨架：统计 + 已完成结论 + 待验证清单；不再补跑 |
| 端口/进程残留（如 `acc.exe`、18200） | 结束进程并记录；不做业务数据操作 |
| 发现 `git status` 有代码改动 | 立即停止验收，报告“验收环境不干净”，交回开发处理 |
| 网络/依赖缺失 | 记 BLOCKED + 尝试记录，不修环境 |

## 4. 报告格式与统计口径

路径：`docs/acceptance/acceptance-report-<sha>.md`；表格列固定：

```markdown
| 要求编号 | 结论(PASS/FAIL/BLOCKED) | 证据位置 | 备注 |
|---|---|---|---|
| R1.1 | PASS | docs/evidence/acceptance/<sha>/test-output.txt:42 | |
```

末尾统计：

- PASS / FAIL / BLOCKED 数量。
- 阻塞分三组列清单：
  1. **环境类**：`-race`（无 gcc/MinGW）、MCP 建仓、meegle、PR、CI、部署、浏览器/视频；
  2. **代码类**：缺路由、缺页面、缺测试、覆盖率不足、状态机分支缺失；
  3. **证据类**：会话导出、`events.jsonl`、`skill-usage.md`、视频时间点目录。
- 最后回复只给：统计、最关键的 3 个问题、报告路径、未验证范围。

## 5. 快速分诊（先查这三处，能定位大半问题）

1. **路由存在性**：`Select-String -Path ./internal/api/server.go -Pattern 'HandleFunc'` 对照 A1–A20。
2. **测试覆盖规则**：`Select-String -Path ./internal -Include *_test.go -Recurse -Pattern 'func Test'` 对照 R/P/C。
3. **数据约束**：读 `migrations/*.sql` + service 层 SQL，缺部分唯一索引/条件更新 → 直接 FAIL。

## 6. 10 分钟时间盒

| 分钟 | 动作 |
|---|---|
| 0–2 | 锁 SHA、`git status`、`gofmt`、`vet` |
| 2–5 | `go test` + 覆盖率 + 逐包判定 |
| 5–6 | `-race` 尝试一次 + 归档 |
| 6–8 | 规则↔测试名对照、抽 3 个关键分支代码 |
| 8–10 | 写报告、统计、清理确认 |

## 7. 最低证据清单（缺任一即不得结论 PASS）

- 门禁原始输出（gofmt/vet/test/覆盖率逐包）与退出码；
- 规则 R1–R8 中每条至少一个测试名或演示证据路径；
- 权限 P1–P7 至少一个越权用例；
- 并发 C1–C10（含唯一结果）至少一条原始输出；
- 页面/四角色证据（文件或录屏索引）；
- `-race` 结果或 BLOCKED 记录；
- 部署/流水线/Git 证据（不存在则 BLOCKED，不得写 PASS）。

## 8. 返工与复验

- FAIL → 回开发分支修复 → 新 PR/部署 → **新 SHA 重新验收**；旧报告保留，不覆盖。
- BLOCKED → 写明阻塞点、尝试命令、替代证据，并向教师索取环境；不得用 BLOCKED 掩盖未实现。
- 验收通过只代表“按本手册与 11 的结论为 PASS”，不替代教师独立验收。
