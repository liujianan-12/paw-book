# Skill 调用：发现 → 修正 → 复检

## 案例 1：发布时段被误判为“过去”（2026-11-12）

- 发现方式：店长页手工操作（页面 `manager.html`），输入北京时间 `2026-11-14 14:00`，返回
  `INVALID_PARAM: 开始时间必须在未来`。
- 定位：教学时钟 `TEACHING_CLOCK_BASE=2026-11-14T06:00:00Z` 等于北京时间 14:00；浏览器把 14:00 本地时间
  转成 `06:00Z`，与“现在”相等，`StartAt.After(now)` 为 false。属于**时区/固定时钟口径**问题，不是用户选了过去时间。
- 修正（提交 `1fb9d09`）：
  1. 教学时钟默认基准改为 `2026-11-14T00:00:00Z`（北京 08:00）；
  2. 错误信息返回服务器当前时间；
  3. `/api/me` 返回 `ServerNow`，店长页按服务器时间预填 +3h/+4h。
- 复检命令与结果：

```powershell
# 登录 mgr 后发布 UTC 06:00Z（北京 14:00）
POST /api/manager/slots {"startAt":"2026-11-14T06:00:00Z","endAt":"2026-11-14T07:00:00Z","capacity":3}
=> {"code":0,"data":{"slotId":4},"message":"ok"}
```

## 案例 2：slot-rule-guard 只读审计（2026-11-12）

```powershell
go run ./cmd/audit -db %TEMP%\paw-preview.db -date 2026-11-14 -store 1 `
  -commit 1fb9d09 -out docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json
```

- 结果：14 项检查全部 `pass=true`，`pass=true`，退出码 0。
- 输出：`docs/evidence/skill/slot-guard-2026-11-14-1fb9d09.json`。
- 局限：该库为空库/仅演示数据，审计只能证明“无违规数据”，不能证明分支已实现；分支覆盖由 `go test` 用例矩阵负责。
