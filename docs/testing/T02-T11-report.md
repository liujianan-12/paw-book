# T02–T11 独立测试报告（Test Agent）

- 角色：Test Agent（与 Dev 会话上下文隔离，独立复读 `.agent/01–06`、`.agent/08`、`.agent/11`、`docs/test-plan.md`、`docs/assumptions.md`）
- 被测版本：`git 1fb9d09`（分支 `dev/02`，HEAD）
- 测试时间：2026-11-12（当次会话）
- 报告原则：**结论只依据本会话亲自执行的命令输出**；未引用任何 Dev 完成声明。

---

## 1. 环境

| 项 | 实际值 |
|---|---|
| OS | Windows（win32） |
| Shell | PowerShell |
| go | `go version go1.26.0 windows/amd64` |
| `go env CGO_ENABLED` | `0` |
| gcc | 未安装：`gcc : 无法将“gcc”项识别为 cmdlet…`（`gcc --version` 失败） |
| 数据库 | SQLite 临时文件库（`t.TempDir()`，WAL + `busy_timeout=5000` + `_txlock=immediate`），符合 D1/D8 |
| 支付渠道 | `internal/paychannel` 模拟；回调签名 `HMAC-SHA256(body, PAW_PAY_SECRET)`，请求头 `X-Mock-Signature` |

### 1.1 工作区状态（重要：被测代码 ≠ 纯 1fb9d09）

```
 M AGENTS.md
 M README.md
 M cmd/paychannel/main.go              ← 仅多一行注释空行
 M docs/evidence/tests/local/api.cover
 M docs/evidence/tests/local/service.cover
 D internal/store/schema.sql           ← 已删除但未提交
?? （.agent/*、migrations/002_seed.sql、migrations/embed.go、cmd/audit、cmd/migrate 等未跟踪文件）
```

结论：本次门禁实际运行在「1fb9d09 + 未提交改动」的工作区上。Go 生产代码差异仅 `cmd/paychannel/main.go` 一行注释空白；但 `internal/store/schema.sql` 的删除与多份规则/迁移文件未跟踪，使“被测版本可追溯性”不成立（见缺陷 `D-T02-03`）。

### 1.2 本次会话改动范围（自查）

仅新增 4 个测试文件（未修改任何生产代码、迁移、规则文档）：

```
?? internal/service/t02_agent_verify_test.go
?? internal/service/t02_agent_verify_extra_test.go
?? internal/api/t02_agent_verify_test.go
?? internal/api/t02_probe_test.go      （//go:build t02probe，默认不参与 go test ./...）
```

---

## 2. 命令与原始输出摘录

### 2.1 `gofmt -l .`（Q1）

```
=== gofmt -l . ===

(空)
gofmt-exit=0
```

### 2.2 `go vet ./...`（Q1）

```
=== go vet ./... ===

vet-exit=0
```

### 2.3 `go test ./... -count=1`（含本会话新增用例）

```
?   	paw-book/cmd/audit	[no test files]
?   	paw-book/cmd/migrate	[no test files]
?   	paw-book/cmd/paychannel	[no test files]
?   	paw-book/cmd/server	[no test files]
ok  	paw-book/internal/api	6.221s
?   	paw-book/internal/config	[no test files]
ok  	paw-book/internal/domain	0.947s
ok  	paw-book/internal/paychannel	1.447s
ok  	paw-book/internal/payclient	4.568s
ok  	paw-book/internal/repo	3.524s
ok  	paw-book/internal/service	15.931s
?   	paw-book/internal/store	[no test files]
?   	paw-book/migrations	[no test files]
```

逐用例统计（`go test ./... -count=1 -v` 汇总）：`PASS = 108`，`FAIL = 0`，`SKIP = 0`。

### 2.4 四个目标包覆盖率（Q2）

```
ok  	paw-book/internal/domain	1.891s	coverage: 100.0% of statements
ok  	paw-book/internal/repo	3.409s	coverage: 96.7% of statements
ok  	paw-book/internal/service	16.791s	coverage: 85.2% of statements
ok  	paw-book/internal/api	5.653s	coverage: 87.9% of statements
--- internal 整体 ---
total:						(statements)	86.7%
```

### 2.5 `go test -race`（E1：真实阻塞输出）

```
=== go env CGO_ENABLED ===
0
=== go test -race ./internal/domain -count=1 ===
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
exit=2
```

进一步验证（显式开启 cgo）：

