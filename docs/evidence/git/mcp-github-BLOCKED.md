# GitHub MCP 排查记录（结论：BLOCKED，不写 PASS）

- 记录时间：2026-11-12
- 记录会话：Dev（Git 收尾 / MCP 安装验证）
- 触发：学生告知「[GitHub] MCP Server 已经安装完成，通过使用工具之一来演示服务器的功能」
- **结论：BLOCKED**。server 配置存在且已启用，但当前会话未注入任何 `mcp__GitHub__*` 工具，「使用工具之一演示」**未执行**；没有任何真实工具返回可展示，不编造输出。
- 项目口径：GitHub MCP 相关验收项（验收报告 G1、PR 描述 BLOCKED 第 1 条）**保持 BLOCKED**；本记录不构成 `.agent/07 §1` 要求的「用 GitHub MCP 创建独立仓库并留原始响应」。

## 1. 配置字段（只列字段与布尔值，未读取任何凭据值）

配置文件：`C:\Users\ndkdl\.codebuddy\mcp.json`

```
topLevelKeys=mcpServers
server=GitHub
  fields=timeout,command,args,env,type,disabled
  command=npx
  type=stdio
  args=-y @modelcontextprotocol/server-github
  envKeys=GITHUB_PERSONAL_ACCESS_TOKEN
disabled=False
timeout=60000
tokenConfigured=True
```

`tokenConfigured` 只是「该键存在且非空」的布尔判断；**env 的值从未被读取、打印、写入命令/文件/提交/日志/聊天**。

## 2. npm / 包版本

```
npm view @modelcontextprotocol/server-github version engines --json → "2025.4.8"   npm-exit=0
npxVersion=8.19.4
npxCacheExists=False   （%APPDATA%\npm-cache\_npx 不存在，包未预热）
```

## 3. Node 版本

```
node=v16.20.2
```

`2025.4.8` 版 server 对 Node 版本可能要求更高；本机 Node 16 偏旧，是潜在启动阻塞点（未实测，仅作风险记录）。

## 4. 三次工具检索均为 0 个 GitHub MCP 工具

| # | 检索内容 | 结果 |
|---|---|---|
| 1 | `tool_names=["github","mcp__github","mcp_github"]`；queries：`github` / `github repository` / `mcp github` / `list repositories` / `get me user` / `create repository` / `github issue` / `github pull request` | `tools: []`，0 个 |
| 2 | queries：`mcp` / `github_mcp` / `gh` / `repository` / `search_code` / `list_issues` / `get_file_contents` / `mcp server` / `create_issue` / `fork` | 0 个 GitHub 工具（仅返回无关的 `invoke_integration` / `web_fetch` / `image_gen`） |
| 3 | `tool_names=["mcp__github__get_me","mcp__github__list_repositories","mcp__github__search_repositories","mcp__github","github_list_repos","mcp_github_list_tools"]`；queries：`mcp__` / `github.com` / `octocat` / `git remote` / `list tools` / `tools/list` | `tools: []`，0 个 |

主工具清单与 deferred 工具清单中同样没有 `mcp__GitHub__*` 前缀工具。

## 5. 未读取 token / 未产生凭据痕迹

- 全程只输出 `envKeys=GITHUB_PERSONAL_ACCESS_TOKEN` 与 `tokenConfigured=True`，**未输出该键的值**。
- 未把令牌写进任何命令文本、文件、提交、日志或聊天；`remote` URL 不含 `user:口令@` 形式。
- 曾尝试用 MCP stdio 协议（`initialize` + `notifications/initialized` + `tools/list`）直接探活，凭据仅通过内存环境变量传给子进程且对输出做脱敏；该命令返回 `Execution Cancelled: Awaiting user approval timed out`，**未执行、无任何返回**。
- 复核：`leftoverProcesses=0`、`git status --short` 为空、`disabled=False` 未变。

## 6. 阻塞原因与解锁步骤

原因：MCP 工具在**会话启动时**注入；该 server 在会话中途安装，当前会话没有加载它的工具。不是配置损坏（`disabled=False`、包可解析、凭据已配置）。

1. 新开会话/窗口（或重启 IDE），让工具重新注入。
2. 新会话里调用一个**只读**工具（查当前账号 / 搜公开仓库），把原始返回原样归档到 `docs/evidence/git/`。
3. 若面板报启动失败，考虑把 Node 升到 18/20 后重启。

## 7. 不得据此改结论

- 验收报告 G1 保持 BLOCKED；`.agent/07 §1` 的 MCP 建仓要求仍未满足。
- 「工具演示成功」≠「用 GitHub MCP 创建独立仓库并留原始响应」，也不等于把 Gitee 仓库改口成 GitHub。
- 本记录不改变验收统计（PASS 73 / FAIL 0 / BLOCKED 24）与验收绑定 SHA `9680d2c32f1473635d0d442b58088caf88ef6f20`。
