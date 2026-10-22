# vox-mi-agent

自托管的 AI 语音助手：接管小爱音箱的点歌请求，播放 NAS 上的本地音乐；其他问题可交给大模型 Agent。Go 后端 + Web 界面，单个 Docker 容器部署。

- 说「小爱同学，播放晴天」「来点陈奕迅的歌」→ 本地曲库搜索，约 1–2 秒开始播放
- Web 界面：调试音箱（无需真机即可试）、曲库与别名、大模型与音箱设置、调用追踪
- 首次打开 Web 界面后，按左侧「快速开始」一步步配置

## Docker 部署

### 1. 准备目录

| 宿主机 | 容器内 | 用途 |
|---|---|---|
| 数据目录，如 `/vol1/docker/vox/data` | `/data` | 配置、数据库、小米登录令牌、曲库索引（需可写，属主 UID 1000） |
| 音乐目录，如 `/vol1/音乐` | `/music`（只读） | 本地音乐，支持 mp3/flac/m4a/aac/wav/ogg/ape |

```sh
mkdir -p /vol1/docker/vox/data && chown 1000:1000 /vol1/docker/vox/data
```

音乐目录可以挂多个（在 compose 里多加几行 `- /路径:/music2:ro`），然后在 Web「曲库」里逐个添加容器内路径。

### 2. 配置 `.env`

```sh
cp .env.example .env
```

| 变量 | 必填 | 说明 |
|---|---|---|
| `VOX_IMAGE` | | 镜像，正式版 `ghcr.io/<owner>/vox-mi-agent:<版本>` |
| `VOX_PORT` | ✅ | 宿主机端口，默认 8080。Web 界面和音箱拉取音乐都用它 |
| `VOX_DATA` | ✅ | 宿主机数据目录 |
| `VOX_MUSIC` | ✅ | 宿主机音乐目录 |
| `VOX_PUBLIC_BASE_URL` | 接音箱时 ✅ | 音箱访问本服务的地址：`http://<NAS局域网IP>:<VOX_PORT>`。也可在 Web「设置 → 音箱 → 对外地址」填写 |
| `VOX_LLM_PROVIDER` / `VOX_LLM_MODEL` / `VOX_LLM_API_KEY` | | 大模型；也可启动后在 Web 里配置。推荐硅基流动 `Qwen/Qwen3-8B` |

> 环境变量非空时，每次启动都会覆盖 Web 里改过的同名设置；想完全在 Web 里管理就留空。
>
> 只用于点歌时，大模型可以先不配：唯一命中的点歌请求不经过大模型。

### 3. 启动

```sh
docker compose --env-file .env up -d
curl http://localhost:8080/healthz
```

浏览器打开 `http://<NAS IP>:<VOX_PORT>`，按「快速开始」完成：大模型 → 曲库（添加 `/music`）→ 调试音箱试一句「播放晴天」→ 对外地址 → 小米账号登录 → 添加音箱并保存。

### 网络说明

- 音箱要能访问 `VOX_PUBLIC_BASE_URL`：先用手机浏览器打开它，能看到 Web 界面就说明音箱也能访问。
- 本服务需要访问外网：小米云（登录、音箱控制、对话记录）和大模型 API。
- 小米登录请在家庭网络进行，异地/机房 IP 更容易触发安全验证。

## 本地开发

```sh
make dev     # 主机直接运行 127.0.0.1:8080（数据在 ./data）
make up      # 构建本地镜像并用 compose 启动（读取 .env）
```

公司代理（如 Zscaler）环境：把 CA 的 PEM 放入 `certs/extra-ca.crt`，详见 `certs/README.md`。正式镜像不包含该证书。

| 目标 | 说明 |
|---|---|
| `make dev` | 本地直接运行 |
| `make test` / `test-docker` | 主机 / 容器内跑测试 |
| `make web` | 构建前端并嵌入 |
| `make build` | 交叉编译 amd64/arm64 到 `dist/` |
| `make image` / `image-local` | 多架构镜像 / 本地单架构镜像 |
| `make up` / `down` / `logs` | 用 compose 运行本地镜像 |

## 目录

| 路径 | 说明 |
|---|---|
| `cmd/app` | 程序入口（`serve` / `chat` / `healthcheck` / `version`） |
| `internal/` | 后端各包 |
| `web/` | 前端 |
| `scripts/`、`tools.lock` | 外部工具（fzq/jq/yq）下载与锁定 |
| `compose.yaml` / `compose.local.yaml` / `compose.test.yaml` | 运行 / 本地 CA 覆盖 / 测试 |
| `certs/` | 本地专用代理 CA（仓库中为空文件） |
| `docs/DESIGN.md` | 设计文档 |
