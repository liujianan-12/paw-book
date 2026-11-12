# 03 接口契约（页面与测试共用）

约定：JSON；统一响应 `{"code":0,"message":"ok","data":{}}`；业务失败 `code!=0` 且 HTTP 状态语义化（400/403/404/409/422/503）；时间 ISO-8601 带时区；金额单位为**分**（整数）。所有写接口支持 `Idempotency-Key` 请求头，冲突返回 `code=IDEMPOTENCY_CONFLICT`。

## 1. 身份

- 预置账号登录：`POST /api/auth/login`（`account`,`password`）→ 返回会话 Cookie 或 JWT，服务端会话保存 `user_id`/`staff_id`、`role`、`store_id`。
- 后续请求**只从会话取身份**，忽略请求体中的 userId/storeId。

## 2. 接口清单

| # | 方法/路径 | 角色 | 校验 / 规则 | 关键返回 |
|---|---|---|---|---|
| A1 | `GET /api/slots?date=` | 所有登录角色 | 店长看本店全部；用户看可预约时段（含剩余名额） | `id,startAt,endAt,capacity,remaining,status` |
| A2 | `POST /api/manager/slots` | 店长 | R1.1：`startAt<endAt`，未来时段 | 时段 ID |
| A3 | `POST /api/reservations` | 用户 | R1.2 R1.3 R1.4 R2.1：宠物属于自己；时段未开始；有名额；同宠物同时段无有效预约 | `reservationId,status=PENDING_PAYMENT,payExpireAt,priceCents=9900` |
| A4 | `POST /api/reservations/{id}/pay` | 用户（本人） | R2.4 R2.7：待支付且未超时；失败可重试（每次新流水/新 `orderNo`，同预约最多一条成功）；调模拟渠道 | `paymentId,orderNo,status,attempt` |
| A5 | `POST /api/payment/notify` | 模拟渠道（签名/密钥头） | R2.5 R2.6 幂等：按 `channelTxnId` 去重；预约状态决定确认/异常退款 | `result: CONFIRMED / DUPLICATE_IGNORED / ANOMALY_REFUND` |
| A6 | `POST /api/reservations/{id}/cancel` | 用户（本人） | R2.3 R3.1 R3.2：未付款→释放；≥2h→自动退款；<2h→需原因转审核；**拒绝后可再次提交**（同一时刻仅一条待审） | `status,refundId?,cancelRequestId?,message` |
| A7 | `GET /api/reservations/{id}` | 用户（本人）/店长/客服（本店） | R4.3 R7.3：返回预约 + 取消申请 + 退款单 + 服务进度 | 全量状态视图 |
| A8 | `GET /api/my/reservations` | 用户 | 分页；按日期/状态筛选 | 列表 |
| A9 | `POST /api/reception/reservations/{id}/checkin` | 前台 | R5.1 R3.3 R6.2：本店、已确认、无待处理取消；重复请求幂等成功（不新增记录） | `checkedInAt, alreadyDone` |
| A10 | `POST /api/service/reservations/{id}/start` | 服务人员 | R5.2：本店、已到店 | `startedAt` |
| A11 | `POST /api/service/reservations/{id}/complete` | 服务人员 | R5.2 R5.3：本店、服务中；幂等 | `completedAt` |
| A12 | `GET /api/cs/cancel-requests?status=PENDING` | 客服 | 本店；含预约与宠物摘要 | 列表 |
| A13 | `POST /api/cs/cancel-requests/{id}/approve` | 客服 | R4.1 R4.2：仅待处理；事务内释放名额 + 创建退款 | `status,refundId` |
| A14 | `POST /api/cs/cancel-requests/{id}/reject` | 客服 | R3.3：拒绝后回到已确认；名额保持 | `status` |
| A15 | `GET /api/cs/refunds?status=FAILED` | 客服 | 本店 | 退款列表 |
| A16 | `POST /api/cs/refunds/{id}/retry` | 客服 | R4.4：仅失败/待处理；重试计数 | `status,retryCount` |
| A17 | `POST /api/manager/reservations/{id}/no-show` | 店长 | R7.1 R7.2：时段结束后、未到店、必填原因 | `noShowAt,reason` |
| A18 | `GET /api/manager/daily-close?date=` | 店长 | R8.1 R8.2 R8.3：按时段分组五类计数 + 三金额 + 明细下钻 | `slots[],totals{},records[]` |
| A19 | `GET /healthz` `/readyz` | 部署 | 存活/就绪（DB 可连） | 200 |
| A20 | `GET /api/smoke` | 部署 | 返回种子数据可用性（不写库） | 200 |