```
$env:CGO_ENABLED='1'; go test -race ./internal/domain -count=1
# runtime/cgo
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
FAIL	paw-book/internal/domain [build failed]
exit=1
```

### 2.6 `scripts/smoke.ps1`（Q6）—— 首次失败、清理后通过、复跑再次失败

第一次执行（环境中存在上一次运行残留实例占用 18080）：

```
powershell : 支付回调结果异常：(@{paymentId=0; refundId=0; reservationId=0; result=DUPLICATE_IGNORED}.result)
At D:\gopath\src\paw-book\scripts\smoke.ps1:65 char:36
exit=1
```

清理 18080 上的残留 `server.exe` 后重跑（干净环境）：

```
PASS 冒烟：slot=1 reservation=1 payment=1 收入=9900 差额=0 种子=1/4/1
smoke exit=0
--- 端口/进程状态（run#1 之后）---
LocalPort OwningProcess
--------- -------------
    18080         41404        ← 脚本退出后 18080 仍被其子进程占用
```

紧接着复跑（不复位端口）：

```
powershell : 支付回调结果异常：(@{...result=DUPLICATE_IGNORED}.result)
At D:\gopath\src\paw-book\scripts\smoke.ps1:65 char:36
run2 exit=1
```

→ 冒烟**一次通过**，但**不可重复执行**，且失败原因为连到了上一次的残留实例（其持久库 `paw-book.db` 中已有 `smoke-txn-1`）。详见缺陷 `D-T02-02`。

### 2.7 本会话独立验证用例（raw）

```
$ go test ./internal/service -run 'TestT02' -count=1 -v
--- PASS: TestT02R31_CancelAtExactTwoHoursIsAutoAccepted (0.13s)
--- PASS: TestT02R32_BeforeTwoHoursRequiresReasonAndKeepsSeat (0.11s)
--- PASS: TestT02R33_CheckInBlockedWhileCancelPending (0.13s)
--- PASS: TestT02R41_R44_ApprovedCancelAndRefundFailureLifecycle (0.14s)
--- PASS: TestT02R53_DuplicateCheckinAndCompleteAreIdempotent (0.14s)
--- PASS: TestT02R25_R27_SingleSuccessPaymentPerReservation (0.15s)
--- PASS: TestT02R62_CancelDeniedInCheckedInServiceAndCompleted (0.17s)
--- PASS: TestT02R61_CheckInVsCancelConcurrentSingleWinner (1.39s)
--- PASS: TestT02R22_PayHoldBoundaryAndSingleRelease (0.15s)
--- PASS: TestT02R26_LatePaymentAfterExpireKeepsStatusAndRefunds (0.13s)
--- PASS: TestT02R7_NoShowRules (0.13s)

$ go test ./internal/api -run 'TestT02' -count=1 -v
--- PASS: TestT02P7PayNotifySignatureRequired (0.16s)
--- PASS: TestT02IdempotencyReplayAndConflictOnCreateReservation (0.13s)
--- PASS: TestT02CrossStoreAndPermissionDenied (0.17s)

$ go test -tags=t02probe ./internal/api -run 'TestT02Probe' -count=1 -v
    t02_probe_test.go:45: u2 复用同一 Idempotency-Key 的响应：status=200
        body={"code":0,"data":{"payExpireAt":"2026-11-14T06:05:00Z","priceCents":9900,"reservationId":1,"status":"PENDING_PAYMENT"},"message":"ok"}
    t02_probe_test.go:51: 库中 u2 自己的预约数 = 0（期望 1）
    t02_probe_test.go:54: 缺陷：不同用户复用同一 Idempotency-Key 时返回了他人（user_id=1）的首次结果 1，且未为 u2 建单…
--- FAIL: TestT02ProbeCrossUserIdempotencyKeyReplay (0.13s)
```

> 说明：`TestT02R53` 与 `TestT02R25_R27` 初次运行因**我的断言写错**（审计动作名应为 `SERVICE_COMPLETE`；同 `channel_txn_id` 重复不会落新通知行）失败 2 次，已修正断言后通过——记录在此以免被误读为产品缺陷。

---

## 3. 用例结果表（规则号 → 结论）

