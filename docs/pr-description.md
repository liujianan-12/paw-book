# PR 描述（由 Dev 会话起草，PR 由学生手动在 Gitee 创建）

- 源分支：`dev/02-pet-booking`
- 目标分支：Gitee 仓库默认分支为 `master`（题目/规则写的是 `main`，见「BLOCKED」）
- 推送提交：`7376961bbcebc60659c9bb3c87eb7a0a826f706f`
- 远程：`https://gitee.com/liujianan12/pet-spa.git`

## 标题

`[feat] 题目02 宠物门店预约经营：R1–R8 业务闭环 + A1–A20 接口 + 四角色页面`

## 变更摘要

1. **数据层**：`migrations/001_init.sql` 建 17 张表；`payment.reservation_id` 去掉唯一约束，改用部分唯一索引 `uk_payment_success(reservation_id) WHERE status='SUCCESS'`，支持支付失败多次尝试、同预约最多一条成功（I13/R2.7）。SQLite 单写连接 + `BEGIN IMMEDIATE` + 条件更新替代行锁（D1）。
2. **业务层（R1–R8）**：时段发布与名额并发控制、5 分钟占位与超时释放、支付回调流水幂等、关闭后支付异常退款、2 小时取消分界与客服审核、退款独立于预约状态并可重试、核销/开始/完成幂等、未到店释放名额不退款、店长日清六项金额勾稽。
3. **分层**：按 D5 机械拆成 `internal/domain`（状态与错误码）、`internal/repo`（事务/审计/时间）、`internal/service`（业务编排）、`internal/api`（HTTP）。
4. **接口与页面**：A1–A20 路由、会话鉴权、角色/门店校验、`Idempotency-Key`（作用域按用户、指纹含路由）、`X-Mock-Signature` 验签；`internal/api/web/` 提供登录/用户/前台/服务/客服/店长页面，真实调用接口并展示失败原因。
5. **测试**：116 个测试函数，含 50 并发抢 3 名额、条件更新败者、触发器注入写失败回滚、API 全路由与越权/签名/幂等用例。
6. **自编 Skill**：`slot-rule-guard`（`checks.sql` + `cmd/audit` 只读审计）、`acceptance-audit`；调用与「发现→修正→复检」证据在 `docs/evidence/skill/`。

## 提交列表（22 条，按时间倒序）

```
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
ca6155a [feat] [v0.1.2] #1001 重构预约经营业务层（R1-R8）：支付多次尝试、取消审核、履约与日清
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

> 所有 `#1001` 均为**占位编号**，未经过 meegle 核验，见 BLOCKED。

## 测试与覆盖率证据

- `go test ./... -count=1`：全绿（116 PASS / 0 FAIL / 0 SKIP）
- `gofmt -l .` 空、`go vet ./...` 通过
- 覆盖率：整体 86.8%；`domain 100.0%` / `repo 96.7%` / `service 85.2%` / `api 88.1%`
- 原始输出：`docs/evidence/tests/local/`、`docs/evidence/acceptance/9680d2c32f1473635d0d442b58088caf88ef6f20/`
- 覆盖率口径与排除项：`docs/coverage-report.md`
- 独立测试与三轮复测：`docs/testing/T02-T11-report.md`、`docs/testing/T02-T11-retest.md`（D-T02-01/02/03/04 全部关闭）
- 业务冒烟：`scripts/smoke.ps1`（连续两次 PASS，无进程/端口残留）
- Skill 只读审计：`docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json`

## 验收报告

- `docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md`
- 结论：**PASS 73 / FAIL 0 / BLOCKED 24**（BLOCKED 不等于 PASS，合并门禁按 `AGENTS.md §4` 仍不成立）

## BLOCKED / 未满足项（不得写成已完成）

1. **GitHub MCP 建仓**：题目 `.agent/07 §1` 要求用 GitHub MCP 创建独立仓库并留原始响应；实际使用的是学生提供的 **Gitee 已有仓库** `pet-spa`，无 MCP 建仓证据。远程默认分支为 `master`，不是规则要求的 `main`；`main` 保护规则读取结果亦缺失。已按用户当前指令执行，未伪造证据。
2. **Meegle 工作项编号**：`feat`/`fix` 提交中的 `#1001` 是占位编号；未拿到飞书/Meegle 真实编号，也未执行 `meegle workitem get` 核验。见 `docs/evidence/meegle/BLOCKED.md`。按 07 §4.1 属情形 A。
3. **教师部署环境 / 流水线 / 回滚 / 上线冒烟**：G6–G8 无流水线运行记录、镜像 digest、部署地址、健康检查与业务冒烟输出；本地 `go run` 与 `scripts/smoke.ps1` 不算自动部署。
4. **`-race`**：本机无 gcc/MinGW、`CGO_ENABLED=0`，`go test -race` 无法执行；原始报错在 `race.txt` / `race-env.txt`。
5. **PR/合并/主分支复跑**：PR 由学生手动创建；目前尚无合并提交，故「合并后重跑门禁并部署」未发生。
6. **AI 过程证据**：会话导出、`events.jsonl`、`docs/ai-usage.md`、视频与 S1–S5 时间点目录缺失。
7. **其它**：`deploy/` 目录不存在；模拟支付为教学演示渠道，不是真实第三方支付接入。

## 自检

- 未向 `main`/`master` 推送，未强推，未把任何令牌写入命令、文件、提交、日志或聊天。
- 暂存仅用显式路径；未发现 `.env*`、`*.local.yaml`、`temp/`、`*.exe`、`*.db` 或明文凭据。
- `git diff --cached --check` 的 3 处空白告警来自验收原始输出文件（`coverage-run.txt`、`race.txt`），为保留命令原样输出未做改动。
