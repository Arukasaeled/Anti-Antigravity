<img src="docs/screenshots/00-banner.png" alt="2Ag · Anti-Antigravity" width="560">

# 2Ag · Anti-Antigravity

**给 Google Antigravity 加一层控制台。**

账号与圆环配额常驻顶部；在宿主内编写提示词、固定消息、收集上下文、使用本地扩展，不修改官方安装。

[![Release](https://img.shields.io/github/v/release/arukas0623-ai/Anti-Antigravity?style=flat-square&color=4285f4)](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)
![Platform](https://img.shields.io/badge/platform-Windows%20x64-0078d4?style=flat-square)

**[⬇ Download v0.1.1](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest)** · [发布说明](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest)

当前源码为 **v0.2.0-rc.3 Interaction Layer / Live Trace 2.0**，公开安装包仍以 Release 页面为准。新功能与已检查的范围见 [v0.2.0 说明](RELEASE-NOTES-v0.2.0.md)，可按 [构建指南](docs/BUILD.md) 打包当前源码。RC2 的实机范围、性能回放与限制见 [验证记录](docs/RC2-REALITY-OBSERVABILITY.md)；RC3 的白屏修复、切号对照及未完成验收见 [实机记录](docs/RC3-ACCOUNT-SWITCH-REALITY.md)。

<img src="docs/screenshots/01-overview-light.png" alt="2Ag Manager 概览" width="880">

---

## 2Ag 是什么

Google Antigravity 是一个很强的 AI 工作台，但它没有给你一个"管理它的地方"。

2Ag 就是那个地方。它在你自己启动的 Antigravity 外面加一圈**本机控制台**：

- 看清宿主现在到底跑着什么；
- 管住多个 Google 账号的配额；
- 换个皮肤；
- 把几个被官方藏起来的行为开关交回你手里。

**它不修改官方 Antigravity 的任何安装文件** —— `app.asar` 原样保留，不重打包、不替换。注入只走运行时，宿主一关就干干净净。

---

## 核心功能

### 🎛 2Ag Manager

一个 WebView2 原生窗口，也是控制中枢。

**概览** 宿主状态与模型配额 · **账号** 账号矩阵与保险库 · **会话** 本地会话索引与导出 ·
**视觉工坊** 壁纸/模糊/主题 · **重力加倍** 运行时开关 · **环境诊断** 探针与兼容性报告。

### 🪟 G-Hub

注入到宿主里的**浮标 + 战术面板**（Shadow DOM，与宿主 DOM 完全隔离）。

不切窗口就能看状态、换账号、重启宿主沙箱。

v0.2.0 提供中英文界面，顶部始终保留账号选择器和 Gemini / Claude+GPT 的 5h、周配额四个圆环。常用入口直接可见，管理工具按需展开：

- **编写**：编辑草稿、插入片段、一键结构化文本、填写原生输入框；
- **会话**：选择宿主历史会话、搜索/筛选已加载消息，直接固定或加入胶囊；
- **胶囊**：用消息与笔记整理可编辑上下文，保存、复制、插入；
- **Live Trace**：直接订阅原生 Agent 事件流，查看真实工具、读写文件、命令、错误、公开回复与 checkpoint 摘要；支持旧步骤回看、搜索、文件变更元数据和送入 Capsule；
- **扩展**：直接打开面板、执行命令、添加上下文，管理本地插件的启用与重载。

**2Ag Interaction Layer = Prompt · Conversation · Context · Command · Extension**。随附提示词工具箱、会话地图、项目上下文三个纯本地示例；插件开发入口见 [plugins/README.md](plugins/README.md)。

Live Trace 在增强形态的 G-Hub 中打开；窗口关闭即取消订阅，不建设后台录制服务，不读取隐藏思维。原生 RPC 不可用时显示原因。数据范围与实机证据见 [账号生命周期与 Live Trace](docs/ACCOUNT-LIFECYCLE-LIVETRACE.md)。

<img src="docs/screenshots/02-g-hub.png" alt="G-Hub 战术面板" width="880">

### 🔐 Account Vault

多账号管理，凭据以 **DPAPI(CurrentUser)** 加密存放。

> 密文只有**同一台机器的同一个 Windows 用户**能解开 —— 换用户、换机器都解不开。
> 索引文件只存邮箱等元数据，token 不进配置、不进日志、不进 API 响应、不进前端。

### 📊 Model Quota

按账号读取 Gemini / Claude / GPT 的 5 小时滑窗与周限额。

- **优先实时探测**：2Ag 自己带凭据向 Antigravity 配额接口查询，不依赖任何第三方工具；
- **本地 Cockpit Tools 缓存只作兜底**，且会明确标成「非实时读数」，绝不冒充实时读数；
- 多账号并发探测，单个账号超时不影响其他账号；
- 读不到显示 `--`，过期就明说过期。

**低配额接力（默认关闭）**：任务中当前 5h 或周配额 ≤5% 时，选择实时余量充足的账号，保存任务交接、事务切号并通过原生输入区接续。开关位于 G-Hub 顶部与 Manager 概览；附带接力记录、可复制交接和不确定发送保护。源码与本地打包包含此功能；本轮未取得满足阈值的真实任务，未进行低配额端到端切号。规则与边界见 [账号接力说明](docs/ACCOUNT-RELAY.md)。

<img src="docs/screenshots/05-accounts.png" alt="账号矩阵与配额" width="880">

### 🎨 Skin Studio

自定义壁纸、模糊半径、遮罩浓度，外加多套内置主题。

壁纸是你自己的图片，2Ag 不预置、不携带任何图片。

### 🔀 Official / Enhanced

两种形态随时切换：

| | 用哪份宿主 | 加了什么 |
|---|---|---|
| **Official** | 你本机的官方安装 | 什么都不加，行为接近"没装 2Ag" |
| **Enhanced** | 2Ag 自己的冻结副本 | 完整注入 + G-Hub + 皮肤 + 重力加倍 |

增强形态的宿主是 2Ag 从你本机官方安装**物理复制**出的一份冻结副本，官方目录全程只读。

### 🛡 Compatibility Guardian

跑起来就自检：官方安装是否可定位、官方文件是否被改过、两套 profile 是否隔离、当前运行形态是什么。

有问题的项如实报出来，不粉饰。

---

## 截图

| | |
|---|---|
| <img src="docs/screenshots/04-diagnostics.png" alt="运行时开关"> | <img src="docs/screenshots/07-sessions.png" alt="会话索引"> |
| **运行时开关** | **本地会话索引** |

<!-- TODO: 以下截图待补充，勿用占位图
     03-skin-studio.png  — 视觉工坊（壁纸 / 主题）
     06-dark.png         — 深色主题
-->

---

## 快速开始

1. **先装官方 Antigravity** —— 2Ag 不含、也不分发它。
2. **下载 2Ag** —— [最新 Release](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest) 里的 `Anti-Antigravity-Setup-x64.exe`。
3. **安装并启动** —— 开始菜单或桌面快捷方式里的 `2Ag`。
4. **挑一个形态** —— 首次用**增强形态**时，2Ag 会从你的官方安装复制一份冻结宿主（约 570 MB，需要数十秒）；之后一直用它。

> 便携版：Release 目前只提供安装包 `Anti-Antigravity-Setup-x64.exe`，**没有独立的 `2ag.exe` 便携包**。想要绿色版请从源码 `go build`，再把 `assets/` `themes/` `plugins/` 放到 `2ag.exe` 同级。

系统要求：**Windows 10/11 x64** · WebView2 运行时（Win11 自带；Win10 若缺，微软官网可单独装）。

---

## 添加账号

官方已经登录时，可以在 Manager 的添加账号窗口点击 **「导入当前官方账号」**，直接存入保险库并刷新账号列表。

添加账号由**你本机的官方 Antigravity** 完成原生 Google 登录：

1. 2Ag 先把你当前的凭据加密归档进保险库，并同步保存 DPAPI 恢复记录；
2. 停止宿主、临时清除当前凭据，以本次选择的 **AUTO / DIRECT / PROXY** 模式启动官方登录窗口；仅调整子进程网络环境和诊断端口，不永久修改系统代理、不注入；
3. 你在官方窗口里点 **Continue with Google**，授权完全由官方与系统浏览器完成；
4. 2Ag 等待完整新登录、原生服务确认有效身份及新 access token 后捕获它、加密入库；
5. 自动恢复你原来的账号及运行形态，核对原生身份后清除恢复记录；流程中断时，下次启动 2Ag 先恢复凭据。恢复凭据与确认原生登录分开报告。

日常切换保险库账号复用当前形态，不打开官方登录窗口。先刷新目标登录并由 Google 核验邮箱，再停止宿主、写入凭据并核验原生登录。预检失败保留当前宿主；原生地区／资格拒绝会显示真实原因并回滚。不能仅凭旧 ID token 的时间判断“缺少 token”，也不能仅凭凭据写入报告成功。增强形态的 OAuth URI 使用同一可执行文件与账号 profile，避免产生默认 profile 的第二个宿主。

**2Ag 不接触你的 Google 密码或登录授权码。** 添加账号由官方宿主完成 Google 登录；切号预检从你本机的原生运行时读取匹配的桌面 OAuth 客户端配置，仅在内存中用于续期，不随仓库或安装包分发、不写日志或前端。隐私边界见 [安全说明](docs/SECURITY.md)。

登录完成后，凭据存进 **2Ag 自己的账号库**（`~/.2ag/`，DPAPI 加密 + 一份不含 token 的账号清单）。
添加账号**不需要**你本机装过任何第三方工具。

---

## 本地优先

**所有数据都留在你自己的机器上。没有任何 2Ag 作者的服务器、统计或上报。**

- 2Ag 自己出网只去 `127.0.0.1`，以及宿主本来就要访问的 Google 端点；
- 账号凭据以 DPAPI 加密存放，只有本机本用户能解开；
- **不安装 CA 证书、不解密 TLS** —— HTTPS `CONNECT` 是不透明隧道，2Ag 改不了也看不到；
- 打包时有结构性泄漏闸门：产物里一旦出现绝对用户路径、构建机用户名或真实邮箱，打包当场失败。

详见 [SECURITY.md](docs/SECURITY.md)。

---

## 它是怎么工作的

2Ag 拉起官方宿主时附加一个 CDP 调试端口，用 `Runtime.evaluate` +
`Page.addScriptToEvaluateOnNewDocument` 把界面注入渲染进程 —— **不碰磁盘上的任何官方文件**。

界面以 Shadow DOM 挂载，与宿主自己的 DOM 完全隔离。宿主一关，注入即刻消失。

宿主、代理、侧车进程的生命周期分两条链：`2ag run` 把它们挂在同一条 Windows Job Object 下，退出时连带清干净；Manager 默认走 `LaunchEnhancedHost` 直接拉宿主，**关掉 Manager 不会带走宿主**。详见 [ARCHITECTURE.md](docs/ARCHITECTURE.md)。

---

## 文档

| | |
|---|---|
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | 内部结构、注入链路、进程生命周期 |
| [SECURITY.md](docs/SECURITY.md) | 凭据处理、加密、隐私边界 |
| [CONFIGURATION.md](docs/CONFIGURATION.md) | `2ag.json` 全部字段 |
| [CLI.md](docs/CLI.md) | 命令行子命令 |
| [BUILD.md](docs/BUILD.md) | 构建与打包 |
| [DEVELOPMENT.md](docs/DEVELOPMENT.md) | 开发环境与约定 |
| [plugins/README.md](plugins/README.md) | 插件契约 |
| [themes/README.md](themes/README.md) | 主题定义 |

---

## 已知限制

- **Windows x64 only** —— 依赖 Windows 凭据管理器与 Job Object。
- **必须自备官方 Antigravity** —— 2Ag 不包含、不分发它。
- **冻结宿主会落后** —— 官方 updater 更新的是你的官方安装，2Ag 的副本不会自动跟。落后多少由更新守护如实报出，要不要同步你决定。
- **非活跃账号的 access 会过期** —— 切号预检会续期并核验目标 Google 身份。配额后台探测不循环续期所有账号，无法取得实时配额时如实标注。刷新授权已撤销时需重新添加账号；原生资格仍须单独核验。2Ag 不内置 OAuth 客户端配置。
- **登录凭据是机器级共享的** —— 官方 Antigravity 与 2Ag 沙箱读同一条系统凭据记录，这是 Windows 凭据模型决定的，2Ag 只能做到 profile 隔离。环境诊断会把这一条标成共享。
- **部分运行时开关受宿主版本限制** —— 面板会标「实验性 / 开发中」并在宿主侧容器未挂钩时明说「切换不会生效」。

---

## 最新版本

**v0.1.1** —— 首次公开发布。

**[下载](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest)** ·
[发布说明](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest)

发布页附带 `Anti-Antigravity-Setup-x64.exe.sha256`，下载后自行校验：

```powershell
Get-FileHash .\Anti-Antigravity-Setup-x64.exe -Algorithm SHA256
```

---

## Credits / Inspirations

- **Google Antigravity** —— 被增强的宿主本体。
- **[Cockpit Tools](https://github.com/jlcodes99/cockpit-tools)**（作者 jlcodes，CC-BY-NC-SA-4.0）—— 模型配额探测的**协议行为参考**。2Ag 的探针照着它观测到的接口形状重写，**没有复制其任何代码**；该项目源码与授权不属本仓库，也不随 2Ag 分发。
- **Material Design 3 / Google Gemini 视觉语言** —— Manager 配色、圆角与明暗双调色板的参照。
- **[jchv/go-webview2](https://github.com/jchv/go-webview2)** —— Manager 的原生窗口容器。
- **Chromium DevTools Protocol** —— 注入链路的技术基础。
- 第三方模型标识 **Gemini / Claude / ChatGPT** 的图形是其各自权利人的商标，在此**仅用于标注数据属于哪个模型池**。

---

## 许可与商标

2Ag **自有代码与文档**以 [Apache License 2.0](LICENSE) 授权。

许可只覆盖 2Ag 自己的内容。2Ag **不包含也不分发** Google Antigravity 运行时；其版权归 Google，授权条款随你本机安装的官方 Antigravity 附带。

**Google**、**Antigravity**、**Gemini**、**Claude**、**ChatGPT** 等名称与标识归各自权利人所有，在此仅用于指称被提及的产品或服务。2Ag 与 Google、Anthropic、OpenAI **没有官方关联**，也未获其赞助、授权或背书。
