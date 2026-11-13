# T02–T11 缺陷复测报告（Test Agent 复测会话）

- 角色：Test Agent（**独立复测会话**，与 Dev 会话上下文隔离）
- 复测对象：`git d315258`（分支 `dev/02`，HEAD），修复提交 `4cbaf00` + 收尾提交 `d315258`
- 上轮报告：`docs/testing/T02-T11-report.md`（被测版本 `1fb9d09`，结论 FAIL）
- 复测范围：缺陷 `D-T02-01`、`D-T02-02`、`D-T02-03` + 受影响包与全量回归 + 四包覆盖率 + `-race` 门禁
- 原则：**结论只依据本会话亲自执行的命令输出**，未引用 Dev 的修复说明或完成声明

---

## 1. 基线与环境

### 1.1 工作区与 SHA（复测前置）

```text
$ git status --porcelain
（空）
$ git rev-parse HEAD
d315258709a529aac5df3a676d1194d3fe3d428f
$ git log --oneline -5
d315258 [chore] [v0.1.2] #1001 规则包与迁移嵌入入库、补充测试会话用例与覆盖率证据，清理工作区
4cbaf00 [fix] [v0.1.2] #1001 修复幂等 key 未按用户隔离(D-T02-01)与冒烟脚本残留进程(D-T02-02)，补自编 Skill 调用证据
1fb9d09 [fix] [v0.1.2] #1001 教学时钟基准改为00:00Z，发布时段提示服务器时间并按服务器时间预填
```

判定：**PASS** —— 复测开始时工作区干净，被测代码 = `d315258` 单一提交，可追溯。

### 1.2 环境

| 项 | 实际值 |
|---|---|
| OS / Shell | win32 / PowerShell |
| `go version` | `go version go1.26.0 windows/amd64` |
| `go` 解析顺序 | `C:\Users\ndkdl\AppData\Local\Go1.25.4\Go\bin\go.exe`、`D:\golang\bin\go.exe`（多版本共存） |
| `go env GOROOT` | `D:\GoUser\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.0.windows-amd64`（GOTOOLCHAIN=auto 切换） |
| `go env CGO_ENABLED` | `0` |
| `gcc` | 未安装：`gcc : 无法将“gcc”项识别为 cmdlet…` |
| 数据库 | SQLite 临时文件库（`t.TempDir()`） |

> 环境告警（非产品缺陷）：`go test ./internal/... -coverprofile=...` 偶发打印
> `compile: version "go1.26.0" does not match go tool version "go1.25.4"`（internal/config、internal/store 等），
> 但被测包最终 `ok`。属本机 1.25.4/1.26.0 双工具链 + 构建缓存混用，见 §7 观察项 O-2。

### 1.3 本会话改动范围（自查）

只新增测试文件与本报告，未修改任何生产代码、迁移、规则文档：

```text
$ git status --porcelain
?? internal/api/t02_retest_dt0201_test.go      ← 新增（默认参与 go test ./...）
?? internal/api/t02_retest_probe_test.go       ← 新增（//go:build t02probe，默认不参与）
?? docs/testing/T02-T11-retest.md              ← 本报告
```

---

## 2. 缺陷 D-T02-01 复测（Idempotency-Key 跨用户读到他人结果）

### 2.1 复测用例设计（独立编写，不复用上轮探针判定条件）

文件：`internal/api/t02_retest_dt0201_test.go`

| 用例 | 断言要点 |
|---|---|
| `TestRetestDT0201_IdempotencyKeyScopedPerUser` | ① `u1` 用 `petId=1`、key `K` 建单 → 200，预约归属 `user=1/pet=1`；② `u2` 用**同一 key**、**自己宠物 `petId=3`** 建单 → 必须 200 且 `reservationId != u1 的`、响应体不含 u1 的 id、库中 `user_id=2` 预约数 = 1、`pet_id=3` 归属正确；③ `idempotency_key` 记录按用户分域（`reservation.create:user:1` / `:user:2` 各 1 条）；④ 同用户同 key 同体重放 → 响应字节与首次一致、预约数仍 2、`reserved_count=2`；⑤ 同用户同 key 异体（换 slot）→ 409 `IDEMPOTENCY_CONFLICT` 且 slot2 无预约 |
| `TestRetestDT0201_UnauthenticatedIdempotencyKeyHasNoScope` | 覆盖修复新增分支：未登录带 `Idempotency-Key` → 401 `UNAUTHENTICATED`，`idempotency_key` 不落任何行、不建单；随后同一 key 由 `u1` 使用必须正常 200 |

