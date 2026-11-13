# Skill 配置与调用方式

## 1. 项目内 Skill（非平台托管）

| Skill | 文件 | 启用方式 |
|---|---|---|
| slot-rule-guard | `skills/slot-rule-guard/SKILL.md` + `checks.sql` | 会话中显式读取该文件，按步骤执行 `go run ./cmd/audit ...` |
| acceptance-audit | `skills/acceptance-audit/SKILL.md` | 验收会话显式读取，按步骤逐项核对并填 `.agent/templates/acceptance-report.md` |

> 本项目未把 Skill 注册进 IDE 的全局 skill 目录；调用证据 = 会话中读取文件 + 实际执行命令 + 输出 JSON（`docs/evidence/skill/`）。

## 2. slot-rule-guard 实际命令

```powershell
go run ./cmd/audit -db <SQLite 文件> -date 2026-11-14 -store 1 `
  -commit <sha> -out docs/evidence/skill/slot-guard-<date>-<sha>.json
```

- 只读打开（`mode=ro`），不写业务表。
- 每条检查返回 0 行 = 通过；输出 `pass=false` 时进程退出码为 1。
- 检查集：`skills/slot-rule-guard/checks.sql`（I1/I3/I9/R3/I5/I8/I10/I13/I14/I6-I7）。

## 3. 已知限制

- 平台没有 Skill 注册表，无法提供“点击即用”证据；只有文件 + 命令 + 输出。
- 审计读取的是“已存在数据”，空库会全绿，必须配合测试数据或生产演示数据使用。
