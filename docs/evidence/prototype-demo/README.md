# 原型可操作演示证据（2026-11-13，对应 02-A1-15）

- 采集人：AI 助手（检查者复评轮整改补证）；**操作对象是学生交付的页面与真实 HTTP 服务**，非示意图。
- 绑定版本：`05fcb7152b8627edabd99e2b26f3b7db31c51528`（main，结构与最终交付一致）
- 采集时间：2026-11-13T09:15–10:55+08:00
- 工具：`playwright-cli`（Chromium/Edge 驱动）+ 本机 Go 服务（`cmd/server`，教学时钟 fixed、模拟支付开启）
- 与「学生本人讲解视频」的区别：本目录是**原型可操作性**证据（操作可行、接口真实、状态流转可见）；
  讲解视频仍须学生本人录制（见 `docs/evidence/ai/video-script-2026-11-13.md`）。

## 一、启动命令（复现用）

```powershell
go build -o "$env:TEMP\paw-demo-server.exe" ./cmd/server
$env:PAW_ADDR='127.0.0.1:18085'; $env:PAW_DB="$env:TEMP\paw-demo-browser.db"
$env:PAW_SEED_DEMO='1'; $env:PAW_ENABLE_MOCK_PAY='1'
$env:TEACHING_CLOCK_MODE='fixed'; $env:TEACHING_CLOCK_BASE='2026-11-14T00:00:00Z'
& "$env:TEMP\paw-demo-server.exe"     # healthz/readyz 均返回 {"code":0,"message":"ok"}
```

## 二、实际执行的浏览器流程（真实点击/输入，产生真实 HTTP 请求）

| # | 账号 | 操作 | 结果 | 截图 |
|---|---|---|---|---|
| 1 | u1/123456 | 登录 → 选「豆豆」→ 点击时段#1「预约」 | 预约成功 `#1`，待支付，5 分钟支付窗口提示 | `01-user-confirmed.png`（含后续支付后状态） |
| 2 | u1 | 点击「去支付」（模拟支付渠道） | 状态 → `已确认`，金额 ¥99.00 | `01-user-confirmed.png` |
| 3 | rec/123456 | 前台接待页输入预约号 1 → 查询 → 核销到店 | 状态 → `已到店`，提示「核销成功」 | `02-reception-checkedin.png` |
| 4 | rec | 再次点击核销 | 幂等提示「已完成过核销」，不重复记收入 | `03-reception-duplicate-idempotent.png` |
| 5 | srv/123456 | 服务页查询预约 1 → 开始服务 → 完成（重复完成一次） | 状态 → `已完成`；重复完成提示幂等 | `04-service-completed.png` |
| 6 | mgr/123456 | 店长工作台 → 日期 2026-11-14 → 刷新日清 | 已完成收入 ¥99.00、已收款 ¥99.00、**勾稽差额 ¥0.00**；时段明细 17:00 完成 1 | `05-manager-daily-report.png` |
| 7 | u2 | 登录 → 选「咪咪」→ 预约 | 被拒 `PET_NOT_OWNED`（宠物非本人名下，服务端口径正确） | `06-u2-pending-payment.png` |
| 8 | u3 | 登录 → 预约 | 被拒 `PET_NOT_OWNED`（u3 无预置宠物） | — |
| 9 | u1 | 选「豆豆」→ 预约时段#2 → 支付 | 新预约 `#2` 状态 `已确认`，时段#2 余量 3→2 | `08-u1-second-slot-confirmed.png` |

补充：`04`/`05` 两图由脚本在**同一会话重放**生成，第二次执行「开始/完成」均得到幂等结果且日清金额仍为 99.00、差额 0——重复操作不重复记收入得到 UI 级验证。

## 三、文件清单

| 文件 | 内容 | 用途 |
|---|---|---|
| `01-user-confirmed.png` | 宠物主人页：时段列表、我的预约、已确认状态 | 角色页真实联动 |
| `02-reception-checkedin.png` | 前台接待页：核销后已到店 | 核销可用 |
| `03-reception-duplicate-idempotent.png` | 重复核销幂等 | R5 幂等在 UI 层可验 |
| `04-service-completed.png` | 服务页：已完成 | 服务流转可用 |
| `05-manager-daily-report.png` | 店长日清：收入/收款/差额 0 | 日清可核 |
| `06-u2-pending-payment.png` | u2 越界预约被拒 | 权限口径正确 |
| `08-u1-second-slot-confirmed.png` | 第二笔预约已确认 | 名额递减可观察 |
| `trace-1794533881173.zip` | Playwright trace（含逐帧 screencast、每步 DOM/Action、资源） | 可操作状态**回放** |
| `trace-1794533881173.network` | 全部请求/响应（URL、方法、状态码、耗时） | **浏览器网络记录**（HAR 等价物） |

回放方法：解压 zip → `npx playwright show-trace trace-1794533881173.trace`（或 Playwright Trace Viewer 直接打开）。

说明（如实登记）：本轮未生成独立 `.webm` 文件——采集环境用系统 Edge（CDP 连接方式），Playwright 的视频录制只对内置 Chromium 生效，`playwright-cli install-browser chromium` 未完成下载。回放能力由 trace 内的逐帧 screencast 提供（可看到每次点击与状态变化），网络记录由 `.network` 提供。若需 `.webm`，可在装有 Chromium 的环境重跑同一流程后 `video-stop <文件名>.webm`。

## 四、演示中发现的问题（如实记录，不代改生产代码）

1. `user.html`「我的预约」在**零记录**时渲染异常：`加载失败：Cannot read properties of null (reading 'length')`（接口返回空列表时前端未判空）。有记录时正常。
2. 用户页宠物下拉列出的是**全库宠物**（u2 登录也显示「豆豆/咪咪」），提交时被服务端 `PET_NOT_OWNED` 拒绝——安全规则成立，但前端列表口径与「仅本人宠物」不一致。
3. 页面未提供「临期取消需填原因」的表单入口（只能走接口）——不影响接口层规则验证（scenarios 已覆盖 422 分支）。

以上三项属界面体验缺陷，不影响题面 R1–R8 服务端规则与金额一致性；如需修复，应由 Dev 会话改代码后在 main 重新跑门禁（本轮仅补证据，不改生产代码）。