> 上轮探针 `t02_probe_test.go` 用 `petId=1` 重放，修复后返回 403 即算 PASS，判定条件偏弱（见 §7 观察项 O-1）。
> 本会话用例改用 `petId=3`（u2 名下宠物），直接断言“u2 得到自己的 reservationId”。

### 2.2 命令与原始输出

```text
$ go test ./internal/api -run 'TestRetestDT0201' -count=1 -v
=== RUN   TestRetestDT0201_IdempotencyKeyScopedPerUser
--- PASS: TestRetestDT0201_IdempotencyKeyScopedPerUser (0.14s)
=== RUN   TestRetestDT0201_UnauthenticatedIdempotencyKeyHasNoScope
--- PASS: TestRetestDT0201_UnauthenticatedIdempotencyKeyHasNoScope (0.13s)
PASS
ok  	paw-book/internal/api	3.498s
```

同一缺陷的**原始复现探针**（上轮步骤，`-tags=t02probe`）：

```text
$ go test -tags=t02probe ./internal/api -run 'TestT02Probe' -count=1 -v
=== RUN   TestT02ProbeCrossUserIdempotencyKeyReplay
    t02_probe_test.go:45: u2 复用同一 Idempotency-Key 的响应：status=403 body={"code":"PET_NOT_OWNED","message":"只能为本人名下的宠物预约"}
    t02_probe_test.go:51: 库中 u2 自己的预约数 = 0（期望 1）
    t02_probe_test.go:58: 其他行为（非本缺陷判定）：403
--- PASS: TestT02ProbeCrossUserIdempotencyKeyReplay (0.13s)
```

对照上轮同命令输出（`1fb9d09`）：`status=200`、`reservationId=1`（u1 的）、`u2 预约数=0` → 本轮已不再回放他人响应。

### 2.3 结论：**PASS**

依据：u2 用同一 `Idempotency-Key` + 自己宠物建单获得**自己的** `reservationId`（≠ u1），库中 u2 预约 1 条且归属正确，响应体不含 u1 结果；幂等记录作用域含操作者维度；同用户同 weight 重放仍返回首次响应、异体 409 且无副作用。

**反向验证限制（如实记录）**：为证明新用例能捕获原缺陷，原计划在临时 worktree 用 `1fb9d09` 的 `server.go` 跑本用例（预期 FAIL）。`1fb9d09` 自身 `internal/store/store.go` 引用了当时未入库的 `paw-book/migrations`，无法编译（`package paw-book/migrations is not in std` / `[setup failed]`），替换为 `d315258 + 旧 server.go` 的临时副本方案未经批准未执行。因此“用例有效性”依据为：断言点与缺陷症状一一对应（u2 拿到 u1 的 id 即 fatal），且有 §2.2 修复前后探针输出对比。

---

## 3. 缺陷 D-T02-02 复测（冒烟脚本不可重复执行 / 残留进程）

### 3.1 复测步骤

1. 记录运行前的 `server.exe` 进程与 18000–18200 监听；
2. 连续执行 `scripts/smoke.ps1` 两次（**中间不做任何清理**，与上轮复现步骤一致）；
3. 每次记录退出码、输出、进程与端口状态；
4. 检查 18080 及 18100–18199 端口残留、`%TEMP%` 冒烟临时文件残留。

### 3.2 原始输出

