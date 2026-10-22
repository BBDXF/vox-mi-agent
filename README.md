# vox-mi-agent

自托管的 AI 语音助手：对接小米音箱，通过 LLM 对话并播放本地音乐库（Go 后端 + Web UI，单容器部署）。

## 快速开始

```sh
cp .env.example .env   # 填写 VOX_PORT / VOX_DATA / VOX_MUSIC / VOX_LLM_API_KEY 等
make up                # 构建本地镜像并 docker compose 启动
curl localhost:${VOX_PORT:-8080}/healthz
```

公司代理（如 Zscaler）环境：把 CA 的 PEM 放入 `certs/extra-ca.crt`，详见 `certs/README.md`。

## Make 目标

| 目标 | 说明 |
|---|---|
| `make dev` | 本地直接运行 (127.0.0.1:8080) |
| `make test` / `test-docker` | 主机 / 容器内跑测试 |
| `make web` | 构建前端并嵌入 |
| `make build` | 交叉编译 amd64/arm64 到 `dist/` |
| `make image` / `image-local` | 多架构镜像 / 本地单架构镜像 |
| `make up` / `down` / `logs` | 用 compose 运行本地镜像 |

## 目录

| 路径 | 说明 |
|---|---|
| `cmd/app` | 程序入口 |
| `internal/` | 后端各包 |
| `web/` | 前端 |
| `scripts/`、`tools.lock` | 外部工具（fzq/jq/yq）下载与锁定 |
| `compose.yaml` / `compose.local.yaml` / `compose.test.yaml` | 运行 / 本地 CA 覆盖 / 测试 |
| `certs/` | 本地专用代理 CA |
| `docs/DESIGN.md` | 设计文档 |

详细设计见 [docs/DESIGN.md](docs/DESIGN.md)。
