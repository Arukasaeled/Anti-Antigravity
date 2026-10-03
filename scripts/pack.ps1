[CmdletBinding()]
param(
    [string]$OutputRoot = (Join-Path $PSScriptRoot '..\dist'),
    [string]$Version = '0.1.1',
    [string]$Go = 'go',
    [string]$ISCC = 'ISCC.exe',
    [switch]$SkipInstaller
)

$ErrorActionPreference = 'Stop'
$goCommand = $Go
if ($Go -eq 'go' -and -not (Get-Command go -ErrorAction SilentlyContinue)) {
    $installedGo = Join-Path ${env:ProgramFiles} 'Go\bin\go.exe'
    if (Test-Path -LiteralPath $installedGo -PathType Leaf) { $goCommand = $installedGo }
}
if (-not (Test-Path -LiteralPath $goCommand -PathType Leaf) -and -not (Get-Command $goCommand -ErrorAction SilentlyContinue)) {
    throw "Go compiler not found: $Go"
}
$isccCommand = $ISCC
if ($ISCC -eq 'ISCC.exe' -and -not (Get-Command ISCC.exe -ErrorAction SilentlyContinue)) {
    $installedISCC = Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'
    if (Test-Path -LiteralPath $installedISCC -PathType Leaf) { $isccCommand = $installedISCC }
}
$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not [IO.Path]::IsPathRooted($OutputRoot)) {
    $OutputRoot = Join-Path $ProjectRoot $OutputRoot
}
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
$Staging = Join-Path $OutputRoot 'staging'
$Binary = Join-Path $Staging '2ag.exe'

# ★ 打包边界：这个脚本**不再携带任何 Google Antigravity 字节**。
#
# 0.1.0 / 0.1.1 的安装包里那份 app\ 冻结运行时，是构建机从**本机**官方安装目录
# 整目录复制出来的 —— 等于把上游运行时随 2Ag 一起再分发，安装包因此到 156 MB，
# 也让「2Ag 发布了什么」与「Google 发布了什么」在字节层面纠缠在一起。
# 现在改由 2Ag 在**用户本机**首次启动增强形态时自己建立那份副本
# （internal\supervisor\frozen_host.go），所以打包过程既不需要、也不再读取
# 任何指向构建机官方安装目录的输入 —— 这个脚本因此少了一个 -AntigravitySource 参数。

New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null
if (Test-Path -LiteralPath $Staging) { Remove-Item -LiteralPath $Staging -Recurse -Force }
New-Item -ItemType Directory -Force -Path $Staging, (Join-Path $Staging 'themes'), (Join-Path $Staging 'assets'), (Join-Path $Staging 'plugins') | Out-Null

$ldflags = "-H=windowsgui -X main.version=$Version"
& $goCommand build -trimpath -ldflags $ldflags -o $Binary (Join-Path $ProjectRoot 'cmd\2ag')
if ($LASTEXITCODE -ne 0) { throw "Go release build failed with exit code $LASTEXITCODE" }

# assets\*.go 是源码（embed 声明），不是运行时资源 —— 发布包里放一个 .go 文件既没用
# 又多一处需要审的字节，所以只搬真正的资源。
Copy-Item -Path (Join-Path $ProjectRoot 'assets\*') -Destination (Join-Path $Staging 'assets') -Recurse -Force -Exclude '*.go'
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'internal\patcher\injected_hub.js') -Destination (Join-Path $Staging 'assets\injected_hub.js') -Force
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'internal\patcher\context_reader.js') -Destination (Join-Path $Staging 'assets\context_reader.js') -Force
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'internal\patcher\context_view.js') -Destination (Join-Path $Staging 'assets\context_view.js') -Force
Copy-Item -Path (Join-Path $ProjectRoot 'themes\*') -Destination (Join-Path $Staging 'themes') -Recurse -Force
Copy-Item -Path (Join-Path $ProjectRoot 'plugins\*') -Destination (Join-Path $Staging 'plugins') -Recurse -Force

