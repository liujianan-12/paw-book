# 09 Skill 规范（本题至少两个自编 Skill）

题目要求：至少自行编写两个与本题相关的 Skill——**业务专项 Skill**（检查/辅助关键业务约束）+ **验收检查 Skill**（依据题面核对实现、异常、证据、部署）；每个 Skill 必须有：用途、触发场景、输入、执行步骤、输出格式、失败处理、完成标准；提交 Skill 文件、配置方式、实际调用记录；至少展示一次“检查发现问题 → 修正 → 重新检查”。

本包已提供两个可直接改用的骨架：
- [skills/slot-rule-guard/SKILL.md](../skills/slot-rule-guard/SKILL.md)（业务专项）
- [skills/acceptance-audit/SKILL.md](../skills/acceptance-audit/SKILL.md)（验收检查）

## 1. Skill 文件格式（强制）

```markdown
---
name: <kebab-case>
description: <一句话说明用途与触发时机>
version: 0.1.0
owner: <学生姓名>
---

# <名称>

## 用途
## 触发场景（什么时候必须调用）
## 输入（必须提供的文件/命令/环境）
## 执行步骤（编号、可复现、含具体 SQL/HTTP/命令）
## 输出格式（表格/JSON，字段固定）
## 失败处理（发现问题时登记缺陷、交回谁、如何复测）
## 完成标准（什么条件算检查通过）
## 已知局限（禁止夸大为“全自动保证”）
```

## 2. 调用与配置方式

- 工具支持 Skill 目录时，放在工具约定位置（如 `.claude/skills/<name>/SKILL.md` 或项目 `skills/` 并在 AGENTS.md 登记）；CLI 工具可用“读取该文件并按其步骤执行”的方式调用，但必须在会话记录里留下读取/调用证据。
- Skill 不允许隐藏“人工步骤”：需要人判断的地方写明“学生确认”。
- Skill 只能**检查/辅助**，不得替学生伪造结论；输出必须引用真实命令与文件。

## 3. 业务专项 Skill（slot-rule-guard）必须覆盖的检查

| 检查 | 方法 | 失败输出 |
|---|---|---|
| 名额账 | 对账 `reserved_count` 与占名额状态预约数（I1/I2） | 偏差明细（时段 ID、两值、差异） |
| 同宠物唯一 | 查 `(slot_id, pet_id)` 有效预约重复（I3） | 重复记录 ID 列表 |
| 状态机合法性 | 扫描非法状态组合/缺失时间戳（如 CONFIRMED 无 paid_at） | 违规预约与字段 |
| 取消政策 | 对取消记录校验时间差与处理方式（≥2h 自动、<2h 审核） | 不合规取消 ID |
| 待审期间不可核销 | 取消申请 PENDING 且存在核销记录 | 冲突记录 |
| 退款独立 | 已取消但无退款/退款金额不符/退款失败后预约恢复 | 异常退款 ID |
| 支付重试唯一 | 同一预约存在多条 SUCCESS 支付（I13） | 预约 ID 与流水 |
| 取消重提 | 同一时刻多条待审核申请（I14）；拒绝后历史是否保留 | 申请 ID 与时间 |
| 金额对账 | 已收款、已退款、未退款、已完成服务金额（I6/I7） | 四项金额与差异 |
| 关闭后支付 | 异常支付是否都有退款、预约是否被错误恢复（I10） | 异常 ID |

输出格式固定（示例）：

```json
{"check":"slot-invariant","date":"2026-11-12","pass":false,
 "violations":[{"rule":"I1","slotId":3,"reservedCount":3,"actual":2,"evidence":"sql#1"}],
 "commands":["..."],"artifact":"docs/evidence/skill/slot-guard-20261112.json"}
```

## 4. 验收检查 Skill（acceptance-audit）必须覆盖

- 题面角色/规则 R1–R8、五组场景 S1–S5、权限 P1–P7、并发 C1–C10 是否逐项有证据。
- 覆盖率原始文件与每包 ≥85% 判定；竞态检测；失败测试是否存在。
- 空壳/伪造检查：页面是否真调接口、是否有写死成功、模拟渠道是否可触发失败/超时/重复。
- Git/MCP/PR/部署/回滚证据链是否闭环。
- AI 过程证据：会话导出、事件 JSONL、AGENTS.md 历史、Skill 调用记录、视频时间点。
- 输出固定为 `.agent/templates/acceptance-report.md` 的表格：`要求编号 | 结论(PASS/FAIL/BLOCKED) | 证据位置 | 备注`。

## 5. 必须留下的调用证据

| 证据 | 位置 |
|---|---|
| Skill 文件与版本 | `skills/<name>/SKILL.md`（提交进入 Git） |
| 工具配置/启用方式 | `docs/evidence/skill/config.md` |
| 调用记录（时间、会话、输入、输出） | `docs/evidence/skill/<name>-<timestamp>.json/md` |
| 一次“发现→修正→复检” | `docs/evidence/skill/cycle.md`（问题、定位、修正提交、复检输出） |
| 汇总 | `skill-usage.md`（模板见下） |

## 6. skill-usage.md 表格（题目指定字段，缺一不可）

| 名称、来源与版本 | 使用位置 | 使用原因 | 使用过程 | 实际帮助 | 证据及局限 |
|---|---|---|---|---|---|
| slot-rule-guard v0.1（自编） | 开发、测试阶段，任务 T5/T7 | 名额/状态/金额多维对账容易漏，需要统一检查 | 提供日期与 DB 连接，执行 8 项 SQL 对账 | 发现 I1 偏差 1 条，修复提交 abc123 | 输出 `docs/evidence/skill/...`；对“未来时段”覆盖有限 |
| acceptance-audit v0.1（自编） | 验收阶段，验收会话 | 需要逐项对照题面，防止漏项 | 输入题面、提交 SHA、证据目录，产出逐项表格 | 发现缺少跨店核销用例 1 项，补测后通过 | 人工复核为主，不能替代教师验收 |

> 表中每行都必须能点到实际文件/提交；“提高效率”“保证质量”这类空话不算，不得编造节省时间或缺陷数据。未实际调用的 Skill 不得列。
