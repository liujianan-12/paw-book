# 覆盖率与测试口径报告（2026-11-12）

> 口径依据：06 §0/§1、D8（SQLite 临时库，不引入 Docker/MySQL）、D5（业务与测试完成后机械拆分）。

## 1. 命令与原始输出

```powershell
gofmt -l .                                            # 空输出
go vet ./...                                          # 通过
go test ./... -count=1 -coverprofile=docs/evidence/tests/local/coverage.out
go tool cover -func=docs/evidence/tests/local/coverage.out
go test -race ./... -count=1                          # BLOCKED：无 gcc/MinGW（E1）
```

原始输出：`docs/evidence/tests/local/{gofmt.txt,vet.txt,test-output.txt,coverage.out,coverage-func.txt,per-package.txt,race-env.txt}`。

## 2. 包级覆盖率

| 包 | 覆盖率 | 结论 |
|---|---|---|
| `internal/biz`（拆分前，含 R1–R8 全部业务与 SQL） | 85.0% | PASS |
| `internal/paychannel`（模拟渠道行为解析） | 100.0% | PASS |
| `internal/payclient`（渠道 HTTP 客户端） | 88.5% | PASS |
| 合计 | 85.3% | PASS |

拆分到 `domain/service/repo/api` 后按目标包重新统计，见 `docs/evidence/tests/<sha>-split/`。

## 3. 排除项（逐文件说明）

| 路径 | 原因 | 是否自编业务代码 |
|---|---|---|
| `cmd/server/main.go` | 进程入口，仅装配 HTTP 服务 | 否（入口） |
| `cmd/migrate/main.go` | 迁移命令入口 | 否（入口） |
| `cmd/paychannel/main.go` | 模拟渠道进程入口（教学演示，非真实支付） | 否（入口） |
| `migrations/embed.go` | 仅 `embed.FS` 声明 | 否 |
| `internal/config/config.go` | 环境变量读取，随部署验证 | 是（基础设施，不参与业务判定） |

> 未排除任何自编 repo/api/业务逻辑；`internal/biz` 全部语句计入分母。

## 4. 测试覆盖方式

- 每个用例 `t.TempDir()` 独立 SQLite 文件库，迁移与种子由 `internal/store` 执行（D8）。
- 正常/边界/非法状态/重复操作/权限/数据库写失败（触发器 ABORT/IGNORE）均有对应用例，命名带规则号。
- 并发：50 并发抢 3 名额（C1）、条件更新败者分支（R6.1）。SQLite 并发正确性来自单写连接 + `BEGIN IMMEDIATE` + 条件更新 + 唯一索引，不声称等价 MySQL 行锁（D1）。
- `-race`：本机缺 MinGW，按 E1 记 BLOCKED；环境修复后补跑，不得以本报告代替 race 结论。
