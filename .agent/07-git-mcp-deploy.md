# 07 Git、GitHub MCP、提交规范与自动部署

本文件对应题目「开发流程」「GitHub 仓库/分支/合并」「提交消息与工作项校验」「自动部署」四节。顺序不能省：**先有仓库与约束，再有业务代码**。

## 1. 建仓（GitHub MCP，唯一合法方式）

1. 用 GitHub MCP 创建**独立仓库**（默认 `main`），记录：
   - 工具名称、请求参数、返回结果、时间、仓库 URL、初始 README 提交
   - 存到 `docs/evidence/github-mcp/create-repo.md`，附原始响应截图/文本
2. 手工网页创建、CLI `gh repo create`、本地 `git init` 后推远程都**不能替代** MCP 创建证据。
3. 创建后配置保护：`main` 禁止直接推送/删除/强推，要求 PR + 状态检查；读取结果存档：

```bash
gh api repos/<owner>/<repo>/branches/main/protection > docs/evidence/github-mcp/main-protection.json
```

截图或该 JSON 二选一必须存在，不能只口头声明。

## 2. 分支

- 默认分支 `main`；所有业务代码、测试、原型、AGENTS.md、Skill、配置、文档都在 `dev/02-pet-booking`（或 `dev/02-<姓名缩写>`）提交。
- 阶段提交顺序（保留真实过程，不许最后一次性补）：
  1. `docs` 需求说明 + 假设
  2. `docs` 原型
  3. `docs` 任务规划
  4. `feat` 骨架/模型/迁移
  5. `feat` 分功能
  6. `test` 测试与并发用例
  7. `fix` 缺陷修复
  8. `docs` 验收报告/部署说明
- 合并前从 `main` 同步一次并解决冲突，重跑门禁。

## 3. 提交消息（严格按 commit-convention.md）

格式：`[类型] [版本号] [需求/Bug编号] 中文描述`

```text
[feat] [v0.1.0] [#123456] 新增时段创建与预约占位接口
[fix]  [v0.1.1] [#123457] 修复待审核取消期间仍可核销的缺陷
[test] [v0.1.2] 补充核销与取消并发唯一结果用例
[docs] [v0.1.3] 补充需求假设与验收报告
[opt]  [v0.1.4] 收敛日清查询为单次聚合
```

- 一次提交只一种类型；混合变更拆分。
- `feat`/`fix` 必须真实工作项编号且语义匹配；不得用 `docs`/`test` 伪装功能提交。
- 版本号 `v主.次.修订`，同一阶段内合理递增。
- 禁止 `git add .`：逐文件暂存并复核 `git diff --cached`；不得夹带他人/构建产物/`.env`。

## 4. 工作项校验（meegle CLI，提交前执行）

```bash
meegle workitem get --work-item-id <数字编号> --project-key r17te9
```

- 在 `feat`/`fix` 提交**之前**执行，原始输出存 `docs/evidence/meegle/<提交主题>.txt`（含命令、时间、返回）。
- 必须确认编号属于对应需求/缺陷；查不到或失败时先确认，**不得伪造编号或声称查询成功**；未核验的提交不算合规。
- 普通缺陷先登记 Bug 再修复；`hotfix` 仅用于真实线上紧急修复。

### 4.1 编号不可用时的处理（2026-11-12 拍板：按可用性判断）

每次 `feat`/`fix` 前先自检，按顺序落到 C / B / A 之一，结果写入任务记录与 `docs/assumptions.md`：

| 情形 | 做法 |
|---|---|
| **C：meegle 可执行且能查到该编号** | 最合规。先 `meegle --version`，再执行 `meegle workitem get ...`，原始输出存 `docs/evidence/meegle/<主题>-<时间>.txt`，然后提交。 |
| **B：能拿到真实编号，但 meegle CLI 不可用/无权限** | 从**下一个** `feat/fix` 起使用真实编号（如 `#123456`），旧提交不回写；同时写 `docs/evidence/meegle/BLOCKED.md`，记录尝试命令、报错原文、编号来源（教师/飞书链接）。报告中不得写“已核验”。 |
| **A：既无真实编号也无 meegle** | 按 11 §8 记 BLOCKED（同上模板），占位编号（如 `#1001`）必须在验收报告与 PR 描述中声明为占位；立即向教师索取真实编号与 meegle 环境（题面规定开考前提供）。 |

铁律：不得用 `docs`/`test`/`opt` 伪装 `feat`/`fix`；不得事后改写历史补编号；不得声称 meegle 核验成功而实际没跑。

模板：`docs/evidence/meegle/BLOCKED.md`（本规则包已提供样例）。

## 5. PR 与合并门禁

1. 从 `dev/02-*` 向 `main` 发起 PR，填写：
   - 需求编号、缺陷编号、变更摘要
   - 实际测试结果、覆盖率（原始文件路径）
   - 验收报告路径、AI 会话与 Skill 使用记录路径
   - 对应开发分支提交 SHA
2. PR 必须通过：CI 全部 job、覆盖率达 85%、并发/race 通过、Review/自检清单完成。
3. 合并方式优先保留原始提交（merge commit 或 rebase，按仓库策略）；若产生合并提交，消息同样带类型与主要工作项，不得抹去原提交关系。
4. **合并后**对 `main` 实际合并提交重新跑门禁并部署；不得拿开发分支旧报告充当主分支证据。
5. 合并后发现问题：回到开发分支修复 → 新 PR，不直接改 `main`；回退要记录操作与结果。

## 6. 自动部署流水线（必须实际跑通）

| 阶段 | 内容 | 失败处理 |
|---|---|---|
| 1 质量检查 | `gofmt -l`、`go vet`、敏感信息扫描 | 失败即停 |
| 2 单元测试 + 覆盖率门禁 | `go test ./... -coverprofile`，脚本校验 ≥85% | 低于 85% 停止 |
| 3 并发/race | `go test -race ./...`（可用独立 job） | 失败即停 |
| 4 集成测试 | `-tags=integration`，docker MySQL | 失败即停 |
| 5 构建 | `go build ./...`；Docker 镜像打 `sha` tag | 失败即停 |
| 6 部署 | 推镜像 → 目标环境拉起（compose/k8s 二选一） | 保留上一版本 |
| 7 健康检查 | `/healthz` `/readyz` 200 | 失败自动回滚 |
| 8 业务冒烟 | 脚本跑最小闭环：建时段→预约→支付→核销→完成→日清 | 失败自动回滚并留日志 |

证据：`docs/evidence/deploy/<run-id>/` 保存工作流配置、运行 URL/ID、镜像 digest、部署提交 SHA、健康检查与冒烟输出；README 写清回滚命令与上一可用版本。

## 7. 上线定义与记录

- “上线”= 部署到教师指定教学验收环境，有可访问地址 + 真实流水线记录 + 冒烟通过。
- 记录表（提交到仓库）：部署时间、版本/提交 SHA、镜像 digest、环境地址、健康结果、冒烟结果、回滚目标。
- 只交 Dockerfile、手工启动截图、本地 `go run` 不算自动部署。

## 8. 禁止与检查项

- 禁止直接推 `main`、强推、删除分支保护、用压缩提交掩盖混合类型。
- 禁止把密钥写进 workflow、README、compose 明文；用 GitHub Secrets/环境变量，截图脱敏。
- 验收 Agent 必须核对：MCP 建仓证据、保护规则读取结果、分支历史、提交格式与编号核验、PR 与合并提交、主分支部署版本是否与冒烟一致。
