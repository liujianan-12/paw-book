# 无法导出 / 仍缺失的范围（不得写成已完成）

> 依据 `.agent/10 §3`：「无法导出的平台内容列出缺失范围」。以下逐项写明缺失原因与可替代证据。

| 缺失项 | 原因 | 可替代证据 | 状态 |
|---|---|---|---|
| 阶段 01–07 的**平台原生会话导出** | 需求分析、原型、任务规划、开发主体、独立 Test 会话、独立 Acceptance 会话均不在当前 Dev 会话上下文；本会话无平台导出工具 | `docs/sessions/README.md`、`08-git-收尾-dev.md`、`.agent/development/T02-T05.md`–`T02-T10.md` | BLOCKED |
| 平台原始逐条时间戳、隐藏推理与内部事件 | 平台不提供；本会话只取 `git log --format=%cI` 的真实提交时间与整理时刻 | `docs/sessions/events.jsonl`（`ts` 来源已注明） | BLOCKED |
| 三会话隔离的会话级证明（AI2） | 有独立测试/验收报告，但无会话导出证明隔离 | `docs/testing/T02-T11-report.md`、`docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md` | BLOCKED |
| `main` 保护规则截图（G2） | 学生声明已设置，但 `docs/evidence/git/main-protection.png` `Test-Path` = MISSING；`--symref` 仍指向 `master`（Gitee） | `docs/pr-description.md`「main 默认分支与保护规则证据」 | BLOCKED |
| 视频与 S1–S5 录屏、时间点目录（AI7 / S1–S5） | 必须由学生本人讲解录制；当前无视频文件 | `docs/evidence/ai/video-timeline.md`（待填模板）、`docs/prototype-shots/` 静态截图 | BLOCKED |
| 教师部署环境、流水线运行记录、部署地址、健康检查、上线冒烟、回滚验证（G6–G8） | 环境未提供；仅可提交配置与说明，未实际运行 | `deploy/README.md`、`deploy/rollback.md`、`.github/workflows/ci.yml`、`docs/evidence/deploy/README.md` | BLOCKED |
| `-race` 原始通过结果（Q4 / Q7） | 本机无 gcc、`CGO_ENABLED=0`，GitHub / winget CDN 不可达，无法装 MinGW-w64 | `docs/evidence/git/race-attempt-2026-11-12.md` | BLOCKED |
| Meegle / 飞书工作项编号核验（G3） | 学生明确排除本轮补齐；无权限 / 无应用 | `docs/evidence/meegle/BLOCKED.md`，`#1001` 为占位编号 | BLOCKED |
| PR 合并提交、主分支合并后门禁复跑、`<合并SHA>` 证据目录（G4 / G5） | PR 尚未创建/合并；两边 `main` 仍是 `a92e5cd Initial commit` | `docs/evidence/tests/sha-binding-risk.md`（含合并后待执行清单） | BLOCKED |
| 班级群提交截图 | 学生侧操作，未发生 | — | BLOCKED |

## 口径

`docs/sessions/` 与 `docs/ai-usage.md` 是**部分补齐**，不是「全部可见会话已导出」。按 `.agent/10 §8`，上表任一项缺失即不得声明最终交付完成。
