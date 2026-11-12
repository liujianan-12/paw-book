# PR 描述（由 Dev 会话起草，PR 由学生手动创建）

- 源分支：`dev/02-pet-booking`
- 目标分支：**`main`**（GitHub `origin` 的默认分支，`--symref` 已核验）
- 仓库：`origin` → `https://github.com/liujianan-12/paw-book.git`；`gitee` → `https://gitee.com/liujianan12/pet-spa.git`（保留双推）
- `main`：`a92e5cd79d1d43c761c3abbd0365e6ebe9d6576b`（`Initial commit`），**两边一致、尚未合并**
- dev 头：以 `git log dev/02-pet-booking` 为准（起草时 `0c7c1ef`）
- PR 状态：**尚未创建、尚未合并**；本会话未开 PR、未强推、未改远程默认分支
- 核验原件：`docs/evidence/git/github-repo-2026-11-12.md`

## 标题

`[feat] 题目02 宠物门店预约经营：R1–R8 业务闭环 + A1–A20 接口 + 四角色页面`

## 变更摘要

1. **数据层**：`migrations/001_init.sql` 建 17 张表；`payment.reservation_id` 去掉唯一约束，改用部分唯一索引 `uk_payment_success(reservation_id) WHERE status='SUCCESS'`，支持支付失败多次尝试、同预约最多一条成功（I13/R2.7）。SQLite 单写连接 + `BEGIN IMMEDIATE` + 条件更新替代行锁（D1）。
2. **业务层（R1–R8）**：时段发布与名额并发控制、5 分钟占位与超时释放、支付回调流水幂等、关闭后支付异常退款、2 小时取消分界与客服审核、退款独立于预约状态并可重试、核销/开始/完成幂等、未到店释放名额不退款、店长日清六项金额勾稽。
3. **分层**：按 D5 机械拆成 `internal/domain`、`internal/repo`、`internal/service`、`internal/api`。
4. **接口与页面**：A1–A20 路由、会话鉴权、角色/门店校验、`Idempotency-Key`（作用域按用户、指纹含路由）、`X-Mock-Signature` 验签；`internal/api/web/` 提供登录/用户/前台/服务/客服/店长页面，真实调用接口并展示失败原因。
5. **测试**：116 个测试函数，含 50 并发抢 3 名额、条件更新败者、触发器注入写失败回滚、API 全路由与越权/签名/幂等用例。
6. **自编 Skill**：`slot-rule-guard`（`checks.sql` + `cmd/audit` 只读审计）、`acceptance-audit`；调用与「发现→修正→复检」证据在 `docs/evidence/skill/`。

## 提交列表（截至 `0c7c1ef`，按时间倒序）

```
0c7c1ef [docs] [v0.1.1] 核验 main 默认分支与保护证据，更新 PR 描述、验收 G2 与 BLOCKED 清单
5155639 [docs] [v0.1.1] 归档 GitHub MCP 排查记录（BLOCKED，不写 PASS）
bafc059 [docs] [v0.1.1] 记录验收 SHA 与发布 SHA 不一致风险及合并后复跑清单
bd614e0 [docs] [v0.1.1] PR 描述目标分支改为 main，补齐 BLOCKED 清单与 SHA 绑定风险
ca7cc24 [docs] [v0.1.1] 归档 PR 草稿与推送验证证据
7376961 [docs] [v0.1.0] 初始化题目02项目约束与需求文档
9680d2c [test] [v0.1.2] #1001 T02-T11 三轮复测：D-T02-01/02/03/04 全部关闭
9a641c8 [fix] [v0.1.2] #1001 幂等指纹纳入路由(D-T02-04)、冒烟改用独立二进制并彻底清理子进程/端口(D-T02-02)
f54ba15 [fix] [v0.1.2] #1001 幂等作用域加入路由维度(D-T02-04)，冒烟脚本改用独立二进制并清理子进程/端口(D-T02-02)
d315258 [chore] [v0.1.2] #1001 规则包与迁移嵌入入库、补充测试会话用例与覆盖率证据，清理工作区
4cbaf00 [fix] [v0.1.2] #1001 修复幂等 key 未按用户隔离(D-T02-01)与冒烟脚本残留进程(D-T02-02)，补自编 Skill 调用证据
1fb9d09 [fix] [v0.1.2] #1001 教学时钟基准改为00:00Z，发布时段提示服务器时间并按服务器时间预填
2be6b97 [style] [v0.1.2] #1001 前端改为低饱和中性配色，去掉高亮红紫
470a4fb [fix] [v0.1.2] #1001 重做前端：共享样式/请求层、状态徽章与空态、Bearer 回退、模拟支付按钮
7a75cc6 [fix] [v0.1.2] #1001 页面未登录自动跳转登录并回跳，启动可选预置演示时段
e20f5e1 [feat] [v0.1.2] #1001 实现 A1-A20 接口、鉴权/幂等/签名与五角色页面（E3/Q6）
5f88fa2 [refactor] [v0.1.2] #1001 按 D5 机械拆分 biz 为 domain/repo/service（不改业务逻辑）
ca615a5 [feat] [v0.1.2] #1001 重构预约经营业务层（R1-R8）：支付多次尝试、取消审核、履约与日清
e8e81fa [feat] [v0.1.0] #1001 未到店标记与店长日清金额勾稽
764a857 [feat] [v0.1.0] #1001 核销履约、退款重试与并发唯一结果
5db55a4 [feat] [v0.1.0] #1001 取消政策与客服审核（提前自动退款、临期审核）
1eb7337 [feat] [v0.1.0] #1001 支付回调幂等、支付异常退款与全局超时扫描
53d60e6 [feat] [v0.1.0] #1001 时段名额与预约创建（含并发抢位）
54a14ba [feat] [v0.1.0] #1001 模拟支付渠道服务与行为注入
6ce9e4d [feat] [v0.1.0] #1001 业务状态常量与数据模型
95d737d [feat] [v0.1.0] #1001 项目骨架、配置与数据库初始化
3d6a1a3 [docs] [v0.1.0] 需求说明、原型与任务规划文档
```

