# 架构

> 面向想理解 2Ag 内部结构的人。只关心怎么用请回 [README](../README.md)。

## 总览

`2ag.exe` 一个可执行文件同时是引擎和控制台。不带参数启动的是 **2Ag Manager**（WebView2 原生窗口，里外都是本机页面）。

```
2ag.exe  ──┬─ 本机回环控制面 (API + 内嵌前端)
           ├─ 本机回环 HTTP 转发代理
           ├─ 侧车插件进程
           └─ 官方 Antigravity 宿主 ── CDP ──> G-Hub / G-Cockpit（注入）
                 └─ 以上全部挂在同一条 Windows Job Object 下：宿主退出，一并清干净
```

## 模块

| 目录 | 职责 |
|---|---|
| `cmd/2ag/` | 进程入口、Manager 窗口、子命令 |
| `internal/api/` | 回环控制面（state / accounts / sessions / wallpaper / compat …） |
| `internal/config/` | 配置模型与持久化 |
| `internal/core/` | 状态机与事件总线 |
| `internal/netproxy/` | 本机回环转发代理 |
| `internal/patcher/` | CDP 桥与注入载荷（`injected_hub.js`） |
| `internal/supervisor/` | 宿主启动、账号扫描、官方登录 Broker、DPAPI 账号保险库、兼容性与更新守护、壁纸、侧车 |
| `web/` | 内嵌前端（`go:embed`，手工维护的单文件 SPA） |
| `assets/` | logo / 图标 / 内嵌资源 |
| `themes/` `plugins/` | 主题定义 / 插件契约 |
| `scripts/pack.ps1` | 发布构建 + 泄漏闸门 |
| `installer.iss` | Inno Setup 安装脚本 |

## 注入链路

2Ag 不修改官方 Antigravity 的任何安装文件 —— `app.asar` 原样保留、不重打包、不替换。

注入全部走 **CDP runtime injection**：

1. 2Ag 拉起官方宿主，通过 `--remote-debugging-port=<动态端口>` 打开调试通道；
2. 用 `Runtime.evaluate` + `Page.addScriptToEvaluateOnNewDocument` 把 `injected_hub.js` 送进渲染进程；
3. 界面以 **Shadow DOM** 挂载（`__2ag_root`），与宿主自己的 DOM 完全隔离，不污染宿主样式与选择器；
4. 宿主进程结束，注入即刻消失 —— 不需要"卸载"，也不留残留。

CDP 端口**动态分配**：历史实现硬编码 28472，一旦被占用宿主会静默换随机端口，而 2Ag 仍在戳旧端口 —— 表现为"连不上"但没有报错。现在改为启动前预留空闲端口，并从 `DevToolsActivePort` 回读实际值。

## 进程生命周期

宿主、代理、侧车进程全部挂在同一条 **Windows Job Object** 下，并设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。2Ag 退出（包括崩溃）时，OS 会连带清掉整棵进程树 —— 不会留下孤儿宿主。

## 运行形态

| 形态 | 用哪份宿主 | 行为 |
|---|---|---|
| 官方形态 | 你本机的官方安装 | 零启动参数、零注入、零凭据改写，行为接近"没有安装 2Ag" |
| 增强形态 | 2Ag 自己的冻结宿主副本 | 完整注入 + G-Hub + 皮肤 + 重力加倍 |

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
