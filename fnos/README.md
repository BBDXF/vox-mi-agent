# fnOS (飞牛 NAS) .fpk 打包

把 Docker 镜像 `ghcr.io/bbdxf/vox-mi-agent:<version>`（amd64/arm64）包装成 fnOS 应用。

## 文件说明（`vox-mi-agent/`）
- `manifest`：应用元信息（版本由构建时替换 `@VERSION@`）
- `ICON.PNG` / `ICON_256.PNG`、`app/ui/images/`：占位图标，可替换
- `app/ui/config`：桌面入口（网关模式：gatewayPrefix `/app/vox-mi-agent`，gatewaySocket `app.sock`，即 `${TRIM_APPDEST}/app.sock`；容器内 `/sock/app.sock`，同时监听 tcp 8080 供音箱/局域网直连）
- `app/docker/docker-compose.yaml` + `.env`：容器定义；`.env` 由向导回调改写
- `cmd/main`：start/stop/status（运行 0，未运行 3）
- `cmd/*_init`、`*_callback`：生命周期钩子；`install_callback`/`config_callback` 经 `write_env.sh` 写 `.env`
- `config/privilege`、`config/resource`：运行用户与 docker-project 声明
- `wizard/*`：安装/配置/卸载/升级向导

## 构建
```sh
make fpk            # 等价 fnos/build.sh $(VERSION)，输出 dist/fpk/
fnpack build vox-mi-agent   # 在 dist/fpk 内（build.sh 检测到 fnpack 会自动执行）
```
无 fnpack 时只生成 tar.gz。新建骨架可用 `fnpack create`。

## 安装测试
```sh
appcenter-cli install-fpk vox-mi-agent.fpk
```
或在应用中心「手动安装」上传。安装后通过飞牛桌面/远程访问 `/app/vox-mi-agent/`；局域网直连 `http://NAS-IP:18080/app/vox-mi-agent/`。在 Web 中配置 LLM 与小米账号。

## 真机待验证清单
- [ ] 向导字段是否以 `wizard_*` 环境变量传给 cmd 脚本
- [ ] compose 是否能读到 `docker/.env`（及 `TRIM_PKGVAR`/`TRIM_UID` 插值）
- [ ] 音乐目录是否需通过数据共享授权才能挂载
- [ ] `platform=all` 配合多架构镜像是否可行
- [ ] 网关能访问 `${TRIM_APPDEST}/app.sock`（权限 0666，网关不剥前缀）
- [ ] SSE（`/sse/chat`）经网关不被缓冲、可流式输出
- [ ] 容器 uid（TRIM_UID）能写入 `${TRIM_APPDEST}`（创建 socket）
- [ ] manifest 更新日志键名：`changelog` 还是 `change_log`
- [ ] 端口固定为 18080（manifest service_port / compose `TRIM_SERVICE_PORT` / ui/config）；确认 checkport 生效、桌面入口可打开
- [ ] 平台自行启动 compose 时能否读到 `docker/.env`（`cmd/main` 已显式 `--env-file`）
- [ ] 卸载向导 `wizard_keep_data` 的处理（目前 uninstall 回调未删数据）
