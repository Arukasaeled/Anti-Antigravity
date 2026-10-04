# 开发

## 环境

- Windows 10/11 x64
- Go 1.22+
- PowerShell（构建脚本用；**没有 Bash 依赖**）

## 快速上手

```powershell
git clone https://github.com/Arukasaeled/Anti-Antigravity.git
cd Anti-Antigravity

go build -o 2ag.exe .\cmd\2ag
.\2ag.exe
```

Manager 前端是手工维护的 SPA `web/dist/index.html` 与 companion modules，通过 `go:embed` 进二进制。G-Hub / Activity / Context 是 `internal/patcher/` 中的运行时注入模块；发布需要同时携带这些模块。没有 npm / 打包步骤 —— 改完重新 `go build` 即可。

## 项目约定

### 界面文案

简体中文与 English 是一级支持语言，`auto` 使用 Windows 系统语言。Manager 的文案由 `web/dist/i18n.js`、`locales.zh-CN.json` 和显式 `t()` / `data-i18n` bindings 管理；English 是缺失翻译的 fallback，不扫描全 DOM 替换字符串。`native_messages.js` 只映射明确的 2Ag 自有后端消息，未知原始探针详情保留原文。

新增文案同时补齐两种语言。用户草稿、路径、文件名、命令、代码与原始输出不进入翻译字典。G-Hub 跟随解析后的 UI language，ReAct 可独立 override，`force_zh_cn` 不随 UI language 改变。语言切换即时更新并通过现有 debounce 保存。

### 「如实报出」是硬约定

这是本项目最常被违反、也最不被容忍的约定：

- 数据读不到就显示 `--` / 不可用，**不要用默认值伪装成真实状态**；
- 宿主没起来就不要报 PID；
- 生命周期等实际执行后返回成功；配置接受、落盘成功、凭据 applied 和宿主 identity verified 分别报告；
- 保持 Request / Context / Session 分离，Unknown 不补 0，未分类输入不归入 Cache，Reasoning 不重复加入 Total；
- Activity 复用 allowlist / ActivityEntry / 共享 usage，禁止从官方 DOM 文案或隐藏 CoT 伪造过程；
- 历史数据过期就明说过期，不要当成实时读数。

代码里多处注释记录了违反这条约定的历史缺陷形态，改代码时先读注释。

### 注入载荷

`internal/patcher/injected_hub.js` 是注入到宿主的整段脚本，体量大且**改动必须逐条验证**：

```powershell
node --check internal/patcher/injected_hub.js
```

历史上一次性大块替换曾引入语法错误，且无法定位（截断 bisect 不可靠）。**逐条编辑 + 每条 `node --check`** 是可行做法。

### 前端语法校验

`web/dist/index.html` 不能用 `node --check`。抽出 `<script>` 块做 `new Function(body)` 校验。

## 测试

```powershell
go test ./...
```

账号 / API 测试可能读写本地选择标记。执行这些测试时将该 shell 的 `USERPROFILE` 指向独立临时目录，并将 `TWOAG_LIVE_CREDENTIAL_TESTS` 设为 `0`；不要在用户实际账号目录运行写操作测试。部分旧测试仍依赖真机凭据或旧身份契约，失败应单独说明，不应放宽产品校验来通过测试。

Windows 上可手动运行 `TestManagerReadinessRealUI`（需 `TWOAG_REAL_MANAGER_UI=1`），检查实际 WebView2 与嵌入页面。它使用独立配置 / WebView2 profile，允许的写动作仅为语言、模式和欢迎页完成状态；禁止宿主、账号和 Vault 写操作，不启动注入巡检。`TWOAG_REAL_MANAGER_CONFIG` 可复用本次检查保存的配置；普通测试默认跳过此手动检查。

涉及宿主启动、注入、切号的行为依赖真实环境。遵循当前任务授权：不为验证主动停止、重启或刷新正在工作的 Antigravity；未授权的生命周期验证留给用户手动执行，不用假状态代替。

> 注意：把 Antigravity 作为沙箱 pwsh 的子进程拉起时，Chromium 会立即退出。真机验证要用 `Start-Process explorer.exe -ArgumentList '"<exe>"'` 之类的**分离方式**启动，模拟真实用户路径。

## 提交

- 一个提交做一件事；
- 提交前 `git status --porcelain` 应当只包含你要提交的东西；
- 不要提交 `dist/`、`tmp/`、`app/`、`vault/`、凭据备份、日志。

## 相关文档

- [ARCHITECTURE.md](ARCHITECTURE.md) — 内部结构
- [SECURITY.md](SECURITY.md) — 凭据与隐私边界
- [BUILD.md](BUILD.md) — 构建与发布
- [CONFIGURATION.md](CONFIGURATION.md) — 配置字段
- [CLI.md](CLI.md) — 命令行
