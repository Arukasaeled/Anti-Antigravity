<h1 align="center">2Ag · Anti-Antigravity</h1>

<p align="center">
  <strong>Google Antigravity 的本地桌面增强与控制层。</strong>
</p>

<p align="center">
  Windows x64 · Local-first · Runtime Injection · No official-file patching
</p>

<p align="center">
  <strong>简体中文</strong> · <a href="README.en.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/Arukasaeled/Anti-Antigravity/releases/latest"><img src="https://img.shields.io/github/v/release/Arukasaeled/Anti-Antigravity?style=flat-square&amp;color=4285f4" alt="最新版本"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" alt="Apache License 2.0"></a>
</p>

<p align="center">
  <strong><a href="https://github.com/Arukasaeled/Anti-Antigravity/releases/latest">下载最新版本</a></strong> ·
  <a href="#快速开始">快速开始</a> ·
  <a href="#文档与构建">文档与构建</a> ·
  <a href="https://github.com/Arukasaeled/Anti-Antigravity/releases">Release Notes</a>
</p>

<p align="center">
  <img src="docs/screenshots/00-banner.png" alt="2Ag · Anti-Antigravity" width="560">
</p>

2Ag 为 Antigravity 增加观察、控制、管理与定制能力：看清执行过程和 Token 消耗，管理账号、配额与会话，明确控制宿主的运行方式，并调整日常工作界面。

工作时，从 Antigravity 内的 **G-Hub** 使用增强能力；管理时，从独立的 **Manager** 查看本地资源与运行状态。两者服务于同一个 Antigravity 工作环境。

提供 Windows x64 安装包与便携 ZIP；使用前需安装官方 Google Antigravity。

**简体中文与 English 均为一级界面语言。** 首次运行跟随 Windows 系统语言，也可即时切换并保存；Manager 与 G-Hub 共用语言设置，ReAct 允许单独选择语言。宿主汉化开关独立于 2Ag 界面语言。

## 2Ag 带来什么

| 能力 | 为工作环境增加什么 |
|---|---|
| **Observe · 观察** | 从执行阶段、工具行为到 Request Trace、Context 与 Session Token，查看当前任务并复盘历史。 |
| **Control · 控制** | 选择 Official / Enhanced，控制宿主生命周期与运行时开关，区分配置模式、实际运行模式与进程所有权。 |
| **Manage · 管理** | 集中查看 Accounts、Quota 和 Antigravity Sessions，浏览已安装 Skills，检查 Environment、Doctor 与配置保存状态。 |
| **Customize · 定制** | 在 G-Hub 中整理 Prompt、固定消息与 Capsule，调整主题与壁纸，加载本地扩展。 |

<p align="center">
  <img src="docs/screenshots/01-overview-light.png" alt="浅色 Manager 总览：账号配额、配置模式与宿主运行状态" width="880">
</p>

*Manager 将账号、配额和宿主状态放在同一管理入口。*

欢迎页帮助新用户检查安装与本地环境、选择运行形态，再主动启动宿主。总览中的 **Compatibility / Capabilities** 使用已有 Runtime、Doctor、存储与配额探针，分别呈现配置、支持情况与当前生效状态；开关已开启不等于功能已生效。

## Antigravity 内的工作入口

**G-Hub 是工作中的入口，Manager 是管理中的入口。** G-Hub 通过 runtime injection 驻留在 Antigravity 内，可以查看配额、打开 Inspector、编辑 Prompt 和收集上下文。执行过程增强放在原生会话 Activity 区域，Request / Session 读数放在输入框控制行，沿用原有会话布局与滚动行为。

Manager 负责账号、会话、本地环境、诊断和宿主生命周期。你可以在工作时留在 Antigravity，集中管理时再打开 Manager。

<p align="center">
  <img src="docs/screenshots/02-g-hub.png" alt="Antigravity 内的 G-Hub：配额、外观调节与运行时控制" width="880">
