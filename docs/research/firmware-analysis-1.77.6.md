# 小米音箱 1.77.6 固件分析

## 1. 结论与分析范围

分析对象是 `mico_firmware_0395b_1.77.6.bin`。这是 **HDR1 格式升级包，不是整片 NAND Flash 的原始备份**。

包内主要是版本信息、SquashFS 根文件系统和启动镜像。设备使用 `boot0 + system0`、`boot1 + system1` 两套系统槽位，共享可写的 `data` 分区。`binwalk` 中的 gzip、DTB 和 eCos 字符串命中不能各自当成独立分区。

本文依据升级包头、设备树、已提取的根文件系统、ramdisk 初始化逻辑及升级脚本整理；没有连接真机验证实际 MTD 布局，没有执行刷写操作。

## 2. 固件身份

| 项目 | 值 |
|---|---|
| 文件名 | `mico_firmware_0395b_1.77.6.bin` |
| 文件大小 | 42,293,688 字节（约 40.33 MiB） |
| SHA-256 | `11b1da3fed1fd7f237a397052711f362fb2659633cee7ff4304d889df998f547` |
| ROM | `1.77.6` |
| 渠道 | `stable` |
| 包内 HARDWARE | `LX06` |
| 构建时间 | `Tue, 07 Sep 2021 17:59:04 +0800` |
| 构建提交 | `b9e9b6640c2491c7a77a22612e47790e6c8c0356` |
| Linux 内核 | `4.9.61` |
| 用户空间 | OpenWrt 风格的 Linux，而非 Android 应用系统 |

### LX06 与 L06A 的关系

不能只根据包内的 `HARDWARE=LX06` 判定它不包含 L06A 支持，也不能据此认定任意 LX06/L06A 固件可互刷。

根文件系统的 `/etc/init.d/boot` 包含明确的 L06A 分支：

- `board_id` 为 `12`、`9`、`8`、`7` 时执行 `l06a_board_config_fix()`。
- 加载 `/usr/share/mico.l06a/*.cfg`，将运行时型号设置为 `L06A`。
- 将 miio 模块设置为 `xiaomi.wifispeaker.l06a`。
- 通过 bind mount 覆盖通用配置、版本和 manifest 文件。

这证明该包包含 L06A 适配逻辑，但**不构成对特定硬件版本刷写安全性的保证**。

## 3. 升级包内部布局

下表偏移全部是**升级包文件偏移**，不是 NAND 写入地址。每个记录的包头占 48 字节；表中的载荷偏移指镜像内容起点。

| 包内条目 | 条目头偏移 | 载荷偏移 | 载荷字节数 | 大小 | 用途 |
|---|---|---|---:|---:|---|
| `mico_version` | `0x38` | `0x68` | 514 | — | ROM 版本、渠道、平台、构建信息 |
| `root.squashfs` | `0x288` | `0x2B8` | 37,042,833 | 35.33 MiB | 写入目标系统槽位的只读根文件系统 |
| `boot.img` | `0x2353D68` | `0x2353D98` | 5,249,790 | 5.01 MiB | 写入目标启动槽位的内核、ramdisk 和设备树 |

包中没有两份 `boot.img` 或两份 `root.squashfs`。升级程序根据当前活动槽位，把同一份镜像写入另一套分区。

包尾存在额外数据；本文没有验证其签名算法或验签过程，不能据此推断修改后仍能通过原厂升级检查。

### SquashFS 属性

- SquashFS 4.0，小端，XZ 压缩。
- 块大小：131,072 字节。
- inode 数量：2046。
- 文件系统创建时间：`2021-09-07 09:49:11`（binwalk 输出，未标注时区）。

## 4. 设备 NAND 分区及用途

设备树有两组分区配置。对 NAND，应参考 `/mtd_nand/nand_partition`；不要混用另一组通用 `/partitions` 数据。

