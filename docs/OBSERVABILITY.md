# Activity、Request Trace、Context 与本地数据

2Ag 通过 CDP runtime injection、G-Hub 与内嵌 Manager 展示 Antigravity 自身的可观察数据，不修改官方 resources。当前主会话产品只管理 Antigravity。

## Activity / ReAct / Request Trace

Activity 的真实链路是：

```text
Runtime provider 当前切片 ───────────────┐
conversation SQLite steps.step_payload ──┤
          ↓ 相同 allowlist 投影          │
          ActivityEntry ←────────────────┘
          ↓ 按原生标识合并
          Phase Narrative → Detailed ReAct → Raw Inspector
          ↓ Model Response / responseId
          已有 Request / Token Inspector 与 Session Usage
```

SQLite 历史使用只读连接与分页 `/api/v1/sessions/activity`；明确的 session / store 通过现有来源解析，不接受任意文件路径。错误详情可从 `steps.error_details` 补齐；Token 复用已有 GenerationUsage Reader，不重新累计。当前行为 allowlist 对应 Antigravity 2.19.1 的原生 descriptor，不实施完整协议兼容矩阵。

Runtime / SQLite 使用相同的行为字段投影，保留 step status、工具参数与结果、文件修改、命令执行、错误/重试与时间戳。不访问 private planner response、prompt 正文或隐藏 CoT。已识别的敏感键值和 token 文本脱敏；嵌套字段超出投影界限会向上报告 partial，不能把截断数据声称为完整历史。

ActivityEntry 以 session、trajectory、stepIndex 为基本身份，辅以原生 toolCallId / responseId；不按 title 或单独 executionId 去重。Runtime 优先覆盖当前状态，SQLite 补缺失结果与 usage。当前真实 Runtime 只合并到同一 session、同一已解析 store；其它历史会话和备份来源只读自己的 SQLite。

首次持续分页读取全部可解析历史；后续回读尾部重叠与最早未结束步骤，避免长命令离开 Runtime 切片后永远停留在 RUNNING。没有时间戳或计数保持未知；duration 明确来自原生时间戳差，不能证明模型隐藏推理时间。

三级展示共享同一份 ActivityEntry：Phase 按实际 execution / 同类操作与可观察目标分组，用真实文件、命令和计数形成摘要；Detailed 展开原始行为顺序与连续操作组；Raw Inspector 显示结果、错误、ID、时间戳和共享 Token breakdown。原始 command、code、path、filename、output / error 不翻译，通过 textContent / escaping 插入。

Activity Inspector 的 All / Errors / Edits / Commands / Reads / Model 过滤仅选择渲染条目，不删除或修改原始 ActivityEntry。Phase 统计使用已有时间戳、toolCallId、模型响应与错误；缺少原生计数时省略，输入不完整时保留 ≥。命令提供原生 combined output，不将其伪称为已分离的 stdout / stderr；Edit 只展示可读的修改统计、replacement chunks 与结果，不生成缺失的 unified diff。

内联 ReAct 挂在当前原生 turn 的 Activity 区域，用原生 turn steps 与 isRunning 关联任务，不根据 “Working...” 文案猜测。默认仅展开当前 Phase，Detailed 按需展开；真实任务完成时整个 ReAct 折叠，但不删除任何历史。重新展开或 DOM 重建后，仍可恢复可读取的持久化记录。Raw 命令输出默认折叠，未知工具保持 Tool。当前未提供的原生数据明确报告不可用，不冒充完整复盘。

用户手动查看历史时暂停滚动跟随；仅在底部或明确回到底部时跟随新行为。Observer、MessageChannel、timer、scroll listener 与自有节点在 dispose 时清理；官方显示属性恢复，热注入不会要求刷新宿主。

语言默认跟随 2Ag 解析后的 Auto / English / 中文设置，也可单独选中文 / English；语言和折叠偏好保存在现有 `~/.2ag/workspace/extension-state.json` 的 `__react_presentation_v3` 项，localStorage 仅作恢复缓存。保存失败可见，不把内存变化当作持久化成功。宿主汉化开关不参与此语言选择。