```text
=== BEFORE: server.exe ===
29740,35628
=== BEFORE: listeners 18000-18200 ===
18176=35628 18105=29740
=== RUN #1 ===
PASS 冒烟：slot=1 reservation=1 payment=1 收入=9900 差额=0 种子=1/4/1
run1-exit=0
=== AFTER #1: server.exe ===
29740,35628,41600                      ← 新增 41600
=== AFTER #1: listeners ===
18176=35628 18131=41600 18105=29740     ← 新增监听 18131
=== RUN #2 (不清理) ===
PASS 冒烟：slot=1 reservation=1 payment=1 收入=9900 差额=0 种子=1/4/1
run2-exit=0
=== AFTER #2: server.exe ===
29740,31840,35628,41600                 ← 又新增 31840
=== AFTER #2: listeners 18000-18200 ===
18176=35628 18161=31840 18131=41600 18105=29740
=== AFTER #2: listeners 18080 ===
（空）
```

> 上述运行前已存在 `29740`(18105)、`35628`(18176) 两个**历史残留** `server.exe`，说明“残留”是持续性问题，不是本次运行的偶发。
> 首次运行输出在 PowerShell 捕获中出现中文乱码（GBK/UTF-8 捕获差异），脚本自身退出码为 0、无 stderr。

冒烟临时文件残留（`%TEMP%`）：

```text
paw-smoke-*.db / *.db-shm / *.db-wal 系列 = 32 个文件
paw-smoke-*.log / *.log.err 系列        = 22 个文件
```

### 3.3 逐项判定（按上轮缺陷记录的通过标准）

| 通过标准 | 实测 | 判定 |
|---|---|---|
| 连续两次运行均 `exit 0` | `run1-exit=0`、`run2-exit=0`，两次均 `PASS` | **PASS**（已修复：端口 `Get-Random 18100-18199` + 回调流水号 `smoke-txn-<guid>`） |
| 两次结束后 18080 无残留监听 | `18080` 无监听 | **PASS**（脚本已不再使用 18080） |
| 两次结束后**无残留进程** | 每次运行结束后都**新增一个 `server.exe`** 子进程并长期监听随机端口（run#1 → PID 41600/18131；run#2 → PID 31840/18161），进程数 2→3→4 单调增长 | **FAIL** |
| 临时库/日志清理 | `paw-smoke-*.db*` 32 个、`*.log*` 22 个残留（子进程仍持有句柄，`Remove-Item` 静默失败） | **FAIL** |
| 启动后校验子进程存活（绑定失败即报错） | `scripts/smoke.ps1` 启动后**没有** `$proc.HasExited`/绑定失败校验（仅 `finally` 里 `Stop-Process -Id $proc.Id`，杀的是 `go run` 外壳，非 `server.exe`） | **FAIL（未修）** |

### 3.4 结论：**FAIL（部分修复）**

- **已修复**：可重复执行（连续两次 `exit 0`）、18080 不再被占用、回调流水号随机化后不再撞持久库。
- **未修复**：`go run` 派生的 `server.exe` 未被回收（缺陷记录中修复建议的 ②「杀进程树」与 ①「启动绑定校验」均未实现），每次冒烟泄漏 1 个进程 + 1 个端口 + 临时库/日志文件。
- **后果（可推算的确定性风险）**：泄漏端口落在脚本自身的随机池 18100–18199 内。当累积 N 个残留时，新运行命中已占用端口的概率 ≈ N/99；一旦命中，新实例 `log.Fatalf` 退出，而 `/readyz` 探针会连到**旧实例**（其库中已有上一轮的 9000→9900 已完成预约），日清 `CompletedIncomeCents` 变成 19800，脚本抛错 `exit 1`。即：残留进程会让后续冒烟变成“偶发失败 + 可能连错实例出假证据”。因此**不建议按 P3 降级**，维持 **P2**。

---

## 4. 缺陷 D-T02-03 复测（工作区脏 / 版本不可追溯）

```text
$ git status --porcelain          # 复测开始时
（空）
$ git rev-parse HEAD
d315258709a529aac5df3a676d1194d3fe3d428f
```