</p>

*G-Hub 将配额、外观与工作流入口带回 Antigravity。图中展示外观调节界面。*

## 执行过程可观察性

2Ag 将官方压缩显示的 `Working...`、`Analyzed...` 等状态展开为可观察的 execution timeline。读取文件、运行命令、搜索、修改与模型响应都有对应行为入口，耗时、文件范围和命令结果在数据可用时显示。

| 展示层 | 用途 |
|---|---|
| **Phase Narrative** | 按真实行为组织阶段摘要、计数和当前动作，快速理解任务进展。 |
| **Detailed ReAct** | 展开 Read、Command、Search、Edit、Model Response；连续同类操作可以分组折叠。 |
| **Raw Inspector** | 点击行为查看原生标识、时间、结果与错误；点击模型响应进入该次 Request Trace / Token Inspector。 |

执行中持续更新，同一步骤的 `RUNNING / GENERATING → DONE / ERROR` 原位变化。Runtime 提供当前切片，conversation SQLite 提供持久化历史，合并时使用原生标识并保持会话来源一致。

**完成后自动折叠，执行记录不删除。** 最终答案回到主视觉；需要复盘时可重新展开，原生会话数据仍存在时可重建历史。支持全部展开 / 折叠与中文 / English，语言可跟随 2Ag 或单独设置；命令、路径、文件名、代码和原始输出 / 错误保留原文。

展示的是 **observable execution**。阶段叙事根据实际 Activity 组织，不读取或生成隐藏 chain-of-thought。

Activity Inspector 可按 All / Errors / Edits / Commands / Reads / Model 查看行为；阶段摘要汇总已有耗时、原生工具调用、模型请求与错误。命令输出、退出码与文件修改统计仅在原生数据可用时显示。

<p align="center">
  <img src="docs/screenshots/03-react-observability.png" alt="真实 Antigravity 会话：完成后展开的阶段执行记录、命令与模型响应，左侧为 G-Hub" width="880">
</p>

*2Ag 将压缩的 Working / Analyzed 状态展开为阶段化 execution timeline，并保留原始行为入口。这张真实截图展示任务完成后展开复盘的过程。*

## Context / Request / Session

输入框控制行常驻 Request / Session 读数，兼容中央的新会话输入框和底部的已有会话输入框。悬停查看同源 Token 明细，点击进入完整 Inspector。

| 读数 | 回答的问题 |
|---|---|
| **Request** | 这一次模型请求处理了多少输入、产生了多少输出？ |
| **Session** | 整个会话累计处理了多少 Token，包含多少次请求？ |
| **Context** | 当前上下文占用与上限是多少？有可靠来源时显示，估算明确标注。 |

**Request ≠ Session，Session ≠ Context。** 最近请求优先使用 Runtime 原生 usage，会话累计优先使用持久化 generation 数据并按 responseId 去重。已加载历史单独展示，不冒充当前 Prompt 或 Context Window。

数值保留来源与不确定性：未知显示 `—`，**unknown ≠ 0**；原生类别尚未确认的输入显示 **Unclassified Input**，不伪装成 Cache。不完整计数显示 `≥` 下界，估算显示 Estimated；Context limit 不按模型名猜测，缓存分类不足时保持 `Cache —`。

Request 与 Session 共用 Token breakdown，所有计入 Total 的 Token 都有归属；Reasoning 作为 Output 的子集展示，不重复累计。字段、计算口径与来源契约见 [OBSERVABILITY.md](docs/OBSERVABILITY.md)。

## 账号与配额

本地 **Account Vault** 使用 **DPAPI(CurrentUser)** 加密保存凭据。可以导入当前官方凭据，也可以通过本机 **Official Login Broker** 完成原生 Google 登录；2Ag 不处理 Google 密码。

集中查看账号的 Gemini 与 Claude / GPT 模型池配额。G-Hub 与账号视图使用所选账号的同一配额来源：真实数据正常显示，未知显示 `—`，缓存明确标注。

