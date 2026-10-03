# 账号生命周期与 Live Trace

本轮基于已有 Interaction Layer 与 Windows 原生登录 Broker 增量修改，保留 COMPOSE / LENS / CAPSULE / Extensions。没有更改原版安装文件，也没有关闭用户运行中的 Manager。

## 四项真实问题

| 问题 | 直接证据与根因 | 修复 |
| --- | --- | --- |
| G-Hub 切号超时 | 实际按钮的第一次 POST 很快返回 HTTP 500，前端又向未运行的另一个端口重复 POST。原错误被连接错误覆盖，最终显示超时。 | GET 探测 API 来源后，变更请求只发送一次；保留 HTTP 错误。切号使用异步任务及可回看的完成回执。 |
| 被报“缺少 access_token / id_token” | 两份实际 DPAPI 归档均含 access、refresh、可解析 ID token 和身份；其 access / ID 已过期。旧切号复用了新登录的 ID 过期判定，并错误转述成缺字段。没有观察到 token 丢失。 | 完整归档可用于启动原生刷新；最终成功必须完整凭据、新 access、原生 GetAuthStatus 有效及 GetUserStatus 邮箱一致。缺字段、错归属仍拒绝。 |
| “当前地区不支持” | 两次真实启动目标归档后，原生 LanguageServer/GetAuthStatus 返回 failureDetails=ineligible，uiMessage 明确包含 not currently available in your location；原版前端按此结果显示资格拒绝。 | 显示原生错误并回滚，分别报告目标验证与原账号恢复。没有绕过服务端资格判断。底层具体地区规则不能由这条响应进一步确定。 |
| 原版与增强宿主双实例 | Electron 的 setAsDefaultProtocolClient 只写可执行路径，而增强宿主使用独立 --user-data-dir。URI 启动默认 profile，单实例锁与增强 profile 不同；注册项还可能指向旧目录。 | 增强启动就绪后，把 HKCU URI command 绑定到实际可执行文件与同一 profile。实测系统 URI 回调保留同一个宿主根 PID，零原版根实例。 |

邮箱与 token 原值未写入本文、测试 fixture 或发布物。凭据检查只输出存在性、过期时间及其他元数据。

## 统一切号链路

G-Hub 和 Manager 都调用 `POST /api/v1/host/switch-and-restart`，新版 UI 使用 `{email,async:true}`。后端返回 202 后，UI 读取 `GET /api/v1/host/switch-status`；旧同步请求仍兼容。

切号过程为：获取跨进程凭据锁 → 检查运行形态和目标归档 → 归档当前凭据 → 停止宿主 → 再归档最后刷新的凭据 → 保存 DPAPI 恢复记录 → 写目标归档 → 按原运行形态启动 → 读取原生登录与实际身份 → 保存原生刷新后的凭据 → 清除恢复记录。

失败则停止目标宿主、恢复原凭据、按原形态重启并核验原生身份。`verified` 与 `rollback_verified` 分开。当前已是目标邮箱也须核验；成功时保留现有进程及最新凭据。宿主不存在时，同邮箱仍要实际启动验证。

Manager 每分钟检查已有保险库账号的原生刷新结果；只保存完整、已验证的登录，跳过登出与 OAuth 中间态。保存错误记录在日志中。保险库写入、删除与旧格式迁移在进程内串行，临时文件采用独立名称、Sync 与原子替换。

启动与接管不会从仅有旧主控标记、邮箱或不完整登录的中间态自动找回保险库账号。显式用户选择账号仍走完整归档检查。独立宿主启动／停止／重启与形态重启也使用账号操作锁，避免在 Broker 或切号期间打断事务；排队前检查并在执行时再次检查，阻断时返回冲突或记录真实失败。

普通切号不需要官方登录窗口。添加新账号仍借助本机官方 Antigravity 的 Google 登录：先停止增强宿主，登录后关闭官方窗口，再恢复先前运行形态。Broker 要同时确认完整新登录、当前 access 与原生身份。原来未登录时，取消／失败会恢复未登录；仅成功首次登录保留新账号。恢复记录也覆盖原本未登录的状态。

