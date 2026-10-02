# Anti-Antigravity (2Ag) v0.1.2

> 未单独发布；以下改动已并入 v0.2.0 源码。当前交互修复与检查范围见 [v0.2.0 说明](RELEASE-NOTES-v0.2.0.md)。

**correctness / honesty patch。** Windows x64 · 单个 `2ag.exe` · 安装包 `Anti-Antigravity-Setup-x64.exe`

这一版不是功能大版本，目标只有一个：让**代码、产品行为和公开叙事一致**。

## 已知问题在本版的处理

### 1. 配额：不再从 Cockpit Tools 缓存冒充实时读数

v0.1.1 的账号矩阵配额读取的是本机 Cockpit Tools 缓存
（`~/.antigravity_cockpit/cache/quota_api_v1_desktop/authorized`），不是 2Ag 自己发起的查询。

v0.1.2：

- 2Ag 自己按账号凭据向 Antigravity 配额接口做 **live 探测**（`loadCodeAssist` + `retrieveUserQuotaSummary`）；
- Gemini / Claude+GPT 的 5h 与 weekly 四桶按 `quota_summary` 解析；
- Cockpit 缓存**只作 fallback**，绝不冒充 live：回落时池上带 `stale=true`，
  Manager 与 G-Hub 的配额出处行据此显示「⚠ 非实时读数：实时探测未成功，这份是本机缓存快照」，
  账号卡片与 G-Hub 配额池标签也标「非实时」。
  （池的 `source` 字段仍是缓存文件自身的出处 `authorized · desktop`；
  「这是缓存」由 `stale` 表达，不由 `source` 表达。）
- 查询失败显示 `--` / `unavailable`，不伪造百分比；
- 多账号各读各的凭据，邮箱不匹配就拒绝探测，不串池；
- 多账号**并发**探测（上限 4；单账号 18s、整批 22s 超时），单账号失败或超时不影响其他账号。

#### 凭据选取按「新鲜度」而不是固定顺序

同一个账号可能有两份 access_token：系统凭据管理器里那份（宿主持续刷新），
以及 2Ag 保险库里的快照（切换/登录那一刻写入）。v0.1.2 优先用**未过期**的那份 ——
否则会出现「宿主正用该账号正常工作、2Ag 却报 token 过期」的假失败。

#### 关于 token 续期（如实说明）

2Ag **不内置、也不需要 Google OAuth 客户端密钥**。access_token 由本机官方
Antigravity 自己续期，2Ag 只读取最新凭据。因此：

- 当前正在使用的账号：能拿到 live 四桶真实读数；
- 长期未使用的保险库账号：access_token 会过期，且 2Ag 无法代替它刷新，
  界面如实显示「已过期」，**不伪造数字**，也不声称「完全独立实时查询」。
  用官方 Antigravity 登录一次该账号即可恢复。

### 2. 添加账号不再依赖 Cockpit Tools（最高优先级）

v0.1.1 的账号清单、凭据读取与账号切换**全部**建立在本机 Cockpit Tools 数据目录上：

- `ScanLocalAccounts()` 只读 `~/.antigravity_cockpit/accounts.json`；
- 切换身份时 `ApplyAntigravityCredential()` 从 `~/.antigravity_cockpit/accounts/<id>.json` 取凭据，
  取不到就报「cockpit 账号库里没有 … 的可用凭据」；
- "接入新账号" 把这些数据**写回**第三方目录。

结果：**没装过 Cockpit Tools 的用户装完 2Ag 也加不了账号**，账号矩阵永远是空的。

v0.1.2 起 2Ag 用**自己的账号库**，Cockpit 降级为可选的、只读的历史来源：

- 新增 `~/.2ag/accounts.json`（`internal/supervisor/account_registry.go`）—— 只存邮箱/名称/时间戳等
  **元数据，不含任何 token**；
- 账号清单来源按优先级合并：**2Ag 保险库（DPAPI）→ 2Ag 登记表 → Cockpit（可选、只读）**，
  按邮箱去重，先出现的胜；
- 账号切换改为 `credentialPayloadForAccount()`：**先取 2Ag 保险库里的凭据**，
  Cockpit 仅作次选；两者都没有时报的是可操作的提示
  「请先在「账号矩阵」里用该账号登录一次（登录后凭据会存进 2Ag 自己的保险库）」，
  文案里不再出现 Cockpit；
- 凭据归属识别（`identifyCredentialOwner`）也先查保险库 —— 没装过 Cockpit 的用户
  凭据只可能在保险库里，顺序反了会导致归档被拒；
- 2Ag **永不创建、永不修改** Cockpit 目录（旧库只读兼容）；
- 「移除账号」同时清保险库与登记表，不存在的记录视为成功（JSON 导入的无凭据账号也能移除）。v0.2.0 修正为任一来源失败时明确报告未完全移除。

老用户升级后账号 ID 规则不变（`acc-<sha256 前 12 位>`），主控标记也不会丢
（自有记录为空时才回落到 Cockpit 的 `current_account_id`）。