| 分区 | NAND 配置大小 | 用途 | 此升级包是否包含对应镜像 |
|---|---|---|---|
| `bootloader` | 无法由本包确定 | 最早期引导代码，负责后续启动；升级脚本支持处理 `u-boot.bin`、`u-boot.bin.usb.bl2` | 否 |
| `tpl` | 设备树未指定固定容量 | 后续启动加载器区域；准确布局需要真机及引导器信息确认 | 否 |
| `boot0` | 6 MiB | 第 0 套启动镜像，含内核、ramdisk、设备树 | 是，使用 `boot.img` |
| `boot1` | 6 MiB | 第 1 套启动镜像，与 `boot0` 交替使用 | 是，使用同一份 `boot.img` |
| `system0` | 40 MiB | 第 0 套 SquashFS 根文件系统 | 是，使用 `root.squashfs` |
| `system1` | 40 MiB | 第 1 套 SquashFS 根文件系统，与 `system0` 交替使用 | 是，使用同一份 `root.squashfs` |
| `data` | 动态分配，不能由本包确定容量 | 两套系统共享的持久化用户数据，使用 UBI/UBIFS，挂载到 `/data` | 否 |

### 两组设备树配置的差异

| 节点 | boot0 / boot1 | system0 / system1 | data |
|---|---|---|---|
| `/mtd_nand/nand_partition` | 各 6 MiB | 各 40 MiB | offset 为全 1、size 为 0，动态布局标记 |
| `/partitions` | 各 8 MiB | 各 32 MiB | size 为全 1，动态布局标记 |

`root.squashfs` 本身已经约 35.33 MiB，大于 32 MiB；不能用通用 `/partitions` 中的 32 MiB 参数直接刷写。

设备树中的多个 offset 为 0，不意味着所有分区都从物理地址 0 开始。驱动、引导器保留区和 NAND 坏块处理均会影响实际布局，**不能简单累加表中容量推导刷写地址**。

此前解析的 ramdisk 将 `system0`、`system1` 分别映射至 `/dev/mtdblock4`、`/dev/mtdblock5`。根文件系统启动脚本将 `/dev/mtd6` 用于 data。以上编号只描述本固件逻辑，真机操作必须以 `/proc/mtd` 为准。

## 5. boot.img 内部组成及 binwalk 结果解释

`boot.img` 采用 Android boot image 容器格式，页大小为 2048 字节；容器格式不代表系统运行 Android。

| binwalk 文件偏移 | 内容 | 作用 |
|---|---|---|
| `0x2353D98` | Android boot image 头 | 描述内核、ramdisk 和后续组件的位置与长度 |
| `0x2354598` | gzip 内核，压缩长度 3,718,382 字节 | Linux 4.9.61 内核 |
| `0x26E0598` | gzip ramdisk，压缩长度 1,455,362 字节 | CPIO 格式早期用户空间；执行 `/init`，选择系统槽位并切换根文件系统 |
| `0x2844598` | DTB，32,823 字节，`xiaomi_lx06_v01` | 一套硬件描述，包含 NAND 分区配置 |
| `0x284CD98` | DTB，32,823 字节，`xiaomi_lx06_v02` | 另一套硬件描述，包含相同的 NAND 分区容量配置 |

boot image 头中的 second 区域长度为 71,680 字节，包含后续设备树数据。两份 DTB 的存在不意味着有两个额外 Flash 分区。

binwalk 报告的 `ecos` / `ecos_memory` 字符串位于 DTB 数据中，不能当作存在独立 eCos 操作系统分区的证据。

## 6. 根文件系统与持久化数据

### system0 / system1

两者存放完整系统，而不是一个正常系统加一个仅用于修复的精简 recovery。

| 路径 | 主要内容 |
|---|---|
| `/bin`、`/sbin` | BusyBox、系统管理、升级及初始化工具 |
| `/etc` | 服务启动脚本、系统默认配置 |
| `/lib` | 动态库、内核模块、升级相关脚本 |
| `/usr/bin` | 音箱语音、播放、蓝牙、小米通信等程序 |
| `/usr/share` | 设备配置、提示音、资源文件 |

