# meegle 工作项核验 — BLOCKED（模板，复制后按实际填写）

- 状态：BLOCKED（meegle CLI 不可用 / 无真实工作项编号）
- 日期/时间：
- 关联提交/任务：
- 决定依据：`docs/assumptions.md` D4（按可用性 C/B/A 处理）

## 尝试记录（必须原文保留）

```text
$ meegle --version
<实际输出 / command not found>

$ meegle workitem get --work-item-id 1001 --project-key r17te9
<实际输出 / 报错原文>
```

- 执行环境（OS、Shell、时间）：
- 截图/附件路径：

## 影响与声明

- 使用占位编号的提交：`<sha1>`、`<sha2>`…（占位值：`#1001`）
- 上述提交**不作为“已核验”证据**，验收报告与 PR 描述中已声明。
- 已有真实编号但 CLI 不可用：编号来源（教师/飞书链接）：`<...>`；从下一个 `feat/fix` 起改用真实编号，历史不回写。
- 无任何真实编号：已向教师/组织者索取（时间、渠道、回复）：`<...>`

## 待办

- [ ] 教师提供真实需求/Bug 编号与 meegle 可用环境
- [ ] 从下一个 `feat/fix` 起执行核验并归档输出到 `docs/evidence/meegle/`
- [ ] 在验收报告 G3 项记录当前 BLOCKED 状态与后续补救
