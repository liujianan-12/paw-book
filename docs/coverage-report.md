# 覆盖率与测试口径报告（2026-11-12，D5 拆分后）

> 口径依据：06 §0/§1、D5（业务与测试完成后机械拆分）、D8（SQLite 临时库，不引入 Docker/MySQL）。

## 1. 命令与原始输出

```powershell
gofmt -l .                                            # 空输出
go vet ./...                                          # 通过
go test ./... -count=1                                # 全绿
go test ./internal/<pkg> -count=1 -coverprofile=docs/evidence/tests/local/<pkg>.cover
go tool cover -func=docs/evidence/tests/local/<pkg>.cover
go test -race ./... -count=1                          # BLOCKED：无 gcc/MinGW（E1）
```

原始输出：`docs/evidence/tests/local/`（`gofmt.txt`、`vet.txt`、`test-output.txt`、`{domain,repo,service,api}.cover`、`per-package.txt`、`race-env.txt`）。

## 2. 目标包覆盖率（拆分后）

| 包 | 覆盖率 | 门禁 |
|---|---|---|
| `internal/domain`（状态/规则/错误码） | 100.0% | PASS |
| `internal/repo`（DBTX/事务/审计/时间） | 96.7% | PASS |
| `internal/service`（R1–R8 编排） | 85.1% | PASS |
| `internal/api`（A1–A20/鉴权/幂等/页面） | 88.9% | PASS |
| `internal/payclient` | 88.5% | PASS |
| `internal/paychannel` | 100.0% | PASS |

## 3. 排除项（逐文件说明）

| 路径 | 原因 | 是否自编业务代码 |
|---|---|---|
| `cmd/server/main.go` | 进程入口，仅装配 | 否（入口） |
| `cmd/migrate/main.go` | 迁移命令入口 | 否（入口） |
| `cmd/paychannel/main.go` | 模拟渠道进程入口（教学演示） | 否（入口） |
| `migrations/embed.go` | 仅 `embed.FS` 声明 | 否 |
| `internal/config/config.go` | 环境变量读取，无业务判定；由部署冒烟覆盖 | 是（基础设施） |
| `internal/store/store.go` | 打开库/迁移/种子，被全部集成测试实际执行 | 是（基础设施） |

> 未排除 domain/repo/service/api 任一自编业务逻辑。

## 4. 测试方式与已覆盖风险

- 业务：`internal/service` 100+ 用例，按规则号命名覆盖 R1–R8、I1–I13；含 50 并发抢 3 名额（C1）、唯一索引冲突、条件更新败者（R6.1）。
- 失败注入：触发器 `RAISE(ABORT/IGNORE)` 覆盖审计/记录/支付/退款/名额写入失败的事务回滚（04 §4）。
- API：A1–A20 全路由（鉴权、角色、幂等重放/冲突、`X-Mock-Signature`、参数与状态错误）、静态页面可访问。
- 冒烟：`scripts/smoke.ps1` 使用固定教学时钟与临时库跑通最小闭环，输出 `PASS`。
- `-race`：BLOCKED（E1），环境修复后补跑，不得以本报告代替 race 结论。