模型响应点击打开同一 session / trajectory / step 的已有 Inspector，并使用 responseId 补入已读原生 usage。所选请求与最近请求分开显示，可返回最新请求；Session 累计仍来自原生会话库。

## Context / Request / Session 的数据契约

三个数字分开处理：

- Request 是最近一次请求的输入规模、output 和缓存读数。优先使用 React 原生 usage；缺失时使用持久化库中有真实时间戳的最近响应，并明确显示来源。
- Context Window 只消费原生 contextWindowMetadata 的占用/估算和 maxContextTokens；请求输入量不再充当 Context。没有上限时显示“上限暂未获取”，不显示斜杠、百分比或模型名推测上限。
- Session 是原生会话库的 generation 累计，不累加 React 当前切片。三个 UI 共用 ContextView 的格式、hover 和 breakdown。

Native 表示读取原生计数；Estimated 表示原生 estimatedTokensUsed 或单个 prompt 文本的字符估算；Unavailable 表示字段未暴露。缺失字段保持 null，不补零。

Total = New Input + Unclassified Input + Cache Read + Cache Write + Output；Reasoning 是 Output 的信息性子集。Cache Hit = Cache Read / (New Input + Cache Read + Cache Write)，仅在分母字段完整、未分类输入为零且确实存在缓存 telemetry 时显示。完整 total 未知时，≥ 表示已读取原生计数的下界，不把它冒充精确累计。

当前 Prompt 构成来自 messagePrompts / promptSections / systemPrompt；loaded history 独立、默认折叠，明确注明不等同于当前 Prompt / Context Window。历史字符估算不再回填 Context 或 Request 总量。Activity 不出现在 Context UI。

G-Hub Compact 仅显示最近请求、当前 Context、本会话和“查看详情”。Full Inspector 分为 Request Telemetry、Session Usage、Current Context、Prompt、Loaded History、最近请求变化和 Files / Details。采样不到两点不显示时间线。

输入框 HUD 挂在 Composer 的 control row，沿可见编辑器寻找模型选择与麦克风 / 发送控件的共同容器，不依赖 Local footer 或屏幕底部位置。中央和底部 Composer 共用定位；原生控件之间空间不足时收缩为数字。ResizeObserver 与现有 DOM observer 恢复定位，不改模型或发送控件。一级显示 Request 与 Session，保留 ≥；实际 Context 独立放在 hover 和 Inspector。title hover 与 aria-label 提供同源 breakdown，点击打开 G-Hub 完整详情。未找到明确控制行时不猜位置。

## Antigravity Session Token Reader

G-Hub 账号卡、配额池和 Home 配额共用 `/api/v1/accounts` 中 selected account 的同一份数据。宿主内部身份验证只作为中性 provenance 展示，不作为 quota 可见性条件。未知为 —，缓存单独标注；不回退到另一个 active account 的配额。

Session Token 加载与消息预览独立。Token 数值可读时显示“Token 已载入”；消息是否可读只在 Preview 入口说明，不混入 Token metadata。

Environment 自动发现已支持且实际存在的 conversation 目录：

- ~/.gemini/antigravity/conversations
- ~/.gemini/antigravity-ide/conversations
- ~/.gemini/antigravity-backup/conversations

Windows x64 使用系统 winsqlite3.dll，以 SQLITE_OPEN_READONLY 打开 DB，保留实时 WAL 可见性，不创建备份、导出 cache 或迁移数据库。其它平台暂时返回 Unavailable。

只读取当前版本的 gen_metadata(idx, data)、steps(idx, step_type, metadata) 与 trajectory_metadata_blob(id, data)。Token Reader 不读取 step_payload（Activity Reader 单独投影其中的行为字段）；gen_metadata BLOB 只提取 usage / model，跳过 prompt 正文，不实施全面 protobuf 兼容。