**selected / applied / verified 分别报告。** 选择账号、应用凭据和确认宿主内部登录身份是不同状态。凭据 owner 匹配与写入回读只证明凭据已应用；缺少可靠宿主身份来源时保留“宿主身份未验证”作为辅助信息。

<p align="center">
  <img src="docs/screenshots/05-accounts.png" alt="Manager Accounts：本地账号列表与各模型池配额" width="880">
</p>

*账号与配额集中管理，便于查看各账号的可用资源。*

## 会话、Skills 与环境

- **Sessions Browser**：只统计 Antigravity 会话，按项目浏览，查看 Session Token、请求数与 Activity 历史，支持删除指定会话。消息预览和 Markdown 导出需要可读 transcript，所有操作沿同一真实 store / source 进行。
- **Skills**：只读浏览已安装的 Global / Workspace Skills，查看 metadata、`SKILL.md` 及资源目录。当前不提供安装、启用 / 禁用或 Marketplace。
- **Environment / Doctor / Diagnostics**：查看安装与存储环境，检查进程、CDP、注入、Vault 和本地数据可用性，导出不含凭据的诊断报告。配置保存状态区分 pending、saved 与 save_failed。

| 运行时增强开关 | Antigravity Sessions |
|---|---|
| <img src="docs/screenshots/04-diagnostics.png" alt="Manager 运行时增强开关与实验性选项" width="420"> | <img src="docs/screenshots/07-sessions.png" alt="Manager Sessions：项目筛选、会话列表与批量操作" width="420"> |
| 按需选择运行时增强，实验性选项单独标识。 | 在同一入口浏览、管理与复盘本地会话。 |

## 运行时控制

| 模式 | 行为 |
|---|---|
| **Official** | 使用用户本机的官方安装，不注入 2Ag 增强 UI。 |
| **Enhanced** | 从本机官方安装建立冻结的本地宿主副本，通过 runtime injection 加载 G-Hub 与会话内增强。 |

**configured mode** 是下一次启动配置，**effective mode** 是当前运行事实。选择 Official / Enhanced 只改配置；启动、停止、重启与接管由明确的用户动作执行。运行时开关按需选择，其可用性取决于宿主版本。

进程按权限分为 `owned`（2Ag 启动并托管）、`adopted`（用户明确授权接管）与 `external`（仅发现）。2Ag 默认不停止 external；需要关闭外部实例时先要求明确确认，发现 PID 或 Electron single-instance 转交不会自动取得管理权。

