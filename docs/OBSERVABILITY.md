# Context、Runtime 与本地数据浏览

本轮沿用 2Ag 的 CDP runtime injection、G-Hub 和内嵌 Manager。没有修改官方 resources，也没有执行任何宿主关闭、重启、接管或页面刷新操作。

## Context 的数据契约

只读侦查在现有运行实例中发现了：

- `conversation-view` 的 React fiber 中存在语义属性 `trajectorySlice`。
- `stepsInSlice[].metadata.modelUsage` 提供真实 `inputTokens`、`outputTokens`、`cacheReadTokens`、`cacheWriteTokens`。
- 当前实例的 generator metadata 没有提供可用的 chat-model prompt metadata 或 context 上限。
- 前端 bundle / protobuf 描述中存在 `ChatModelMetadata`、`chatStartMetadata.contextWindowMetadata.maxContextTokens`、`estimatedTokensUsed`、`messagePrompts.numTokens` 和 `toolCallOutputTokens`。字段存在于 schema 不代表当前会话已经提供读数。
- Network resource entries 包含 LanguageServerService RPC。没有拦截 fetch/WebSocket、改变网络请求或调用会修改 Agent 状态的 RPC。

读取器只返回计数、模型、类别、步骤标签和文件引用。它不返回 prompt、工具正文、认证对象或完整 RPC payload。React 属性通过语义键定位，不依赖压缩后的组件函数名。

| 路线 | 条件 | UI |
| --- | --- | --- |
| Native | 有原生完整 prompt token，或者没有缓存 token 的原生 input token | `Native · 最近请求` |
| Estimated | input 与 cache 重建，原生 `estimatedTokensUsed`，或实际 prompt 文本估算 | `≈ Estimated` |
| Composition Only | 只有组成/DOM，无法可靠得到总量 | 不显示虚构用量 |

缓存 token 的口径尚未确认。有缓存时，`input + cache-read + cache-write` 仅作重建估算，可能存在重复计数；原始字段单独展示。每次模型调用的 input 不累计为上下文总量。output 单独列出，不添加到最近请求的 input。

上限只来自原生 `maxContextTokens`。缺失时显示“上限未知”，隐藏使用百分比与剩余量。没有依据模型名字硬编码上限。

组成分为当前 prompt、已加载历史和可见页面。历史中的消息、工具结果和 checkpoint 可能已被压缩或截断，因此历史估算不能当成当前上下文。缺少正文/计数的项标为未知；有部分可计数内容的类别明确显示“部分可计数”。文件引用也不证明文件仍在当前 prompt 内。

Manager 与 G-Hub 共用显示模块。明细按占用降序排列；时间线只记录采集后发生的真实请求变化，切换会话/模型时开始新序列。只有当前 prompt 的可比组成才进行粗略增长归因；其它变化标为归因未知。当前时间线保留最近 120 点，不落盘。

## CDP 与 DOM Runtime

`CDPRuntimeManager` 复用已有持久连接和脚本注册逻辑，维护每个 target 的连接、注入特性、计数、上下文与时间线。

- 支持符合宿主语义的 page / webview targets。
- 枚举成功时清理已消失 targets 及其自有连接。
- 保活失败注销旧连接，由持续巡检重建并重新注册当前补丁。
- 页面重建后检查真实 Shadow root 与 runtime 消费者；需要恢复时重新读取当前配置和源码。
- 端口不可达时重新发现已存在的端口，并交给该端口唯一的维护循环；不会通过启动宿主获得端口。
- 初次监听超时仍保留恢复通道。
- JavaScript exception 不再被当成注入成功。
- Official 模式下 Context 的按需读取使用短连接，仅执行只读 Runtime 查询，不安装脚本或开启域。

DOM Runtime 将 mutation 中变化的节点收集到 Set，在 requestAnimationFrame 中处理去重后的局部候选。Translation、Context / Activity 和 Diagnostics 共用观察器，并允许注册额外 processor。编辑器、输入区、原生控件和 2Ag 自身 DOM 不进入通用处理。

body 上的 portal 移除只触发状态读取与挂载检查，不转换成全页扫描。原来的每两秒全量 `tick()` 已移除；保留的低频 Context 采样读取框架状态。初始化和明确的配置更新仍复用现有守卫。

## Quota、Doctor 与 Environment

额度历史从现有授权缓存采集，使用缓存本身的 `updatedAt`，重复读取不会生成新样本。历史按账号、额度池、窗口隔离，保存到 2Ag 自己的 `quota-history.json`，保留七天、每个序列最多 2048 点。消费显示已观察到的下降百分点与采样覆盖时间。重置周期变化分段处理，不预测还能使用多久。原生 reset timestamp 支持动态倒计时。

Doctor 复用已有宿主与身份探针，只检查进程、CDP targets、注入、身份元数据、Vault 索引/文件、授权缓存、会话 DOM 与存储路径。未探测远端 Quota API 时如实显示未知；Vault 可读索引不等于已解密验证凭据。

