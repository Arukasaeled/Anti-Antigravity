# RC3：Manager 白屏与账号切换实机记录

## 本轮范围

基线为 RC2 提交 `3c29b98`。RC3 修复 Manager 隐藏启动白屏、原生实例绑定、目标登录续期和凭据覆盖；保留原生登录核验与失败回滚。

**账号切换成功验收尚未完成。** 本机 Account B 仍返回原生资格拒绝。本轮没有把写入凭据、Google 身份验证或第三方工具的成功提示算作 Agent 可用。

## 白屏：已复现并修复

旧版通过隐藏窗口方式启动时，后台 API 返回完整页面，WebView2 中的 DOM、标题、状态卡片均已加载，但原生截图的内容区全白。普通可见启动显示正常。Windows `STARTUPINFO` 的 `SW_HIDE` 使 WebView2 在隐藏的父窗口内初始化；之后只显示 Manager 框架不足以恢复浏览器表面。单独显示子窗口的尝试也未恢复，未保留该实现。

最终修复在创建 WebView2、取得 Manager 单实例锁、启动账号服务之前识别隐藏启动，保留参数、环境和工作目录，以默认可见方式启动 Manager 并退出隐藏父进程。子进程没有继承 `SW_HIDE`，不会递归重启。

实机以相同隐藏启动方式复测：父进程退出、仅有一个 Manager，WebView2 初始可见，原生截图显示完整控制台。没有删除浏览器 profile 或用户数据。

## 账号路径修复

- CDP 端口必须由托管根进程拥有。两个实例同时存在时，不再借用官方实例的调试端口并误判增强实例身份。
- 普通启动保留当前原生刷新后的同账号凭据；事务启动使用刚恢复的确切凭据，不再从旧 Vault 覆盖一次。
- 识别顶层及 `token.id_token`，拒绝身份／audience 冲突；过期归档可以用于续期，但缺字段或错误归属仍不能直接恢复。
- 停止宿主前，使用本机原生运行时的登录配置刷新目标 grant，再通过 Google userinfo 验证目标邮箱。配置只在内存中使用，不随包分发。
- 续期结果先保存到 DPAPI 加密的 pending candidate；之后身份查询失败也不丢失可能轮换的 grant。重试验证后才归档，候选不代表已登录，也不写入活动系统凭据。
- 身份 GET 最多重试两次，仍受原来的总期限限制；未知结果的 refresh POST 不盲目重放。
- 网络诊断仅保留固定的步骤、原因和 HTTP 状态标签，不输出响应、token 或原始请求参数。预检失败不停止当前宿主。
- Login Broker 可指定预期邮箱；错误账号不会被提前归档。同账号重新登录后保留新 grant。原生明确拒绝目标账号时立即恢复，不再对中间态凭据等待十分钟。
- 原生网络瞬断保留核验期限；只有身份匹配的资格、验证或条款拒绝才立即终止。始终需要原生有效状态、实际邮箱、当前 access 和 Host 一致才提交。
- 删除 Vault 账号按归档邮箱清理 pending，包括按账号 ID 删除的入口。

## Cockpit Tools 对照

读取 [Cockpit Tools](https://github.com/jlcodes99/cockpit-tools) 的实现，参考版本 `2d0f13f9e2b1c7a2b30bab08b3805829808aa85f`。它为 Antigravity 2.x 写入 `auth_method=consumer` 和精简 `token`（access、refresh、type、expiry），不依赖旧 ID token；随后启动官方宿主。2Ag 独立比较该协议与实际启动行为，没有复制第三方实现源码。

2026-10-03 本机观测：

| 场景 | 真实结果 |
| --- | --- |
| 完整 Vault → 刷新目标 → Google 身份验证 → 增强 Host | Google 身份通过；目标原生 `ineligible`；原账号恢复并核验 |
| 精简 consumer 凭据 → 官方 2.19.1 | 目标原生 `ineligible`；原账号恢复并核验 |
| Cockpit 自己保存的新 access → 精简凭据 → 官方 | Google 身份通过；目标原生 `ineligible`；原账号恢复并核验 |
| 直接操作本机 Cockpit 的目标账号切号按钮 | Cockpit 显示切换成功；确实写凭据并启动官方 2.19.1；目标原生状态连续 90 秒为 `ineligible`；随后恢复并核验原账号 |
| 精简凭据 → 明确采用当前系统代理启动官方 | 目标原生 `ineligible`；原账号恢复并核验 |

用户确认曾通过 Cockpit 使用同一个目标账号完成 Agent 工作；本轮未复现这个成功结果。以上观测只能说明本次调用的原生拒绝，**不能证明永久账号限制，也不能解释用户此前成功与本次失败的全部差异**。没有绕过原生资格、把拒绝改为成功或伪造 Agent 验收。

原账号的原生有效状态与恢复结果已确认；成功切到目标后跨重启、目标真实 Agent 工作仍缺少成功证据。

## 检查与边界

两轴代码审查未发现新增可行动 P1/P2，Spec 明确保留目标切号未成功的限制。必要测试覆盖 grant 轮换保留、身份 GET 重试而不重复 refresh、归属拒绝、候选加密、CDP 进程绑定、代理与绕过规则、Broker 拒绝和错误文案。

实机探针、截图、账号信息和日志只保留在忽略的本地构建目录，未提交，也不进入安装包。RC3 不包含 Google 运行时、凭据或调研克隆。没有新增 Live Trace 数据采集范围，不涉及隐藏 CoT。

最终检查：

- `go test ./cmd/2ag ./internal/supervisor ./internal/api ./internal/patcher ./internal/netproxy -count=1` 通过；Manager 包无测试文件，白屏通过原生截图实测。
- `node --check internal/patcher/account_health.js` 与 `git diff --check` 通过。
- 打包泄漏检查通过：21 个发布文件、186 个自有源码文件；随后 Inno Setup 编译成功。
- RC3 安装程序升级本机已有目录，退出码为 0；已安装二进制与构建产物 SHA-256 一致。
- 安装后 Manager 页面正常，原生 AuthStatus 有效且邮箱一致；同账号切换入口返回 `verified=true`、`already_active=true`，保留同一个宿主 PID。
- 最终仅一个 Manager、一个增强 Host 根实例，无额外官方根实例或临时 Login Broker。

本地安装包：`dist/v020-account-switch-rc3/Anti-Antigravity-Setup-x64.exe`（9,237,202 字节）。SHA-256：`242B691A0CA340298C256B4A768CF4CA4DF0DE0A4AC3D3F2FE6F71EC5B54156E`。安装包未自动发布到 GitHub Release。