进程中断后的启动恢复首先恢复凭据并回读字节，这是“凭据恢复”，不能据此声称服务端登录已有效。正常事务恢复还会重启原宿主并核验原生登录。增强启动若回调绑定失败，会停止刚启动的宿主并返回清理结果。

## Live Trace

G-Hub 新增 **TRACE / Live Trace**，命令面板可直接打开。基于报告中确认的 `window.Wj.lsClient`，使用原生 `streamAgentStateUpdates`，订阅 PROD_UI 事件；更早记录使用 `getCascadeTrajectorySteps`，文件元数据来自 `getTrajectoryFileDiffs`。不使用 DOM 文案推测隐藏执行状态。

提供中英文、原生工作／空闲状态、步骤状态与时间、工具名称、读文件路径与行范围、编辑说明与增删行数、可显示命令、错误、重试计数、可用任务状态、公开回复、checkpoint 公开摘要。搜索与分类过滤针对当前保留窗口。文件变更显示路径、首次／末次触碰步骤及 artifact 标记，不显示或保留文件全文。

每个窗口最多保留 600 步，默认渲染 180 条，“显示更多”可看完整窗口；“更早步骤”逐窗读取，回看期间不让新步骤覆盖当前窗口，“回到实时”恢复最新事件。已选事件可复制或送入 Capsule，带会话、步骤与时间来源。关闭 TRACE 取消其订阅；没有其他扩展订阅时取消原生流并清空缓存，不常驻后台记录服务。

规范化对象只提取白名单字段，不包含 thinking、rawThinking、generatorMetadata、文件正文或完整任意工具参数。公开文本中的常见 JSON token、Bearer / Basic、URL userinfo 会遮罩；出现敏感旗标／赋值的命令保守隐藏整条内容。此功能不是通用秘密识别器。发送或导出 Capsule 前，用户仍可检查和编辑正文。

Interaction API 升为 **1.3**，保留七类注册及 dispose，新增 `api.host.trace.observe(callback)` 和 `snapshot()`。开发最小例子见 [插件说明](../plugins/README.md)。

## 实机与回归证据

2026-10-03，实际增强宿主 Antigravity 2.19.1：

- 实测旧 API 对目标账号返回缺 token 报错，同时只读检查证明字段完整但过期。
- 捕获实际 G-Hub 点击的重复 POST 与错误覆盖。
- 原生资格拒绝复现两次，均恢复原账号；新同账号分支成功且未重启。
- 系统 URI 回调前后同一增强根进程，原版根实例数为零，Manager 进程未关闭。
- 原生历史入口列出 18 个会话；实际目标会话 1623 步，实时窗口保留 600 步。
- 读取 153 条真实文件变更元数据；更早窗口含 viewFile / codeAction / runCommand 等真实类型。
- 命令分类、选中送 Capsule、关闭订阅均通过真实按钮路径检查；关闭后缓存为零。
- Supervisor / API / Patcher 相关包测试通过；新登录／归档判定、signed_out 中断恢复、发送领取所有权与 Live Trace 脱敏／取消订阅有回归覆盖。

发布物由 `scripts/pack.ps1 -OutputRoot dist/v020-live-trace` 生成，包含新 Manager 后端与内嵌 UI。本轮没有安装覆盖正在运行的旧 Manager；热更新 G-Hub 的脚本不会升级旧 Manager 的后端。

## 真实限制

目标账号的原生地区／资格拒绝仍存在，2Ag 只能诊断与恢复。此次没有重新走一轮需要人工交互的 Google 登录，因此新 Broker 的完整真人授权流程未重验。也没有满足真实低配额条件的任务，不能声称端到端自动接力已实测。

Live Trace 依赖宿主提供的原生 RPC，目前只在 2.19.1 实测。拿不到的数据显示不可用，不生成推理过程；不提供隐藏 CoT、后台永久录制、Git diff 界面或跨设备同步。当前账号与配额真实性和原生服务资格是不同事项。窗口数量及统计只覆盖保留范围，文件列表最多 400 条。
