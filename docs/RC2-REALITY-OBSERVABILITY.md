# RC2：账号实机验证与 Live Trace 2.0

日期：2026-10-03。版本：0.2.0-rc.2，Windows 文件版本 0.2.0.2。宿主实测版本：Antigravity 2.19.1。本文中的 A、B 代表本机两份实际保险库归档，不包含邮箱、凭据或用户目录。

## Git checkpoint

上一轮 22 个 tracked 修改、24 个 untracked 文件按源码、文档、测试、原始调查、构建产物、临时文件、私有状态分类，见 [checkpoint 审计](GIT-CHECKPOINT-AUDIT.md)。因账号、API、注入和 Trace 注册存在耦合，正式实现合并为一个 checkpoint。

提交顺序：

- `35c31fe`：上一轮账号生命周期及 Trace checkpoint。
- `21f706a`：共用事务、账号健康和 Trace 2.0。
- `79e6d1c`：停止宿主前写恢复记录，归档选择及文件打开体验修正。
- 后续 RC2 收尾提交：文件摘要加载状态、健康文案、版本和此验证记录。

原始调查报告、诊断脚本、性能回放、安装日志和安装包留在被忽略的本机目录。没有提交保险库、账号快照、token、运行配置或上游运行时。仅显式暂存正式文件。

## Reality Test

成功标准是完整凭据、当前 access、原生 GetAuthStatus 有效、原生实际邮箱与目标一致以及 Host 正常；不是 HTTP 200 或 UI 文案。

| 场景 | 实际结果 |
| --- | --- |
| A → B | 真正执行目标写入与启动，但原生返回 `ineligible` / location restriction；目标未成功，A 经原生核验恢复 |
| B → A | 未取得可工作的 B，因此不能宣称成功反向切换 |
| Manager 发起 B 切换 | 10:40 实际按钮路径：目标拒绝，rollback_verified=true，A 有效 |
| G-Hub 发起 B 切换 | 11:15 实际按钮路径：经过写入、启动、核验、回滚、再核验；两端消费同一回执 |
| A → A，Host 已运行 | 原生核验成功，保留根 PID，不恢复旧归档、不重启 |
| A 上真实 Agent 工作 | 新建原生会话，只读 README；真实工具读取和公开答复，4 个步骤，无错误 |
| Manager 与 Host 完全退出再启动 | 旧 Manager / Host 均退出；安装新版后仍是 A，完整归档、相同 profile、原生状态和邮箱一致 |
| 跨冷启动刷新 | 启动前 access 到期时间 11:43:55，冷启动验证发生在 12:05；原生刷新后到期时间 13:05:35，健康记录刷新时间 12:05:48 |
| 进程中断恢复边界 | 恢复 checkpoint、同归属更新、停止／写入／回读失败保留恢复资料等测试通过；未故意破坏真实凭据 |

跨重启刷新用了已过期 access 的实际凭据。没有修改到期时间、伪造 token 或依赖上一 Manager/Host 的内存。不过，“成功切到 B、在 B 工作、重启后仍为 B”未完成，不能用 A 的冷启动结果替代。

B 的拒绝来自原生 LanguageServer/GetAuthStatus，不是 2Ag 自己的地区规则。底层地区政策、账号资格以及 Google 真人 OAuth 本轮没有进一步验证。完整 Host 启动失败／原生超时的实机故障注入也没有逐一执行。

## Account Transaction / Health

G-Hub 与 Manager 都发起同一个异步切号 API，并轮询同一个真实后端事务。状态包括 Idle、BackingUp、StoppingHost、SavingCurrentCredential、WritingTargetCredential、StartingHost、VerifyingNativeAuth、Committed、RollingBack、RollbackVerifying、Failed。

DPAPI 恢复记录现在在停止 Host 前落盘，失败会保留运行中的 Host。Host 完成退出后的最后凭据刷新，只能更新同一归属的已有恢复记录。提交或原生核验成功的回滚才清理记录。回读凭据成功与原生登录有效分开报告。

Account Health 在两端复用同一个组件，展示实际当前账号、原生状态、最近验证／观察到刷新时间、保险库是否完整、refresh credential 是否已保存、Host PID、当前 profile、事务及上次结果。检查另一个归档时展示的是带时间的历史原生观察，不冒称实时状态。全局事务和上次切换明确标注目标账号。

健康元数据只保存在 `~/.2ag/workspace/account-health.json`；完成回执在 `account-switch.json`。首次观察不会猜测刷新时间；“已保存 refresh credential”也不等于服务器保证它永远有效。组件只在展开时轮询，销毁时清理计时器。没有返回 access、refresh、ID token、Cookie 或 credential JSON。