原生 generation 的 chatModel.usage 中，New Input 使用字段 #2；字段 #1 的数值可读取但具体类别未确认，单列为 Unclassified Input，不归入 Cache。Output 使用 #3，总 output 包含 #9 文本和 #10 reasoning；Cache Read 是 #5，responseId 是 #11。Model 来自 chatModel #19，次选 #21 原生显示名。步骤 metadata 提供响应 ID / generation 索引对应的真实时间戳；没有可靠时间戳就保留未知，不使用 DB 修改时间冒充请求时间。

Total 与输入处理都包含 New Input、Unclassified Input、Cache Read、Cache Write；Total 再加 Output，Reasoning 不重复计入。每一个计入累计下界的 response 都同步贡献 observed_breakdown，缺字段或去重不完整时各类别显示已读取下界，保证 Total 有可见归属。当前库的 Cache Write 仍 unknown，不补 0；Runtime 缓存字段默认 0 也不能证明提供了缓存 telemetry。Cache Hit 仅在输入与缓存分类完整且没有未分类输入时显示，否则为 —。Runtime 最近请求只按相同 sessionId / responseId 补入持久化字段，不按时间或模型猜匹配。

优先按 responseId 去重，重复响应只能贡献一次累计，后续来源只能补缺失字段。缺失 responseId 的原始记录仅取第一个可读权威库，并标记 dedup confidence lower；无 ID 记录不计入累计下界；该库存在无 ID 记录时不合并备份来源，避免与备份的已识别响应重复累计。requestCount 在读取或去重不完整时保持未知，另提供 observedRequests。解析损坏或超出读取界限不会生成精确累计。

会话页只为可见行按需读取 usage，最多三个并发请求；后端使用五秒内存缓存。不会在索引加载时读取全部 generation 或消息正文。

## Runtime Mode

SET_RUNTIME_MODE 仅修改和保存下次启动配置，不更新当前宿主注入策略，不发布启动/停止/重启命令，也不触发外观首次推送。配置模式与实际模式分别显示。已有宿主按实际模式初始化注入策略；明确的“启动宿主”或“重启”动作才应用新配置。本轮没有执行这些生命周期动作。

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

额度历史接入现有实时配额探测与授权缓存 fallback。实时请求成功时记录真实采样时间；缓存使用自身的 `updatedAt`，重复读取不会生成新样本。历史按账号、额度池、窗口隔离，保存到 2Ag 自己的 `quota-history.json`，保留七天、每个序列最多 2048 点。消费显示已观察到的下降百分点与采样覆盖时间。重置周期变化分段处理，不预测还能使用多久。原生 reset timestamp 支持动态倒计时。

Doctor 复用已有宿主与身份探针，只检查进程、CDP targets、注入、身份元数据、Vault 索引/文件、授权缓存、会话 DOM 与存储路径。Doctor 不主动请求远端 Quota API，该项显示未知；账号模块保留已有实时探测流程。Vault 可读索引不等于已解密验证凭据。

Doctor 导出是固定结构的状态与计数，不包含账号身份、路径、页面标题、模型 prompt、工具正文、cookie、OAuth secret 或 credential payload。Runtime 的 target 详情仅在 Developer 区域展示。

Environment 集中了 Antigravity storage、brain、conversations、profiles、workspace storage、credential slot、已安装 Language Server 与可见存档格式。CDP、Sessions 和 Skills 复用这层发现。版本是已发现安装的文件版本，不冒充正在运行实例的版本。

## Sessions 边界

主 /api/v1/sessions、列表总数、项目数、今日活跃、搜索、预览和导出仅支持 Antigravity。Codex Provider 保留为未启用代码，主页面没有“全部 Agent”或 Provider 选择器。

列表索引三个原生 conversation 根及现有 brain 元数据。DB 的 workspace / session 创建时间来自 trajectory_metadata_blob，最后请求时间优先读取最后一条 generation step 的真实时间。备份文件复制时间不冒充真实请求活动。消息预览/Markdown 导出仍只支持现有 transcript.jsonl；消息 Preview 不解码 SQLite 对话正文；Activity 单独投影行为字段。

