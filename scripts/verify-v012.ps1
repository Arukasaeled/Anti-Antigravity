# v0.1.2 事实一致性 · 真机验证
#
# 在 Windows 开发机上运行（需要 Go 1.22+）。
# 这个脚本只做验证，不改产品代码。

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
Push-Location $repo
try {
    Write-Host "== 1) 单元测试（恢复 / 操作锁 / 网络模式 / 失败分类 / 四桶配额）=="
    go test ./internal/supervisor/ -run 'BrokerRecovery|CredentialOperation|LoginNetwork|AccountFailures|CredentialUsableEmail|CDP|Quota|Pools|Probe' -count=1
    if ($LASTEXITCODE -ne 0) { throw "单元测试失败" }

    Write-Host "`n== 2) 全量编译 =="
    go build ./...
    if ($LASTEXITCODE -ne 0) { throw "编译失败" }

    Write-Host "`n== 3) 静态一致性：不得继续定义或使用废弃的 block_beacons 字段 =="
    $ownedGo = Get-ChildItem -LiteralPath .\cmd,.\internal -Recurse -Filter *.go
    $bad = $ownedGo | Select-String -Pattern '^\s*BlockBeacons\b|\.BlockBeacons\b|"block_beacons"\s*:'
    if ($bad) { $bad | ForEach-Object { Write-Host $_ }; throw "block_beacons 字段残留" } else { Write-Host "OK: 无废弃字段定义或使用（说明性注释允许保留）" }

    Write-Host "`n== 4) 静态一致性：生产路径不得再直连 queryCacheFor 作为主数据源 =="
    $main = Select-String -Path .\internal\supervisor\account_scanner.go,.\internal\supervisor\oauth_flow.go -Pattern 'queryCacheFor\(' -ErrorAction SilentlyContinue | Where-Object { $_.Line -notmatch '^\s*func\s+queryCacheFor\(' }
    if ($main) { $main | ForEach-Object { Write-Host $_ }; throw "仍有生产路径直接调用 queryCacheFor" } else { Write-Host "OK: 生产路径只走 probeQuotaForAccount" }

    Write-Host "`n== 5) 真机：没装 Cockpit Tools 时能否拿到当前账号实时配额 =="
    Write-Host "手动步骤："
    Write-Host "  a) 用单元测试中的空 COCKPIT_TOOLS_DATA_DIR 验证无兼容库环境"
    Write-Host "  b) 真机启动 2Ag，进入账号面板；兼容库保持只读"
    Write-Host "  c) 观察：配额出处行应显示 live（或明确 unavailable），绝不能显示 cockpit-cache 冒充实时"
    Write-Host "  d) 查询失败时观察 fallback 是否标注 cockpit-cache / 非实时"

    Write-Host "`n== 6) 真机：A/B 两账号不串池 =="
    Write-Host "  Manager 正常可见启动、页面加载后先运行 scripts/verify-manager-paint.ps1，确认原生窗口已绘制"
    Write-Host "  两个不同邮箱账号各自 Live 探测后，核对 Gemini/Claude 四桶与账号邮箱一一对应"

    Write-Host "`n== 7) 真机：Manager vs 2ag run 生命周期 =="
    Write-Host "  - 2ag manager 启动宿主 → 关闭 Manager → 宿主仍在（预期）"
    Write-Host "  - 2ag run 启动 → 结束 run → 宿主被 Job Object 带走（预期）"

    Write-Host "`n== 8) blocked_hosts 仍生效 =="
    Write-Host "  配一个 blocked_hosts 域名，观察明文 HTTP 被 403；HTTPS CONNECT 只按目标域名匹配"
} finally {
    Pop-Location
}