> 所有 `#1001` 均为**占位编号、未核验**（`docs/evidence/meegle/BLOCKED.md`）；本轮按学生要求不补 Meegle。本文件所属提交及之后的合并提交未列入上表，以 `git log dev/02-pet-booking` 为准。

## 测试与覆盖率证据

- `go test ./... -count=1`：全绿（116 PASS / 0 FAIL / 0 SKIP）
- `gofmt -l .` 空、`go vet ./...` 通过
- 覆盖率：整体 86.8%；`domain 100.0%` / `repo 96.7%` / `service 85.2%` / `api 88.1%`
- 原始输出：`docs/evidence/tests/local/`、`docs/evidence/acceptance/9680d2c32f1473635d0d442b58088caf88ef6f20/`
- 覆盖率口径：`docs/coverage-report.md`；缺陷与复测：`docs/defects/README.md`、`docs/testing/T02-T11-retest.md`
- 业务冒烟：`scripts/smoke.ps1`；Skill 只读审计：`docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json`
- 以上**不代表 `main` 合并提交的复跑结果**，见「发布 SHA 绑定风险」。
- `-race`：本机无 gcc、`CGO_ENABLED=0`，本轮再次尝试失败，记录见 `docs/evidence/git/race-attempt-2026-11-12.md`；仍 BLOCKED。

## 验收报告

- `docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md`
- 结论：**PASS 73 / FAIL 0 / BLOCKED 24**（BLOCKED 不等于 PASS）
- 报告 §6 有 Dev 会话修订记录（G2 证据列、本轮补齐索引）；**独立验收 Agent 未复验，结论与统计未调整**。

## `main` 默认分支与保护规则证据（G2，仍 BLOCKED）

- GitHub `origin`：`git ls-remote --symref origin HEAD` → `ref: refs/heads/main HEAD`，**默认分支 `main` 已核验**。
- Gitee `gitee`：`git ls-remote --symref gitee HEAD` → `ref: refs/heads/master HEAD`；`git remote show origin`（历史核验）→ `HEAD branch: master`。两个远程默认分支不一致。
- 两边 `main` 均为 `a92e5cd Initial commit`；`git pull origin main --ff-only` → `Already up to date.`。
- **保护规则读取结果仍缺**：`docs/evidence/git/main-protection.png` `Test-Path` = MISSING（工作区/桌面/下载/图片 0 命中）；路径已按学生指令写入本文与验收报告 G2 证据列，但标注「待落盘」，不作为已存在证据。
- 结论：**G2 保持 BLOCKED，不写 PASS**。

## 合并前置风险（无共同祖先）

- `a92e5cd Initial commit` 与 `dev/02-pet-booking` **无共同祖先**（`git merge-base` 退出码 1）。PR `dev/02-pet-booking → main` 是两条无关历史，网页端可能提示「无法自动合并」。
- 截至本轮两边 `main` 仍为 `a92e5cd`，**尚未合并**；本会话未执行 `merge --allow-unrelated-histories`、未强推、未 rebase、未改写历史、未开 PR。

## 发布 SHA 绑定风险（验收 SHA ≠ 发布 SHA）

- 验收报告绑定 `9680d2c32f1473635d0d442b58088caf88ef6f20`；合并提交 SHA **尚不存在**。
- **验收 SHA ≠ 发布 SHA**：按 `AGENTS.md §4.7` 合并门禁当前不成立。合并后须在 `main` 合并提交上重跑 `gofmt -l .` / `go vet ./...` / `go test ./...` / 覆盖率（四包 ≥85%），归档到 `docs/evidence/tests/<合并SHA>/`，并由独立验收会话绑定新 SHA。
- 本轮未创建 `<合并SHA>` 目录、未伪造复跑输出、未改验收报告统计与绑定 SHA。详见 `docs/evidence/tests/sha-binding-risk.md`。