# ★ 中性初始配置。
#   wallpaper_path 留空 = Native（使用 Antigravity 自己的原生背景）。
#   这里**不能**写任何具体图片路径 —— 那是一台具体机器的数据，装到别的机器上
#   就是一个必然读不到的文件，而且会让用户以为 2Ag 自带了一张壁纸。
#   与 internal/config 的 Default() 保持同一语义（两处都必须是空）。
#   plugins 给空数组：示例插件是否可用由用户自己决定，不在出厂配置里预置。
$config = @'
{
  "wallpaper_path": "",
  "blur": 20,
  "opacity": 0.55,
  "modal_opacity": 0.85,
  "language": "zh-CN",
  "runtime_mode": "enhanced",
  "network": { "enabled": true, "endpoint_overrides": [], "rule_targets": [] },
  "privacy": { "blocked_hosts": ["google-analytics.com", "www.google-analytics.com", "analytics.google.com", "www.googletagmanager.com", "crashlyticsreports-pa.googleapis.com"], "block_beacons": true },
  "global_rules": "",
  "env_overrides": {},
  "plugins": []
}
'@
$configPath = Join-Path $Staging '2ag.json'
[IO.File]::WriteAllText($configPath, $config, [Text.UTF8Encoding]::new($false))

$logoPath = Join-Path $ProjectRoot 'assets\logo.png'
if (-not (Test-Path -LiteralPath $logoPath -PathType Leaf)) { throw "2Ag logo not found: $logoPath" }
$iconPath = Join-Path $ProjectRoot 'assets\icon.ico'
Add-Type -AssemblyName System.Drawing

# Build a PNG-backed ICO. Icon.FromHandle/GetHicon converts the transparent
# gradient through the legacy Windows icon path and can turn it into a cyan
# silhouette. A PNG payload keeps the exact RGBA pixels of the supplied logo.
$sourceBitmap = [Drawing.Bitmap]::new($logoPath)
$bitmap = [Drawing.Bitmap]::new(256, 256, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$pngStream = [IO.MemoryStream]::new()
$iconStream = $null
try {
    $graphics.Clear([Drawing.Color]::Transparent)
    $graphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $graphics.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $scale = [Math]::Min(256.0 / $sourceBitmap.Width, 256.0 / $sourceBitmap.Height)
    $drawWidth = [int][Math]::Round($sourceBitmap.Width * $scale)
    $drawHeight = [int][Math]::Round($sourceBitmap.Height * $scale)
    $drawX = [int][Math]::Round((256 - $drawWidth) / 2.0)
    $drawY = [int][Math]::Round((256 - $drawHeight) / 2.0)
    $graphics.DrawImage($sourceBitmap, $drawX, $drawY, $drawWidth, $drawHeight)
    $bitmap.Save($pngStream, [Drawing.Imaging.ImageFormat]::Png)
    $pngBytes = $pngStream.ToArray()

    # ICO header + one 256x256 PNG entry. All integer fields are little-endian.
    $iconStream = [IO.File]::Create($iconPath)
    $writer = [IO.BinaryWriter]::new($iconStream)
    $writer.Write([uint16]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]1)
    $writer.Write([byte]0) # width 0 means 256
    $writer.Write([byte]0) # height 0 means 256
    $writer.Write([byte]0)
    $writer.Write([byte]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]32)
    $writer.Write([uint32]$pngBytes.Length)
    $writer.Write([uint32]22)
    $writer.Write($pngBytes)
    $writer.Flush()
    $writer.Dispose()
} finally {
    if ($iconStream) { $iconStream.Dispose() }
    $pngStream.Dispose(); $graphics.Dispose(); $bitmap.Dispose(); $sourceBitmap.Dispose()
}
Copy-Item -LiteralPath $iconPath -Destination (Join-Path $Staging 'assets\icon.ico') -Force

