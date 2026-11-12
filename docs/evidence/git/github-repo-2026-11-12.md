# GitHub 仓库与双远程核验（2026-11-12，Dev 会话）

- 触发：学生提供截图，声明「代码已上传到 GitHub 与 Gitee，origin → GitHub，gitee → 原 Gitee 地址」。
- 结论：GitHub 仓库存在、默认分支为 `main`、三个分支均已推送；**PR 未合并**、**非 MCP 建仓**，G1 保持 BLOCKED。

## 真实命令输出

```
git remote -v
gitee   https://gitee.com/liujianan12/pet-spa.git (fetch / push)
origin  https://github.com/liujianan-12/paw-book.git (fetch / push)

git ls-remote --symref origin HEAD
ref: refs/heads/main  HEAD
a92e5cd79d1d43c761c3abbd0365e6ebe9d6576b  HEAD

git ls-remote --heads origin
9680d2c32f1473635d0d442b58088caf88ef6f20  refs/heads/dev/02
0c7c1ef09c2ff162483d275c050e6312e17854f7  refs/heads/dev/02-pet-booking
a92e5cd79d1d43c761c3abbd0365e6ebe9d6576b  refs/heads/main

git ls-remote --symref gitee HEAD
ref: refs/heads/master  HEAD
a92e5cd79d1d43c761c3abbd0365e6ebe9d6576b  HEAD

git status --short   → 空（工作区干净）
```

## 分支 SHA 对照

| 分支 | origin（GitHub） | gitee |
|---|---|---|
| `main` | `a92e5cd` | `a92e5cd` |
| `dev/02-pet-booking` | `0c7c1ef` | `0c7c1ef` |
| `dev/02` | `9680d2c` | 无此分支 |
| `master` | 无此分支 | `a92e5cd` |

## 判定（不得写成 PASS 的部分）

- GitHub 默认分支 `main`：**git 协议核验通过**（`--symref` → `refs/heads/main`）；Gitee 默认分支仍是 `master`。
- 两边 `main` 内容均为 `a92e5cd Initial commit`，**没有合并提交** → G4 / G5 仍 BLOCKED；「验收 SHA ≠ 发布 SHA」风险仍成立，见 `docs/evidence/tests/sha-binding-risk.md`。
- 仓库由学生手动创建 + `git push` 上传，**不是 GitHub MCP 建仓** → G1 保持 BLOCKED（学生明确要求不写 PASS），见 `docs/evidence/git/mcp-github-BLOCKED.md`。
- `main` 保护规则读取结果仍缺（截图 `docs/evidence/git/main-protection.png` 未落盘，`Test-Path` = MISSING）→ G2 保持 BLOCKED。
- GitHub 上未见 PR / 合并记录，本会话未开 PR、未合并、未强推、未改远程默认分支。
