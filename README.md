# 题目02 · 宠物门店预约经营 — `.agent` 约束包

这是为该 Go 场景题准备的**项目规则包**（不是业务代码）。复制到你的题库仓库根目录后，`AGENTS.md` 位于根目录、规则正文在 `.agent/`、两个自编 Skill 在 `skills/`。

## 目录

```
AGENTS.md                       # 入口 + 硬性约束（题目要求学生自行编写并实际使用的项目级约束）
.agent/
  01-requirements-rules.md      # R1–R8 规则编号 + 可断言条件 + 五组场景
  02-domain-model-db.md         # 实体、DDL、状态枚举、数据不变量 I1–I12、金额口径
  03-api-contract.md            # 接口清单、错误码、幂等、模拟渠道、页面要求
  04-permissions.md             # 角色矩阵、归属校验 P1–P7、越权测试
  05-state-machine-concurrency.md  # 状态迁移、教学时钟、名额/并发/退款设计、C1–C10
  06-testing-coverage.md        # ≥85% 覆盖率、测试矩阵、证据格式、作弊识别
  07-git-mcp-deploy.md          # MCP 建仓、分支、提交规范、meegle、PR 门禁、流水线、回滚
  08-agent-workflow.md          # Dev/Test/Acceptance 三会话隔离、交接门、返工回路
  09-skills.md                  # 两个自编 Skill 的规范与 skill-usage 要求
  10-evidence-ai-record.md      # 会话导出、events.jsonl、脱敏、视频时间点目录、交付清单
  11-acceptance-checklist.md    # 逐项验收表（E/R/S/P/Q/G/AI 全编号）
  12-plan-8h.md                 # 8 小时时间盒、任务编号 T02-Txx、风险
  13-acceptance-runbook.md      # 验收子代理执行手册（10 分钟命令清单/分诊/卡住处理/BLOCKED 口径）
  development/README.md         # 任务记录索引与五阶段看板
  templates/                    # 需求、任务记录、缺陷、验收报告、skill-usage 模板
docs/
  assumptions.md                # 4 项拍板决策 + 业务/环境假设（必须与实现一致）
  evidence/meegle/BLOCKED.md    # 工作项编号不可用时的 BLOCKED 记录模板
skills/
  slot-rule-guard/SKILL.md      # 业务专项：名额/状态/取消/退款/金额只读审计（含 SQL）
  acceptance-audit/SKILL.md     # 验收检查：题面逐项 + 证据链 + 覆盖率 + Git/部署
  README.md
```

## 怎么用

```powershell
# 1) 复制到你的题目仓库根目录（仓库由 GitHub MCP 创建）
Copy-Item -Recurse .\.agent, .\skills, .\AGENTS.md  <你的仓库路径>\

# 2) 先改三处再开工
#    - AGENTS.md：填姓名、初始提交 SHA
#    - .agent/02：确认数据库类型（MySQL/PostgreSQL）与 DDL 细节
#    - skills/*/SKILL.md：把 <学生姓名> 与占位命令改成实际环境
# 3) 创建 docs/ 目录结构（见 AGENTS.md 第 3 节）
# 4) 第一次提交只含约束与需求，再开始业务代码（保留“约束先于开发”的历史）
```

## 本轮拍板记录（2026-11-12）

| # | 决策 | 结果 |
|---|---|---|
| 1 | 数据库 | 继续 SQLite（按 02 允许条款写明替代方案） |
| 2 | 重构范围 | 先重构数据层（表/状态枚举），分层暂缓，之后机械迁入目标包结构 |
| 3 | 取消拒绝后 | 允许再次提交（覆盖 01 §5 A2，写入 assumptions） |
| 4 | 工作项编号 | 按 C/B/A 自检：meegle 可用→真实编号+核验；不可用→真实编号+BLOCKED；都没有→BLOCKED+占位声明 |

完整记录与理由见 [docs/assumptions.md](docs/assumptions.md)；meegle 阻塞模板见 [docs/evidence/meegle/BLOCKED.md](docs/evidence/meegle/BLOCKED.md)。

## 与考核要求的对应

| 考核项 | 本包落点 |
|---|---|
| 项目级 AGENTS.md（含职责/权限/门禁/反作弊/证据） | `AGENTS.md` |
| 需求分析、状态说明、假设清单 | `.agent/01`、`.agent/templates/requirement.md` |
| 任务规划与编号 | `.agent/12`、`.agent/development/README.md` |
| 单测 ≥85%、核心包 ≥85%、race、并发 | `.agent/06` |
| 三 Agent 会话隔离与交接 | `.agent/08` |
| 至少两个自编 Skill + 使用记录 | `skills/`、`.agent/09`、`templates/skill-usage.md` |
| 会话导出、事件清单、AI 使用说明、视频目录 | `.agent/10` |
| 逐项验收 | `.agent/11`、`templates/acceptance-report.md` |
| GitHub MCP、分支、提交规范、meegle、PR、自动部署 | `.agent/07` |

## 重要提醒

- 题目要求 **学生自行编写** 并**实际使用**这份约束，且要保留创建与修改历史。本包是**起点/参考稿**：请通读、按自己的理解修改、在开工前提交，并让后续每个阶段都能在会话记录中看到它被读取（不要完工后补写）。
- 本包不含业务代码与答案；`.agent/02` 的 DDL 与 SQL 是设计草稿，实施时以你的数据库与测试结果为准。
- 金额、时间、状态、接口一旦调整，同步更新 01/02/03/05/11 与测试用例，避免“文档一套、实现一套”。

## 目录外说明

本包生成的 `AGENTS.md` 已被当前会话识别为项目约束；实际提交时请把 `pet-store-booking/` 内的内容平铺到你的仓库根目录（即根目录下是 `AGENTS.md` 与 `.agent/`，而不是再套一层目录）。
