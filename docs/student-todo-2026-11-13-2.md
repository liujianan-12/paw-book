# 学生人工项操作指引（2026-11-13 复评后整改）

> 初评 5.62 → 复评 6.96（待核定）。本文件给出剩余失分项中**必须由学生本人/教师侧完成**的步骤；
> AI 可完成的部分（CI 归档、main 分支保护、meegle BLOCKED 填写、原型可操作演示、材料准备）已在同批完成，见各节「已完成」。

## 1. 平台原生三会话导出（对应 02-A3-07 / 02-B1-03 / 02-B1-04 / 02-B2-03/04/05）

**为什么 AI 替不了**：评分规则要求平台原生导出（带平台时间戳/会话元数据的原始记录），本地 Markdown 整理稿不算。

**步骤**（CodeBuddy CN IDE）：
1. 打开三个会话（开发 Dev、独立测试 Test、独立验收 Acceptance——Acceptance 若是子代理会话，从其任务记录/导出入口取）。
2. 每个会话右上角「…」菜单 → 导出/复制全文（如 IDE 无导出按钮：全选会话内容 → 复制 → 粘贴为 `.md`；平台如提供 `events`/审计页，同样导出）。
3. 保存为（保持既有命名衔接）：
   - `docs/sessions/04-开发-dev-平台导出.md`
   - `docs/sessions/05-测试-test-平台导出.md`
   - `docs/sessions/06-验收-acceptance-平台导出.md`
4. 对照 `.agent/10` §3 的 events.jsonl schema，把 Test/Acceptance 两个会话的关键事件（提示→工具调用→结果→提交）补入 `docs/sessions/events.jsonl`（时间升序）。
5. 无法导出的部分（模型名、隐藏推理等）如实写入 `docs/evidence/ai/missing-scope.md`。
6. 导出后自查：文件头部保留平台会话 ID/开始时间等元数据（若有）；敏感字段按 `docs/evidence/ai/redaction.md` 脱敏。

## 2. Token usage 逐请求台账（教师基准登记前仍记待核定）

1. CodeBuddy CN：设置/账号 → 用量（或个人中心的 Token 用量页）→ 按天导出/截图（含模型、请求数、token 数）。
2. 把 2026-11-12 至 2026-11-13 每阶段的用量汇总为 `docs/evidence/ai/usage-ledger-20261113.md`：阶段｜时间范围｜模型｜请求数｜输入/输出 token（官方页面数值，不得估算）。
3. 教师侧：登记同题 Token 基准 B（`student-project-audit` 技能 `profiles/02.json` 的 `token_baselines`）。此前 Token 维持 0/1.5 且整体待核定，不判学生不合格。

## 3. 视频（本人讲解）与班级群提交（02-A4-08/09/10）

1. 讲稿与演示脚本已备好：`docs/evidence/ai/video-script-2026-11-13.md`（含演示服务启动命令与浏览器操作顺序）。
2. 按讲稿录制（10–12 分钟，绑定提交 05fcb71），录完把时间点目录填真实时间 → 覆盖 `docs/evidence/ai/video-timeline.md`。
3. 视频上传教师指定渠道；班级群按模板发当天提交：`docs/evidence/ai/class-group-template.md`，截图群消息落盘 `docs/evidence/ai/group-submission-2026-11-13.png`（自截聊天记录即可）。

## 4. Gitee 平台 PR 记录补建（02-B4-05，当前令牌缺 pull_requests scope）

1. 登录 Gitee → `liujianan12/pet-spa` → 「+」新建 Pull Request。
2. 源分支 `dev/02-pet-booking` → 目标分支 `master`（Gitee 默认主分支；GitHub 侧 main 已在合并提交 8544ad4，Gitee master 已同步同一提交）。
3. 标题沿用 PR#1 描述：`Merge PR#1 dev/02-pet-booking -> main（8544ad4，main 门禁复跑全 PASS）`；说明里引用 `docs/evidence/git/pr-merge-2026-11-13.md`。
4. 建好即截图保存 `docs/evidence/git/gitee-pr-2026-11-13.png`，并在 PR 页面填链接回 `pr-merge-2026-11-13.md`。
5. 若 Gitee 提示「分支无差异」：改在 Gitee 网页对该 PR 用「建立空 PR 记录/说明型 PR」方式留痕，并在文件中注明"仅平台记录补建，代码合并已在 GitHub 完成"。

## 5. meegle 编号（02-B4-03，教师侧）

- BLOCKED 模板已按 2026-11-13 真实尝试填写：`docs/evidence/meegle/BLOCKED.md`（meegle CLI 不存在，PATH 无该命令）。
- 待教师提供真实工作项编号与 CLI/权限后：从下一个 feat/fix 起使用真实编号并附核验输出，历史不回写。此项保持 unknown/teacher。

## 6. 部署（02-A4-06/07，教师部署环境）

- 教师提供部署环境/流水线目标后：部署 → 健康检查（healthz/readyz）→ 上线冒烟（可用 `scripts/smoke.ps1` 改指向部署地址）→ 回滚演练（上一版本回退），全部原件落 `docs/evidence/deploy/`。
- 教师环境仍不可得时：维持 BLOCKED 口径，不得把本地冒烟写成上线冒烟。

---

## 同批已由 AI 完成（供复评核对）

| 项 | 证据 |
|---|---|
| CI 流水线 13 次真实运行归档（含 05fcb71 全步成功 + 逐包覆盖率） | `docs/evidence/ci/actions-runs-raw.json`、`jobs-43339381660.txt`、`jobs-43336277319.txt`、`job-131603575026-log.txt` |
| main 分支保护（PR + 状态检查 quality-gate/gate、含管理员、禁强推/删除） | `docs/evidence/ci/main-protection-response.json`（GitHub API 实时响应） |
| meegle BLOCKED 记录如实填写 | `docs/evidence/meegle/BLOCKED.md` |
| 原型可操作演示（浏览器自动化：录屏+trace 网络记录+截图） | `docs/evidence/prototype-demo/` |
| 视频讲稿/群消息模板/导出指引 | `docs/evidence/ai/video-script-2026-11-13.md`、`class-group-template.md`、本文件 |