### 3. 增强形态的网络：借道用户自己的上游代理

`2ag run` 会给宿主加 `--proxy-server=http://127.0.0.1:<port>` 指向 2Ag 的回环转发代理。
Chromium 一旦拿到这个参数就**不再读环境变量与系统代理设置** —— 而 2Ag 的转发代理原先
只做裸直连（`transport.Proxy = nil`，CONNECT 用 `net.Dialer` 直连目标），
于是把用户的上游代理整个从链路上摘掉了。

在**必须经代理才能访问外网**的环境里，宿主的请求会被直连出去而全部失败，
表现就是官方登录页报 `oauth2.googleapis.com` 连不上。这是 2Ag 造成的真实缺陷。

v0.1.2 把用户的上游代理**接回来**（`internal/netproxy/upstream.go`）——「插入一层」而不是「替换一层」：

- 解析顺序：`HTTPS_PROXY` → `HTTP_PROXY` → `ALL_PROXY` → Windows 系统代理
  （`HKCU\…\Internet Settings` 的 `ProxyEnable`/`ProxyServer`/`ProxyOverride`，**只读**）；
- `NO_PROXY`（含系统 `ProxyOverride`）、环回地址、指回 2Ag 自身的地址一律直连；
- 只支持 `http` 代理（`socks5://` 等忽略并记日志）；带口令时日志只出 `scheme://user:***@host`；
- HTTP 走 `transport.Proxy`，HTTPS `CONNECT` 由 2Ag 向上游再发一次 CONNECT 建立隧道；
  隐私拦截与端点改写仍留在 2Ag 这一层（仍然**不装 CA 证书、不做 MITM、不解密 TLS**）。

### 4. 登录失败不再只能等到超时

官方登录页会把 `proxyconnect tcp: dial tcp 127.0.0.1:1: …` 这类原始错误直接显示在页面上，
而 2Ag 完全看不见 —— 用户只能干等到 10 分钟超时，拿不到「是网络/代理不通」这个结论。

v0.1.2 增加**只读**的登录页诊断：等待期间（启动 15 秒后、每 5 秒一次）用 CDP 读一次
官方页面的 `innerText`，识别官方页面自己报的错（代理连接失败 / 连接被拒 / DNS 解析失败 /
网络断开 / 隧道失败 / 超时 / 官方通用句），在 2Ag 界面上给出**指向网络与代理**的提示。

诊断只发一次 `Runtime.evaluate` 取文本，不注入、不建常驻会话；任何一步失败都保持原提示，
**绝不因为读不到页面就谎报故障**。措辞纪律：只陈述官方页面报了什么、把排查方向指向
所选登录模式与用户的网络出口。AUTO 沿用原环境，DIRECT 强制子进程直连，PROXY 使用
显式解析的代理；只作用于此次宿主，不永久改写系统或父进程代理设置。失败原因如实显示。

#### Broker 恢复与导入

- 清除原凭据前同步保存 DPAPI 恢复记录；Manager/run 启动先恢复，逐字节回读成功后才清记录。
- 中断时已完成的新登录先归档再恢复原账号；恢复失败保留记录并阻止正常初始化。
- 账号操作使用跨进程锁；官方宿主的 CDP 目标被注入入口与巡检拒绝。
- Manager 新增「导入当前官方账号」，写入保险库及自有账号表后刷新列表。
- 状态与 API 分别报告凭据缺失、尚未登录、网络/代理、EOF/reset、token 端点、未写入、超时、身份不符及恢复失败。

### 5. `block_beacons` 不再是虚假能力

`PrivacyConfig.block_beacons` 从未被 `netproxy` 消费，且代理不解密 TLS，无法按路径识别信标。

v0.1.2 从配置模型删除该字段（旧配置里的它会被忽略），文档只承诺
`privacy.blocked_hosts` 的 **host/domain 级阻断**，不再声称「信标拦截」。

### 6. Job Object / Manager 生命周期如实区分

- `2ag run`：宿主、代理、侧车挂同一 Windows Job Object（`KILL_ON_JOB_CLOSE`），退出连带清掉；
- Manager（`2ag manager` / 无参数）：走 `LaunchEnhancedHost` + `exec.Command` + PID/`taskkill`，
  **关掉 Manager 不会带走宿主**（保持原设计，不为迁就旧文案改行为）。

`docs/ARCHITECTURE.md` 与 README 已按两条链分别写清。

### 7. 文档与公开面

- README 仍保持产品首页风格，只修事实性句子（配额、生命周期、便携版）；
- `docs/SECURITY.md` 不再写「按主页公布的联系方式私下告知」——主页没有联系方式，只留 GitHub Issues；
- v0.1.1 Release Notes 顶部加了 Known Issue，**不替换** v0.1.1 已发布二进制；
- Release 目前只提供安装包，README 明确没有独立 portable `2ag.exe` 资产。

## 发布产物

与 v0.1.1 相同：`Anti-Antigravity-Setup-x64.exe` + `Anti-Antigravity-Setup-x64.exe.sha256`。
