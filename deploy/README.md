# 部署说明（配置与步骤草案，**未实际部署**）

> 状态：**BLOCKED**。教师部署环境、流水线运行记录、部署地址、健康检查与上线冒烟结果均未提供。本文件只给出基于仓库现有脚本的可执行步骤，**不是部署成功记录**；G6 / G8 保持 BLOCKED。

## 本地构建与启动（已在本机用于冒烟，非生产部署）

```
go build -o paw-server.exe ./cmd/server
go run ./cmd/migrate
./paw-server.exe
```

- 配置只通过环境变量注入；密钥、DSN、token 不写入仓库、日志或截图。
- 数据库为 SQLite（`docs/assumptions.md` D1/D8：单写连接、条件更新、部分唯一索引、对账）；预置数据见 `migrations/002_seed.sql`。
- 本地业务冒烟脚本：`scripts/smoke.ps1`（独立二进制 + 随机端口 + 按端口清理），曾连续两次 PASS，见 `docs/testing/T02-T11-retest.md`。

## 建议的发布流程（未在教师环境执行）

1. PR `dev/02-pet-booking → main` 合并后，在**合并提交**上重跑质量门禁：`gofmt -l .`、`go vet ./...`、`go test ./... -count=1`、`go test ./... -coverprofile`、`go test -race ./...`。
2. 归档原始输出到 `docs/evidence/tests/<合并SHA>/`，覆盖率整体与四包各 ≥85%。
3. 构建并记录版本（提交 SHA / 镜像 digest），执行健康检查与**业务**冒烟（非仅端口监听）。
4. 记录部署地址、部署版本与回滚说明；由独立验收会话复验 G6–G8 后再改结论。

## CI 配置

`.github/workflows/ci.yml` 已提交质量门禁配置（gofmt / vet / test / coverage / race）。**本会话未核看任何 Actions 运行结果**，不得把配置文件当成运行记录；如需补 Q4/Q7，须下载 Actions 原始日志归档并由独立验收复验。

## 模拟依赖声明

`internal/paychannel` 为教学用**模拟支付渠道**，不是真实第三方支付接入；演示中不得包装成真实支付成果。
