# PR 描述（由 Dev 会话起草，PR 由学生手动在 Gitee 创建）

- 源分支：`dev/02-pet-booking`
- 目标分支：**`main`**（不是 `master`）
- `main` 基线：`a92e5cd79d1d43c761c3abbd0365e6ebe9d6576b`（`Initial commit`，由 `origin/master` 同名镜像建立）
- 起草时 dev 头：`ca7cc246378d7af4cd1e27b8955872aafb95e772`（`[docs] [v0.1.1] 归档 PR 草稿与推送验证证据`）
- 上一版推送提交：`7376961bbcebc60659c9bb3c87eb7a0a826f706f`
- 远程：`https://gitee.com/liujianan12/pet-spa.git`
- PR 状态：**尚未创建**。学生先在 Gitee 网页把默认分支改为 `main` 并保护 `main`；确认后再由其手动开 PR，本会话未开 PR。

## 标题

`[feat] 题目02 宠物门店预约经营：R1–R8 业务闭环 + A1–A20 接口 + 四角色页面`

## 变更摘要

1. **数据层**：`migrations/001_init.sql` 建 17 张表；`payment.reservation_id` 去掉唯一约束，改用部分唯一索引 `uk_payment_success(reservation_id) WHERE status='SUCCESS'`，支持支付失败多次尝试、同预约最多一条成功（I13/R2.7）。SQLite 单写连接 + `BEGIN IMMEDIATE` + 条件更新替代行锁（D1）。
2. **业务层（R1–R8）**：时段发布与名额并发控制、5 分钟占位与超时释放、支付回调流水幂等、关闭后支付异常退款、2 小时取消分界与客服审核、退款独立于预约状态并可重试、核销/开始/完成幂等、未到店释放名额不退款、店长日清六项金额勾稽。
3. **分层**：按 D5 机械拆成 `internal/domain`（状态与错误码）、`internal/repo`（事务/审计/时间）、`internal/service`（业务编排）、`internal/api`（HTTP）。
4. **接口与页面**：A1–A20 路由、会话鉴权、角色/门店校验、`Idempotency-Key`（作用域按用户、指纹含路由）、`X-Mock-Signature` 验签；`internal/api/web/` 提供登录/用户/前台/服务/客服/店长页面，真实调用接口并展示失败原因。
5. **测试**：116 个测试函数，含 50 并发抢 3 名额、条件更新败者、触发器注入写失败回滚、API 全路由与越权/签名/幂等用例。
6. **自编 Skill**：`slot-rule-guard`（`checks.sql` + `cmd/audit` 只读审计）、`acceptance-audit`；调用与「发现→修正→复检」证据在 `docs/evidence/skill/`。

## 提交列表（截至 `ca7cc24`，按时间倒序）

```
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

> 所有 `#1001` 均为**占位编号、未核验**：无飞书/Meegle 应用权限、无可用 CLI，`meegle workitem get` 未执行；不得当作真实工作项编号，见 `docs/evidence/meegle/BLOCKED.md` 与下方 BLOCKED 第 2 条。
> 本文件所属提交及之后的合并提交未列入上表，以 `git log dev/02-pet-booking` 为准。

## 测试与覆盖率证据

- `go test ./... -count=1`：全绿（116 PASS / 0 FAIL / 0 SKIP）
- `gofmt -l .` 空、`go vet ./...` 通过
- 覆盖率：整体 86.8%；`domain 100.0%` / `repo 96.7%` / `service 85.2%` / `api 88.1%`
- 原始输出：`docs/evidence/tests/local/`、`docs/evidence/acceptance/9680d2c32f1473635d0d442b58088caf88ef6f20/`
- 覆盖率口径与排除项：`docs/coverage-report.md`
- 独立测试与三轮复测：`docs/testing/T02-T11-report.md`、`docs/testing/T02-T11-retest.md`（D-T02-01/02/03/04 全部关闭）
- 业务冒烟：`scripts/smoke.ps1`（连续两次 PASS，无进程/端口残留）
- Skill 只读审计：`docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json`
- 以上为本地/验收绑定版本的门禁结果，**不代表 `main` 合并提交的复跑结果**，见「发布 SHA 绑定风险」。

## 验收报告

- `docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md`
- 结论：**PASS 73 / FAIL 0 / BLOCKED 24**（BLOCKED 不等于 PASS，合并门禁按 `AGENTS.md §4` 仍不成立）

## 合并前置风险（无共同祖先，必须先看）