# ★ 泄漏闸门：只审「2Ag 自己拥有 / 生成 / 发布」的字节。
#
# 0.1.1 起 staging 里已经没有上游运行时可言（app\ 不再随包分发，见文件头的打包
# 边界），所以 release 作用域第一次等于「staging 里的全部内容」，不再需要任何
# 目录级豁免。下面那条 app\ 排除保留着，但它现在的身份是一道**断言**而不是豁免：
# 万一有人手工把官方运行时塞回 staging，闸门必须继续按「上游 vendored 字节」对待它
# （Google 和那些第三方作者自带的邮箱/绝对路径不是 2Ag 的用户数据，把它们算成
# 2Ag 的泄漏会让闸门永远是红的，真实命中就会被当成噪音忽略掉 —— 那才是最危险的
# 结局），同时文件头那条边界也已经被破坏了，这是需要人去修的事，不是闸门能代替的。
#
# 反过来，2Ag 自己写进产物或源码的每一个字节都必须干净。所以扫两个作用域：
#
#   1) release —— staging 的全部内容（app\ 已经不存在了，见上）：
#      生成的 2ag.json 模板、assets\、themes\、plugins\。
#   2) source  —— 源码树里的自有内容：README/installer/go.mod/.gitignore、
#      assets\ cmd\ internal\ themes\ plugins\ web\ scripts\。
#      其中 web\dist\* 被 //go:embed 进 2ag.exe，不会单独出现在 staging 里，
#      只有回到源码树才审得到；scripts\audit\ 是工程审计留档、不是发布物，
#      2ag.json / *.log 是开发机运行时残留（已 gitignore），两者都不在清单里。
#
# 判据刻意写成**结构性**的，而不是一串具体的人名/邮箱：
#
#   1. 真实的绝对 Windows 用户目录 —— 正则要求 C:\Users\ 后面紧跟一个用户名字符
#      （字母/数字/._-）。所以文档里的占位符 C:/Users/<me>/ 和闸门自己的
#      字面量 'C:/Users/' 都不算命中，而 C:/Users/<某人的目录名>/... 一定算。
#      这同时覆盖开发机和**任何**其他用户，不需要知道他们叫什么。
#   2. 指向本地用户目录的 file:/// 引用（同上）。
#   3. 当前构建机上那个用户名的硬编码（动态取，不写进源码）。
#   4. 一组「只可能来自个人数据」的通用邮箱域名（gmail/outlook/qq/163…）。
#
# 为什么不写死具体身份：把作者自己的邮箱写进这个列表，本身就是一次泄漏 ——
# 泄漏闸门的源码也是会公开的。结构性判据在任何机器上都成立，且不泄露任何人。
$currentUser = $env:USERNAME
$userPathRe = '[Cc]:[\\/]+Users[\\/]+[A-Za-z0-9]'
$fileUrlRe = 'file:[\\/]{3}[Cc]:[\\/]+Users[\\/]+[A-Za-z0-9]'
$emailRe = '[A-Za-z0-9._%+-]+@(gmail|googlemail|outlook|hotmail|qq|163|126)\.[A-Za-z]{2,}'
# Google OAuth 客户端密钥（GOC+SPX 前缀）与私钥块：只可能来自真实凭据，任何位置出现都算泄漏。
# 本行刻意用字符串拼接写出前缀，免得闸门自己的源码被自己的正则命中。
$secretRe = 'GOC' + 'SPX-|-----BEGIN [A-Z ]*PRIVATE KEY-----'
$textExt = @('.json', '.js', '.css', '.html', '.md', '.txt', '.iss', '.ps1', '.yml', '.yaml', '.xml', '.go', '.mod')