## 3. 模拟支付渠道（独立进程或同进程路由，报告中注明“模拟”）

| 方法/路径 | 作用 |
|---|---|
| `POST /mock/pay` `{orderNo, scenario}` | scenario=`success|fail|timeout|duplicate`；`duplicate` 会发送两次回调 |
| `POST /mock/refund` `{refundNo, scenario}` | scenario=`success|fail|timeout` |
| `POST /mock/notify-late` `{orderNo}` | 预约关闭后再发成功回调（R2.6 演示） |
| `GET /mock/records` | 查看模拟渠道已处理流水（测试断言用） |

要求：成功、失败、超时、重复通知四类都能触发；回调签名可简化为固定 HMAC/密钥头，但服务端必须校验，缺签名拒绝。

## 4. 错误码（统一枚举，测试按码断言）

| code | 场景 |
|---|---|
| `SLOT_NOT_FOUND` / `SLOT_STARTED` / `SLOT_FULL` | A3 |
| `PET_NOT_FOUND` / `PET_NOT_OWNED` | A3 |
| `DUPLICATE_ACTIVE_RESERVATION` | A3（R1.2） |
| `RESERVATION_STATE_INVALID` | 状态不允许该操作（R6.2 等） |
| `PAYMENT_EXPIRED` / `PAYMENT_NOT_PENDING` | A4 |
| `PAYMENT_SIGNATURE_INVALID` | A5 |
| `CANCEL_REASON_REQUIRED` / `CANCEL_NOT_ALLOWED` | A6 |
| `CANCEL_ALREADY_PENDING` / `CANCEL_REQUEST_NOT_PENDING` | A6/A13/A14（待审中重复提交 / 申请不在待处理状态） |
| `CHECKIN_NOT_ALLOWED_PENDING_CANCEL` / `CHECKIN_NOT_ALLOWED` | A9（R3.3 R5.1） |
| `SERVICE_NOT_CHECKED_IN` / `SERVICE_NOT_STARTED` | A10/A11 |
| `STORE_MISMATCH` / `ROLE_FORBIDDEN` / `UNAUTHENTICATED` | 全部 |
| `NO_SHOW_TOO_EARLY` / `NO_SHOW_REASON_REQUIRED` / `NOT_NO_SHOWABLE` | A17 |
| `REFUND_NOT_RETRYABLE` / `REFUND_CHANNEL_FAILED` | A16 |
| `IDEMPOTENCY_CONFLICT` | 同 key 不同载荷 |

## 5. 幂等与并发要求

- 重复核销/重复完成：状态已达成的重复请求返回**幂等成功**（`alreadyDone=true`），不新增记录、不重复记收入；仅当预约状态不允许时才返回 `RESERVATION_STATE_INVALID`。
- 至少以下写接口必须实现幂等：创建预约、支付、取消、审批、退款重试、核销、开始/完成、未到店标记；其余写接口以唯一约束兜底。
- 支付回调以 `channel_txn_id` 唯一；重复回调返回 `DUPLICATE_IGNORED`，不改业务状态、不重复记收款。
- 支付失败重试（R2.7）：每次重新发起 = 新 `payment` 流水 + 新 `order_no`；同一预约最多一条 `SUCCESS`（部分唯一索引/生成列兜底）；仅在 5 分钟占位窗口内允许，窗口外成功回调按 R2.6 走异常退款。
- 取消被拒后重提（D3）：拒绝即恢复 `CONFIRMED`；可再次提交，但同一时刻仅允许一条 `PENDING`；历史申请不删除。
- 状态迁移必须用**条件更新**（`WHERE id=? AND status IN (...)`）或行锁串行化，禁止先查后改的裸写。
- 所有并发冲突路径返回明确业务错误，不能 500；错误信息不得泄露内部 SQL/堆栈。
- 接口响应中的 `priceCents` 一律整数分，禁止 `float64` 参与金额计算。

## 6. 页面（最小可操作原型，须真实调接口）

| 页面 | 角色 | 必须能做的操作 |
|---|---|---|
| 预约页 | 用户 | 看 3 个时段与剩余名额、选宠物预约、去支付（模拟渠道按钮）、取消、查看结果/退款状态 |
| 前台页 | 前台 | 输入/扫描预约号核销、看到拒绝原因（未付款/已取消/待审核/跨店） |
| 客服页 | 客服 | 待审列表、批准/拒绝（填备注）、退款失败列表与重试、看结果 |
| 店长页 | 店长 | 发布时段、看当日各时段五类统计与三金额、列明细、标记未到店、写原因 |

页面必须展示失败原因（业务 code + 人话），不能只有成功弹窗。
