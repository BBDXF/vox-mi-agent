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
- `wizard/*`：安装/配置/卸载/升级向导。安装/配置向导的「NAS 局域网 IP」（`wizard_lan_ip`，可留空自动检测）与「音频端口」（`wizard_port`，默认 18080）合成 `VOX_PUBLIC_BASE_URL`；该地址在 Web 中只读，修改请走应用设置

## 构建
```sh
make fpk            # 等价 fnos/build.sh $(VERSION)，输出 dist/fpk/
# 装了 fnpack 时自动在 dist/fpk/vox-mi-agent 内执行 fnpack build，输出 dist/fpk/vox-mi-agent_<版本>.fpk
```
无 fnpack 时只生成 tar.gz。发版时 CI 用官方 fnpack 1.2.3 打包，`.fpk` 附在 GitHub Release 中。新建骨架可用 `fnpack create`。

## 安装测试
```sh
appcenter-cli install-fpk vox-mi-agent.fpk
```
或在应用中心「手动安装」上传。安装后通过飞牛桌面/远程访问 `/app/vox-mi-agent/`；局域网直连 `http://NAS-IP:18080/app/vox-mi-agent/`（端口以向导设置为准）。在 Web 中配置 LLM 与小米账号。

## 真机待验证清单
- [ ] 向导字段是否以 `wizard_*` 环境变量传给 cmd 脚本（`write_env.sh` 用到 `wizard_music_dir`、`wizard_lan_ip`，以及 `TRIM_APPDEST`、`TRIM_PKGVAR`、`wizard_port`）
- [ ] 改「音频端口」后 compose 端口映射与对外地址同步；端口被占用时安装/配置报错提示（`TRIM_TEMP_LOGFILE`）；且自动检测的 NAS 局域网 IP 正确（`docker/.env` 中 `VOX_PUBLIC_BASE_URL=http://<IP>:<端口>`）；Web 设置页该项只读
- [ ] compose 是否能读到 `docker/.env`（及 `TRIM_PKGVAR`/`TRIM_UID` 插值）
- [ ] 音乐目录是否需通过数据共享授权才能挂载
- [ ] `platform=all` 配合多架构镜像是否可行
- [ ] 网关能访问 `${TRIM_APPDEST}/app.sock`（权限 0666，网关不剥前缀）
- [ ] SSE（`/sse/chat`）经网关不被缓冲、可流式输出
- [ ] 容器 uid（TRIM_UID）能写入 `${TRIM_APPDEST}`（创建 socket）
- [ ] manifest 更新日志键名：`changelog` 还是 `change_log`
- [ ] 端口由向导配置（`checkport=false`，`write_env.sh` 自行检查占用）；桌面入口走网关 socket，与端口无关
- [ ] 平台自行启动 compose 时能否读到 `docker/.env`（`cmd/main` 已显式 `--env-file`）
- [ ] 卸载向导 `wizard_keep_data` 的处理（目前 uninstall 回调未删数据）

## 参考文档

- 飞牛开发者文档（全文，适合 AI 检索）：https://developer.fnnas.com/llms-full.txt
- 网关入口（socket 转发）：https://developer.fnnas.com/docs/core-concepts/gateway-registration
- fnpack 打包工具：https://developer.fnnas.com/docs/cli/fnpack
- 参考项目：[TencentCloud/Octop](https://github.com/TencentCloud/Octop/tree/main/fnos/docker)、[conversun/fnos-apps](https://github.com/conversun/fnos-apps)、[langbot-app/LangBot](https://github.com/langbot-app/LangBot/tree/master/packaging/fnos)

## 离线安装包

Release 附带三个包：`vox-mi-agent_<V>.fpk`（在线，启动时从 ghcr.io 拉镜像）、`_x86.fpk` / `_arm.fpk`（离线，内含对应架构镜像 `app/docker/image.tar.gz`，`cmd/main start` 首次 `docker load` 后删除压缩包）。本地构建：`FPK_IMAGE=ghcr.io/bbdxf/vox-mi-agent:<V> sh fnos/build.sh v<V>`。
