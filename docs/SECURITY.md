# 隐私与安全

## 一句话

**所有数据都留在你自己的机器上。2Ag 没有任何作者服务器、统计或上报。**

## 网络行为

内置控制面与账号 / Quota 功能主要访问：

1. `127.0.0.1` —— 回环控制面与转发代理；
2. 宿主本来就要访问的 Google 账号 / API 端点（`accounts.google.com`、`oauth2.googleapis.com`、`www.googleapis.com`）。

没有作者遥测目的地。用户显式配置的上游代理、endpoint override 或本地扩展可能访问用户指定地址；它们不是作者统计服务。

## 账号凭据

### 不持有 OAuth 客户端

2Ag **不是** Google OAuth 客户端：仓库里没有、发布包里也没有任何 Google OAuth Client 凭据（`client_id` / `client_secret`）。

添加账号走 **Official Antigravity Login Broker**：由本机官方 Antigravity 自己完成原生 Google 登录，2Ag 只捕获登录结果。

清除现有登录凭据前，Broker 将原始凭据用 DPAPI 加密并同步写入 `~/.2ag/broker-recovery.json`。Manager 和 `2ag run` 在正常初始化前先恢复未完成的事务；逐字节回读验证成功后才删除记录。恢复失败会保留记录并停止初始化。账号导入、切换、恢复和 Broker 共用跨进程锁，避免同时改写共享凭据。

「导入当前官方账号」读取同一份系统凭据，校验 credential owner 后写入保险库和自有账号表，不触发重新登录；这不是宿主内部身份验证。

这条路径**不需要**本机装过任何第三方工具。2Ag 用**自己的**账号库（`~/.2ag/`）：DPAPI 保险库存凭据，`~/.2ag/accounts.json` 存不含 token 的账号清单。第三方 Cockpit Tools 的数据目录（若存在）只作为**可选、只读**的历史来源被兼容 —— 2Ag 不创建、不修改它。

### 存储：DPAPI(CurrentUser)

账号凭据以 **DPAPI(CurrentUser)** 加密存放在 `~/.2ag/vault/*.bin`：

- 密文只有**同一台机器的同一个 Windows 用户**能解开 —— 换用户、换机器都解不开；
- `index.json` 只登记邮箱、显示名与时间戳，**不含任何 token**；
- token **不进** `2ag.json`、**不进**日志、**不进** API 响应、**不进**前端。

旧版本留下的明文凭据会在首次启动时自动就地加密迁移。

### 共享凭据（设计上必须披露）

登录身份存在 Windows 凭据管理器的 `gemini:antigravity` 条目（`CRED_PERSIST_LOCAL_MACHINE` ⇒ 机器级、所有 `--user-data-dir` 共享）。

因此：**官方 Antigravity 与 2Ag 沙箱读的是同一条凭据记录**。2Ag 换号时会改写它，并且退出时**不改回** —— 这是系统凭据模型决定的，不是 2Ag 的选择。环境诊断会如实把这一条标为共享。

2Ag 能做到的隔离是 **profile 隔离**：每个账号一个 `~/.2ag/profiles/<email>/` 沙箱，`--user-data-dir` 各自独立。

### 切换是事务化的

先验证账号与 StoredCredential，再检查宿主所有权；必要的凭据改写、启动与回读验证都成功后才返回操作成功。失败按事务恢复原凭据并报告恢复结果。

**credential_applied ≠ host_identity_verified**：回读仅确认系统凭据归属与写入结果。当前没有可靠的宿主内部身份来源，界面保留“宿主身份未验证”，不把 requested / selected account 当作已确认 active account。

StoredCredential 要求 owner 匹配并存在必要的恢复材料；短期 access / ID token 过期进入宿主恢复，不直接要求重新登录。BrokerFreshCredential 对刚完成官方登录的凭据仍检查新鲜度。只有明确 refresh failure / invalid_grant 等证据才要求重新登录。Quota 请求失败不能证明宿主登录失效。

## 本地控制面与进程所有权

Manager 每次启动生成随机 control token；控制 API 校验 `X-2Ag-Control-Token` 或 Bearer；只读 API 也默认需要 token，只有不含控制能力的 ContextView 静态脚本例外。不可信 Origin 直接拒绝，不能依靠浏览器读不到响应来保护副作用。控制面只绑定回环地址，拒绝非回环 Host。token 用于本机控制请求与浏览器来源防护，不作为系统级跨用户权限隔离。

