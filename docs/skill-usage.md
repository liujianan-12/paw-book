# Skill 使用记录

> 只列实际调用过的 Skill；未调用不写。每条均可点到文件/命令/提交。

| 名称、来源与版本 | 使用位置 | 使用原因 | 使用过程 | 实际帮助 | 证据及局限 |
|---|---|---|---|---|---|
| slot-rule-guard v0.1.0（自编） | Dev 自测（R1–R8 完成后） | 名额、取消、退款、金额多维对账容易漏，需统一只读检查 | 读取 `skills/slot-rule-guard/SKILL.md`，执行 `go run ./cmd/audit -db <预览库> -date 2026-11-14 -store 1 -commit 1fb9d09` | 14 项检查全通过，确认演示库无 I1/I3/I8/I13/I6-I7 违规；同时暴露“空库全绿”的局限并在报告注明 | `docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json`、`config.md`、`cycle.md`；局限：不覆盖未实现分支、不模拟并发 |
| acceptance-audit v0.1.0（自编） | 预验收（PR 前，2026-11-12） | 逐项对照题面与证据，防漏项；合并前需要独立验收会话结论 | 在独立子代理会话（与开发会话上下文隔离、只读）中按 `skills/acceptance-audit/SKILL.md` 七步执行：版本锁定 6f92f4b → 本地复跑 gofmt/vet/go test/go test -race → 核对归档覆盖率与五组场景/冒烟/race 证据 → AGENTS.md §4 八项门禁逐项标注 → 反作弊 grep（t.Skip/_ = err 0 命中）→ 输出结论 | 无 FAIL 项；确认生产代码 0ee9aee..6f92f4b 零变更故测试证据仍有效；发现 2 项非阻塞整改（过时 race 注释、归档编码）并已修复 | 报告落盘 `docs/acceptance/acceptance-report-6f92f4bb659abc15ca2c7d58c8e75087bb787b0a.md`；局限：CI/部署/视频等平台与人工项 BLOCKED，合并后需以 main 合并提交复验（G5–G8） |
