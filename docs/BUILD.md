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
.\scripts\pack.ps1 -Version 0.2.2

# 只出 staging（不生成安装包）
.\scripts\pack.ps1 -Version 0.2.2 -SkipInstaller
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

GitHub Release 提供安装包 `Anti-Antigravity-Setup-x64.exe` 与 `2Ag-v0.2.2-windows-x64-portable.zip`。便携包由完整 staging 目录生成，不能只复制 exe 后删除 companion modules。

## 可复现性

发布使用固定源码提交、`-trimpath` 和版本参数构建。Inno Setup 安装包会包含压缩与构建时刻信息，不能假定两次打包字节相同。

## 仓库边界

`/dist` 与 `app\` 都**不进仓库**：

- `dist\` 是构建产物；
- `app\` 是运行时在用户本机生成的派生物 —— 仓库里既没有上游字节，也没有指向某台机器的链接。

## 发布

```powershell
.\scripts\pack.ps1 -Version 0.2.2
Compress-Archive -LiteralPath dist\staging -DestinationPath dist\2Ag-v0.2.2-windows-x64-portable.zip
```

提交源码并推送对应 tag，再把安装包和 ZIP 上传到 GitHub Release。更新说明直接写在 Release 页面，使用临时正文文件传给 `gh release create --notes-file`，仓库不保留逐版本 release notes。

打包不启动 2Ag 或 Antigravity。用户手动检查应与编译结果分开记录；切换运行形态或账号可能重启宿主，应等待当前任务结束。