| 规则/编号 | 验证内容（本会话独立编写） | 结论 | 证据 |
|---|---|---|---|
| R1.1 | 时段起止/容量校验、非店长拒绝（沿用既有用例 + api 正反例） | PASS | `go test ./internal/service ./internal/api` |
| R1.2 | 同宠物同时段唯一（`DUPLICATE_ACTIVE_RESERVATION`）、取消后可再约 | PASS | `slot_test.go` / `reservation_test.go` |
| R1.3 | 名额不超卖；名额释放后新用户可约（`TestT02R22` 末段断言） | PASS | `TestT02R22_PayHoldBoundaryAndSingleRelease` |
| R1.4 | 时段已开始不可约 | PASS | `slot_test.go` / `reservation_test.go` |
| R2.1 | `payExpireAt = createdAt + 5m`，名额 +1 | PASS | `TestT02R22` |
| R2.2 | 4m59s 仍有效、5m 过期；重复扫描只释放一次、名额不为负 | PASS | `TestT02R22` |
| R2.3 | 未付款取消释放名额且无退款 | PASS | `reservation_test.go` / api 取消分支 |
| R2.4 | 支付成功确认、`paid_at` 有值、金额 9900 | PASS | `TestT02R25_R27` |
| R2.5 | 同流水重复回调 ×3 幂等、换流水重复成功仍 `DUPLICATE_IGNORED` | PASS | `TestT02R25_R27` |
| R2.6 | 过期后成功回调 → `ANOMALY_REFUND`、异常单 1 条、退款 `LATE_PAYMENT` 9900、状态不变 | PASS | `TestT02R26` |
| R2.7 | 失败重试新 `order_no`、预约仍待支付、名额不释放、只有一条 SUCCESS | PASS | `TestT02R25_R27` |
| R3.1 | **边界 `=2h` 自动受理 + 全额退款 9900 + 释放名额** | PASS | `TestT02R31` |
| R3.2 | **边界 `2h-1s` 无原因拒绝（`CANCEL_REASON_REQUIRED`）、有原因转审核、名额保持** | PASS | `TestT02R32` |
| R3.3 | **待审期间核销拒绝（`CHECKIN_NOT_ALLOWED_PENDING_CANCEL`）、无核销记录、名额不减** | PASS | `TestT02R33` |
| R4.1 | 批准后核销拒绝（`CHECKIN_NOT_ALLOWED`）、无核销记录 | PASS | `TestT02R41_R44` |
| R4.2 | 重复批准不二次扣减名额、只 1 张退款 | PASS | `TestT02R41_R44` |
| R4.3 | 退款状态/重试次数/失败原因可查（接口 A15 + 详情字段） | PASS | `refund_test.go` / api |
| R4.4 | **退款失败 → 预约仍 CANCELLED、仍不可核销；重试 PROCESSING(retry=1) → 成功 9900** | PASS | `TestT02R41_R44` |
| R5.1 | 未付款/已取消/待审取消/跨店四种核销均拒绝 | PASS | `TestT02R33`、`TestT02CrossStoreAndPermissionDenied` |
| R5.2 | 未核销不能开始、未开始不能完成 | PASS | `checkin_test.go`、`TestT02R62` |
| R5.3 | **重复核销 ×3（含换员工）/重复完成 ×3 幂等，核销/服务记录各 1 条、收入 1 次** | PASS | `TestT02R53` |
| R6.1 | **核销 vs 取消并发 ×10 轮：每轮唯一合法结果、无双重副作用、名额一致** | PASS | `TestT02R61` |
| R6.2 | 已核销/服务中/已完成取消均拒绝且无退款单 | PASS | `TestT02R62` |
| R7.1 | 时段未结束（`NO_SHOW_TOO_EARLY`）/无原因（`NO_SHOW_REASON_REQUIRED`）/非店长（`ROLE_FORBIDDEN`）拒绝；结束后成功且释放名额一次、不可重复 | PASS | `TestT02R7` |
| R7.2 | 未到店不产生退款、日清 `NoShowHeldCents=9900`、不计入已完成收入 | PASS | `TestT02R7` |
| R7.3 | 用户可查 `NO_SHOW` 状态与原因 | PASS | `TestT02R7` |
| R8.1 | 日清五类计数（未到店计数=1） | PASS | `TestT02R7`、`daily_test.go` |
| R8.2 | 金额勾稽 `UnaccountedCents=0` | PASS | `TestT02R53`、`TestT02R7` |
| R8.3 | 已付款 ≠ 已完成（未到店/待退款分别列示） | PASS | `TestT02R7` |
| A9（幂等重放） | 同 key 同体重放返回首次结果、不重复建单/占名额 | PASS | `TestT02IdempotencyReplayAndConflict…` |
| A9（幂等冲突） | 同 key 不同载荷 → 409 `IDEMPOTENCY_CONFLICT` 且无副作用 | PASS | 同上 |
| A9/A10（幂等**作用域**） | 不同用户复用同一 key 不得读到他人结果 | **FAIL** | 探针 `TestT02Probe…`，缺陷 `D-T02-01` |
| P1–P7（越权/跨店） | 未登录 401、角色越权 403、跨店核销 403 `STORE_MISMATCH` 且无记录、查看他人预约 404、跨店客服审批 403 且申请仍 PENDING | PASS | `TestT02CrossStoreAndPermissionDenied` |
| P7（回调验签） | **缺签名/错签名支付回调 → 422 `PAYMENT_SIGNATURE_INVALID`，支付单仍 INIT、通知流水 0 条、状态不变；退款回调同理** | PASS | `TestT02P7PayNotifySignatureRequired` |
| I1（名额对账） | 各路径后 `reserved_count` 与有效预约一致、无负值 | PASS | 上述多例断言 |
| I13（唯一成功支付） | 同一预约 `SUCCESS` 支付恒为 1 条（含并发重复通知） | PASS | `TestT02R25_R27` |
| Q1 格式/静态 | `gofmt -l .` 空、`go vet ./...` 通过 | PASS | §2.1/2.2 |
| Q2 覆盖率 | domain 100% / repo 96.7% / service 85.2% / api 87.9% / internal 整体 86.7% | PASS（余量小） | §2.4 |
| Q4 并发/竞态 | 并发用例通过；`-race` **无法执行** | BLOCKED | §2.5 |
| Q6 冒烟 | 干净环境通过；**不可重复执行** | PASS（有条件） | §2.6，缺陷 `D-T02-02` |
| Q7 race 环境 | 缺 gcc / `CGO_ENABLED=0` | BLOCKED | §2.5 |