- 上轮遗留的 `D internal/store/schema.sql`、未跟踪的 `.agent/*`、`migrations/*`、`cmd/audit`、`cmd/migrate` 均已入库（`d315258` 提交内容可见）。
- `1fb9d09` 曾无法编译（`internal/store/store.go` 引用当时未入库的 `paw-book/migrations`），`d315258` 已可独立 checkout 编译。

### 结论：**PASS**（已关闭）。复测期间工作区仅有本会话新增的 2 个测试文件 + 本报告。

---

## 5. 全量回归

```text
$ gofmt -l .
（空）                       gofmt-exit=0

$ go vet ./...
（无输出）                   vet-exit=0

$ go test ./... -count=1
?   	paw-book/cmd/audit	[no test files]
?   	paw-book/cmd/migrate	[no test files]
?   	paw-book/cmd/paychannel	[no test files]
?   	paw-book/cmd/server	[no test files]
ok  	paw-book/internal/api	6.457s
?   	paw-book/internal/config	[no test files]
ok  	paw-book/internal/domain	0.977s
ok  	paw-book/internal/paychannel	1.458s
ok  	paw-book/internal/payclient	4.491s
ok  	paw-book/internal/repo	3.490s
ok  	paw-book/internal/service	15.369s
?   	paw-book/internal/store	[no test files]
?   	paw-book/migrations	[no test files]
test-exit=0
```

逐用例统计（`go test ./... -count=1 -v` 汇总）：

```text
pass=116 fail=0 skip=0
```

受影响的 `internal/api` 包（本轮修复 + 新增用例）单独重跑亦为 `ok`。

---

## 6. 覆盖率（四包 + internal 整体）

```text
$ go test ./internal/domain  -count=1 -coverprofile=...   → coverage: 100.0% of statements
$ go test ./internal/repo    -count=1 -coverprofile=...   → coverage: 96.7%  of statements
$ go test ./internal/service -count=1 -coverprofile=...   → coverage: 85.2%  of statements
$ go test ./internal/api     -count=1 -coverprofile=...   → coverage: 88.1%  of statements
$ go test ./internal/...     -count=1 -coverprofile=... ; go tool cover -func
total:						(statements)	86.8%
```

| 包 | 覆盖率 | 门槛 | 判定 | 上轮对比 |
|---|---|---|---|---|
| `internal/domain` | 100.0% | ≥85% | PASS | 100.0%（持平） |
| `internal/repo` | 96.7% | ≥85% | PASS | 96.7%（持平） |
| `internal/service` | 85.2% | ≥85% | PASS（余量 0.2pp） | 85.2%（持平，仍偏险） |
| `internal/api` | 88.1% | ≥85% | PASS | 87.9% → 先跌至 87.5%，补测后 88.1% |
| `internal` 整体 | 86.8% | ≥85% | PASS | 86.7% |

说明：修复新增的 `idem()` 未登录分支（`sessionOf` 失败直接交给 handler）在修复提交里**没有对应测试**，单独跑 api 包时覆盖率由 87.9% 降到 87.5%、`idem` 函数 83.3%；本会话补 `TestRetestDT0201_UnauthenticatedIdempotencyKeyHasNoScope` 后 api 回到 88.1%，符合 `.agent/06 §5`「新增未覆盖分支必须补测试」。

覆盖率作弊抽查（`.agent/06 §6`）：未发现 `t.Skip` 掩盖失败、未按包整体排除、未发现只断状态码的占位断言；本会话新增断言均校验业务字段（reservationId 归属、idempotency_key 作用域、支付单数、名额计数）。

---

## 7. `-race` 门禁（BLOCKED，环境限制）

```text
$ go test -race ./internal/domain -count=1
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
race-exit=2

$ $env:CGO_ENABLED='1'; go test -race ./internal/domain -count=1
# runtime/cgo
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
FAIL	paw-book/internal/domain [build failed]
race-cgo-exit=1
```

结论：**BLOCKED**（与上轮同因）。本机 `CGO_ENABLED=0` 且 PATH 无 `gcc`，`-race` 无法执行，不据此判 PASS；并发正确性仍只由“多轮并发 + 终态/记录数断言”支撑（`TestT02R61` 10 轮、`TestT02R53` 重复幂等、`TestT02R25_R27` 并发重复支付）。

