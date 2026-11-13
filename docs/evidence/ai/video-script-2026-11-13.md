# 讲解视频讲稿与时间点目录（学生本人录制，绑定交付提交 05fcb71）

> 依据 `.agent/10-evidence-ai-record.md` §6。**必须由学生本人讲解**，AI 只提供讲稿与证据索引。
> 录制建议：OBS/Windows 11 自带「截图工具→录屏」；先讲后演示，全程不出现真实手机号/令牌/DSN。
> 录制完成后把 `时间` 列替换为真实时间戳，落盘 `docs/evidence/ai/video-timeline.md` 并把视频链接写进群消息（见本目录 `class-group-template.md`）。

## 一、讲稿（约 10–12 分钟，12 段）

1. **来源与背景（1 分钟）**：宠物护理门店单店经营闭环；来源岗位与改编点；业务目标一句话（发布→预约→支付→核销→服务→取消/退款→日清）。
2. **角色与规则 R1–R8（1.5 分钟）**：六角色；逐条读 R1–R8，强调 2h 取消分界、幂等、名额守恒、未到店不自动退款。指向 `.agent/01-requirements-rules.md`。
3. **需求理解与假设（0.5 分钟）**：`docs/requirements.md`、`docs/assumptions.md`（SQLite 口径 D1、允许取消重提 D3、race 需 MinGW D5）。
4. **原型如何服务角色（1 分钟）**：五个页面（user/manager/reception/service/cs）各管什么；打开 `docs/prototype-shots/` 或直接切浏览器。
5. **任务拆分（0.5 分钟）**：`docs/03-任务规划.md` 任务编号→依赖→完成条件。
6. **Agent 约束（1 分钟）**：打开 `AGENTS.md`，讲三会话隔离、四条门禁、反作弊红线；展示 git log 证明 7376961（约束）早于 95d737d（业务代码）。
7. **开发过程与学生审核（1.5 分钟）**：举一个真实纠错——独立测试发现 D-T02-01（幂等 key 跨用户）→ Dev 修复 `4cbaf00` → 定向复测关闭；引用 `docs/defects/README.md`。
8. **Skill 实际使用（1 分钟）**：slot-rule-guard 只读审计（`docs/evidence/skill/*.json`）与 acceptance-audit 预验收报告（`docs/acceptance/acceptance-report-6f92f4b*.md`）；`docs/skill-usage.md` 含局限说明。
9. **测试与质量证据（1.5 分钟）**：覆盖率原件 86.8%、逐包 ≥85%（`docs/evidence/tests/8544ad4/coverage-func.txt`）；race 归档（`race-final.txt`）；CI 流水线 13 次全绿（GitHub Actions，含最终提交 05fcb71，`docs/evidence/ci/`）。
10. **独立验收与合并（1 分钟）**：验收 Skill 七步、无 FAIL；PR#1（GitHub REST）合并 8544ad4 → main 门禁复跑 → PR#2/#3 → 05fcb71；main 已设分支保护（PR+状态检查 `quality-gate / gate`，`docs/evidence/ci/main-protection-response.json`）。
11. **多角色效果演示（2.5–3 分钟）**：见「二、演示脚本」——S1 全链路 + 两个异常（无原因取消 422、重复核销幂等）+ 日清勾稽差额 0；强调**模拟支付**仅教学演示。
12. **总结与局限（0.5 分钟）**：部署类 BLOCKED（教师环境未提供）、meegle 编号 BLOCKED（如实登记）、会话导出待平台补齐；`docs/pr-description.md` BLOCKED 清单口径。

## 二、演示脚本（第 11 段用，本地起服务）

```powershell
# 教学时钟 fixed，预置演示时段，模拟支付开启（演示库放 %TEMP%，不入库）
$env:PAW_ADDR='127.0.0.1:18085'; $env:PAW_DB="$env:TEMP\paw-demo-video.db"
$env:PAW_SEED_DEMO='1'; $env:PAW_ENABLE_MOCK_PAY='1'
$env:TEACHING_CLOCK_MODE='fixed'; $env:TEACHING_CLOCK_BASE='2026-11-14T00:00:00Z'
go run ./cmd/server   # 或 go build -o server.exe ./cmd/server 后运行
```

浏览器操作顺序（密码均 123456）：
1. u1 登录 → user.html：选时段 → 为豆豆预约 → 待支付 → 点「模拟支付」→ 状态变已确认。
2. rec 登录 → reception.html：核销 → srv 登录 → service.html：开始 → 完成。
3. mgr 登录 → manager.html：日清看已完成收入 ¥99.00、勾稽差额 0。
4. 异常演示：u2 预约另一时段 → 取消时不填原因（临期）→ 看 422 提示；rec 重复核销 → 「已核销，不重复记收入」。

## 三、时间点目录（录制后填真实时间）

| 时间 | 内容 | 对应文件/证据 |
|---|---|---|
| （待填） | 题目来源与业务背景 | 题面、`docs/requirements.md` |
| （待填） | 角色与规则 R1–R8 | `.agent/01-requirements-rules.md` |
| （待填） | 原型四角色 | `docs/prototype-shots/` |
| （待填） | 任务拆分与依赖 | `docs/03-任务规划.md` |
| （待填） | AGENTS.md 关键约束（7376961 先于开发） | `AGENTS.md`、git log |
| （待填） | 开发过程与学生审核（D-T02-01 纠错） | `docs/defects/README.md` |
| （待填） | Skill 使用 | `docs/evidence/skill/`、`docs/skill-usage.md` |
| （待填） | 测试与覆盖率原始结果 | `docs/evidence/tests/8544ad4/`、`docs/evidence/ci/` |
| （待填） | 独立验收逐项 | `docs/acceptance/acceptance-report-6f92f4b*.md` |
| （待填） | Git/PR/合并/保护/门禁复跑 | `docs/evidence/git/`、`docs/evidence/ci/` |
| （待填） | 效果演示 S1 + 异常分支 | 本机录屏 |
| （待填） | 总结与局限（BLOCKED 口径） | `docs/pr-description.md` |

## 四、录制注意事项

- 全程绑定版本：终端先跑 `git rev-parse HEAD`（应显示 05fcb7152b8627edabd99e2b26f3b7db31c51528）再开始讲解。
- 演示库放 `%TEMP%`，结束删除；不展示真实令牌/DSN；页面登录用预置演示账号。
- 视频命名：`题目02-讲解-刘迦南-05fcb71.mp4`（或 webm），上传到教师指定渠道后把链接写入群消息模板。