SquashFS 本身只读。固件通过 `/data` 保存可变状态，并对部分路径使用 bind mount；不能直接把运行时持久配置写入原始 SquashFS。

### data

`/etc/init.d/boot` 的关键逻辑为：

```sh
ubiattach -p /dev/mtd6
mount -t ubifs /dev/ubi0_0 /data
```

典型用途包括：

| 路径 | 用途 |
|---|---|
| `/data/etc` | 持久配置、设备信息及部分服务相关状态 |
| `/data/wifi`、`/data/bt` | 无线网络及蓝牙相关配置 |
| `/data/miio` | 小米设备通信、注册及账号关联状态 |
| `/data/mico` | 型号适配配置及运行时覆盖文件 |
| `/data/log` | 持久日志 |
| `/data/miio_mpkg` | 可更新应用组件 |

**重要风险：**启动脚本在 `/data` 挂载失败后会执行 `ubidetach`、`flash_erase /dev/mtd6 0 0`、重新 attach、创建 data 卷再挂载。此分区异常时可能被自动擦除重建，不能将其视为绝对安全的唯一备份位置。

## 7. 双系统启动与升级机制

`/bin/boardupgrade.sh` 通过 `fw_env -g boot_part` 判断当前启动槽位，并通过 `/proc/mtd` 按分区名称定位目标。

| 当前 boot_part | 当前系统 | 升级写入目标 | 升级成功后的 boot_part |
|---|---|---|---|
| `boot0` | `system0` | `boot1` + `system1` | `boot1` |
| `boot1` | `system1` | `boot0` + `system0` | `boot0` |

ramdisk 的 `/init` 选择并挂载对应根文件系统，之后切换到真实系统根目录。此前分析还确认其存在根文件系统挂载失败时切换槽位、重启的路径；这不是对任意损坏均可自动恢复的保证。

升级脚本依次调用设备树、引导器、内核、SquashFS 的更新函数。函数调用存在不代表本升级包一定包含对应条目：本包没有 bootloader 镜像。

## 8. 对 SSH 和自定义程序部署的影响

- 用户程序及配置应优先放在 `/data`，而不是尝试直接修改只读 `system` 文件系统。
- 把二进制放进 `/data` 不等于开机自启；还需要确认该固件实际执行的初始化入口及持久化方式。
- 若修改的是某个系统槽位中的启动脚本，OTA 切换到另一槽位后该修改可能不再生效。
- 本文没有提供已验证的 SSH 开启配方，也没有验证自定义程序在 L06A 真机上的开机启动；这些与分区静态分析分开处理。
- vox-mi-agent 的 bridge 接入属于应用层功能，不改变本升级包的分区结构，也不证明其可以直接安全刷入某台设备。

## 9. 复核方法与分析限制

### 本机可复核命令

在固件所在目录执行：

```sh
sha256sum mico_firmware_0395b_1.77.6.bin
binwalk mico_firmware_0395b_1.77.6.bin
unsquashfs -s -o 696 mico_firmware_0395b_1.77.6.bin
# 提取到新目录，不覆盖已有提取结果：
unsquashfs -no-xattrs -o 696 -d /tmp/mico-rootfs-analysis mico_firmware_0395b_1.77.6.bin
```

非 root 用户提取时可能因无法创建 `/dev/console` 字符设备而返回非零；此前已提取普通文件并完成脚本分析，但该提取目录不能当作可直接启动的完整根文件系统副本。

### 真机只读核对命令

以下仅用于读取信息，不执行升级、擦除或修改环境变量：

```sh
cat /proc/mtd
cat /proc/cmdline
mount
binfo -a
fw_env -g boot_part
```

### 已验证与尚未确定

