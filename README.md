# Anti-Antigravity (2Ag)

**2Ag 是 Windows 上的 Antigravity 增强外壳。**

它**不修改官方 Antigravity 的任何安装文件** —— `app.asar` 原样保留、不重打包、不替换。
2Ag 做的是：在自己拉起官方宿主的时候，额外起一圈**本机回环服务**，并通过 CDP
把一块界面注入宿主的渲染进程。

```
2ag.exe  ──┬─ 本机回环控制面 (API + 内嵌前端)
           ├─ 本机回环 HTTP 转发代理
           ├─ 侧车插件进程
           └─ 官方 Antigravity 宿主 ── CDP ──> G-Hub / G-Cockpit（注入）
                 └─ 以上全部挂在同一条 Windows Job Object 下：宿主退出，一并清干净
```

当前版本：**v0.1.1**（首次公开发布）。

---

## 组成

`2ag.exe` 一个可执行文件同时是引擎和 GUI。不带参数（或 `2ag manager`）打开的就是
**2Ag Manager** —— 一个 WebView2 原生窗口，里外都是本机页面：

| 面板 | 内容 |
|---|---|
| 概览 | 宿主状态、模型配额、协议网关、上游兼容性 |
| 账号 | 账号矩阵、OAuth 登录、JSON 凭据导入、主账号切换 |
| 会话 | 会话列表、导出、删除 |
| 视觉工坊 | 壁纸 / 模糊 / 不透明度 / 界面主题 |
| 重力加倍 | 并发与端点相关的运行时开关 |
| 环境诊断 | 宿主探针、运行时模式、兼容性守护报告 |

**G-Hub / G-Cockpit** 是注入到官方宿主里的那一层（Shadow DOM）：一个浮标 + 一块战术面板，
不切窗口就能看状态、换账号、重启宿主沙箱。

## 运行

安装版：运行 `Anti-Antigravity-Setup-x64.exe`，从开始菜单或桌面快捷方式启动 `2Ag`。

源码版：

```powershell
go build -o 2ag.exe .\cmd\2ag
.\2ag.exe                # 打开 2Ag Manager
```

命令行子命令（给脚本和排障用）：

```
2ag manager                    打开 Manager 图形控制台（等同无参数启动）
2ag run [--debug]              只启动引擎 + 宿主，不开 Manager
2ag restore                    还原被改动过的宿主侧状态
2ag skin show                  查看当前视觉参数
2ag skin set-wallpaper <path>  设置壁纸
2ag skin set-blur <0..N>       设置模糊半径
2ag skin set-opacity <0..1>    设置不透明度
2ag profile list|save|switch   账号档案
2ag doctor                     打印环境诊断
```

### 前置条件：官方 Antigravity 需要你自己安装

2Ag **不包含、也不分发** Google Antigravity —— 这个仓库和安装包里没有任何 Google
运行时字节。请先在本机安装官方 Antigravity（默认在
`%LOCALAPPDATA%\Programs\Antigravity`），然后：

| 形态 | 用哪份宿主 | 说明 |
|---|---|---|
| 官方形态 | 你本机的官方安装 | 零启动参数、零注入、零凭据改写 |
| 增强形态 | 2Ag 自己的冻结宿主副本 | 首次启动时从你本机的官方安装**物理复制**一份出来 |

冻结宿主副本的落点是 `2ag.exe` 同级的 `app\`（约 570 MB，首次建立需要数十秒）。
建立过程对官方安装目录**只读**：不修改、不移动它的任何文件。副本建好后 2Ag 一直
使用它，官方 updater 更新的则是你那份官方安装 —— 两者物理隔离，版本是否落后由
**更新守护**如实报出，要不要同步由你决定，2Ag 不会自动替换你的宿主。

本机既没有官方安装、也还没有冻结副本时，Manager 照常打开，只显示
「未检测到 Antigravity」，不会伪造版本、PID 或配额。

## 数据与隐私

**所有数据都留在你自己的机器上。**

- 2Ag 自己出网只去两个地方：`127.0.0.1`（回环控制面与代理），以及宿主本来就要访问的
  Google 账号 / API 端点（`accounts.google.com`、`oauth2.googleapis.com`、`www.googleapis.com`）。
  **没有任何 2Ag 作者的服务器、统计、上报。**
- 账号凭据、会话、壁纸路径等全部从**当前用户本机**读取，并写回本机的 `2ag.json`。
- 仓库里**没有**任何写死的开发机数据。新装（配置里为空）时，需要用户数据的面板显示
  `--` / 不可用，而不是某个人的真实数值。
- **不安装 CA 证书、不解密 TLS。** HTTPS `CONNECT` 是不透明隧道，因此本项目的代理
  无法改写 HTTPS 请求体 —— 这是刻意的设计边界，不是待办事项。
- 打包时有**结构性泄漏闸门**（见 `scripts/pack.ps1`）：产物里一旦出现
  `C:\Users\<某人>\` 形式的绝对路径、构建机用户名或真实邮箱，打包当场失败。
  发布包里没有任何上游 / 第三方运行时字节，所以闸门的作用域就是 2Ag 自己的全部产物。

## 配置

`2ag.json` 与 `2ag.exe` 同目录，首次启动自动生成：

```json
{
  "wallpaper_path": "",
  "blur": 20,
  "opacity": 0.55,
  "modal_opacity": 0.85,
  "language": "zh-CN",
  "runtime_mode": "enhanced",
  "network": { "enabled": true, "endpoint_overrides": [], "rule_targets": [] },
  "privacy": { "blocked_hosts": ["google-analytics.com"], "block_beacons": true },
  "global_rules": "",
  "env_overrides": {},
  "plugins": []
}
```

`wallpaper_path` 出厂为空 = 跟随宿主原生背景。壁纸是**用户自己的数据**，
2Ag 不预置、不携带任何图片。

## 代理

本机回环转发代理，由宿主通过 `--proxy-server=http://127.0.0.1:<port>` 使用：

