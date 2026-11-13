# AI 使用说明（一页）

> 只写本会话中真实发生、可指向文件/命令/提交的事；未发生的审核与纠错不编造。

| 阶段 | AI 完成 | 学生完成 | 学生审核内容 | 有依据的纠错 |
|---|---|---|---|---|
| 需求与约束 | 草拟 `AGENTS.md` 与 `.agent/01`–`.agent/13` 规则包、需求/原型/任务文档 | 拍板 D1–D8（SQLite 替代口径、取消拒绝后可重提、`-race` 需 MinGW、SQLite 集成口径等） | 规则编号与题面映射、门禁是否过严 | 「取消拒绝后允许再次提交」覆盖 `.agent/01 §5` A2，写入 `docs/assumptions.md`，并改用部分唯一索引与回归测试 |
| 原型与开发 | 生成 R1–R8 实现、A1–A20 接口、四角色页面、116 个测试函数 | 提供仓库与凭据、确认 Gitee / GitHub 双远程 | 事务、并发、状态迁移、diff 逐段 | 独立 Test 会话发现 D-T02-01/02/03/04（幂等跨用户、冒烟残留、工作区脏、跨路由幂等），三轮复测关闭，见 `docs/testing/T02-T11-retest.md` |
| 测试与验收 | 跑 `gofmt` / `go vet` / `go test` / 覆盖率，起草独立验收报告 | 审核报告与原始输出是否一致 | 覆盖率四包 ≥85%、PASS/FAIL/BLOCKED 口径 | 验收子代理多次超时后，学生要求取消 HTTP 实测、限时 8 分钟、BLOCKED 不得写成 PASS（`.agent/13`） |
| Git 与部署 | 显式暂存、提交、推送、证据归档、PR 描述起草 | 手动建 Gitee / GitHub 仓库、授权 Git Credential Manager、设置默认分支 | 分支与默认分支、保护规则、远程 SHA | 学生要求「GitHub MCP 保持 BLOCKED，不写 PASS」；Dev 复核发现 Gitee `--symref` 仍为 `master`、保护截图未落盘，如实记 BLOCKED（`docs/pr-description.md`、验收报告 §6） |
| 过程证据 | 整理会话导出、`events.jsonl`、Skill 调用记录、部署与回滚说明 | 决定哪些环境项可豁免 | 证据是否指向文件/命令/时间/SHA | 学生指出 todo 状态停在 2/5，已修正；`git diff --cached --check` 拦下尾随空行，修复后才提交 |

## 明确未做、不得写成已完成

- 视频与 S1–S5 录屏、教师部署环境 / 流水线运行 / 上线冒烟、`-race`（无 gcc）、Meegle 编号核验、PR 合并与主分支复跑：均保持 BLOCKED，详见 `docs/pr-description.md`「BLOCKED 清单最新状态」。
- 模拟支付渠道仅用于教学演示，不是真实第三方支付接入。
- 本说明不统计「提示词数量」或「AI 生成比例」。

## 依据

`AGENTS.md`、`docs/assumptions.md`、`docs/testing/T02-T11-report.md`、`docs/testing/T02-T11-retest.md`、`docs/skill-usage.md`、`docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md`、`docs/evidence/git/`。
