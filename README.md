# vox-mi-agent

[![ci](https://github.com/BBDXF/vox-mi-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/BBDXF/vox-mi-agent/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/BBDXF/vox-mi-agent)](https://github.com/BBDXF/vox-mi-agent/releases)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

自托管的 AI 语音助手：接管小爱音箱的点歌请求，播放 NAS 上的本地音乐；其他问题交给大模型 Agent。Go 后端 + Web 控制台，单个 Docker 容器（约 80MB，amd64 / arm64）。

## 为什么好用：fzq 模糊搜索 + LLM Agent 原生工具调用

语音点歌的难点是**识别出来的文字不准**：同音字、错别字、简繁、半句歌名、只说歌手。本项目把这件事拆成两层：

- **[fzq](https://github.com/BBDXF/fzq) 本地模糊搜索负责「找歌」**：针对中文语音场景做了拼音/同音、错别字、部分匹配和多歌手拆分。「周杰轮的七里香」「海阔天空」「Iselin」都能命中。单次搜索 20–40ms，569 首曲库同步不到 0.5 秒，内置 79 条点歌回归用例全部通过。
- **LLM Agent 原生调用（OpenAI function calling）负责「理解和兜底」**：`search_audio`、`play_audio`、`query_json` 等都是标准工具，模型自己决定何时搜索、何时播放、何时追问。不写提示词模板解析，不锁定某家厂商，任何兼容 OpenAI 接口的模型都能用。
- **快路径不经过大模型**：点歌请求搜到唯一高分结果时直接播放，从说完话到开口约 1 秒（主要是轮询小米对话记录的间隔）。「来点陈奕迅的歌」会随机播放该歌手的歌。只有歧义、搜不到或非点歌问题才交给 LLM。

### 小模型就够，大部分场景费用为 0

- 搜索的「智能」在 fzq 里，LLM 只需做简单的工具调用，所以 **Qwen3-8B 这类 8B 小模型就能稳定完成**点歌、追问、确认播放。
- 推荐 [硅基流动](https://siliconflow.cn) 的 `Qwen/Qwen3-8B`（平台免费模型；内置预设已关闭思考模式以降低延迟）。也支持 DeepSeek、通义、OpenAI、OpenRouter、本地 Ollama 等。
- 日常点歌绝大多数走快路径，根本不调用大模型。**只用来点歌时，大模型甚至可以不配置。**
- 需要更复杂的能力（比如用 jq 统计曲库）时，可以随时换成更强的模型。

## 快速体验

不接音箱也能试：启动后打开 Web 控制台，在「对话」里对「调试音箱」说「播放晴天」，就能看到搜索 → 播报 → 播放的完整过程，并在浏览器里试听。

### docker run

```sh
docker run -d --name vox-mi-agent --restart unless-stopped \
  -p 8080:8080 \
  -v /vol1/docker/vox/data:/data \
  -v /vol1/音乐:/music:ro \
  -e TZ=Asia/Shanghai \
  -e VOX_LLM_PROVIDER=siliconflow \
  -e VOX_LLM_MODEL=Qwen/Qwen3-8B \
  -e VOX_LLM_API_KEY=sk-xxxx \
  ghcr.io/bbdxf/vox-mi-agent:latest serve --listen tcp://0.0.0.0:8080 --data /data
```

打开 `http://<NAS IP>:8080`，左侧「快速开始」会一步步引导配置。

### 镜像与二进制

| 形式 | 地址 |
|---|---|
| Docker 镜像（amd64 / arm64） | `ghcr.io/bbdxf/vox-mi-agent:latest`、`ghcr.io/bbdxf/vox-mi-agent:<版本>`（如 `0.1.0`、`0.1`） |
| 二进制 | [Releases](https://github.com/BBDXF/vox-mi-agent/releases) 中的 `app_<版本>_linux_{amd64,arm64}.tar.gz`（需自备 fzq/jq/yq，推荐用镜像） |

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
| `VOX_IMAGE` / `VOX_VERSION` | | 用仓库 compose 时设镜像；用上面的 compose 时设版本，默认 `latest` |
| `VOX_PORT` | ✅ | 宿主机端口，默认 8080。Web 界面和音箱拉取音乐都用它 |
| `VOX_DATA` | ✅ | 宿主机数据目录 |
| `VOX_MUSIC` | ✅ | 宿主机音乐目录 |
| `VOX_PUBLIC_BASE_URL` | 接音箱时 ✅ | 音箱访问本服务的地址：`http://<NAS局域网IP>:<VOX_PORT>`。也可在 Web「设置 → 音箱 → 对外地址」填写 |
| `VOX_LLM_PROVIDER` / `VOX_LLM_MODEL` / `VOX_LLM_API_KEY` | | 大模型；也可启动后在 Web 里配置。推荐硅基流动 `Qwen/Qwen3-8B` |

> 环境变量非空时，每次启动都会覆盖 Web 里改过的同名设置；想完全在 Web 里管理就留空。
>
> 只用于点歌时，大模型可以先不配：唯一命中的点歌请求不经过大模型。

### 3. 启动

不需要克隆仓库，新建一个目录放 `compose.yaml` 和 `.env` 即可：

```yaml
# compose.yaml
services:
  app:
    image: ghcr.io/bbdxf/vox-mi-agent:${VOX_VERSION:-latest}
    container_name: vox-mi-agent
    restart: unless-stopped
    user: "1000:1000"
    ports:
      - "${VOX_PORT:-8080}:8080"
    environment:
      TZ: Asia/Shanghai
      VOX_PUBLIC_BASE_URL: ${VOX_PUBLIC_BASE_URL:-}
      VOX_LLM_PROVIDER: ${VOX_LLM_PROVIDER:-siliconflow}
      VOX_LLM_MODEL: ${VOX_LLM_MODEL:-Qwen/Qwen3-8B}
      VOX_LLM_API_KEY: ${VOX_LLM_API_KEY:-}
    command: ["serve", "--listen", "tcp://0.0.0.0:8080", "--data", "/data"]
    volumes:
      - ${VOX_DATA:-./data}:/data
      - ${VOX_MUSIC:-./music}:/music:ro
```

```sh
docker compose --env-file .env up -d
curl http://localhost:8080/healthz          # {"status":"ok",...}
docker compose pull && docker compose up -d # 升级
```

（克隆了仓库的话，直接用仓库里的 `compose.yaml`，在 `.env` 里设 `VOX_IMAGE=ghcr.io/bbdxf/vox-mi-agent:latest`。）

浏览器打开 `http://<NAS IP>:<VOX_PORT>`，按「快速开始」完成：大模型 → 曲库（添加 `/music`）→ 调试音箱试一句「播放晴天」→ 对外地址 → 小米账号登录 → 添加音箱并保存。

### 怎么说

| 说法 | 效果 |
|---|---|
| 小爱同学，播放一首周杰伦的晴天 | 直接播放（不经过大模型） |
| 我想听海阔天空 / 来一首七里香 | 直接播放 |
| 来点陈奕迅的歌 | 随机播放该歌手的一首 |
| 放一首关于下雨的歌 | 搜不到或有歧义 → 交给 Agent 处理（搜索、追问或推荐） |
| 今天天气怎么样 | 不接管，仍由小爱回答 |

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
| `fnos/` | 飞牛 fnOS `.fpk` 打包（`make fpk`） |
| `certs/` | 本地专用代理 CA（仓库中为空文件） |
| `docs/DESIGN.md` | 设计文档 |

## 许可证

[MIT](LICENSE) © BBDXF

## Optional runtimes & run_shell

bun / node / uv can be installed on demand into `/data/runtime/` via `GET/POST/DELETE /api/runtimes[/{name}[/install]]`. The `run_shell` tool (bash, no sandbox) is disabled by default; remove it from `agent.tools_disabled` to enable. See docs/DESIGN.md §6.10.