---

## 8. 复测中新发现的问题

| 编号 | 级别 | 结论 |
|---|---|---|
| `D-T02-04` | P3（相邻风险，非本轮缺陷复现失败） | 幂等指纹**不含路由路径/路径参数**：同一用户对**不同预约**复用同一 `Idempotency-Key`（`POST /api/reservations/{id}/pay`，body 为空）时，第二次原样回放第一次的响应，第二个预约的支付请求被**静默吞掉** |

`D-T02-04` 证据（`internal/api/t02_retest_probe_test.go`，`-tags=t02probe`）：

```text
$ go test -tags=t02probe ./internal/api -run 'TestT02RetestProbe' -count=1 -v
    t02_retest_probe_test.go:38: res2 用同一 key 发起支付：status=200 body={"code":0,"data":{...,"orderNo":"P1-1794636000000000000","paymentId":1,"status":"INIT"},"message":"ok"}
    t02_retest_probe_test.go:44: 库中 res2 的支付单数 = 0（期望 1，或第二次返回 409 冲突）
    t02_retest_probe_test.go:47: 风险：同一 Idempotency-Key 跨不同预约重放时回放了首个预约的响应，res2 未建支付单（幂等指纹不含路由路径/路径参数，server.go idem() 仅对 body 求 hash）；res1=1 res2=2
--- FAIL: TestT02RetestProbeIdempotencyKeyNotScopedByResourcePath (0.15s)
```

- 关联规则：`.agent/05 §3.4`（同 key 不同载荷 → `IDEMPOTENCY_CONFLICT`）、`.agent/11` P1–P7 精神（不得返回与请求资源无关的结果）。
- 触发条件：需要客户端跨请求复用同一 key（正常客户端每请求一个新 key 则不触发），故判 P3，**不阻塞**本轮缺陷关闭。
- 建议（交 Dev，可选）：幂等指纹加入 `r.Method + r.URL.Path`（或 `scope+path`）后再求 hash，命中不同资源时按 409 处理；同时补一条用例。
- 说明：该探针带 `//go:build t02probe` 标签，默认不参与 `go test ./...`，不影响 §5 全绿结果（与上轮 `t02_probe_test.go` 同一约定）。

### 观察项（非缺陷 / 建议）

- **O-1 上轮探针判定偏弱**：`t02_probe_test.go` 在修复后 PASS 的原因是 `403 PET_NOT_OWNED`（u2 借 u1 的 petId 请求被归属校验拦下），而非“u2 得到自己的结果”。若只看该探针会把“仍会串号”的修复判成通过。建议后续以 `internal/api/t02_retest_dt0201_test.go` 为回归口径（u2 用 `petId=3`），保留原探针仅作历史证据。
- **O-2 工具链混用**：本机存在 1.25.4 与 1.26.0 两套 `go`，`GOTOOLCHAIN=auto` 会切到模块工具链，偶发 `compile: version ... does not match go tool version ...`。建议固定 `GOTOOLCHAIN=local` 或统一 PATH，否则 CI/验收复跑可能出现不可复现的编译噪音。
- **O-3 冒烟临时文件残留**：`%TEMP%` 已有 32 个 `paw-smoke-*.db*`、22 个 `*.log*`。修 `D-T02-02` 时一并清理历史残留，并在脚本里改用 `Remove-Item -Recurse` + 确认删除。
- **O-4 需人工清理的残留进程**（本会话两次冒烟新产生，未擅自 kill）：`41600`(18131)、`31840`(18161)；另有历史残留 `29740`(18105)、`35628`(18176)。建议 `taskkill /PID <pid> /T /F` 后重跑一次冒烟确认无新增。
- **O-5 service 覆盖率余量 0.2pp**（85.2%）未改善，建议继续按上轮建议补资金链路回滚分支用例。

---

## 9. 最终结论

