# 原型目录（索引）

> 原型说明正文见 `docs/02-原型说明.md`；静态截图见 `docs/prototype-shots/`。本目录只放索引，不复制图片。

| 角色 / 场景 | 截图 |
|---|---|
| 用户：时段列表、预约弹窗、待支付、我的预约列表、取消弹窗 | `docs/prototype-shots/用户-*.png` |
| 前台：核销 | `docs/prototype-shots/前台-核销.png` |
| 客服：审核 | `docs/prototype-shots/客服-审核.png` |
| 店长：日清 | `docs/prototype-shots/店长-日清.png` |

## 口径

- 页面实现在 `internal/api/web/`，真实调用接口，不是静态页（验收报告 E3 记录为 BLOCKED，原因是按 `.agent/13` 手册不实测 HTTP / 无录屏，不是文件不存在）。
- 截图为静态原型，不能替代 S1–S5 的录屏与浏览器网络记录。