function Get-OwnedLeaks {
    param([string]$Label, [IO.FileInfo[]]$Files)
    $found = foreach ($file in $Files) {
        if ($file.Length -ge 8MB) { continue }
        if ($textExt -notcontains $file.Extension.ToLowerInvariant()) { continue }
        $rel = $file.FullName.Replace($Staging, '<staging>').Replace($ProjectRoot, '<repo>')
        foreach ($pat in @($userPathRe, $fileUrlRe)) {
            $m = Select-String -LiteralPath $file.FullName -Pattern $pat -ErrorAction SilentlyContinue
            if ($m) { "$Label $rel`:$($m[0].LineNumber) [绝对用户路径]" }
        }
        if ($currentUser) {
            $m = Select-String -LiteralPath $file.FullName -Pattern ([regex]::Escape($currentUser)) -SimpleMatch -ErrorAction SilentlyContinue
            if ($m) { "$Label $rel`:$($m[0].LineNumber) [构建机用户名]" }
        }
        $m = Select-String -LiteralPath $file.FullName -Pattern $emailRe -ErrorAction SilentlyContinue |
             Where-Object { $_.Line -notmatch '@example\.(com|invalid|org)' }
        if ($m) { "$Label $rel`:$($m[0].LineNumber) [真实邮箱]" }
        # 密钥判据不加 @example 豁免：假值写不出该前缀，命中即真凭据。
        $m = Select-String -LiteralPath $file.FullName -Pattern $secretRe -ErrorAction SilentlyContinue
        if ($m) { "$Label $rel`:$($m[0].LineNumber) [OAuth 客户端密钥/私钥]" }
    }
    return @($found)
}

$stagedFiles = @(Get-ChildItem -LiteralPath $Staging -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName.Substring($Staging.Length).TrimStart('\') -notlike 'app\*' })

$ownedSourceFiles = @()
foreach ($rel in @('README.md', 'RELEASE-NOTES-v0.1.1.md', 'installer.iss', '.gitignore', 'go.mod', 'LICENSE')) {
    $p = Join-Path $ProjectRoot $rel
    if (Test-Path -LiteralPath $p -PathType Leaf) { $ownedSourceFiles += (Get-Item -LiteralPath $p) }
}
foreach ($dir in @('assets', 'cmd', 'docs', 'internal', 'themes', 'plugins', 'web')) {
    $ownedSourceFiles += @(Get-ChildItem -LiteralPath (Join-Path $ProjectRoot $dir) -Recurse -File -ErrorAction SilentlyContinue)
}
$ownedSourceFiles += @(Get-ChildItem -LiteralPath (Join-Path $ProjectRoot 'scripts') -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -notlike '*\audit\*' })

$leaks = @()
if ($stagedFiles.Count -gt 0) { $leaks += Get-OwnedLeaks -Label 'release' -Files $stagedFiles }
if ($ownedSourceFiles.Count -gt 0) { $leaks += Get-OwnedLeaks -Label 'source ' -Files $ownedSourceFiles }
$leaks = $leaks | Sort-Object -Unique
if ($leaks.Count -gt 0) {
    Write-Host "RELEASE LEAK DETECTED — 2Ag 自有内容里出现了个人数据：" -ForegroundColor Red
    $leaks | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
    throw "packaging aborted: $($leaks.Count) leaked reference(s)"
}
Write-Host "Leak gate passed: 2Ag-owned content clean (release $($stagedFiles.Count) files / source $($ownedSourceFiles.Count) files; no upstream runtime in staging)"

if ($SkipInstaller) {
    Write-Host "Portable staging ready at $Staging (-SkipInstaller)"
    return
}

$installer = Join-Path $ProjectRoot 'installer.iss'
if ((Test-Path -LiteralPath $isccCommand -PathType Leaf -ErrorAction SilentlyContinue) -or (Get-Command $isccCommand -ErrorAction SilentlyContinue)) {
	& $isccCommand "/DSourceDir=$Staging" "/DOutputDir=$OutputRoot" "/DMyAppVersion=$Version" $installer
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }
    Write-Host "Created $(Join-Path $OutputRoot 'Anti-Antigravity-Setup-x64.exe')"
} else {
    Write-Warning "ISCC.exe was not found; portable staging is ready at $Staging"
}
