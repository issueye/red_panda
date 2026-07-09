# red_panda desktop

桌面端目标技术栈：

- Wails v3
- JavaScript
- React
- shadcn/ui
- WebSocket client

当前目录已经包含 React/Vite 前端、Wails v3 `main.go`、`Taskfile.yml` 和 `build/config.yml`。前端通过 WebSocket 对接本地网关，Wails Go 侧只暴露桌面壳层 bootstrap 信息。

桌面端业务请求只访问 `red-panda-gateway`：

- HTTP：`/healthz`、`/readyz`、`/api/v1/app/bootstrap`
- WebSocket：`/api/v1/ws`

不直接访问 Agent Runtime，也不直接执行工具。
