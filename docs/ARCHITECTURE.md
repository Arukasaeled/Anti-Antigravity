# 架构

> 面向想理解 2Ag 内部结构的人。只关心怎么用请回 [README](../README.md)。

## 总览

`2ag.exe` 一个可执行文件同时是引擎和控制台。不带参数启动的是 **2Ag Manager**（WebView2 原生窗口，里外都是本机页面）。

```
2ag.exe  ──┬─ 本机回环控制面 (API + 内嵌前端)
           ├─ 本机回环 HTTP 转发代理      （仅 `2ag run` 启动时）
           ├─ 侧车插件进程               （仅 `2ag run` 启动时）
           └─ 冻结宿主副本（增强形态）── CDP ──> G-Hub / G-Cockpit（注入）
```

### 两条启动链

| 启动方式 | 代理 / 侧车 | 宿主进程归属 | Manager 关闭后 |
|---|---|---|---|
| `2ag run`（Enhanced） | 启动 | 同一 Windows Job Object（`KILL_ON_JOB_CLOSE`） | run 退出时一并清掉 |
| `2ag run`（Official） | 不启动 | 官方启动路径，仍检查 ownership | 不走增强 Job Object |
| `2ag manager` / 无参数 | 不启动 | `exec.Command` 直接拉起，按进程身份与 ownership 管理 | 宿主**继续运行**（设计如此，不改） |

下面分别说明 Manager 与 run；Enhanced run 的 Job Object 语义不适用于 Official 或 Manager。

## 模块

| 目录 | 职责 |
|---|---|
| `cmd/2ag/` | 进程入口、Manager 窗口、子命令 |
| `internal/api/` | 鉴权回环控制面（state / accounts / sessions / Activity / Context 等） |
| `internal/activity/` | 原生行为 descriptor allowlist、安全投影，不读取隐藏 CoT |
| `internal/control/` | 当前进程 control token 与可信 Origin 登记 |
| `internal/config/` | 配置模型与持久化 |
| `internal/core/` | 状态机与事件总线 |
| `internal/netproxy/` | 本机回环转发代理 |
| `internal/patcher/` | CDP 桥与注入载荷（`injected_hub.js`） |
| `internal/supervisor/` | 宿主启动、账号扫描、官方登录 Broker、DPAPI 账号保险库、兼容性与更新守护、壁纸、侧车 |
| `web/` | 内嵌 SPA（`go:embed`），Manager 页面与独立 i18n / Observability / Launch Readiness modules |
| `assets/` | logo / 图标 / 内嵌资源 |
| `themes/` `plugins/` | 主题定义 / 插件契约 |
| `scripts/pack.ps1` | 发布构建 + 泄漏闸门 |
| `installer.iss` | Inno Setup 安装脚本 |

## 注入链路

2Ag 不修改官方 Antigravity 的任何安装文件 —— `app.asar` 原样保留、不重打包、不替换。

注入全部走 **CDP runtime injection**：

1. 2Ag 拉起增强形态的冻结宿主副本，通过 `--remote-debugging-port=<动态端口>` 打开调试通道；
2. 用 `Runtime.evaluate` + `Page.addScriptToEvaluateOnNewDocument` 把 `injected_hub.js` 送进渲染进程；
3. G-Hub 以 **Shadow DOM** 挂载（`twoag-injected-root`）；内联 ReAct 在原生 turn 内挂载自有 Shadow root，Dream-Skin 等另有宿主样式层；
4. DOM 重建时恢复；卸除注入清理自有节点、监听与样式，恢复官方显示。宿主结束后运行时注入自然消失。

官方形态和登录 Broker 只使用原安装客户端，不注入。注入入口和持续巡检不仅检查形态配置，也拒绝官方 profile 的 CDP 目标，防止增强配置误捕获官方窗口。Broker 清凭据前保存 DPAPI 恢复记录，Manager/run 在 API 和宿主初始化前先恢复；恢复成功经逐字节校验后清记录。跨进程锁覆盖整个登录流程与其他凭据操作。

CDP 端口**动态分配**：历史实现硬编码 28472，一旦被占用宿主会静默换随机端口，而 2Ag 仍在戳旧端口 —— 表现为"连不上"但没有报错。现在改为启动前预留空闲端口，并从 `DevToolsActivePort` 回读实际值。

## 可观察数据链

React / Language Server 更新提供当前 Runtime 切片；原生 conversation SQLite 只读提供历史 Activity 与 generation usage。ActivityEntry 合并后由 Phase / Detailed / Raw 三层展示；Request / Session 使用同一 Token 语义。主 Sessions 只统计 Antigravity，preview / export / delete / usage 沿真实 SessionSource 操作。

