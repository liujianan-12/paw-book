---
name: acceptance-audit
description: 验收检查 Skill。依据题目原文逐项核对宠物门店预约项目的实现、异常路径、权限、覆盖率、Git/部署证据与 AI 过程记录，输出逐项 PASS/FAIL/BLOCKED，不修改任何代码。
version: 0.1.0
owner: <学生姓名>
---

# acceptance-audit（独立验收检查）

## 用途

在**独立验收会话**中，对某个确定版本（发布/PR 提交 SHA）做只读审查：把题面的角色、8 条规则、5 组场景、权限、质量门禁、Git/部署、AI/Skill 证据逐项映射到可验证证据，产出 `.agent/templates/acceptance-report.md` 全表。缺证据 = FAIL，不采信开发/测试会话的完成声明。

## 触发场景

- 功能与测试完成后、发起 PR 前（预验收）。
- PR 合并后、部署到教学环境后（发布验收，使用合并提交 SHA）。
- 修复任何 FAIL 项后的复验。

## 输入（必须固定版本）

1. 题目原文（完整题面，含共同要求、Git/提交规范、视频要求）。
2. 目标提交 SHA（PR 或 main 合并提交）。
3. 仓库路径、`AGENTS.md`、`.agent/11-acceptance-checklist.md`。
4. 证据目录：`docs/evidence/`、`docs/acceptance/`、`docs/sessions/`、`skills/`、CI 运行记录、部署地址与冒烟输出。
5. 教师提供的规则文件（commit-convention.md、MCP/meegle 要求）。

## 执行步骤

### 步骤 1：锁定版本与仓库状态

```bash
git fetch --all
git rev-parse <PR-SHA>
git log --oneline --decorate -20
git branch -a --contains <PR-SHA>
git status --porcelain          # 必须干净（验收对象=提交，不是本地未提交改动）
```

记录：目标 SHA、其所在分支、是否已在 main、是否存在未推送改动。任何“验收的是本地工作区”即 FAIL。

### 步骤 2：流程证据（G1–G5）

逐项打开并记录：MCP 建仓记录（工具/请求/结果/时间/URL）、`main` 保护读取结果、开发分支提交历史（是否分阶段、是否直接推 main）、meegle 原始输出（feat/fix 是否都有、编号是否匹配；不可用时是否按 `docs/assumptions.md` D4 记录 C/B/A 处理与 BLOCKED、占位编号是否声明）、PR 内容与检查结果、合并提交是否保留原始提交关系。

```bash
gh api repos/<owner>/<repo>/branches/main/protection > /tmp/prot.json || echo MISSING
git log --oneline main..dev/02-*        # 待合并提交
git log --merges --oneline -5 main
```

### 步骤 3：质量门禁（Q1–Q6）

```bash
gofmt -l . ; go vet ./... ; go test ./... -count=1 -coverprofile=coverage.out
go tool cover -func=coverage.out > coverage-func.txt
# 逐包解析：total 与 core 包（domain/service/repo/api）均需 ≥85%
go test -race ./... -count=1
go test -tags=integration ./... -count=1
```

检查：覆盖率原始文件与函数输出是否来自**同一 SHA**；排除清单是否只含生成/第三方代码；是否存在 `t.Skip`、注释断言、`_ = err` 等作弊迹象；并发用例是否真断言最终一致性、是否可重复（≥10 轮）。

### 步骤 4：业务逐项（R1–R8 / S1–S5 / P1–P7）

- 优先**自己执行**可执行的检查：调接口、跑冒烟脚本、SQL 对账（可复用 slot-rule-guard 的 SQL，但必须自己跑一遍，不能只看它的输出文件）。
- 页面：用真实浏览器走四角色操作，检查失败原因是否展示；确认页面请求真实接口（网络面板/服务日志），不是写死。
- 模拟渠道：亲自触发成功/失败/超时/重复通知/关闭后支付，核对库中结果。
- 并发：运行仓库并发测试并查看原始输出；不通过则 FAIL。
- 每条结论写“证据位置（文件:行/命令/时间点）+ 结论”，无证据即 FAIL。

### 步骤 5：Skill 与 AI 证据（AI1–AI7）

检查 `AGENTS.md` 提交时间早于业务代码、有版本历史、会话中确实读取过；三个会话是否独立（会话 ID/时间线）；Skill 是否 ≥2 且字段完整、是否真有调用记录与“发现→修正→复检”；`skill-usage.md` 六列是否逐项可溯源；会话导出与 `events.jsonl` 是否时间有序且含失败重试；视频是否有时间点目录且与交付版本一致。

### 步骤 6：部署与回滚（G6–G8）

打开流水线运行记录，核对：质量/覆盖率/构建/部署/健康/冒烟各阶段是否真实执行；部署镜像 digest 与提交 SHA 对应；访问服务地址跑冒烟（至少建时段→预约→支付→核销→完成→日清）；回滚说明是否可执行/已验证；不得把开发分支的旧成功记录当作主分支证据。

### 步骤 7：输出报告

按 `.agent/templates/acceptance-report.md` 填全表，结论只用 `PASS / FAIL / BLOCKED`，每个 FAIL 写清缺什么、回到哪个任务/分支。报告路径：`docs/acceptance/acceptance-report-<sha>.md`。

## 失败处理

1. 有 FAIL → 不签通过；列出最小修复集合，交回学生决定是否返工。
2. 返工必须回到开发分支，修复后重新走 PR 检查与**新的**验收（新 SHA），原 FAIL 报告保留。
3. BLOCKED（环境/权限缺失）必须记录已尝试的命令与输出；不得以“环境问题”掩盖未实现功能。
4. 本 Skill 全程只读：不得改代码、改测试、改证据、改需求。

## 完成标准

- 报告覆盖 `.agent/11` 全部编号，无空项、无“应该没问题”类结论。
- 每个 PASS 至少有 1 个可打开的证据位置；每个 FAIL 有可复现步骤。
- 报告与目标 SHA 绑定；部署版本与验收版本一致。
- 学生根据报告决定合并/返工，结论不是 Agent 自行宣布。

## 已知局限

- 人工/Agent 混合判断：视频讲解质量、业务合理性需教师最终确认。
- 无法获取平台隐藏指令或模型隐藏推理，按题目要求标注“不可获取”。
- 覆盖率数字可被精心构造的测试抬高，需结合“作弊迹象”人工复核（步骤 3）。
