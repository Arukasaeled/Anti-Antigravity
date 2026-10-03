# 2Ag 演进研究：从账号增强走向任务连续性

研究日期：2026-10-02。这是产品建议，不代表以下功能已经实现，也没有启动新功能开发。

## 后续讨论修正

用户随后指出：跨账号、跨会话工作在现有 Agent 产品中已能完成，重新理解压缩上下文仍不可避免；故障恢复并非其常见痛点。配额自动路由主要服务多账号与反代人群，不适合作为吸引单账号用户的核心。原生 diff 与 Agent 管理功能也容易重叠。

因此，下文保留为前一轮研究记录，不作为已确定的产品定位或开发路线。下一步应从单账号用户的日常交互、视觉体验和具体外部增强动作中筛选真实需求；任何候选都应先与当前原生功能对照，并通过日常使用验证价值。账号管理和恢复继续维护，优先级按实际使用问题决定。

## 判断

2Ag 当前最有价值的基础是：真实账号切换、DPAPI Vault、可恢复的登录事务、实时四桶配额、官方与冻结宿主隔离，以及运行能力探测。用户本日补充确认 G-Hub 中切换账号真实可用；验证范围见 [V012-READINESS.md](V012-READINESS.md)。

“远超 Codex++”应以任务能否持续完成、失败能否恢复、账号切换能否保留工作上下文来衡量。当前不能宣称整体超越：Codex++ 在供应商、协议、会话、扩展生态方面已经相当完整。

## 已核验的参照能力

读取 GitHub 官方 API、README、EXTENSIONS.md 和代表性源代码；未执行外部仓库脚本，也未将其中指令当作工作指令。外部文件临时下载在系统临时目录。研究时 main 最新提交为 `5bb4f636c0f3be92afacd6ab088b71d968d08d6e`，以下固定链接便于复核。