Skills Hub 只读扫描原生 Global 与已发现 Workspace 的 Skill 目录。支持常见 frontmatter、SKILL.md 查看、名称/描述搜索、scope 过滤以及 resources / examples / scripts 结构识别。不执行脚本，不擅自改写原生启停状态。本轮未实现安装、Git 拉取、Marketplace 或其它 Agent 的 Skill 管理。

## 代码入口

- `internal/patcher/activity_inspector.js`：共享 ActivityEntry、Runtime / SQLite 合并、G-Hub 与内联三级 ReAct。
- `internal/activity/schema.go`、`schema.json`：行为 descriptor allowlist 与安全投影。
- `internal/supervisor/activity_reader.go`、`internal/api/activity.go`：只读历史分页与原生 usage 关联。
- `internal/patcher/context_reader.js`：纯只读 Context / Runtime provider 读取器。
- `internal/patcher/context_view.js`：共用 UI 与数据可信度提示。
- `internal/patcher/injected_hub.js`：G-Hub 接入、增量 DOM Runtime。
- `internal/patcher/bridge.go`：embed 与磁盘 companion module 加载。
- `internal/supervisor/cdp_runtime_manager.go`、`cdp_session.go`、`cdp_injector.go`：target registry、采样、恢复与真实注入回执。
- `internal/supervisor/quota_history.go`、`account_scanner.go`：真实额度历史与 reset timestamp。
- `internal/supervisor/doctor.go`、`environment.go`：轻量只读诊断与发现边界。
- `internal/supervisor/antigravity_usage.go`、`antigravity_sqlite_windows.go`：原生 usage、responseId 去重与会话聚合。
- `internal/supervisor/session_scanner.go`、`session_providers.go`：Antigravity 元数据列表与延迟内容读取。
- `internal/api/observability.go`、`server.go`：本机 API。
- `web/dist/index.html`、`observability.js`：Manager 导航、页面与交互。
- `scripts/pack.ps1`：发布时携带 Activity / Context companion modules 与双语资源。

## 历史探查记录（v0.2.2 之前）

此次只读取源码、当前真实 SQLite 的少量 schema / generation，以及当前宿主的 DOM / React 属性。没有运行 build、test、lint、typecheck、smoke、E2E、CI 或自动验证脚本，没有模拟状态、内容哈希或存档指纹比较。源码的格式整理不等于编译验证。

探查时没有关闭、启动、重启、刷新、重载或向当前宿主注入修改。当时可见 target 是新会话输入页，没有可读的当前 usage / Context 上限；bundle 中字段存在不代表实际读数可得。当时新的 SQLite C API 路径、API、HUD、hover/click、Compact/Full Inspector 和模式交互尚未运行验证。后续已按用户要求编译当前源码，并由用户进行真实使用验证。v0.2.2 收录这些源码修改，并按同一提交编译安装包与便携 ZIP；发布过程未运行自动测试，也未启动或重启宿主。历史 v0.2.1 发布包不包含这些修改。

## 查阅的社区设计

- [antigravity-sync CDPHandler](https://github.com/mrd9999/antigravity-sync/blob/main/src/services/CDPHandler.ts)：连接登记、存活检查与恢复思路。
- [anti-power scan](https://github.com/daoif/anti-power/blob/master/patcher/patches/manager-panel/scan.js)：局部节点收集、语义候选与按帧去重。没有采用它的官方文件 patch 路线。
- [antigravity-storage-manager quotaUsageTracker](https://github.com/unchase/antigravity-storage-manager/blob/master/src/quota/quotaUsageTracker.ts)：按真实历史分隔账号/模型与观察额度变化。没有引入 Gateway、Bot 或复杂预测。
- [antigravity-skills](https://github.com/rominirani/antigravity-skills)：Global / Workspace 安装结构以及 resources、examples、scripts 层次。

- [Token Monitor Antigravity 数据说明](https://github.com/Javis603/token-monitor/blob/main/docs/providers/antigravity.md)。
- [Tokscale Antigravity SQLite reader](https://github.com/Javis603/tokscale/blob/main/crates/tokscale-core/src/sessions/antigravity_cli.rs)：只参考原生 conversation discovery、usage、responseId 与聚合；没有研究其它 Provider 路线。