---

## 4. 缺陷表

### D-T02-01（P2）幂等键作用域未按用户隔离 → 跨用户复用同 key 读到他人结果

| 项 | 内容 |
|---|---|
| 级别 | P2（越权读取他人业务数据 + 静默丢单；不涉及资金变动，故不判 P0/P1，但必须在验收前关闭） |
| 关联规则 | `.agent/05 §3.4`（写接口幂等）、`.agent/11 §4` P1–P7（越权用例必须全部拒绝）、I11 |
| 复现步骤 | 1) `u1` 登录，`POST /api/reservations`，头 `Idempotency-Key: t02-shared-key`，体 `{"slotId":<slot>,"petId":1}` → 200，`reservationId=1`（owner=user 1）。<br>2) `u2` 登录，用**同一** key、**同一** body 请求同一路由。 |
| 期望 | 不同会话/用户维度的同名 key 应互不影响：要么为 `u2` 正常建单返回 `u2` 自己的预约，要么拒绝（409/400），**绝不能返回 u1 的响应**。 |
| 实际 | 200，响应体与 u1 完全一致：`{"reservationId":1,...,"status":"PENDING_PAYMENT"}`；库中 `u2` 自己的预约为 0 条（u2 的预约请求被静默吞掉）。 |
| 根因（只读定位，不修复） | `internal/api/server.go:215 idem()` 以 `(scope, key_value)` 为唯一键读写 `idempotency_key`，`scope` 仅取路由名（如 `reservation.create`），**不含 `userID`/会话**；命中即原样回放存储的响应体。 |
| 证据 | §2.7 探针原始输出；`internal/api/t02_probe_test.go`（`-tags=t02probe`） |
| 修复建议（交 Dev） | 幂等键唯一键改为 `(scope, actor_scope, key_value)`，`actor_scope` 取 `userID` 或 `staffID`（未认证接口用 IP+路由）；命中不同 actor 时按冲突处理，不复用响应。迁移需新增列/索引并回填历史行。 |
| 复测方式 | 修复后 `go test -tags=t02probe ./internal/api -run TestT02Probe` 必须由 FAIL 转 PASS，并重跑受影响包 + 全量 + 覆盖率。 |

### D-T02-02（P2）`scripts/smoke.ps1` 不可重复执行：子进程残留占用端口 + 固定回调流水号打持久库