普通错误使用短的中英文用户消息；底层原因可以展开查看。地区提示只有原生 ineligible 且原始消息明确包含 location 时才简化为地区受限。

## 进程与安装

| 检查点 | Manager PID | 增强根 PID | 官方根实例 | Broker |
| --- | --- | --- | --- | --- |
| RC2 首次安装后 | 20320 | 34884 | 0 | 0 |
| 完整退出 Host 后 | 20320 | 无 | 0 | 0 |
| 再次安装、冷启动 | 45512 | 54064 | 0 | 0 |
| 最终包安装、正常启动（无临时 Manager CDP） | 46520 | 50476 | 0 | 0 |

Electron 的 renderer / GPU / utility 子进程属于同一个宿主，不计为多个官方根实例。冷启动前后读取真实 PID 树、profile 和原生登录。安装程序静默升级退出码为 0，保留本机 profile 与 Vault；相同 AppId 支持覆盖升级。

普通切号直接恢复保险库后启动增强 Host，不需要拉起官方登录实例。URI callback 与同一增强 profile 绑定，避免默认 profile 的另一套根进程。根 PID 探针修正了错误选择较小 renderer PID 的问题。

本轮没有启动新真人 OAuth Broker，所以 Broker 新登录后的完整实机退出流程仍未重验；其恢复／清理路径有已有代码与相关测试支持，不能据此伪称真人流程实测成功。

## Live Trace 2.0

真实来源仍是原生 SDK 的 streamAgentStateUpdates、getCascadeTrajectorySteps、getTrajectoryFileDiffs 和 getAllCascadeTrajectories。无后台录制、AI 分类或合成推理。

统一事件保留原来的 `kind`，新增 `eventKind`、timestamp、summary、file、tool、phase、phaseSource、可取得的 duration。事件类别覆盖文件读取／修改、搜索、工具、命令、错误、checkpoint、公开助手回复、用户和系统事件。未取得的字段省略。

阶段是连续真实事件类型的归组：读取／调查、修改、验证、构建、普通命令、工具、错误、checkpoint、会话。`phaseSource: event-kind` 明确标注来源，不声称知道模型的内部计划。命令中可见的 test/build 信息可用于分类。

- 顶部展示当前事件、已加载步骤／总步骤、命令、错误、文件和时间。
- 当前动作只从非空闲会话的 running / generating / waiting / pending 状态提取；未知就写未取得，空闲写空闲。
- 错误／中断入口直接跳到对应事件；搜索支持关键词、`file:`、`kind:`。
- 窗口化时间线固定行高，详细内容放在单独面板；滚上去后停止跟随，按钮返回实时。
- 最近会话最多 12 个，点击走原生历史入口。
- 真实事件的 created / completed 计算时长；会话与阶段是可见时间跨度，包含暂停和空闲，不是 CPU 耗时。保留窗口外的统计明确为部分范围。

实机历史会话读到 1808 个步骤、194 个可见命令、14 个错误／中断、184 条原生文件元数据。该长会话跨越多次工作，其 643 分钟时间跨度不能当作连续运行耗时。

文件元数据最多 400 条。打开 TRACE 自动取得一次摘要；尚未取得时显示 `—`，避免误报零文件。可手动刷新。184 条中的 32 条有可安全计算的完整前后基线，其余不编造增删行数。实际 diff 预览含真实 `@@` hunks，长度 5244 字符。

Diff 按需计算，保留上下文片段而不是全文件预览；缺失 before 且有 hash 引用不当作新文件。大内容／计算量上限、敏感文件排除、脱敏、6 项缓存限制同时生效。Copy Diff 可复制片段；Open File 已实机打开本机记事本中的 README。Antigravity 2.19.1 忽略普通文件 CLI 参数，所以不借此另起 Antigravity，也不声称它能在宿主内打开编辑器。

## 性能：真实样本回放

实际原生最长会话是 1808 步。5000 / 10000 压力数据来自该会话的脱敏规范化事件在独立 Trace 实例内重复回放；是浏览器实测，不是 5000 / 10000 原生实时会话。

| 回放规模 | 保留事件 | 时间线 DOM 行 | Trace 全部 DOM 节点 | 滚动处理平均 / 最大 |
| --- | --- | --- | --- | --- |
| 1000 | 1000 | 13 | 215 | 4.24 / 8.0 ms |
| 5000 | 5000 | 13 | 244 | 3.96 / 6.9 ms |
| 10000 | 10000 | 13 | 285 | 3.24 / 4.6 ms |