| 门禁项 | 结论 |
|---|---|
| 基线可追溯（`d315258`、工作区干净） | **PASS** |
| `D-T02-01` 幂等键跨用户隔离 | **PASS（已关闭）** |
| `D-T02-02` 冒烟可重复执行 | **PASS（已修复）** |
| `D-T02-02` 冒烟不残留进程/临时文件 | **FAIL（未修复，维持 P2）** |
| `D-T02-03` 工作区/版本可追溯 | **PASS（已关闭）** |
| `gofmt -l .` | **PASS**（空输出） |
| `go vet ./...` | **PASS** |
| `go test ./... -count=1` | **PASS**（116 用例通过，0 失败 0 跳过） |
| 四包覆盖率 | **PASS**（100.0 / 96.7 / 85.2 / 88.1，internal 整体 86.8%） |
| `go test -race ./...` | **BLOCKED**（无 gcc、`CGO_ENABLED=0`） |

### 总体：**FAIL**

理由（只依据本会话输出）：

1. `D-T02-01`、`D-T02-03` 复测通过，可关闭；`-race` 仍 BLOCKED（环境限制，需装 MinGW 后补跑归档 `race-env.txt`）。
2. **`D-T02-02` 仅部分修复**：`smoke.ps1` 连续两次均 `exit 0`、18080 无残留 → 上轮“第二次必然失败”的现象已消失；但每次运行仍泄漏一个 `server.exe`（PID 41600/18131、31840/18161）与临时库/日志文件，且启动阶段仍无绑定失败校验。按上轮缺陷记录的通过标准（“结束进程无残留”）判 **FAIL**，维持 P2，本次不降级。
3. 新登记 `D-T02-04`（P3，幂等指纹不含路由路径，跨资源重放回放首次响应）为非阻塞建议项。
4. 在产品侧“全量回归 + 覆盖率 + 格式/静态检查”上无回退，除上述遗留项外未发现新的功能性缺陷。

### 交回 Dev 的待办（按优先级）

1. **P2 `D-T02-02`（未完成项）**：`smoke.ps1` 结束前杀进程树（`taskkill /PID $proc.Id /T /F` 或记录并杀 `server.exe` 子进程），启动后用 `$proc.HasExited` + 日志校验绑定结果，临时库/日志在进程退出后再删；修完连续跑两次，用 `Get-Process -Name server` 与 `Get-NetTCPConnection` 证明零残留。
2. **P3 `D-T02-04`**：幂等指纹纳入 `Method + URL.Path`（或按资源分 scope），跨资源复用同 key 时返回 409。
3. **BLOCKED `-race`**：安装 MinGW-w64（`CGO_ENABLED=1` + `gcc` 可用），执行 `go test -race ./... -count=1` 并归档 `docs/evidence/tests/<sha>/race.txt` 与 `race-env.txt`，再交 Test 补跑。
4. 建议项：`O-1`（以新用例为回归口径）、`O-2`（固定工具链）、`O-3/O-4`（清理残留）、`O-5`（抬高 service 覆盖率余量）。

---

## 附录：本会话新增文件

| 文件 | 性质 | 内容 |
|---|---|---|
| `internal/api/t02_retest_dt0201_test.go` | 测试（参与 `go test ./...`） | `D-T02-01` 独立复测：跨用户同 key 各自建单、归属与库内计数、幂等记录分域、同体重放、异体 409；未登录带 key 分支 |
| `internal/api/t02_retest_probe_test.go` | 测试（`//go:build t02probe`，默认不参与） | `D-T02-04` 相邻风险探针：同 key 跨预约重放 |
| `docs/testing/T02-T11-retest.md` | 报告 | 本文件 |

---

## 10. 第三轮定向复测（`9a641c8`，仅 `D-T02-02` 残留 / `D-T02-04` 两项）

- 角色：Test Agent，**独立复测会话**（与 Dev 上下文隔离），零扩展范围
- 被测版本：`9a641c8`（Dev 修复提交：`f54ba15` + 收尾）
- 原则：结论只依据本节亲自执行的命令原始输出

### 10.1 基线与自查