任务结束只折叠 ReAct，不删除原生历史；DOM 恢复与展示偏好复用现有本地 workspace store。边界与字段来源见 [OBSERVABILITY.md](OBSERVABILITY.md)。

## 语言与首次运行

`config.language` 保存 `auto` / `en-US` / `zh-CN`。配置层使用 Windows 系统 locale 解析 Auto；Manager 获取同一解析信息，G-Hub 初始注入与增量推送都携带解析后的语言和原始 preference。ReAct 独立 override 保存在现有 workspace；宿主 `force_zh_cn` 不参与 UI language 解析。

首次欢迎页与能力总览复用 Bootstrap、Environment、Doctor、Guardian、Context 与 Quota 数据。它们区分 configured / supported / effective；只观察到 SQLite 文件时不会宣称 Activity 已读通。欢迎页只在明确点击启动时执行生命周期操作，单独创建 frozen copy 不启动进程。

## 进程生命周期

Job Object 只在 **Enhanced 的 `2ag run`** 这条链上成立：宿主、代理、侧车进程全部挂在同一条 Windows Job Object 下并设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，`2ag run` 退出时 OS 会连带清掉整棵进程树。

**Manager（`2ag manager` / 无参数）默认不走 Job Object。** 它用 `LaunchEnhancedHost` → `exec.Command` 直接拉起宿主，记录 PID、进程身份与 ownership。停止前验证目标仍是同一个 owned / adopted 进程；external 默认拒绝停止。因此：

- 关掉 Manager 窗口，增强宿主**不会**被带走，会继续在后台跑；
- 想停宿主，要在界面里点「结束进程」，或在 CLI 里用 `2ag run` 对应的托管方式。

不要把两条链混成一句话写。上图的「全部挂在同一条 Job Object」只对 Enhanced 的 `2ag run` 成立。

进程所有权分为 `owned`（2Ag 启动）、`adopted`（用户明确授权接管）与 `external`（只读发现）。Official single-instance 转交不会把外部进程认领为 managed。Broker、切号、恢复和生命周期入口共用所有权检查，外部实例需明确确认。start / stop / restart / takeover / switch 等关键 API 等实际执行结果后返回，不以发布 CommandEvent 当作成功。

## 运行形态

| 形态 | 用哪份宿主 | 行为 |
|---|---|---|
| 官方形态 | 你本机的官方安装 | 零启动参数、零注入、零凭据改写，行为接近"没有安装 2Ag" |
| 增强形态 | 2Ag 自己的冻结宿主副本 | 完整注入 + G-Hub + 皮肤 + 重力加倍 |

configured mode 是下一次启动配置；effective mode 来自当前运行事实。选择 Official / Enhanced 不执行生命周期操作，明确启动、重启或接管才应用 configured mode。

冻结宿主副本落点在 `2ag.exe` 同级的 `app\`（约 570 MB），首次建立时从本机官方安装**物理复制**，对官方目录只读。建立后 2Ag 一直使用副本，官方 updater 更新的是官方安装 —— 两者物理隔离。

> 为什么不直接改官方安装：官方 updater 会在任意时刻覆盖 `app.asar`，任何原地补丁都会在某次后台静默更新后失效甚至损坏安装。冻结副本把"2Ag 的宿主"和"官方要更新的宿主"彻底解耦。

## 账号隔离

每个账号一个独立 profile 沙箱：`~/.2ag/profiles/<sanitized-email>/`，以 `--user-data-dir` 传入。

登录身份则存在 **Windows 凭据管理器**的 `gemini:antigravity` 条目里（`CRED_PERSIST_LOCAL_MACHINE` ⇒ 机器级，所有 `--user-data-dir` 共享）。这意味着换账号必须改写这条系统凭据 —— 见 [SECURITY.md](SECURITY.md) 里对"共享凭据"的说明。

## 版本与更新

2Ag 从本机官方安装复制冻结宿主时会记录其版本。官方 updater 之后更新的可能是官方安装，于是冻结副本会**落后**。这个差距由**更新守护**如实报出，要不要同步由用户决定 —— 2Ag 不会自动替换你的宿主。

## 相关文档

- [SECURITY.md](SECURITY.md) — 凭据处理、加密、隐私边界
- [CONFIGURATION.md](CONFIGURATION.md) — `2ag.json` 全部字段
- [CLI.md](CLI.md) — 命令行子命令
- [BUILD.md](BUILD.md) — 构建与打包
- [DEVELOPMENT.md](DEVELOPMENT.md) — 开发环境与约定