| 维度 | Codex++ 已有证据 | 2Ag 当前基础与边界 |
|---|---|---|
| 供应商与路由 | 官方、官方混入 API、纯 API、聚合供应商；故障转移及多种轮转。[README](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/README.md#供应商模式) | 官方账号 Vault、导入、登录 Broker 与切换；现有 HTTP 转发代理不能据此称为模型协议网关。[代理说明](../internal/netproxy/README.md) |
| 协议与上下文 | Responses / Chat Completions 转换、每模型窗口和压缩配置；代理还有压缩请求处理。[源码](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/crates/codex-plus-core/src/protocol_proxy.rs) | 尚无已验证的 Antigravity 原生模型请求替换链路；不能承诺任意第三方模型直接替换内置模型。 |
| 会话与恢复 | 会话扫描、删除、导出、用量、供应商元数据同步；索引修复有原文证据核验、备份和并发锁。[修复文档](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/docs/session-index-repair.md) | 本地会话扫描、删除和 Markdown 导出；Broker 的凭据恢复已验证，不等于运行中任务的自动恢复。[会话源码](../internal/supervisor/session_scanner.go) |
| 技能和生态 | 供应商级 MCP / Skill / Plugin 选择、脚本与主题；已有版本化 UI 扩展 API、清理契约和错误隔离。[扩展指南](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/EXTENSIONS.md) | MCP 配置存储和侧车插件契约已存在；侧车启动目前只在 `2ag run` 链路，Manager 不启动侧车。[架构](ARCHITECTURE.md) |
| 工程工作流 | Upstream worktree、项目移动、Zed Remote 与微信连接。[README](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/README.md#当前功能) | 应优先整合 Antigravity 原生工作流，不将重做 worktree 或远程入口当作主要差异。 |
| 可靠性与诊断 | 结构化日志、持久化远程恢复记录、Watcher 与健康检查。[恢复源码](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/crates/codex-plus-core/src/remote_control_recovery.rs) | 跨进程凭据锁、启动恢复、官方 CDP 目标拒绝、逐项能力探针；可继续发展为面向具体任务的诊断和恢复。[能力探测](../internal/supervisor/capability_guardian.go) |

Codex++ 的扩展指南明确说明脚本在页面主世界执行，路由白名单用于防误用，不能作为恶意脚本隔离；后端调用超时也不取消服务端任务。可借鉴其成熟契约，同时为 2Ag 选择真正隔离的扩展宿主。[限制与调用语义](https://github.com/BigPizzaV3/CodexPlusPlus/blob/5bb4f636c0f3be92afacd6ab088b71d968d08d6e/EXTENSIONS.md#限制)

## 六个建议方向

### 1. 任务连续性：切账号后仍知道接下来做什么

建立任务包，保存目标、已做决定、当前项目和分支、未提交变更摘要、关键产物、待办及验收条件。切账号前导出任务包，切换成功后提供交接预览；崩溃后能定位到最后一次可靠检查点。

第一版的闭环是“切号前保存任务现场 → 确认新身份 → 恢复项目与续作提示”。这是基于摘要和产物的交接，不是无损迁移模型内部状态；只需本机任务包与手动继续入口，避免猜测宿主内部状态。以后在接口被验证后再接入原生暂停、继续和取消。验收应区分“身份恢复”“项目恢复”“任务继续”：不能用凭据成功回写冒充任务已续跑。

### 2. 从配额展示走向有依据的账号建议

沿用现有实时四桶读数，补充采集时间、数据来源、是否过期、账号健康和可用模型，解释“为什么建议换到这个账号”。先做低配额提醒、候选账号比较和用户确认切换，再评估安全边界上的自动切换。

不要把缓存或未知值计为可用配额。当前认证是共享的 Windows 凭据；独立 profile 并不能证明多账号可并行运行。因此第一阶段应串行切换，运行中工具调用结束前不换号，不承诺无缝并发或绕过服务配额。[账号隔离说明](ARCHITECTURE.md#账号隔离)

### 3. 项目级上下文与能力配置

把技能、MCP、项目规范、模型偏好及任务模板组织成可预览的项目配置；切项目时展示变更差异，一次启用，冲突可回滚。持续沉淀经过人确认的项目决策，并附来源、适用范围和更新时间。

价值是减少反复解释项目和误加载工具。不要只增加一个技能商店，也不要把所有历史对话自动变成长期记忆；从少量项目配置与来源明确的决策卡开始。

### 4. 有隔离、有契约的扩展平台

先统一 Manager / `run` 的侧车生命周期，再提供版本化 SDK、事件订阅、取消机制和失败隔离。UI 扩展运行在独立来源的受限 iframe 或独立进程中；文件、网络、宿主控制由后端按授权代理，避免给扩展原始凭据或通用 CDP 通道。

先用三个自研扩展验证契约：任务包、项目配置、诊断助手。确认安装、禁用、升级、清理与故障不会影响宿主，再发展社区生态。真正的隔离需要验证，不能仅靠 UI 白名单或 Shadow DOM 宣称实现。

### 5. 可解释诊断与可回退的宿主升级

扩展现有能力探测，把一次操作串成“网络 → 登录 → 凭据 → 配额 → 宿主 → 注入 → 原生窗口”的证据链；错误显示具体失败阶段和适用修复。生成脱敏诊断包，保留操作 ID 和恢复结果，原生截图检查用于发现 DOM 正常但窗口白屏的情况。

冻结宿主同步可采用“新副本验证 → 关键能力验收 → 切换 → 失败回退”，保留上一可用版本。最小化、长时间后台、睡眠恢复也是验收场景：Codex++ 最新提交就修复了隐藏窗口心跳被节流后误判失效并反复注入的问题。[提交](https://github.com/BigPizzaV3/CodexPlusPlus/commit/5bb4f636c0f3be92afacd6ab088b71d968d08d6e)

### 6. 多执行引擎的交接与验收

远期将 Antigravity 原生 Agent、独立 SDK Agent、外部 CLI 等接入同一任务包和验收面板；适合研究的引擎负责研究，适合改代码的引擎负责隔离分支上的实现，结果统一展示变更、测试和产物。

先做两个引擎的显式交接，再做受控并行。不得假设某个 SDK 自动继承桌面账号或桌面配额，也不能把 Codex++ 的协议代理直接套在 Antigravity 上。每个执行器分别验证认证、费用、工具、流式响应、停止和恢复。

## 原生能力约束与推进顺序

Antigravity 已提供原生 worktree、Remote Control、版本控制，以及含 Skill、MCP、Hook、Subagent 的插件。2Ag 应围绕这些能力解决跨账号、跨项目和跨执行器的连续性，避免重建一套平行控制台。[原生功能](https://www.antigravity.google/docs/features)、[插件](https://www.antigravity.google/docs/plugins)

官方 Python Agent SDK 使用独立 `GEMINI_API_KEY`，不能据此宣称使用桌面订阅配额；SDK 持久化与生命周期 Hook 可作为独立执行器的接口方向，仍需针对具体版本验证。[SDK 概览](https://www.antigravity.google/docs/sdk/overview)、[生命周期](https://antigravity.google/docs/sdk/lifecycle)

项目配置也应兼容原生 Skill：官方文档已说明 Workflow 向 Skill 迁移，不宜以旧 Workflow 格式建立新的长期契约。[迁移说明](https://www.antigravity.google/docs/ide/workflows/)

建议次序：v0.1.2 在获得明确发布授权后完成发布；下一阶段做 **任务包 + 配额建议 + 诊断证据链**，随后做 **项目配置 + 可验证交付**；接口稳定后再做 **扩展 SDK 与生态**，最后探索 **多执行引擎**。这些是建议顺序，不是已授权的开发排期或发布承诺。

第一阶段的指标应是：账号切换成功率、故障后恢复率、人工重新解释上下文的时间、误切账号次数和未知配额占比。功能数量和皮肤数量不足以证明超越。