```text
$ git rev-parse HEAD
9a641c8763d8d38d270d0b8ea6171413c79b59b1
$ git status --porcelain
?? docs/testing/T02-T11-retest.md
$ git log --oneline -3
9a641c8 [fix] [v0.1.2] #1001 幂等指纹纳入路由(D-T02-04)、冒烟改用独立二进制并彻底清理子进程/端口(D-T02-02)
f54ba15 [fix] [v0.1.2] #1001 幂等作用域加入路由维度(D-T02-04)，冒烟脚本改用独立二进制并清理子进程/端口(D-T02-02)
d315258 [chore] [v0.1.2] #1001 规则包与迁移嵌入入库、补充测试会话用例与覆盖率证据，清理工作区
```

判定：**PASS** —— HEAD 与约定基线一致；工作区唯一未跟踪项为本报告（上轮遗留），无生产代码/迁移改动。

代码侧确认（只读）：

- `internal/api/server.go:245`：`sum := sha256.Sum256(append([]byte(r.Method+"\n"+r.URL.Path+"\n"), body...))` —— 指纹已含 method + path；
- `scripts/smoke.ps1:16`：`go build -o $exe ./cmd/server` 改用独立 exe；`:84-86` 按端口兜底 `Stop-Process`；`:88` 统一删除 db/log/exe。

### 10.2 `D-T02-02` 复测（冒烟残留进程/端口/临时文件）：**PASS**

复测步骤：记录基线 → 连续执行 `scripts/smoke.ps1` 两次（**中间不清理**）→ 每次记录退出码 + `server.exe` 进程数 + `18100–18199` 监听数。

```text
=== BEFORE server.exe PIDs ===
(none)
count=0
=== BEFORE listeners 18100-18199 ===
(none)
lcount=0
=== RUN #1 ===
PASS 鍐掔儫锛歴lot=1 reservation=1 payment=1 鏀跺叆=9900 宸=0 绉嶅瓙=1/4/1
run1-exit=0
=== AFTER1 server.exe PIDs ===
(none)
count=0
=== AFTER1 listeners 18100-18199 ===
(none)
lcount=0
=== RUN #2 ===
PASS 鍐掔儫锛歴lot=1 reservation=1 payment=1 鏀跺叆=9900 宸=0 绉嶅瓙=1/4/1
run2-exit=0
=== AFTER2 server.exe PIDs ===
(none)
count=0
=== AFTER2 listeners 18100-18199 ===
(none)
lcount=0
```

冒烟临时文件（`%TEMP%`，两次运行后统计）：

```text
db  = 32   (paw-smoke-*.db*)
log = 22   (paw-smoke-*.log*)
exe = 0
```

`db/log` 计数与上轮 §3.2 记录的**历史残留基数完全一致**（32 / 22），本轮新增为 0；每个 guid 命名的 `paw-smoke-*.exe` 均已被脚本删除。输出中的中文乱码为 `powershell.exe` 子进程 stdout 编码（GBK/UTF-8 捕获差异），与上轮同因，不影响退出码判定。

| 通过标准 | 实测 | 判定 |
|---|---|---|
| 连续两次 `exit 0` | `run1-exit=0`、`run2-exit=0`，均 PASS（收入 9900、差额 0） | **PASS** |
| 结束后 `server.exe` 零残留 | 运行前 0 → 运行后 0 → 第二次后 0（上轮为 2→3→4 单调增长） | **PASS** |
| 结束后 `18100–18199` 零监听 | `lcount=0` 三次快照一致（上轮每次新增 1 个监听端口） | **PASS** |
| 临时文件零新增 | `exe=0`，`db/log` 与历史基数持平 | **PASS** |
| 启动后子进程存活/绑定校验 | `smoke.ps1:50` 已加 `if ($proc.HasExited) { throw ... }` | **PASS** |

结论：**PASS** —— 上轮 P2 的两项未完成项（杀子进程、启动绑定校验）均已落地；连续两次运行后进程数、监听端口数均为 0，可判定 `D-T02-02` **已关闭**，建议由 P2 降级并归档。