首次加载测量含固定 180ms 等待，依次 812 / 1006 / 1024ms，不能当成纯渲染耗时。另一次回放排除固定等待后为 177 / 744 / 715ms，滚动平均 3.55 / 2.99 / 2.93ms。不同加载时序造成波动，不作为性能承诺。

实际会话展开详情并跳错误时渲染 19 行。12,000 事件边界测试保留最近 10,000。关闭三个回放实例后均保留 0 个事件，AbortSignal 已取消。关闭真实 Trace 后同样缓存为零；清理订阅、路由 timer、diff、最近会话和选中状态。关闭过程中尚未完成的文件摘要不会重新填充缓存。

JS heap 实测：另一次回放在 10,000 规模从约 51.2MiB 到 57.4MiB，关闭并 GC 后约 55.2MiB。相较加载态回落约 2.2MiB，没有完全回到基线。测量包含原生页面、回放脚本／编译对象，不能当成独立 Trace 存活堆的精确计量；没有长期连续 CPU 或泄漏曲线。

一次 Manager working set 47.4MiB；冷启动且展开健康页后 92.7MiB。不同页面、WebView 子进程和系统缓存影响占用，不能推断全为 Trace。Host tree 曾达到约 876MiB，与宿主会话／多个 renderer 有关。此次只确认无无意义的官方根实例，没有声称整套 Electron 内存会回到启动值。

## 导出、Capsule 与隐私

MD / JSON 导出会话摘要、范围标记、文件元数据、阶段和有限重点事件。不会序列化原始 RPC、全文件内容或 credential。选中最多 12 个事件送 Capsule，带摘要、文件、时间、会话和步骤来源；每条正文限制为 2000 字符，不塞整条 Trace。沿用已有 Capsule 可编辑上下文流程。

公开回复、checkpoint 公开摘要、工具事件、命令、文件变更和状态可以显示。thinking、rawThinking、generatorMetadata、隐藏 CoT、内部 reasoning token 从未作为展示字段。凭据参数命令保守隐藏整条；常见 token / Basic / Bearer / URL 认证信息脱敏。敏感文件预览被排除。此规则不能识别所有自定义业务秘密，导出和 Capsule 仍可先由用户检查。

## UX 审计与验证范围

额外修正：首次打开 G-Hub 就读取账号；切号列表只提供实际 Vault 归档，避免扫描配额得到的伪候选；账号健康不会把另一账号的实时验证套用到所选归档；全局事务／回执标邮箱；原生文件参数无效改为真实本机编辑器；文件摘要未加载不显示 0。

`go test ./internal/supervisor ./internal/api ./internal/patcher -count=1` 通过。Node Trace 4 个测试通过，涵盖隐私、12k 有界流、关闭时在途文件 RPC、真实基线 diff、缓存刷新及敏感文件排除。代码复审发现的身份归属、根 PID、回调作用域和 diff 缓存问题已修正。冷启动后另一次真实只读 Agent 任务同样完成 4 步、公开答复、无错误。

构建使用 Go Windows GUI 和 Inno Setup；安装包版本 0.2.0-rc.2。发布检查只扫描正式自有源码和 staging，不打包测试数据、凭据、原始日志或 Google 运行时。安装包路径：`dist/v020-live-trace-rc2/Anti-Antigravity-Setup-x64.exe`。

最终包大小 9,155,175 字节，SHA256：`174DDB8CE5897CB17457F9934F941F89888B8FF687E54405998089C8DF0A5C59`。最后一次升级安装退出码 0；最终界面实测自动显示 184 条文件元数据及带账号归属的健康结果。发布扫描覆盖 21 个 staging 文件及 175 个自有源码文件。

## 仍然有限制

1. B 原生地区资格受限，成功 A→B、B→A 及 B 工作后跨重启矩阵没有完成。
2. 真人 OAuth / 新 Broker 本轮未启动；完整异常启动／核验超时实机注入未逐项验证。
3. 原生 RPC 依赖 Antigravity 版本；目前只有 2.19.1 实机结果。
4. 5000 / 10000 是真实样本回放。没有原生 10k 长会话或长期 CPU／内存 soak 数据。
5. 事件最多 10k、文件最多 400、会话最多 12，部分历史与行数可能不可得；时间跨度含空闲。
6. 文件摘要在会话开始观察时加载一次，之后需要手动刷新；不做后台持续全文件差异抓取。
7. Open File 是本机记事本，非宿主编辑器。未读取隐藏推理，也未尝试暴露它。
