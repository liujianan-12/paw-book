# 学生本人待办清单（复评剩余失分项，2026-11-13）

下列事项只能由你本人或教师完成，AI 不能也不应伪造；完成一项即在「状态」栏填日期与证据位置。

| # | 事项 | 为什么必须由你做 | 现状 | 状态 |
|---|---|---|---|---|
| 1 | 演示视频录制（按 `.agent/10` 时间点目录，绑定最终交付提交 `8544ad4`） | 需本人出镜/讲解，视频内容不可由 AI 生成 | `docs/evidence/deploy/README.md` 视频项 BLOCKED，时间点目录模板在 `.agent/10` | ☐ |
| 2 | 班级群截图（题目要求的发布/提交佐证） | 需本人账号与群聊记录 | 仓库无此项证据，不伪造 | ☐ |
| 3 | 平台原生会话导出（Dev/Test/Acceptance 三个会话） | 平台功能，AI 只能生成本地 Markdown 记录 | `docs/sessions/` 有本地整理版（README.md/events.jsonl/08-git-收尾-dev.md），非平台原生导出 | ☐ |
| 4 | Token usage 台账（按 `Token用量明细.csv` 维度补齐真实截图/导出） | 需本人从平台导出用量 | 教师已提供 `Token用量明细.csv`（输入材料），学生侧台账缺失 | ☐ |
| 5 | Gitee 平台 PR 记录 | 现有 Gitee 令牌无 `pull_requests` scope（401），需网页端或换有权限令牌 | `main` 已同步到 `8544ad4`，但平台上没有 PR 记录；详见 `docs/evidence/git/pr-merge-2026-11-13.md` | ☐ |
| 6 | MCP 建仓 / meegle 工作项核验输出 | 工具未注入 / 编号平台侧不可达，按 D4 已如实 BLOCKED | `docs/evidence/git/mcp-github-BLOCKED.md`、`docs/evidence/meegle/BLOCKED.md` | ☐ 或向教师索取真实编号后回填 |
| 7 | CI 流水线运行记录 + 教学环境部署 + 健康检查 + 回滚验证 | 需教师提供部署环境与平台运行记录 | `docs/evidence/deploy/README.md` 五项 BLOCKED；`deploy/rollback.md` 为未验证草案 | ☐ |
| 8 | 若教师要求：main 保护规则截图 | `gh` 未安装，仅 API 侧曾查过；需平台截图 | 记录见 `docs/evidence/git/` 既有文件 | ☐ |

## 已由本轮整改完成（无需你重复）

- AGENTS.md v0.1.4 维护人「刘迦南」与 v0.1.0–v0.1.4 真实提交 SHA 回填，占位符清零。
- `go test -race ./...` 解除 BLOCKED（MSYS2 MinGW-w64 + TUNA 镜像），全 PASS：`docs/evidence/tests/0ee9aee/race-final.txt`。
- 五组场景服务端全链路实测 `scripts/scenarios.ps1` 与归档 `docs/evidence/tests/0ee9aee/scenarios-output.txt`；冒烟 `smoke-final.txt`。
- 重启持久化/未到店/日清勾稽（S5）在同一脚本内实测，差额 0。
- 独立验收子代理调用 acceptance-audit（无 FAIL）：`docs/acceptance/acceptance-report-6f92f4bb659abc15ca2c7d58c8e75087bb787b0a.md`；`docs/skill-usage.md` 已补调用记录。
- GitHub PR#1 创建与合并（`8544ad4`）、Gitee main 同步、main 上复跑门禁归档 `docs/evidence/tests/8544ad4/`。