已验证：升级包条目及长度、SquashFS 属性、两段 gzip 内容解压、ramdisk CPIO 解析、两份设备树解析、根文件系统普通文件提取及启动/升级脚本分析。文档整理时重新复核了 binwalk 结果、包头条目、设备树分区容量和 SHA-256。

尚未确定：真机 Flash 总容量、全部分区的物理起始地址、bootloader/tpl 的真实容量、保留区与坏块布局、实际活动槽位、升级验签机制，以及 SSH、自启和刷写在具体 L06A 硬件上的效果。

**禁止把本包当作整片 Flash 备份，也不要把 binwalk 偏移或本文中的名义分区容量直接用作刷写参数。**

## 10. duhow/xiaoai-patch 成品固件核查

核查日期：2026-10-29。实际下载 GitHub 最新 release 的 LX06 资产，校验 SHA-256，读取 tar 元数据、SquashFS 内的版本文件、程序与启动服务，并解压内核检查版本；未刷机，也未运行 ARM 镜像中的服务。

### 10.1 版本与下载

| 项目 | 实际结果 |
|---|---|
| Release 标签 | `2026.3.26`，标题 `Xiao Ai Patched v2026.3.26` |
| LX06/L06A 成品文件 | `mico_firmware_080b2a0_260329_lx06.tar` |
| tar 大小 | 40,673,280 字节 |
| SHA-256 | `0e4edc5de66118ff23f6bcf321851421468c2e1fad62a9df697495608c2584de`，与 GitHub 资产 digest 一致 |
| 镜像内 ROM 版本 | `2026.03.29`，渠道 `release` |
| 原厂基线 | **1.74.10**，metadata 和 `/usr/share/mico/version` 的 ROOTFS、LINUX 字段均如此标记 |
| 实际内核版本 | **Linux 4.9.61**，不是 Linux 1.74.10 |
| 内核构建信息 | `jenkins@a69f86dfe04f`，`Mon Jun 28 02:23:39 2021` |
| 镜像构建时间 | `Sun, 29 Mar 2026 22:55:17 +0000` |
| 镜像记录提交 | `fb070495955668f825700bbd988d5443e9bcadb3` |
| 安装文档声明支持 | L06A 和 LX06 共用 `_lx06.tar` |

注意区分 release 标签、成品 ROM 日期版本和原厂基线版本：它不是本目录 **1.77.6** 固件的补丁版，而是较早 **1.74.10** 基线上的定制系统。文件名中的 `080b2a0` 不应当作原厂版本号。

tar 包含 `update.sh`（9,236 字节）、`root.squashfs`（35,405,824 字节）、`boot.img`（5,249,790 字节）和 `metadata`。这是可刷写的镜像包，不是小米 HDR1 原厂 OTA 包，也不是整片 Flash 备份。

### 10.2 增加的功能

| 能力 | 镜像及代码证据 | 使用边界 |
|---|---|---|
| SSH 管理 | Dropbear，端口 22，启用密码/root 密码认证，存在开机启动链接 | 工厂默认 root 密码为 `root`；首次使用应立即改密。已有 `/data/console/shadow` 时会保留该持久密码，不保证总能用默认密码登录 |
| Web 管理/API | Python 3.9、Flask，HTTP 80；配置、音量、静音、唤醒、服务控制、设备信息；Avahi 发布 `_xiaoai._tcp` | HA 授权是关联 HA 的流程，不应当视作所有管理接口都有访问认证；仅放可信局域网 |
| AirPlay 接收 | `shairport-sync` 二进制及启动链接 | 配置及真机音频输出未实测 |
| DLNA/UPnP 播放 | `upmpdcli` + MPD，均有启动链接 | 替代原厂播放器架构 |
| 音乐播放服务 | MPD、Squeezelite | 对接相应媒体控制端/服务器；不是自带所有在线音乐源 |
| 多房间同步 | Snapcast 的 `snapclient`、`snapserver` | 客户端有启动链接；服务端虽打包但未见默认启动链接，需按配置启用 |
| 蓝牙音频 | BlueALSA、配对脚本及蓝牙服务修改 | release 特别提及改善配对 |
| 自定义语音入口 | Porcupine 唤醒、ALSA 录音、SoX/FFmpeg 音频处理、HA STT/Conversation 对接 | 默认唤醒词 `alexa`；`/data/listener` 未配置时 listener 不启动，不是开箱即用中文小爱替代品 |
| 语音播报 | 默认 launcher 使用 `tts_google`；镜像另含 eSpeak NG | 默认播报仍依赖外部 Google 服务，不能宣称默认全离线 |
| 本地意图匹配 | 英语、西班牙语、加泰罗尼亚语语句；蓝牙配对/断开、暂停、音量等 | “本地意图”是识别文本后的本地规则；语音转文字仍由所配置的 HA STT 提供，不代表音箱本机运行 Whisper |
| 持久化与维护 | `/data/console/shadow` 保留密码，更新 CA 证书及时间同步等修改 | 保留 data 挂载失败后可能自动擦除的原厂逻辑 |

