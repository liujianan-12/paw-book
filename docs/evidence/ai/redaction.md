# 脱敏说明

> 依据 `.agent/10 §5` 与 `AGENTS.md §2.3`。本文件记录**实际处理过的脱敏动作**，未出现的敏感字段不虚构。

| 字段/类别 | 处理方式 | 依据 |
|---|---|---|
| Gitee / GitHub 访问令牌 | 由 Windows 凭据管理器保管；**从未读取值、从未打印**，命令与文档只出现变量名与布尔值 `tokenConfigured=True` | `docs/evidence/git/mcp-github-BLOCKED.md`、`git remote -v` 输出 |
| GitHub MCP `GITHUB_PERSONAL_ACCESS_TOKEN` | 只输出键名 `envKeys=...`；探活设计里值仅在内存环境变量传递，且命令未执行 | `docs/evidence/git/mcp-github-BLOCKED.md` |
| `remote` URL | 公开仓库 HTTPS 地址，不含 `user:token@` 形式，不含端口或内网地址 | `git remote -v` |
| 手机号 / 身份证 / 邮箱 / 住址 / 密码 / DSN / 公网与内网 IP / 仓库 Secrets | 本会话输出与提交文档中均未出现；预置演示数据为合成姓名、宠物名与尾号 0000 的手机号 | `migrations/002_seed.sql`、`.agent/04` |
| 命令输出中的凭据扫描 | 每次提交前用严格模式扫描待提交文档；命中项人工判定（如「Bearer 回退」是提交标题功能描述，非凭据） | `docs/pr-description.md` 自检段 |
| 截图 / 视频 | 尚未落盘；后续录制前须确认终端无敏感信息，必要时裁剪 | `missing-scope.md` |
| 学生提示词中的凭据 URL 形式示例 | 原句含 `user:` + 占位口令 + `@` 的 URL 形式；已替换为「禁止在 URL 中内嵌 `user:<占位口令>@` 形式」，只改值不改结构、不改变原意 | `docs/sessions/08-git-收尾-dev.md:8` |

## 口径

- 脱敏只改值、不改结构；本轮没有需要替换的敏感值，因此**没有改动任何原始输出文件**。
- 凭据未进入命令文本、文件、提交、日志或聊天；本文件本身也不含任何凭据值。