### 10.3 `D-T02-04` 复测（同 key 跨预约回放首次响应）：**PASS**

用例：临时文件 `internal/api/zz_retest3_probe_test.go`（`//go:build retest3`，**默认不参与 `go test ./...`**，复测后已删除，**未改动任何生产代码**）。断言：`u1` 对 `res1` 支付 → 200；随后 `u1` 用**同一** `Idempotency-Key` 对 `res2` 支付 → 必须 `409 IDEMPOTENCY_CONFLICT`，响应不得含 `paymentId`/不等于首次响应体，且 `res2` 支付单数为 0；正向对照：`res2` 换新 key 必须 200。

```text
$ go test -tags=retest3 ./internal/api -run 'TestRetest3DT0204' -count=1 -v
=== RUN   TestRetest3DT0204CrossResourceIdempotencyConflict
    zz_retest3_probe_test.go:27: u1 名下两个预约：res1=1 res2=2
    zz_retest3_probe_test.go:34: res1 支付：status=200 body={"code":0,"data":{"amountCents":9900,"attempt":1,"orderNo":"P1-1794636000000000000","paymentId":1,"status":"INIT"},"message":"ok"}
    zz_retest3_probe_test.go:37: res2 复用同一 key 支付：status=409 body={"code":"IDEMPOTENCY_CONFLICT","message":"同一 Idempotency-Key 的请求载荷不一致"}
    zz_retest3_probe_test.go:53: 库中 res2 支付单数=0（冲突分支应为 0）
    zz_retest3_probe_test.go:64: res2 换新 key：status=200 body={"code":0,"data":{"amountCents":9900,"attempt":1,"orderNo":"P2-1794636000000000000","paymentId":2,"status":"INIT"},"message":"ok"}
--- PASS: TestRetest3DT0204CrossResourceIdempotencyConflict (0.14s)
PASS
ok  	paw-book/internal/api	3.871s
retest3-exit=0
```

对照上轮 §8 同场景输出（`status=200` 且回放 `paymentId=1`、`res2` 支付单数 0）：本轮第二次改为 `409 IDEMPOTENCY_CONFLICT`，未回放 `paymentId`，`res2` 换新 key 后正常得到 `paymentId=2`。

结论：**PASS** —— 幂等作用域/指纹纳入路由维度后，同一用户跨资源复用同一 key 由“静默吞掉”变为显式冲突，`D-T02-04` **已关闭**。

### 10.4 全量回归（本轮修复无回退）

```text
$ go test ./... -count=1
?   	paw-book/cmd/audit	[no test files]
?   	paw-book/cmd/migrate	[no test files]
?   	paw-book/cmd/paychannel	[no test files]
?   	paw-book/cmd/server	[no test files]
ok  	paw-book/internal/api	6.866s
?   	paw-book/internal/config	[no test files]
ok  	paw-book/internal/domain	1.008s
ok  	paw-book/internal/paychannel	1.518s
ok  	paw-book/internal/payclient	4.658s
ok  	paw-book/internal/repo	3.574s
ok  	paw-book/internal/service	16.049s
?   	paw-book/internal/store	[no test files]
?   	paw-book/migrations	[no test files]
test-exit=0
```

### 10.5 第三轮结论

| 门禁项 | 结论 |
|---|---|
| 基线可追溯（`9a641c8`，仅报告未跟踪） | **PASS** |
| `D-T02-02` 冒烟零残留（进程/端口/临时文件） | **PASS（已关闭）** |
| `D-T02-04` 同 key 跨预约必须 409 | **PASS（已关闭）** |
| `go test ./... -count=1` | **PASS**（全部包 ok，exit 0） |

总体：**PASS** —— 上轮遗留的 P2（冒烟残留）与 P3（幂等跨资源回放）均已修复且经独立验证，本轮无新增缺陷。遗留事项仅上轮的 `-race` BLOCKED（本机无 gcc、`CGO_ENABLED=0`）与 `O-5`（service 覆盖率余量偏小），不属本轮两项范围。