语音路径为 Porcupine 唤醒后录音，上传 Home Assistant 的 STT 提供方（例如 NAS 上的 faster-whisper），文本先匹配本地意图，否则调用 HA `/api/conversation/process`，再由 TTS 播报。配置外部服务后可脱离小米语音云，但不能由此推断为完全离线系统。

### 10.3 删除或改变的原厂功能

这不是仅开启 SSH 的轻量补丁。发布构建的脚本关闭大量原厂服务，并删除相应程序和库：

- `mico_aivs_lab`、原厂唤醒模型、mibrain/AIVS 相关库被移除，原厂小爱语音路径不可照常使用。
- 原厂 `mediaplayer`、`miio_client`、消息代理、米家自动化、部分蓝牙 Mesh 和语音通话组件被删除或停用。
- 原厂 `ota`、`boardupgrade.sh`、`flash.sh` 被移除，不能继续假定原厂 OTA 和恢复出厂后的应用功能不变；项目提供自己的 `update.sh`。
- `/bin/mphelper` 与 `/bin/miplayer` 改为指向 `miplay` 的链接。该脚本主要播放本地音频，并不实现原厂 `mphelper pause/play/mute_stat` 完整接口。
- 镜像仍残留部分原厂 init 脚本和 `/usr/sbin/tts_play.sh`；文件存在不代表对应服务可用。后者仍依赖已经移除的 mibrain 和原厂播放器接口。

**对现有 vox-mi-agent bridge 的影响：不能直接当作兼容底座。**现有 bridge 依赖原厂识别日志、TTS、播放器接口，而此发行镜像替换了这些组件。若选它，应改接其录音/STT 与 MPD/ALSA 等接口；不能仅把现有 bridge 二进制复制进去就宣称链路可用。

### 10.4 本次 release 变化与刷写风险

Release 说明列出：修复旧内核设备 libgcrypt 的 getentropy、改善蓝牙配对、加入三种语言本地意图、更新 CA 证书、修复新版 HA 令牌保存、修复 LX06 AUX-in 事件处理。作者仍标注 **Currently untested**。

当前安装文档特别提醒：项目不修改或重编译内核，随包附的是较旧的原厂 boot.img。部分新版 LX06 使用 Foresee/ESMT `FSNS8A001G` NAND，旧内核无法识别，刷入后可能出现 `No NAND device found` 并循环重启。**原机 boot0/boot1 工作正常时，文档建议保留自己的启动镜像，而非盲目替换为随包版本。**

文档提供 USB/U-Boot 工具刷写方式，需要拆机接触 micro USB，并明确适用于 L06A/LX06；并非一定需要焊接 TTL 才能安装，但应保留引导器救援路径。其示例备份容量仍是作者设备参数，不能直接当作自己设备的真实布局。

若目的仅为保留原厂中文小爱、开启 SSH 并部署现有 bridge，保留原机 1.77.6 系统、做最小启动和 SSH 修改更符合现有依赖；若目的是改造成 HA 语音终端和通用网络音箱，该项目更接近目标，但需另行配置和真机验证。

