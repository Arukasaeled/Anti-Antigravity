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

前端是**手工维护的单文件 SPA** `web/dist/index.html`，通过 `go:embed` 进二进制。没有 npm / 打包步骤 —— 改完重新 `go build` 即可。

## 项目约定

### 界面文案

中文为主，关键产品名保留英文（`Dashboard` / `G-Hub` / `Skin Studio` / `Gravity Boost` …）。

### 「如实报出」是硬约定

这是本项目最常被违反、也最不被容忍的约定：

- 数据读不到就显示 `--` / 不可用，**不要用默认值伪装成真实状态**；
- 宿主没起来就不要报 PID；
- 校验没做就不要宣称成功；
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

涉及真机行为的部分（宿主启动、CDP 注入、账号切换）**必须在真机上验证**，单元测试覆盖不到。

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
