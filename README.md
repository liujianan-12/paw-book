# paw-book — 宠物门店预约系统（题目 02）

宠物护理门店的预约、接待、服务与取消退款场景实现。

## 文档索引
- `docs/01-需求说明.md` — 需求、规则编号（R*）、状态机、场景（S*）
- `docs/02-原型说明.md` — 原型页面清单（原型见 `prototype/`）
- `docs/03-任务规划.md` — 任务拆分（T0–T14）、依赖与完成条件
- `AGENTS.md` — AI Agent 工作约束

## 快速开始
```bash
go mod tidy
go build ./...
go run ./cmd/server
# 健康检查: curl http://localhost:8080/healthz
```

## 教学时钟（可配置，测试与演示使用相同代码路径）
| 环境变量 | 默认 | 含义 |
|---|---|---|
| PAW_ADDR | :8080 | HTTP 监听地址 |
| PAW_DB | paw-book.db | SQLite 文件路径 |
| PAW_PAY_HOLD | 5m | 未付款预约占名额时长 |
| PAW_CANCEL_CUTOFF | 2h | 临近开始的取消分界 |
| PAW_PAY_CHANNEL | http://localhost:8081 | 模拟支付渠道地址 |

## 数据库
SQLite（零部署、单文件、支持事务）；建表脚本见 `internal/store/schema.sql`，服务启动时自动初始化，可重复执行。