### 10.5 来源

- [最新 release](https://github.com/duhow/xiaoai-patch/releases/latest) / [固定 release 2026.3.26](https://github.com/duhow/xiaoai-patch/releases/tag/2026.3.26)
- [LX06/L06A 成品 tar 下载](https://github.com/duhow/xiaoai-patch/releases/download/2026.3.26/mico_firmware_080b2a0_260329_lx06.tar)
- [当前安装说明及 NAND 兼容警告](https://github.com/duhow/xiaoai-patch/blob/master/research/lx06/install.md)
- [发布镜像记录提交的语音配置说明](https://github.com/duhow/xiaoai-patch/blob/fb070495955668f825700bbd988d5443e9bcadb3/docs/voice-assistant.md)
- [关闭原厂服务脚本](https://github.com/duhow/xiaoai-patch/blob/fb070495955668f825700bbd988d5443e9bcadb3/scripts/20_disable_services.sh)
- [删除原厂程序脚本](https://github.com/duhow/xiaoai-patch/blob/fb070495955668f825700bbd988d5443e9bcadb3/scripts/24_remove_property_bin.sh)
- [默认 root 密码设置脚本](https://github.com/duhow/xiaoai-patch/blob/fb070495955668f825700bbd988d5443e9bcadb3/scripts/12_root_password_factory.sh)

## 11. 固件下载网页与版本

整理日期：2026-10-29。以下为 `idootop/open-xiaoai` 已发布的 1.88 及更新版本，发布名称使用 **LX06**。本节仅汇总版本、下载网页和文件直链，不评估 L06A 兼容性或刷写风险。

参考项目：[idootop/open-xiaoai](https://github.com/idootop/open-xiaoai)，可参考其刷机流程及固件下载资源。

- [刷机教程](https://github.com/idootop/open-xiaoai/blob/main/docs/flash.md)：设备连接、刷写系统分区、SSH 登录和系统槽位切换。
- [固件制作与补丁说明](https://github.com/idootop/open-xiaoai/blob/main/packages/client-patch/README.md)：下载或自行制作补丁固件的参考入口。
- [固件发布与下载列表](https://github.com/idootop/open-xiaoai/releases)：各版本原版及补丁版系统镜像，已整理的版本见下表。

| 版本 | 下载网页 | 原版系统镜像 | 补丁版系统镜像 |
|---|---|---|---|
| **1.94.13** | [LX06_1.94.13](https://github.com/idootop/open-xiaoai/releases/tag/LX06_1.94.13) | [LX06_1.94.13.squashfs](https://github.com/idootop/open-xiaoai/releases/download/LX06_1.94.13/LX06_1.94.13.squashfs) | [LX06_1.94.13_patched.squashfs](https://github.com/idootop/open-xiaoai/releases/download/LX06_1.94.13/LX06_1.94.13_patched.squashfs) |
| **1.88.221** | [LX06_1.88.221](https://github.com/idootop/open-xiaoai/releases/tag/LX06_1.88.221) | [LX06_1.88.221.squashfs](https://github.com/idootop/open-xiaoai/releases/download/LX06_1.88.221/LX06_1.88.221.squashfs) | [LX06_1.88.221_patched.squashfs](https://github.com/idootop/open-xiaoai/releases/download/LX06_1.88.221/LX06_1.88.221_patched.squashfs) |
| **1.88.206** | [LX06_1.88.206](https://github.com/idootop/open-xiaoai/releases/tag/LX06_1.88.206) | [LX06_1.88.206.squashfs](https://github.com/idootop/open-xiaoai/releases/download/LX06_1.88.206/LX06_1.88.206.squashfs) | [LX06_1.88.206_patched.squashfs](https://github.com/idootop/open-xiaoai/releases/download/LX06_1.88.206/LX06_1.88.206_patched.squashfs) |

