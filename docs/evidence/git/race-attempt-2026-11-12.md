# `-race` 补齐尝试记录（2026-11-12，结论 BLOCKED）

- 依据：`AGENTS.md §0.1` 已拍板「Windows 本机装 MinGW-w64（便携版）后执行，环境输出归档」；`.agent/11` Q4 / Q7、`.agent/06`。
- 结论：**本轮未能执行 `go test -race ./...`，保持 BLOCKED**；不写 PASS、不伪造 race 输出。

## 真实输出

```
go version go1.26.0 windows/amd64 ; node v16.20.2
gcc / clang / cc / zig：PATH 与 C:\mingw64、C:\msys64、C:\TDM-GCC-64、C:\Program Files\LLVM 等候选路径均 0 命中
CGO_ENABLED=0 ; CC=gcc
winget v1.29.380（源：msstore / winget / winget-font）
网络探测：registry.npmjs.org=200；api.github.com=FAIL；cdn.winget.microsoft.com=FAIL
go test -race ./... -count=1 → "go: -race requires cgo; enable cgo by setting CGO_ENABLED=1"；race-exit=2
```

## 阻塞链

`-race` 在 Windows 依赖 cgo → cgo 依赖 gcc 兼容工具链 → 本机无 gcc → 需下载 MinGW-w64 便携版 → GitHub 与 winget CDN 均不可达（仅 npm registry 可达）→ 本轮无法安装。

## 学生可自行补齐（命令）

1. 装 MinGW-w64 便携版（如 winlibs UCRT），解压到 `C:\mingw64`。
2. `$env:PATH="C:\mingw64\bin;$env:PATH"; $env:CGO_ENABLED=1; $env:CC="gcc"`
3. `go test -race ./... -count=1`，把原始输出、`gcc --version`、`go env CGO_ENABLED` 归档到 `docs/evidence/tests/<sha>/race.txt` 与 `race-env.txt`。
4. 由独立验收会话复验 Q4 / Q7 后再改结论。

## 不影响的部分

现有 `go test ./... -count=1` 与覆盖率证据仍有效：`go.mod` 使用纯 Go 的 `modernc.org/sqlite v1.34.5`，不需要 cgo。`-race` 缺失只影响 Q4 / Q7 与 `AGENTS.md §4.1` 门禁，其余结论不变。
