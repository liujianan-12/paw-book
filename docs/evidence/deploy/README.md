# 部署证据目录（当前为空证据）

> 依据 `.agent/10 §6` 中 `docs/evidence/deploy/` 的引用路径建立。本目录**暂无任何运行记录**。

| 期望证据 | 状态 | 说明 |
|---|---|---|
| 流水线运行记录（含质量/测试/构建/部署/健康/冒烟） | BLOCKED | `.github/workflows/ci.yml` 仅配置，未核看运行结果 |
| 部署地址与部署版本（SHA / 镜像 digest） | BLOCKED | 教师部署环境未提供 |
| 健康检查输出 | BLOCKED | 未部署 |
| 上线业务冒烟结果 | BLOCKED | 本地 `scripts/smoke.ps1` 不算上线冒烟 |
| 回滚演练记录 | BLOCKED | `deploy/rollback.md` 为未验证草案 |

不得把 `deploy/README.md`、`deploy/rollback.md`、CI 配置文件的存在写成 G6–G8 已通过。