- `origin/master` 是 Gitee 建仓自动生成的 `a92e5cd Initial commit`，与 `dev/02-pet-booking` **无共同祖先**：`git merge-base origin/master dev/02-pet-booking` 无输出、退出码 1。
- `main` 由 `origin/master` 直接建立，因此 PR `dev/02-pet-booking → main` 是两条无关历史；Gitee 网页端可能提示「无共同祖先 / 无法自动合并」或产生冲突。
- 本会话**未**执行 `merge --allow-unrelated-histories`、**未**强推、**未** rebase、**未**改写历史、**未**改远程默认分支、**未**开 PR。是否合并两条历史由学生在网页端决定并另行授权。
- `git branch main origin/master` 使本地 `main` 的 upstream 为 `origin/master`；当前 `origin/main` 与 `origin/master` 指向同一提交 `a92e5cd`。

## 发布 SHA 绑定风险（验收 SHA ≠ 发布 SHA）

- 独立验收报告绑定 SHA `9680d2c32f1473635d0d442b58088caf88ef6f20`；起草时 dev 头为 `ca7cc24`，`main` 基线为 `a92e5cd`，**合并后的发布 SHA 尚未产生**。
- **验收 SHA ≠ 发布 SHA**：合并前无法对「合并提交」重跑门禁，也无法让验收报告绑定一个还不存在的提交。按 `AGENTS.md §4.7`，合并门禁当前不成立。
- 待学生确认合并完成后，须在 `main` 的合并提交上重跑 `gofmt -l .` / `go vet ./...` / `go test ./...` / 覆盖率（四包 ≥85%），把新 SHA 写入 `docs/evidence/tests/<新SHA>/`，并让验收报告绑定新 SHA（或由独立验收会话出具复验附录）。
- 本轮**未**创建任何 `<新SHA>` 证据目录、**未**伪造复跑输出、**未**改动验收报告结论。

## BLOCKED / 未满足项（不得写成已完成）

1. **GitHub MCP 建仓**：`.agent/07 §1` 要求用 GitHub MCP 创建独立仓库并留原始响应；实际使用学生提供的 **Gitee 已有仓库** `pet-spa`，无 MCP 建仓证据。`main` 分支已按用户当前明确指令建立并推送（等同 `origin/master` 的 `a92e5cd`），但**默认分支切换与 `main` 保护规则**由学生在 Gitee 网页操作，尚无确认与证据。
2. **Meegle 工作项编号（无权限 / 无应用）**：`feat`/`fix` 提交中的 `#1001` 是**占位编号、未核验**；无飞书/Meegle 应用权限与可用 CLI，`meegle workitem get` 未执行。见 `docs/evidence/meegle/BLOCKED.md`，按 07 §4.1 属 BLOCKED，禁止伪装为真实编号。
3. **教师部署环境**：G6–G8 无流水线运行记录、镜像 digest、部署地址、健康检查与业务冒烟输出、回滚说明；本地 `go run` 与 `scripts/smoke.ps1` 不算自动部署。
4. **`-race` 无 gcc**：本机无 gcc/MinGW、`CGO_ENABLED=0`，`go test -race ./...` 无法执行；原始报错 `go: -race requires cgo` 在 `race.txt` / `race-env.txt`。无 race 结果即 `06` 门禁不合格。
5. **S1–S5 录屏**：视频文件与时间点目录缺失。
6. **AI 会话导出 / `events.jsonl` / 视频**：会话导出、`events.jsonl`、`docs/ai-usage.md`、Skill 调用事件清单、视频与 S1–S5 时间点目录缺失。
7. **PR / 合并 / 主分支复跑 / 部署**：PR 未创建、未合并；「合并后重跑门禁并自动部署」未发生，见「发布 SHA 绑定风险」。
8. **其它**：`deploy/` 目录不存在；模拟支付渠道仅用于教学演示，不是真实第三方支付接入。

## 自检

- 按用户当前明确指令执行 `git branch main origin/master` + `git push origin main`：仅把既有 `origin/master` 的 `a92e5cd` 镜像为 `main` 基线；**未把 dev 合并进 main、未强推、未改写历史、未改远程默认分支、未开 PR**。
- 未把任何令牌写入命令、文件、提交、日志或聊天；凭据仅由 Windows 凭据管理器提供，`remote` URL 不含 `user:token@`。
- 暂存仅用显式路径（`docs/pr-description.md`、`docs/evidence/git/push-7376961.txt`）；未发现 `.env*`、`*.local.yaml`、`temp/`、`*.exe`、`*.db` 或明文凭据。
- 严格凭据扫描：2 个待提交文件 0 命中；宽泛扫描的 1 处命中是提交标题中的功能描述「Bearer 回退」，非凭据值。
- 本轮 `git diff --cached --check` 通过（0 告警）。历史 3 处空白告警来自验收原始输出文件（`coverage-run.txt`、`race.txt`），为保留命令原样输出未做改动。