| 项 | 内容 |
|---|---|
| 级别 | P2（Q6/A6 证据不可靠：第二次运行必然失败，且失败会被误判为“支付幂等缺陷”） |
| 关联规则 | `.agent/11 §5` Q6（集成/冒烟通过）、`.agent/06 §3`（原始输出归档）、`.agent/05 §3.5`（清理任务幂等思路） |
| 复现步骤 | 1) 确认 18080 空闲；2) `powershell -ExecutionPolicy Bypass -File scripts/smoke.ps1` → PASS，exit 0；3) `Get-NetTCPConnection -State Listen -LocalPort 18080` → 仍被 `server.exe`（PID 变化）占用；4) **不清理**直接再次执行同一脚本 → 失败。 |
| 期望 | 脚本结束（成功或失败）后释放端口与子进程；连续两次运行结果一致（幂等、可重复）；就绪探针只认自己启动的实例。 |
| 实际 | `finally` 中 `Stop-Process -Id $proc.Id` 只杀掉 `go run` 外壳进程，其派生的 `server.exe` 不受影响继续监听 18080；第二次运行时新实例绑定失败（`log.Fatalf`）但 `$proc.HasExited` 未检查，`/readyz` 探针命中残留实例（连的是持久库 `paw-book.db`，其中已存在 `channel_txn_id=smoke-txn-1`），于是支付回调返回 `DUPLICATE_IGNORED`，脚本抛错退出 1。 |
| 证据 | §2.6 三段原始输出（run#1 PASS/exit0 + 18080 仍被占 + run#2 FAIL/exit1） |
| 修复建议（交 Dev） | ① 启动后校验 `$proc.HasExited`/绑定日志，失败即报“端口被占用”；② 结束前杀进程树（`taskkill /PID <pid> /T /F` 或记录子进程 PID）；③ 回调流水号改为随机（`smoke-txn-$([guid]::NewGuid())`）；④ 端口与 DB 路径均随机化，避免任何跨运行污染。 |
| 复测方式 | 连续执行两次脚本均 exit 0，且两次结束后 18080/进程均无残留。 |

### D-T02-03（P3）被测版本不可追溯：工作区含未提交的生产/结构改动

| 项 | 内容 |
|---|---|
| 级别 | P3（流程/证据合规） |
| 关联规则 | `.agent/08 §1`（Test 必须针对**指定提交 SHA** 的代码）、`.agent/11 §6` G3/G5 |
| 复现步骤 | `git status --porcelain` → `D internal/store/schema.sql`、`M cmd/paychannel/main.go`、`M AGENTS.md`、`M README.md`、以及 `.agent/*`、`migrations/002_seed.sql`、`migrations/embed.go`、`cmd/audit`、`cmd/migrate` 全部未跟踪。 |
| 期望 | 测试对象是干净 checkout 的 `1fb9d09`，任何改动都有提交与 SHA。 |
| 实际 | 本次门禁跑在“1fb9d09 + 未提交改动”上；`schema.sql` 被删除而 `store.Migrate` 已改用 `migrations/embed.go`，若他人 checkout `1fb9d09` 复跑，文件集合与我不同。 |
| 证据 | §1.1 |
| 修复建议（交 Dev） | 把 `.agent/*`、`migrations/*`、`cmd/audit`、`cmd/migrate`、`internal/store/schema.sql` 的删除等改动提交（或还原），给出新的被测 SHA，再由 Test 复跑门禁。 |

### 建议项（非阻塞，P3/观察）

1. **service 包覆盖率余量仅 0.2pp**（85.2% vs 门槛 85%），且最低函数集中在资金主链路：`applyPaySuccess 69.6%`、`CreatePayment/CancelReservation/CreateReservation/CheckIn/ReviewCancel 81%–82%`（多为 DB 错误回滚分支，已有 trigger 注入测试法可复用）。建议补若干回滚分支用例，避免后续任何小改动即跌破门槛。
2. **`GET /api/smoke` 未鉴权**：返回门店/员工/项目/时段计数（A20 只读用途）。风险低，建议至少限制在生产构建关闭或加鉴权。
3. **`-race` 未执行**：见 §5，环境补齐后必须补跑并归档 `race-env.txt`。

---

## 5. 覆盖率表

| 包 | 语句覆盖率 | 门槛 | 判定 |
|---|---|---|---|
| `internal/domain` | **100.0%** | ≥85% | PASS |
| `internal/repo` | **96.7%** | ≥85% | PASS |
| `internal/service` | **85.2%** | ≥85% | PASS（余量 0.2pp，偏险） |
| `internal/api` | **87.9%** | ≥85% | PASS |
| `internal` 整体（`go test ./internal/...` 合计） | **86.7%** | ≥85%（整体） | PASS |
| 未统计/排除 | `cmd/*`（server/audit/migrate/paychannel）、`internal/store`、`internal/config`、`migrations`（无测试或按 D8 排除，需 Acceptance 按 Q3/Q8 复核理由） | — | 待验收确认 |

