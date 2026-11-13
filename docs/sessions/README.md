# 会话导出目录（范围声明）

> **这不是平台原生全量导出。** 本目录由 Dev 会话基于本会话**可见**的用户输入、Agent 回复、工具调用与命令输出整理，按 `.agent/10 §2` 格式记录；无法导出的范围见 `docs/evidence/ai/missing-scope.md`。不得把本目录说成「全部可见会话已导出」。

| 文件 | 内容 | 状态 |
|---|---|---|
| `08-git-收尾-dev.md` | Dev 会话：Git 收尾、GitHub/Gitee 双远程、MCP 排查、验收报告修订、证据补齐 | 基于本会话真实记录 |
| `events.jsonl` | 结构化事件，按真实提交时间升序；`result` 为 `ok` / `fail` / `blocked` | 基于本会话真实命令与提交 |
| `README.md` | 本文件，范围与口径声明 | — |

## 缺失的阶段文件

按 `.agent/10 §1` 应有 `01-需求分析.md`、`02-原型设计.md`、`03-任务规划.md`、`04-开发-dev.md`、`05-测试-test.md`、`06-验收-acceptance.md`、`07-修复返工.md`。这些阶段的**原始会话内容不在当前 Dev 会话上下文中**，本会话没有拿到平台导出，因此不生成同名文件冒充。可替代的阶段证据：

- 需求/原型/任务：`docs/01-需求说明.md`、`docs/02-原型说明.md`、`docs/03-任务规划.md`、`.agent/development/T02-T05.md`–`T02-T10.md`
- 独立测试：`docs/testing/T02-T11-report.md`、`docs/testing/T02-T11-retest.md`
- 独立验收：`docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md`
- 缺陷与复测：`docs/defects/README.md`

## 口径

- AI2（三会话隔离）：有独立测试/验收报告，但**无会话导出证明**，保持 BLOCKED。
- AI5（会话导出 + `events.jsonl`）：本目录为**部分补齐**，非平台原生全量导出，保持 BLOCKED。
- 时间取自 `git log --format=%cI` 的真实提交时间与整理时刻 `2026-11-12T19:35:42+08:00`；未逐条编造平台时间戳。