**不 patch 官方文件。** Enhanced 副本在用户本机建立，不随 2Ag 分发，也不会自动跟随官方更新。工作机制与生命周期边界见 [ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 定制与扩展

G-Hub 支持内置主题预设、自选壁纸、模糊与透明度调节，也能恢复默认外观。主题预设由当前内置实现提供；`themes/` 保存可选参考资源，并非放入文件即可动态加载的主题目录。

**Prompt / Capsule 工作流**用于保存草稿、复用片段、固定消息与汇集上下文。Capsule 将目标、约束和选定内容整理成可编辑文本，支持复制或插入 Antigravity 输入框，由用户决定发送。

**本地扩展**可添加命令、面板、Prompt 操作、消息操作与 Context Provider，支持启用、停用和重新加载。仓库附带 Prompt Toolkit、Conversation Map、Project Context 示例，分别用于提示词组织、已加载消息导航和项目上下文收集。扩展契约见 [plugins/README.md](plugins/README.md)，外观边界见 [themes/README.md](themes/README.md)。

## 工作原理

```text
Antigravity
├─ React / Runtime + Language Server updates ── CDP ──┐
└─ conversation SQLite ── read-only ─────────────────┤
                                                     ↓
                                                    2Ag
                                                     ├─ G-Hub / 会话内增强
                                                     └─ Manager
```

CDP 读取运行时状态并在 Enhanced 中注入界面；SQLite 只读补充持久化历史。**No app.asar patch · No official binary modification**。数据层供多个视图共用，内部结构见 [Architecture](docs/ARCHITECTURE.md) 与 [Observability](docs/OBSERVABILITY.md)。

## 快速开始

1. 准备 **Windows 10/11 x64 + WebView2**，先安装官方 Google Antigravity。
2. 从 **[latest Release](https://github.com/Arukasaeled/Anti-Antigravity/releases/latest)** 下载 `Anti-Antigravity-Setup-x64.exe` 或 `2Ag-v<版本>-windows-x64-portable.zip`。
3. 运行安装包，或解压完整便携目录并保留 `assets/`、`themes/`、`plugins/`，启动 `2ag.exe`。
4. 首次欢迎页会检查环境并让你选择 Official / Enhanced；可先“打开 Manager”，或明确点击“启动 Antigravity”。**首次 Enhanced 需从本机官方安装建立宿主副本**，也可在欢迎页单独建立副本而不启动。已有用户不会反复进入欢迎页，可从总览重新打开。

## 隐私与安全

本地优先，无 2Ag 作者遥测服务器；账号登录与配额请求仍使用相应官方服务。本机 Control API 校验随机 control token 并拒绝不可信 Origin，外部进程受 ownership 保护。代理不安装 CA、不做 TLS MITM。

Activity / Request 详情和本地扩展涉及用户内容，分享前应检查路径、命令与原始输出。凭据保护、扩展权限与网络边界见 [SECURITY.md](docs/SECURITY.md)。

## 已知限制

- 当前支持 Windows x64，需要用户自备官方 Antigravity。
- 原生字段与 React 结构受宿主版本影响；缺失数值保持 Unavailable，未知工具保持 Tool。
- 历史复盘依赖原生会话数据仍存在；未加载、不可解析或被截断的内容不能完整还原。SQLite 的 Activity / Token 可用不代表消息预览或导出也可用。
- Token 分类、Cache 和 Context 上限可能未知；当前没有可靠的宿主内部身份来源，profile 隔离也不等于共享系统凭据隔离。
- 冻结宿主需由用户选择同步官方更新，部分运行时开关仍受宿主版本限制。

## 文档与构建

[Architecture](docs/ARCHITECTURE.md) · [Observability](docs/OBSERVABILITY.md) · [Security](docs/SECURITY.md) · [Configuration](docs/CONFIGURATION.md) · [CLI](docs/CLI.md)

源码构建与正式打包见 [BUILD.md](docs/BUILD.md)，开发说明见 [DEVELOPMENT.md](docs/DEVELOPMENT.md)。本地扩展与主题参考分别见 [plugins/README.md](plugins/README.md) 和 [themes/README.md](themes/README.md)；逐版本更新放在 [Release Notes](https://github.com/Arukasaeled/Anti-Antigravity/releases)。

## 致谢与商标

- Google Antigravity 提供宿主，Chromium DevTools Protocol 与 [go-webview2](https://github.com/jchv/go-webview2) 提供注入与窗口基础。
- [Cockpit Tools](https://github.com/jlcodes99/cockpit-tools)（CC-BY-NC-SA-4.0）提供配额协议行为参考，2Ag 未复制其代码；[Token Monitor](https://github.com/Javis603/token-monitor) / [Tokscale](https://github.com/Javis603/tokscale) 提供 Antigravity 原生 usage、去重与聚合设计参考。
- Material Design / Gemini 提供视觉参考；其它社区设计参考见 [OBSERVABILITY.md](docs/OBSERVABILITY.md)。

2Ag 自有代码与文档采用 [Apache License 2.0](LICENSE)。不包含或分发 Google Antigravity runtime。Google、Antigravity、Gemini、Claude、ChatGPT 等名称与标识归各自权利人所有；2Ag 与 Google、Anthropic、OpenAI 没有官方关联或背书。
