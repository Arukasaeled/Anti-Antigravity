# 构建与打包

## 前置条件

- **Go 1.22+**
- **Inno Setup 6** —— 只在需要产出安装包时；默认路径 `%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe`

**打包不需要本机安装官方 Antigravity。** 发布包不含任何官方运行时字节。

## 构建

### 本地自用

```powershell
go build -o 2ag.exe .\cmd\2ag
```

### 出发布包

```powershell
# release 二进制 + staging + 安装包
.\scripts\pack.ps1 -Version 0.2.0

# 只出 staging（不调 Inno Setup，用于验证产物内容）
.\scripts\pack.ps1 -Version 0.2.0 -SkipInstaller
```

## `pack.ps1` 做什么

依次三件事：

1. **release 构建** —— `go build -trimpath -ldflags "-H=windowsgui -X main.version=<版本>"`；
2. **写入中性初始配置** —— `2ag.json` 里没有任何开发机数据；
3. **跑泄漏闸门** —— 见 [SECURITY.md](SECURITY.md#打包泄漏闸门)。闸门不过就 `throw`，不会产出安装包。

同时从 `assets/logo.png` 重新生成 `assets/icon.ico`（256×256 PNG-backed ICO，保留透明通道）。

> 走 PNG payload 而不是 `Icon.FromHandle/GetHicon`：后者会把透明渐变压成青色剪影。

## 产物

```
dist\staging\
  2ag.exe                  引擎 + Manager（release 构建）
  2ag.json                 中性初始配置
  assets\                  logo / icon / 注入载荷
  themes\                  主题定义（纯数据）
  plugins\                 示例侧车插件
dist\Anti-Antigravity-Setup-x64.exe
```

`dist\staging\` **本身就是便携形态** —— 自包含，双击 `2ag.exe` 即跑。安装包与它同源同一份 `2ag.exe`，区别只是多写注册表 + 开始菜单快捷方式。

> **GitHub Release 目前只上传安装包**（`Anti-Antigravity-Setup-x64.exe` + `.sha256`），**没有**独立的 portable `2ag.exe` 资产。要绿色版请自己 `pack.ps1 -SkipInstaller` 或 `go build` 后使用 `dist\staging\`。

## 可复现性

`2ag.exe` 是**字节可复现**的：同一份源码连续构建得到完全相同的字节。

安装包**不是**：内部载荷经压缩 + LZMA 编码，Inno Setup 每次编译都会重新压缩并把编译时刻写进头部。因此发布页随附 `Anti-Antigravity-Setup-x64.exe.sha256` 而不是把哈希写死在文档里。

## 仓库边界

`/dist` 与 `app\` 都**不进仓库**：

- `dist\` 是构建产物；
- `app\` 是运行时在用户本机生成的派生物 —— 仓库里既没有上游字节，也没有指向某台机器的链接。

## 发布检查清单

```powershell
# 1) 从干净工作树构建
git status --porcelain          # 应当为空
.\scripts\pack.ps1 -Version 0.1.1

# 2) 记录哈希
Get-FileHash dist\staging\2ag.exe -Algorithm SHA256
Get-FileHash dist\Anti-Antigravity-Setup-x64.exe -Algorithm SHA256

# 3) 生成 .sha256 asset
$h = (Get-FileHash dist\Anti-Antigravity-Setup-x64.exe -Algorithm SHA256).Hash
"$h  Anti-Antigravity-Setup-x64.exe" | Set-Content -NoNewline -Encoding ascii dist\Anti-Antigravity-Setup-x64.exe.sha256

# 4) 负面检查：产物里不该出现的东西
#    注意：needle 必须拼接，否则本文件自己会被泄漏闸门扫中
$needles = @('GOC' + 'SPX-', 'apps.googleusercontent.com', 'Antigravity.exe' + '', 'app' + '.asar', 'refresh' + '_token')
$t = [Text.Encoding]::ASCII.GetString([IO.File]::ReadAllBytes('dist\Anti-Antigravity-Setup-x64.exe'))
foreach ($n in $needles) { "$n => $($t.Contains($n))" }
```

全部应为 `False`。
