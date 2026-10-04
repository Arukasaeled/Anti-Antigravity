<img src="docs/screenshots/00-banner.png" alt="2Ag · Anti-Antigravity" width="560">

# 2Ag · Anti-Antigravity

**让 Antigravity 的执行过程、上下文和资源消耗可见、可追踪、可复盘。**

Google Antigravity 的本地 **Observability & Control Layer**。在原生会话里查看真实工具行为、请求与 Token；在 Manager 中管理账号、配额和运行状态。

**Windows x64 · Local-first · Runtime Injection · No official-file patching**

[![Release](https://img.shields.io/github/v/release/Arukasaeled/Anti-Antigravity?style=flat-square&color=4285f4)](https://github.com/Arukasaeled/Anti-Antigravity/releases/latest)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

**[下载最新版本](https://github.com/Arukasaeled/Anti-Antigravity/releases/latest)** · [发布说明](https://github.com/Arukasaeled/Anti-Antigravity/releases/latest) · [构建指南](docs/BUILD.md)

Release 提供 `Anti-Antigravity-Setup-x64.exe` 和 `2Ag-v<版本>-windows-x64-portable.zip`。2Ag 不包含 Google Antigravity runtime，请先安装官方客户端。

## 为什么需要 2Ag

官方会话里的 `Working...`、`Thought for 13s`、`Analyzed...` 把执行过程压缩成几行。Antigravity 自己的 Runtime 和 conversation SQLite 已经保存了更多可观察行为。

2Ag 消费这些真实数据，把阶段、工具、模型请求和 Token 放回原生会话工作区。你能看见读了哪些文件、运行了什么命令、修改了什么，以及对应请求的资源消耗；结束后可以展开复盘。

展示的是 **observable execution**：行为分组与叙事来自真实 Activity，不读取、恢复或伪造隐藏 chain-of-thought。

## ReAct Observability

| 层级 | 看什么 | 怎么展开 |
|---|---|---|
| **Phase Narrative** | Codex 式阶段摘要、行为计数、当前动作与耗时 | 默认只展开当前 Phase |
| **Detailed ReAct** | DSH 式完整行为过程：Read、Command、Search、Edit、Model Response | 展开阶段与详细过程；连续同类操作可折叠 |
| **Raw Inspector** | 原生 ID、时间戳、命令结果、错误、修改结果与 Token breakdown | 点击单条行为；模型响应进入 Request / Token Inspector |

执行中实时追加，`RUNNING / GENERATING → DONE / ERROR` 原位更新。Runtime 当前切片与 SQLite 历史按原生标识合并，不按标题去重，也不把当前 Runtime 合并进另一会话或备份来源。

任务完成后整个 ReAct 自动折叠，最终答案进入主视觉。**完成不会删除执行记录**：可重新展开完整已读取历史，并从仍存在的原生持久化会话重建。命令输出默认折叠，unknown tool 保持 Tool。

支持全部展开 / 折叠、中文 / English，以及跟随 2Ag 或独立的语言选择。只翻译 UI 和行为叙事；code、command、path、filename、raw output / error 保留原文。用户离开底部查看历史时停止自动跟随，回到底部再跟随新步骤。

内联增强使用已有 CDP runtime injection，挂在原生会话 Activity 区域。DOM 重建后恢复；卸除注入会清理自有节点、样式与监听，恢复官方显示与交互。

## Context、Request 与 Session Token

| 读数 | 含义 |
|---|---|
| **Request** | 单次模型请求实际处理的输入与输出，优先读取 Runtime 原生 usage；模型响应可进入所选请求详情 |
| **Session** | 整个会话的原生 generation 累计，优先读取持久化 conversation DB，按 responseId 去重 |
| **Context** | 当前 Context Window 占用，仅使用可靠原生占用 / 估算字段；不是 Session 累计，也不拿 Request 输入冒充 |

Composer control row 常驻 Request / Session 读数，跟随中央或底部输入框；hover 看同源 breakdown，点击进入完整 Inspector。

Request 和 Session 共用字段语义：**New Input、Unclassified Input、Cache Read、Cache Write、Output、Reasoning**。Total 包含所有输入类别与 Output；Reasoning 是 Output 的信息性子集，不重复累计。每项计入 Total 的 Token 都有可见归属。

- 未提供的字段显示 `—`，unknown 不等于 0。
- 原生计数可读但类别未确认时显示 **Unclassified Input**，不强行归入 Cache。
- 不完整累计显示 `≥` 下界；字符估算显示 Estimated，不冒充精确计数。
- Cache Hit 只在分类完整、没有未分类输入且确有缓存 telemetry 时显示；否则 `Cache —`。
- Context 上限未获取时保持未知，不按模型名硬编码窗口或百分比。
- 当前 Prompt 构成与已加载会话历史分开显示，loaded history 不等于当前 Prompt / Context Window。

数据来源和计算边界见 [OBSERVABILITY.md](docs/OBSERVABILITY.md)。

## Accounts 与 Quota

Account Vault 使用 **DPAPI(CurrentUser)** 加密；索引只存账号元数据。可导入当前官方凭据，或通过本机 **Official Login Broker** 完成原生 Google 登录，2Ag 不处理 Google 密码。

多账号配额读取覆盖 Gemini 与 Claude / GPT 模型池；真实数据正常显示，未知显示 `—`，本地缓存明确标注缓存。Quota 与宿主登录恢复是两个数据面。

**selected account、credential applied、host identity verified 分别报告。** 凭据 owner 匹配和写入回读只能证明 credential applied。没有可靠宿主内部身份来源时保留“宿主身份未验证”，不把选择邮箱当作登录真值。

已有 StoredCredential 归属匹配且有 refresh material 时，短期 access / ID token 过期交给宿主恢复；仅凭过期不要求重新登录。Broker 新登录验收仍保持严格。

## Runtime Control

| 模式 | 使用的客户端 | 注入 |
|---|---|---|
| **Official** | 用户本机的官方安装 | 不注入增强 UI |
| **Enhanced** | 从本机官方安装建立的 2Ag 冻结副本 | G-Hub、内联 ReAct、Context / Token 等增强 |

**configured mode** 是下一次启动配置；**effective mode** 是当前运行事实。选择 Official / Enhanced 不会自动启动、停止或重启宿主；明确的生命周期动作才应用配置。

进程分为 `owned`（2Ag 启动）、`adopted`（用户明确授权接管）和 `external`（仅发现）。2Ag 默认不停止 external；发现 PID 或 Electron single-instance 转交不会自动获得 ownership。需要接管外部实例时必须由用户明确确认。

生命周期 API 返回实际执行结果；凭据应用成功、宿主进程就绪与宿主内部身份已验证各有独立语义。

## G-Hub 与 Manager

**G-Hub** 是工作时的入口：原生会话内 ReAct、Activity Inspector、Context、Request Trace、Token 与 Quota；也保留 Prompt、Capsule 和本地 Extension 工作流。

**Manager** 是管理时的入口：Accounts、Antigravity Sessions、Environment、Doctor、Runtime 和 Configuration。主 Sessions 仅统计 Antigravity，预览、导出、删除与 usage 沿同一真实 store / source 操作。

## How it works

```text
Antigravity
├─ React / Runtime state
├─ Language Server updates
├─ conversation SQLite
└─ CDP
       ↓
      2Ag
       ├─ Activity / ReAct
       ├─ Request Trace
       ├─ Context / Token
       └─ G-Hub / Manager
```

CDP 连接已有受管理宿主并进行 runtime injection，SQLite 只读提供持久化历史。**No app.asar patch · No official binary modification**。增强宿主副本只在用户本机建立，不随 2Ag 分发；官方安装更新后是否同步由用户决定。

## Quick start

1. 安装官方 Google Antigravity。
2. 从 [latest Release](https://github.com/Arukasaeled/Anti-Antigravity/releases/latest) 下载安装包或便携 ZIP。
3. 安装，或解压完整便携目录并保留 `assets/`、`themes/`、`plugins/`。
4. 启动 `2ag.exe`，选择模式；需要启动宿主时点击“启动宿主”。

需要 **Windows 10/11 x64 + WebView2**。首次使用 Enhanced 会从本机官方安装复制宿主，所需空间与时间取决于安装版本。

## Privacy / Security

本地优先，无 2Ag 作者遥测服务器；Vault 使用 DPAPI，不处理 Google 密码；回环 Control API 校验进程级 control token 并拒绝不可信 Origin；外部进程默认受 ownership 保护。代理不安装 CA、不做 TLS MITM。

Activity / Request 详情属于本地用户内容，分享前应检查命令、路径与工具输出。完整边界见 [SECURITY.md](docs/SECURITY.md)。

## Known limits

- 当前支持 Windows x64，需要用户自备官方 Antigravity。
- 原生字段、React 结构与 Activity descriptor 受宿主版本影响；缺失数值保持 Unavailable，未知工具保持 Tool，不猜值。
- 复盘依赖原生会话数据仍存在；未加载或无法解析的步骤、截断结果会明确标记，不声称完整还原不可得数据。
- Token / Cache / Context 上限可能未知；原生未分类输入保持可见。
- 当前没有可靠的宿主内部登录身份来源；系统凭据为共享记录，profile 隔离不等于凭据隔离。
- 冻结宿主不会自动跟随官方更新；部分运行时开关仍受宿主版本限制。

## Docs / Build

[Observability](docs/OBSERVABILITY.md) · [Architecture](docs/ARCHITECTURE.md) · [Security](docs/SECURITY.md) · [Configuration](docs/CONFIGURATION.md) · [CLI](docs/CLI.md) · [Build](docs/BUILD.md) · [Development](docs/DEVELOPMENT.md)

开发构建与正式 pack 见 [BUILD.md](docs/BUILD.md)；本地扩展契约见 [plugins/README.md](plugins/README.md)，主题定义见 [themes/README.md](themes/README.md)。旧版实现与迁移细节保留在文档、安装脚本和 Release history，不占用首页。

## Credits / Trademark

- Google Antigravity：被增强的宿主；Chromium DevTools Protocol、[go-webview2](https://github.com/jchv/go-webview2)：注入与窗口基础。
- [Cockpit Tools](https://github.com/jlcodes99/cockpit-tools)（CC-BY-NC-SA-4.0）：配额协议行为参考，2Ag 未复制其代码。
- [Token Monitor](https://github.com/Javis603/token-monitor) / [Tokscale](https://github.com/Javis603/tokscale)：Antigravity 原生 usage、去重与聚合设计参考。
- Material Design / Gemini：现有界面的视觉参考；其它社区设计参考见 [OBSERVABILITY.md](docs/OBSERVABILITY.md)。

2Ag 自有代码与文档采用 [Apache License 2.0](LICENSE)。不包含或分发 Google Antigravity runtime。Google、Antigravity、Gemini、Claude、ChatGPT 等名称与标识归各自权利人所有；2Ag 与 Google、Anthropic、OpenAI 没有官方关联或背书。