覆盖率作弊检查（`.agent/06 §6`）抽查结果：未发现 `t.Skip` 掩盖失败、未发现排除自编 DB/HTTP 包、未发现只断状态码的占位断言；`internal/paychannel`、`internal/payclient` 覆盖率 100%/88.5%。

---

## 6. 结论

### 门禁逐项

| 项 | 结论 |
|---|---|
| `gofmt -l .` | **PASS**（空输出） |
| `go vet ./...` | **PASS** |
| `go test ./... -count=1` | **PASS**（108 用例全通过，0 失败） |
| 四个目标包覆盖率 | **PASS**（100% / 96.7% / 85.2% / 87.9%；internal 整体 86.7%） |
| `scripts/smoke.ps1` | **PASS（有条件）**：干净环境一次通过；不可重复执行，须修 D-T02-02 |
| `go test -race ./...` | **BLOCKED** |

### 总体结论：**FAIL（含 1 项 BLOCKED）**

理由（只依据本次会话输出）：

1. **FAIL 项**：`D-T02-01`（P2）—— 不同用户复用同一 `Idempotency-Key` 可读到他用户首次结果（探针实测 200 + 他人 `reservationId`），违反 `.agent/11 §4` “P1–P7 越权用例全拒绝且数据未变化”与 `.agent/05 §3.4` 幂等语义；A9/A10 的“幂等作用域”判定为 FAIL。修复前不得进入验收。
2. **需修复的 Q6 风险**：`D-T02-02`（P2）—— 冒烟脚本子进程残留 + 固定回调流水号，导致第二次运行必然失败；当前“冒烟通过”只有在手动清空 18080 后才成立，作为交付证据不可靠。
3. **-race 阻塞说明（E1）**：本机 `CGO_ENABLED=0` 且 PATH 中无 `gcc`，实测输出 `go: -race requires cgo` 与 `cgo: C compiler "gcc" not found`。**没有 race 结果即 `.agent/06 §0` 不合格**，因此该门禁记 **BLOCKED**，不据此判 PASS；并发正确性本次仅以“多轮并发 + 终态/记录数断言”替代（`TestT02R61` 10 轮唯一合法结果、`TestT02R53` 重复幂等），不声称等价 `-race`。
4. 其余 R1–R8、I1/I13、A9（重放/冲突）、P1–P7（含回调验签）均 **PASS**，且全部来自本会话独立编写的用例与命令输出，未引用 Dev 声明。

### 交回 Dev 的待办（按优先级）

1. 修 `D-T02-01`（幂等键按用户/会话隔离 + 迁移 + 用例），修完通知 Test 用 `-tags=t02probe` 复测。
2. 修 `D-T02-02`（冒烟脚本进程树清理、端口/DB/流水号随机化、启动绑定校验），并连续两次运行取证。
3. 提交/还原工作区改动，给出新的被测 SHA（`D-T02-03`）。
4. 安装 MinGW-w64（`CGO_ENABLED=1` + `gcc` 可用），执行并归档 `go test -race ./... -count=1` 与 `race-env.txt`，Test 再复跑全量。
5. 按建议项补 service 包回滚分支用例，抬高 85.2% 的余量。

---

## 附录：本会话新增测试文件清单

| 文件 | 覆盖内容 |
|---|---|
| `internal/service/t02_agent_verify_test.go` | R3.1 边界、R3.2 边界、R3.3 待审不可核销、R4.1/R4.2/R4.4 退款生命周期、R5.3 重复核销/完成幂等、R2.5/R2.7 唯一成功支付、R6.2 三状态不可取消、R6.1 核销 vs 取消并发 ×10 轮 |
| `internal/service/t02_agent_verify_extra_test.go` | R2.1/R2.2 占位边界与单次释放、R2.6 关闭后支付、R7.1–R7.3 未到店口径 + R8 日清勾稽 |
| `internal/api/t02_agent_verify_test.go` | P7 支付/退款回调验签缺失拒绝且无副作用、A9 幂等重放与冲突、P1–P7 未登录/角色/跨店拒绝 |
| `internal/api/t02_probe_test.go` `//go:build t02probe` | `D-T02-01` 复现探针（默认不参与 `go test ./...`，用 `-tags=t02probe` 运行） |