## 本轮补齐清单（2026-11-12，除 Meegle 外）

| 补齐项 | 文件 | 口径 |
|---|---|---|
| GitHub 仓库与双远程核验 | `docs/evidence/git/github-repo-2026-11-12.md` | 真实命令输出；非 MCP 建仓 |
| `-race` 尝试记录 | `docs/evidence/git/race-attempt-2026-11-12.md` | 仍 BLOCKED，含学生可自行补齐的命令 |
| AI 使用说明（AI6） | `docs/ai-usage.md` | 一页，基于真实会话事实 |
| 会话导出与事件清单（AI5） | `docs/sessions/README.md`、`08-git-收尾-dev.md`、`events.jsonl` | **部分补齐**，非平台原生全量导出 |
| 提示词 / 脱敏 / 缺失范围 / 视频时间点模板 | `docs/evidence/ai/*.md` | 视频未录制，时间点不预填 |
| 部署与回滚说明、CI 配置 | `deploy/README.md`、`deploy/rollback.md`、`.github/workflows/ci.yml`、`docs/evidence/deploy/README.md` | 仅配置与草案，**未运行**，G6–G8 仍 BLOCKED |
| 需求 / 任务 / 原型 / 缺陷索引 | `docs/requirements.md`、`docs/tasks.md`、`docs/prototype/README.md`、`docs/defects/README.md` | 索引文件，不冒充正文或原始记录 |

## BLOCKED / 未满足项（不得写成已完成）

1. **GitHub MCP 建仓（G1）**：仓库由学生手动创建 + `git push`，非 MCP 建仓；三次工具检索 0 结果、未读取 token，见 `docs/evidence/git/mcp-github-BLOCKED.md`。**学生明确要求保持 BLOCKED，不写 PASS**。
2. **Meegle / 飞书编号（G3）**：学生本轮明确排除；`#1001` 占位未核验，见 `docs/evidence/meegle/BLOCKED.md`。
3. **`main` 保护规则（G2）**：截图未落盘、Gitee 默认分支仍 `master`，见专节。
4. **PR / 合并 / 主分支复跑（G4 / G5）**：两边 `main` 仍 `a92e5cd`，无合并提交。
5. **教师部署 / 流水线运行 / 回滚验证 / 上线冒烟（G6–G8）**：环境未提供；仅有配置与说明，见 `deploy/README.md`。
6. **`-race`（Q4 / Q7）**：无 gcc、`CGO_ENABLED=0`、下载源不通，见 `docs/evidence/git/race-attempt-2026-11-12.md`。
7. **S1–S5 录屏 / AI7 视频**：未录制，`docs/evidence/ai/video-timeline.md` 为待填模板。
8. **AI2 / AI5 会话级证据**：三会话隔离无会话导出证明；`docs/sessions/` 为部分补齐，见 `docs/evidence/ai/missing-scope.md`。
9. **其它**：`deploy/` 无运行记录；模拟支付渠道仅教学演示，不是真实第三方支付；无班级群提交记录。

## BLOCKED 清单最新状态（本轮关闭：**0 项改为 PASS**）

本轮补齐的是**配置文件、说明、索引与尝试记录**，不是验收项的实测结果；按 `.agent/13` 与 `AGENTS.md §2.4`，BLOCKED 不因新增文档自动转 PASS，且独立验收 Agent 未复验。逐项原因与上表「BLOCKED / 未满足项」一致，证据路径已就地给出。唯一可独立确认的事实是「GitHub 默认分支为 `main`」，但 G1（MCP 建仓）与 G2（保护规则）要求的是别的证据，因此均不能关闭。

## 自检

- 未把任何令牌写入命令、文件、提交、日志或聊天；`remote` URL 不含 `user:口令@` 形式；GitHub MCP 的 env 值全程未读取、未打印。
- 新增文件均为文档 / CI 配置 / `.gitattributes`，未改业务代码、未新增 Go 依赖、未改表结构或接口契约、未放宽任何业务规则。
- 本机 `core.autocrlf=true` 曾使 `gofmt -l .` 列出 44 个 `.go`（CRLF 假阳性）；已用 `.gitattributes` 固定 LF 并 renormalize，`stagedGoFiles=0`、`gofmt -l .` 归零，详见 `docs/evidence/tests/gofmt-eol-2026-11-12.md`。
- 未开 PR、未合并、未强推、未改远程默认分支；工作区在提交后保持干净。
- 验收报告仅由 Dev 会话补充 §6 修订索引；结论、统计、绑定 SHA 均未改动，独立验收 Agent 未复验。