进程分为 `owned`（2Ag 明确启动）、`adopted`（用户明确授权接管）和 `external`（仅发现）。仅发现 PID 或 Electron single-instance 转交不会赋予停止权限。外部实例默认不能被停止；生命周期、账号切换与 Broker 遇到外部实例返回 `external_confirmation_required`，须用户明确确认接管后才能进入管理路径。

模式选择只改变 configured mode；实际 effective mode 由运行进程确认，选择不会自动启动、停止或重启。

Activity 只投影允许的行为字段，不读取私有 planner response / 隐藏 CoT。原始命令与工具结果作为本地用户内容显示，通过 textContent / escaping 插入；已识别的凭据键值和 token 文本脱敏。自由文本仍可能包含用户项目中的敏感内容，分享前应自行检查。

## 代理

添加账号可选 `AUTO`、`DIRECT`、`PROXY`。这些模式只调整本次官方宿主的子进程环境和启动参数，Windows 系统代理与父进程环境保持原样；不控制外部 Google 登录浏览器的网络。官方登录窗口只开放只读 CDP 诊断，不注入 G-Hub。增强宿主的注入入口及持续巡检会拒绝官方宿主的 CDP 目标。

本机回环转发代理，由宿主通过 `--proxy-server=http://127.0.0.1:<port>` 使用：

- 按 `privacy.blocked_hosts` 做 host/domain 级阻断（明文 HTTP 与 CONNECT 目标域名）；
  不实现信标/请求级拦截（不解密 TLS，无法可靠识别 beacon）；
- 按 `network.endpoint_overrides` 把明文 HTTP 请求改道到指定上游；
- `network.rule_targets` + `global_rules` 可往**显式指定**的 JSON 字段前插规则。

### 借道用户自己的上游代理（0.1.2 起）

宿主拿到 `--proxy-server=http://127.0.0.1:<port>` 之后，Chromium 就不再使用环境变量与系统代理设置 —— 代理决策被这个参数完全接管。所以 2Ag 的转发代理必须**替宿主把上游代理接回来**，否则在必须经代理才能出网的环境里，宿主的请求会被 2Ag 直连出去而全部失败（表现为 Google 登录连不上 `oauth2.googleapis.com`）。

因此 2Ag 现在按以下顺序解析一次上游代理，并把它用作自己的出口：

1. `HTTPS_PROXY` / `https_proxy`
2. `HTTP_PROXY` / `http_proxy`
3. `ALL_PROXY` / `all_proxy`
4. Windows 系统代理（`HKCU\...\Internet Settings` 的 `ProxyEnable` / `ProxyServer` / `ProxyOverride`，**只读**）

`NO_PROXY`（及系统 `ProxyOverride`）里的目标、环回地址、以及指回 2Ag 自身的地址一律直连。支持 `http` 和 `https` 上游代理；HTTPS 代理先验证证书并建立到代理的 TLS 连接，再发送 CONNECT。`socks5://` 等会被忽略并记日志。代理地址里带口令时，日志只输出 `scheme://user:***@host`。

这是「插入一层」而不是「替换一层」：隐私拦截、端点改写仍在 2Ag 这一层完成，用户的出口没有被摘掉。

### 不解密 TLS（刻意边界）

**不安装 CA 证书、不做 MITM、不解密 TLS。** HTTPS `CONNECT` 是不透明隧道，因此本项目的代理无法改写 HTTPS 请求体。

这是设计边界，不是待办事项。

## 打包泄漏闸门

`scripts/pack.ps1` 在产出前跑一道**结构性泄漏闸门**：产物里一旦出现

- `C:\Users\<某人>\` 形式的绝对路径
- 构建机用户名
- 真实邮箱
- OAuth 客户端密钥 / 私钥

打包当场 `throw`，不会产出安装包。

闸门的作用域是 **2Ag 自己的全部产物**（源码、`web/dist`、脚本、配置、README/docs、发布包内 2Ag 自有文件、`assets`、构建脚本），并排除 `app/`、官方运行时、`app.asar*`、`node_modules` 等**上游 vendored 内容** —— 上游第三方代码里出现什么由各自作者负责，不构成 2Ag 的泄漏。

> 区分 OWNED 与 VENDORED 是这条闸门的设计前提，不是为了让扫描通过而放宽标准。

## 仓库边界

- **不包含**任何官方 Antigravity 运行时字节；
- **不包含**写死的开发机数据。新装（配置为空）时，需要用户数据的面板显示 `--` / 不可用，而不是某个人的真实数值；
- 壁纸是**用户自己的数据**，2Ag 不预置、不携带任何图片。

## 报告问题

发现安全或隐私问题请开 GitHub issue。仓库主页目前**没有**公布私人邮箱，所以没有私下联系渠道。