- 按 `privacy.blocked_hosts` 拦截遥测域名，`privacy.block_beacons` 拦截信标；
- 按 `network.endpoint_overrides` 把明文 HTTP 请求改道到指定上游；
- `network.rule_targets` + `global_rules` 可以往**显式指定**的 JSON 字段里前插规则。

`CONNECT` 保持不透明（见上）。

## 构建与打包

需要：Go 1.22+、Inno Setup 6（只在做安装包时需要）。

**打包不需要本机安装官方 Antigravity。** 发布包不含任何官方运行时字节：
`app\`（2Ag 自己的冻结宿主副本）由 2Ag 在用户机器上按需建立，不由构建机提供。

```powershell
# 1) 本地自用：普通二进制
go build -o 2ag.exe .\cmd\2ag

# 2) 出发布包：release 二进制 + staging + 安装包
.\scripts\pack.ps1 -Version 0.1.1

# 只出 staging（不调 Inno Setup，用于验证产物内容）
.\scripts\pack.ps1 -Version 0.1.1 -SkipInstaller
```

`pack.ps1` 依次做三件事：release 构建（`-H=windowsgui -X main.version=<版本>`）、
写入中性初始配置、**跑泄漏闸门**。闸门不过就 `throw`，不会产出安装包。

发布包内容：

```
dist\staging\
  2ag.exe                  引擎 + Manager（release 构建）
  2ag.json                 中性初始配置
  assets\                  logo / icon / 注入载荷
  themes\                  主题定义（纯数据）
  plugins\                 示例侧车插件
dist\Anti-Antigravity-Setup-x64.exe
```

`/dist` 与 `app\` 都不进仓库：`app\` 是运行时在用户本机生成的派生物，仓库里既没有
上游字节，也没有指向某台机器的链接。安装包与仓库遵循同一条边界。

## 目录结构

```
cmd/2ag/            进程入口、Manager 窗口、子命令
internal/api/       回环控制面（state / accounts / sessions / wallpaper / compat …）
internal/config/    配置模型与持久化
internal/core/      状态与事件总线
internal/netproxy/  本机回环转发代理
internal/patcher/   CDP 桥与注入载荷（injected_hub.js）
internal/supervisor/宿主启动、账号扫描、兼容性与更新守护、壁纸、侧车
web/                内嵌前端（go:embed，手工维护的单文件 SPA）
assets/             2Ag logo / 图标 / 内嵌资源
themes/ plugins/    主题定义 / 插件契约（各自有 README）
scripts/pack.ps1    发布构建 + 泄漏闸门
installer.iss       Inno Setup 安装脚本
```

## 插件与主题

见 [`plugins/README.md`](plugins/README.md) 与 [`themes/README.md`](themes/README.md)。
两处都写明了契约和边界：插件是外部侧车进程 + 一个 Hub 标签页；
`themes/` 只是纯数据描述，运行时真正生效的主题预设内嵌在注入载荷里。

## Credits / Inspirations

- **Google Antigravity** —— 被增强的宿主本体。2Ag 与 Google 无关联，未获其背书。
- **Cockpit Tools**（[@jlcodes99](https://github.com/jlcodes99/cockpit-tools)，作者 jlcodes，
  CC-BY-NC-SA-4.0）—— 模型配额探测的**协议行为参考**。2Ag 的配额探针是照着它观测到的
  接口形状重写的，**没有复制其任何代码**；该项目的源码与授权都不属于本仓库，
  也不随 2Ag 分发（其许可与本项目的发布方式不兼容）。
- **Material Design 3 / Google Gemini 视觉语言** —— Manager 的配色、圆角与
  明暗双调色板参照。
- **[jchv/go-webview2](https://github.com/jchv/go-webview2)** —— Manager 的原生窗口容器。
- **Chromium DevTools Protocol** —— 注入链路（端口自分配、整段注入、增量推送）。
- **上游第三方组件** —— 官方 Antigravity 运行时里的第三方组件（如
  `resources/app.asar.unpacked/` 下的 `chrome-devtools-mcp`）版权归其各自作者，
  授权条款随上游附带。2Ag **不包含也不分发**这些字节：发布包里没有官方运行时，
  它们只存在于你自己安装的官方 Antigravity 中。
- 第三方模型标识 **Gemini / Claude / ChatGPT** 的图形是其各自权利人的商标，
  在此**仅用于标注数据属于哪个模型池**，不表示任何关联或背书。

## 商标声明

**Google**、**Antigravity**、**Gemini**、**Claude**、**ChatGPT** 等名称与标识归各自权利人
所有，在此仅用于**指称被提及的产品或服务**（描述性使用）。2Ag 与 Google、Anthropic、OpenAI
**没有官方关联**，也未获其赞助、授权或背书。

## 许可

2Ag **自有代码与文档**以 [Apache License 2.0](LICENSE) 授权，完整文本见仓库根 `LICENSE`。

许可只覆盖 2Ag 自己的内容。第三方内容不属于本仓库、也不随发布包分发：Google Antigravity
运行时的版权归 Google，授权条款随你本机安装的官方 Antigravity 附带；`Credits / Inspirations`
里提到的第三方项目各有其自己的许可。
