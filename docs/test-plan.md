# T02-T11 独立测试入口（Test Agent 专用）

- 被测版本：git `1fb9d09`（`dev/02` 分支，2026-11-12）
- 测试范围：题面 R1–R8、I1–I13、A1–A20、页面 E3、幂等/签名 P7
- 约束（08）：Test 只允许新增/修改测试文件与测试报告；发现生产代码缺陷 → 写缺陷记录交回 Dev，不得直接改 `internal/**` 非测试代码。

## 1. 环境

```powershell
cd d:/gopath/src/paw-book
go version            # go1.26 windows/amd64
go test ./... -count=1
```

- 库：SQLite `t.TempDir()` 临时文件（D1/D8），不需要 Docker/MySQL。
- 渠道：`internal/paychannel` 模拟；回调签名 `HMAC-SHA256(body, PAW_PAY_SECRET)`。
- `-race`：本机无 gcc，预期 BLOCKED（E1），记录为环境限制，不得据此判 PASS。

## 2. 必跑命令（原始输出贴进报告）

```powershell
gofmt -l .
go vet ./...
go test ./... -count=1
go test ./internal/domain -count=1 -coverprofile=domain.cover
go test ./internal/repo   -count=1 -coverprofile=repo.cover
go test ./internal/service -count=1 -coverprofile=service.cover
go test ./internal/api    -count=1 -coverprofile=api.cover
go tool cover -func=<pkg>.cover
powershell -ExecutionPolicy Bypass -File scripts/smoke.ps1
```

## 3. 用例矩阵（按规则）

| 规则 | 必测点 | 现有用例（参考） |
|---|---|---|
| R1.1 | 容量 1–3、过去时段拒绝、非店长拒绝 | `service/slot_test.go` |
| R1.2 | 同宠物同时段唯一、取消后可再约 | `reservation_test.go` |
| R1.3/R1.4 | 名额不超卖、并发 50 抢 3 | `TestConcurrentReservations` |
| R2.1/R2.2 | 5 分钟占位、超时释放、重复清理 | `TestCreateReservation_PayDeadlineAndExpire` |
| R2.3 | 未付款取消释放名额、无退款 | `TestCancelUnpaid` |
| R2.4/R2.5 | 支付成功确认、重复回调幂等 | `pay_test.go` |
| R2.6 | 取消/过期后支付 → 异常 + 退款 | `TestPayNotifyAfterCancel`、`TestPayNotifyAfterExpire` |
| R2.7 | 失败重试生成新流水新 orderNo、仅一条 SUCCESS | `TestCreatePaymentRetryCreatesNewAttempt` |
| R3.1/R3.2 | ≥2h 自动受理并退款；<2h 必填原因进审核 | `TestCancelReservation` 边界两侧 |
| R3.3 | 待审名额保留、批准取消+退款、拒绝恢复已确认 | `cancel_test.go` |
| R4.1–R4.4 | 退款失败原因、重试次数、退款不改变预约状态 | `refund_test.go` |
| R5.1/R5.3 | 未付款/取消/待审不可核销、重复核销幂等 | `checkin_test.go` |
| R5.2 | 仅已到店可开始、仅服务中可完成 | `TestServiceStartAndComplete` |
| R6.1/R6.2 | 并发只有一个方向成功、终态不可再变更 | 并发用例、`TestCancelReservation` 三状态 |
| R7.1–R7.3 | 时段结束前不可标记、原因必填、不退款、释放名额 | `TestMarkNoShow` |
| R8.1–R8.3 | 五类计数、六项金额、勾稽差为 0、异常单 | `daily_test.go` |
| A1–A20 | 路由/鉴权/角色/幂等/签名/错误码 | `internal/api/api_test.go` |
| E3 | 六页面可访问且真实调接口 | `TestAuthPagesAndHealth` + 手工冒烟 |

## 4. 缺陷记录要求

```text
缺陷编号 / 严重级别(P0-P3) / 复现步骤 / 期望 / 实际 / 证据(命令+输出) / 关联规则号 / 修复提交 / 复测结论
```

P0（资损/超卖/越权）、P1（主流程不可用或状态错）、P2（边界/提示）、P3（文档/样式）。

## 5. 报告输出

写入 `docs/testing/T02-T11-report.md`：环境、命令原始输出、用例结果表、缺陷表、覆盖率、结论（PASS/FAIL/BLOCKED）。不得引用 Dev 的完成声明作为证据。