Doctor 导出是固定结构的状态与计数，不包含账号身份、路径、页面标题、模型 prompt、工具正文、cookie、OAuth secret 或 credential payload。Runtime 的 target 详情仅在 Developer 区域展示。

Environment 集中了 Antigravity storage、brain、conversations、profiles、workspace storage、credential slot、已安装 Language Server 与可见存档格式。CDP、Sessions 和 Skills 复用这层发现。版本是已发现安装的文件版本，不冒充正在运行实例的版本。

## Sessions 与 Skills 的首批范围

Sessions 在原有页面加入 provider abstraction，首批支持 Antigravity 和 Codex：

- 列表读取目录项、文件时间与原生元数据索引，不读取所有消息内容。缺少标题/项目时明确使用 ID 和未分类项目，不制造会话或步数。
- Antigravity 能索引 brain、protobuf、SQLite 等存档；消息预览/导出目前支持已有 transcript.jsonl，protobuf / SQLite 没有实现解码。
- Codex 使用 `CODEX_HOME` 或标准用户目录、原生 `session_index.jsonl` 与 session 文件目录元数据；点击后读取 response items。
- 预览有消息/文件大小上限并明确标记截断；Codex 导出拒绝输出不完整记录。
- 支持 provider / 项目过滤、搜索、按需预览及单会话 Markdown 导出。批量导出明确命名为“索引”，仅包含元数据。
- 本轮不暴露删除按钮；现有删除接口增加 ID、路径与运行中宿主保护，不删除 workspaceStorage。未实现跨 provider 的可靠删除、打开项目或更多 Agent。

Skills Hub 只读扫描原生 Global 与已发现 Workspace 的 Skill 目录。支持常见 frontmatter、SKILL.md 查看、名称/描述搜索、scope 过滤以及 resources / examples / scripts 结构识别。不执行脚本，不擅自改写原生启停状态。本轮未实现安装、Git 拉取、Marketplace 或其它 Agent 的 Skill 管理。

## 代码入口

- `internal/patcher/context_reader.js`：纯只读 Context / Activity 读取器。
- `internal/patcher/context_view.js`：共用 UI 与数据可信度提示。
- `internal/patcher/injected_hub.js`：G-Hub 接入、增量 DOM Runtime。
- `internal/patcher/bridge.go`：embed 与磁盘 companion module 加载。
- `internal/supervisor/cdp_runtime_manager.go`、`cdp_session.go`、`cdp_injector.go`：target registry、采样、恢复与真实注入回执。
- `internal/supervisor/quota_history.go`、`account_scanner.go`：真实额度历史与 reset timestamp。
- `internal/supervisor/doctor.go`、`environment.go`：轻量只读诊断与发现边界。
- `internal/supervisor/session_scanner.go`、`session_providers.go`、`skills_hub.go`：元数据列表与延迟内容读取。
- `internal/api/observability.go`、`server.go`：本机 API。
- `web/dist/index.html`、`observability.js`：Manager 导航、页面与交互。
- `scripts/pack.ps1`：发布时携带 Context companion modules；本轮没有执行脚本。

## 本轮验证边界

只做了静态阅读、源码格式整理，以及对当前一个真实 target 的必要只读侦查/计数采集。没有运行 build、test、lint、typecheck、smoke、E2E 或截图矩阵，没有构造测试账号、额度或存档。

新 Manager/API/G-Hub 尚未运行验证；多 target、断线恢复、reload/reinject、持久额度历史和 Skills/Sessions 交互均未进行真实运行验证。当前 2Ag 进程不会因源码修改自动拥有新 API。等待用户以后构建并加载新版 2Ag；若需要重启宿主观察，等待用户手动重启后验证。禁止为验证中断正在执行的任务。

## 后续值得继续的工作

1. 确认不同 provider 的缓存 token 口径，并寻找原生当前 prompt 的完整上限/组成。
2. 用户任务结束后，观察新版多 target 恢复、页面重载及共用 Context UI。
3. 接入 Antigravity protobuf / SQLite 的只读会话元数据与消息解码。
4. 确认原生 Skill 启停/安装机制后再增加管理操作。

## 查阅的社区设计

- [antigravity-sync CDPHandler](https://github.com/mrd9999/antigravity-sync/blob/main/src/services/CDPHandler.ts)：连接登记、存活检查与恢复思路。
- [anti-power scan](https://github.com/daoif/anti-power/blob/master/patcher/patches/manager-panel/scan.js)：局部节点收集、语义候选与按帧去重。没有采用它的官方文件 patch 路线。
- [antigravity-storage-manager quotaUsageTracker](https://github.com/unchase/antigravity-storage-manager/blob/master/src/quota/quotaUsageTracker.ts)：按真实历史分隔账号/模型与观察额度变化。没有引入 Gateway、Bot 或复杂预测。
- [antigravity-skills](https://github.com/rominirani/antigravity-skills)：Global / Workspace 安装结构以及 resources、examples、scripts 层次。
