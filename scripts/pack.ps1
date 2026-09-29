[CmdletBinding()]
param(
    [string]$AntigravitySource = (Join-Path $env:LOCALAPPDATA 'Programs\Antigravity'),
    [string]$OutputRoot = (Join-Path $PSScriptRoot '..\dist'),
    [string]$Version = '0.1.0',
    [string]$Go = 'go',
    [string]$ISCC = 'ISCC.exe'
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
$HostExe = Join-Path $AntigravitySource 'antigravity.exe'
if (-not (Test-Path -LiteralPath $HostExe -PathType Leaf)) {
    $HostExe = Join-Path $AntigravitySource 'Antigravity.exe'
}

if (-not (Test-Path -LiteralPath $HostExe -PathType Leaf)) {
    throw "Antigravity host not found: $HostExe"
}

New-Item -ItemType Directory -Force -Path $OutputRoot | Out-Null
if (Test-Path -LiteralPath $Staging) { Remove-Item -LiteralPath $Staging -Recurse -Force }
New-Item -ItemType Directory -Force -Path $Staging, (Join-Path $Staging 'themes'), (Join-Path $Staging 'assets'), (Join-Path $Staging 'plugins') | Out-Null

$ldflags = "-H=windowsgui -X main.version=$Version"
& $goCommand build -trimpath -ldflags $ldflags -o $Binary (Join-Path $ProjectRoot 'cmd\2ag')
if ($LASTEXITCODE -ne 0) { throw "Go release build failed with exit code $LASTEXITCODE" }

Copy-Item -LiteralPath $AntigravitySource -Destination (Join-Path $Staging 'app') -Recurse -Force
Copy-Item -Path (Join-Path $ProjectRoot 'assets\*') -Destination (Join-Path $Staging 'assets') -Recurse -Force
Copy-Item -LiteralPath (Join-Path $ProjectRoot 'internal\patcher\injected_hub.js') -Destination (Join-Path $Staging 'assets\injected_hub.js') -Force
Copy-Item -Path (Join-Path $ProjectRoot 'themes\*') -Destination (Join-Path $Staging 'themes') -Recurse -Force
Copy-Item -Path (Join-Path $ProjectRoot 'plugins\*') -Destination (Join-Path $Staging 'plugins') -Recurse -Force
$defaultWallpaper = 'C:\Users\35074\Pictures\E78978FCDE46412C17F90AAF7813EC8D.jpg'
if (Test-Path -LiteralPath $defaultWallpaper -PathType Leaf) {
    Copy-Item -LiteralPath $defaultWallpaper -Destination (Join-Path $Staging 'themes\default\wallpaper.jpg') -Force
}

$config = @'
{
  "wallpaper_path": "C:/Users/35074/Pictures/E78978FCDE46412C17F90AAF7813EC8D.jpg",
  "blur": 20,
  "opacity": 0.55,
  "language": "zh-CN",
  "network": { "enabled": true, "endpoint_overrides": [], "rule_targets": [] },
  "privacy": { "blocked_hosts": ["google-analytics.com", "www.google-analytics.com", "analytics.google.com", "www.googletagmanager.com", "crashlyticsreports-pa.googleapis.com"], "block_beacons": true },
  "global_rules": "",
  "env_overrides": {},
  "plugins": [{ "name": "eyes-control", "executable": "plugins/eyes-control.exe", "enabled": false }]
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

$installer = Join-Path $ProjectRoot 'installer.iss'
if ((Test-Path -LiteralPath $isccCommand -PathType Leaf -ErrorAction SilentlyContinue) -or (Get-Command $isccCommand -ErrorAction SilentlyContinue)) {
	& $isccCommand "/DSourceDir=$Staging" "/DOutputDir=$OutputRoot" "/DMyAppVersion=$Version" $installer
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }
    Write-Host "Created $(Join-Path $OutputRoot 'Anti-Antigravity-Setup-x64.exe')"
} else {
    Write-Warning "ISCC.exe was not found; portable staging is ready at $Staging"
}
