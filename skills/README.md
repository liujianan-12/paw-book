# skills/（本题自编 Skill）

> 题目要求至少两个自编 Skill：业务专项 + 验收检查。两者都必须有：用途、触发场景、输入、执行步骤、输出格式、失败处理、完成标准，并保留实际调用记录。

| Skill | 类型 | 文件 | 版本 |
|---|---|---|---|
| `slot-rule-guard` | 业务专项：名额/状态/取消政策/退款/金额对账 | [SKILL.md](slot-rule-guard/SKILL.md) | 0.1.0 |
| `acceptance-audit` | 验收检查：题面逐项、证据链、覆盖率、Git/部署、AI 记录 | [SKILL.md](acceptance-audit/SKILL.md) | 0.1.0 |

## 配置与调用方式

- **支持 Skill 目录的工具**：把 `skills/<name>/` 放入工具约定目录（如 `.claude/skills/`），或在项目级配置中登记 `skills/`；在 AGENTS.md 中写明。
- **不支持 Skill 机制的工具**：在会话中明确“读取 `skills/<name>/SKILL.md` 并严格按步骤执行”，保留读取与执行记录（时间、输入、输出路径），效果等同。
- 调用记录统一放 `docs/evidence/skill/`：
  - `config.md`：工具名称、版本、配置位置、启用截图/文本。
  - `<skill>-<timestamp>.json|md`：输入、执行的命令、原始输出、结论。
  - `cycle.md`：一次“Skill 发现问题 → 指导修正 → 复检”的完整记录。

## 使用纪律

1. 只读检查：两个 Skill 都不直接改业务数据或生产代码；发现问题交回 Dev 会话。
2. 输出必须引用真实命令与文件路径；禁止手工编造 PASS 或覆盖率。
3. Skill 版本变化要改版本号，并在 `skill-usage.md` 记录版本与使用位置。
4. 未实际调用的 Skill 不得写进 `skill-usage.md`；调用失败/误判也要记录在“局限”中。
