# 隐私与安全

## 一句话

**所有数据都留在你自己的机器上。2Ag 没有任何作者服务器、统计或上报。**

## 网络行为

2Ag 自己出网只去两个地方：

1. `127.0.0.1` —— 回环控制面与转发代理；
2. 宿主本来就要访问的 Google 账号 / API 端点（`accounts.google.com`、`oauth2.googleapis.com`、`www.googleapis.com`）。

没有第三个目的地。

## 账号凭据

### 不持有 OAuth 客户端

2Ag **不是** Google OAuth 客户端：仓库里没有、发布包里也没有任何 Google OAuth Client 凭据（`client_id` / `client_secret`）。

添加账号走 **Official Antigravity Login Broker**：由本机官方 Antigravity 自己完成原生 Google 登录，2Ag 只捕获登录结果。

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

停宿主 → 写入目标凭据 → 按原形态重启 → **回读校验实际登录身份**。校验不一致就自动回滚到原账号并如实报错 —— 绝不用请求里的邮箱冒充结果。

## 代理

本机回环转发代理，由宿主通过 `--proxy-server=http://127.0.0.1:<port>` 使用：

- 按 `privacy.blocked_hosts` 拦截遥测域名，`privacy.block_beacons` 拦截信标；
- 按 `network.endpoint_overrides` 把明文 HTTP 请求改道到指定上游；
- `network.rule_targets` + `global_rules` 可往**显式指定**的 JSON 字段前插规则。

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

发现安全或隐私问题请开 issue，或按仓库主页公布的联系方式私下告知。
