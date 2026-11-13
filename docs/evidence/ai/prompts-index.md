# 提示词清单（基于本会话可见记录）

> 平台原生逐行导出不可得，故按「会话文件 + 小节」索引，不编造行号；完整上下文见 `docs/sessions/08-git-收尾-dev.md`，缺失范围见 `missing-scope.md`。

| 阶段 | 学生提示词意图 | 索引位置 | 关联产出 |
|---|---|---|---|
| 交付核查 | 「所有内容都完成了吗，单元测试做了吗」 | 会话记录之外的早期阶段，见 `docs/testing/T02-T11-report.md` | 独立 Test 会话建立 |
| 补缺口 | 「那你开始啊」/「继续」（两次） | 同上 | 自编 Skill、三轮复测、验收报告 |
| 验收卡住处理 | 「取消 HTTP 实测、限时 8 分钟、BLOCKED 不得写成 PASS」 | `.agent/13-acceptance-runbook.md` | `docs/acceptance/acceptance-report-9680d2c32f1473635d0d442b58088caf88ef6f20.md` |
| 状态修正 | 截图指出 todo 停在 2/5 | 会话记录之外的早期阶段 | todo 全部更新 |
| 通俗说明 | 「大白话说，飞书账号你想要什么」 | 会话记录之外的早期阶段 | `.agent/07` 通俗答复 |
| Git 推送 | 「凭据已存凭据管理器…严禁令牌…按顺序执行并贴输出」 | `08-git-收尾-dev.md` §2026-11-12T16:54:55 | `7376961`、`docs/evidence/git/push-7376961.txt` |
| Git 收尾 | 「提交推送证据 / 处理默认分支 / PR 描述改 main / 合并后重跑」 | `08-git-收尾-dev.md` §2026-11-12T17:05:56 | `ca7cc24`、`bd614e0`、`bafc059` |
| MCP 排查 | 「GitHub MCP 保持 BLOCKED，不写 PASS」 | `08-git-收尾-dev.md` §2026-11-12T18:40:58 | `docs/evidence/git/mcp-github-BLOCKED.md` |
| 本次补齐 | 「除 Meegle 外看看还有什么没整，去补充」 | `08-git-收尾-dev.md` §2026-11-12T19:35:42 | `docs/evidence/git/github-repo-2026-11-12.md` 等 |

未列出的阶段（需求分析、原型、任务规划、开发主体、独立测试、独立验收、修复返工）原始提示词不在当前会话上下文，见 `missing-scope.md`。
