# Skill 使用记录

> 只列实际调用过的 Skill；未调用不写。每条均可点到文件/命令/提交。

| 名称、来源与版本 | 使用位置 | 使用原因 | 使用过程 | 实际帮助 | 证据及局限 |
|---|---|---|---|---|---|
| slot-rule-guard v0.1.0（自编） | Dev 自测（R1–R8 完成后） | 名额、取消、退款、金额多维对账容易漏，需统一只读检查 | 读取 `skills/slot-rule-guard/SKILL.md`，执行 `go run ./cmd/audit -db <预览库> -date 2026-11-14 -store 1 -commit 1fb9d09` | 14 项检查全通过，确认演示库无 I1/I3/I8/I13/I6-I7 违规；同时暴露“空库全绿”的局限并在报告注明 | `docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json`、`config.md`、`cycle.md`；局限：不覆盖未实现分支、不模拟并发 |
| acceptance-audit v0.1.0（自编） | 待 T02-T13 验收会话 | 逐项对照题面与证据，防漏项 | 尚未执行 | — | 未实际调用，暂不列入有效记录 |
