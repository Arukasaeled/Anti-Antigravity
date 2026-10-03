[CmdletBinding()]
param(
    [string]$OutputPath = ''
)

$ErrorActionPreference = 'Stop'
if (-not $OutputPath) { $OutputPath = Join-Path $PSScriptRoot '..\dist\verification\manager-native.png' }
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class ManagerPaintProbe {
    [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct Point { public int X, Y; }
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
    public static IntPtr FindManager() { return FindWindow(null, "2Ag Manager - Anti-Antigravity Control Center"); }
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr hwnd, out Rect rect);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr hwnd, ref Point point);
    [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr hwnd, IntPtr after, int x, int y, int width, int height, uint flags);
    [DllImport("user32.dll")] public static extern IntPtr SetThreadDpiAwarenessContext(IntPtr context);
    [DllImport("user32.dll")] public static extern int GetWindowLong(IntPtr hwnd, int index);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hwnd);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hwnd, int command);
}
'@

# Screen pixels, rather than CDP screenshots: a hidden WebView can have a healthy DOM.
$hwnd = [ManagerPaintProbe]::FindManager()
if ($hwnd -eq [IntPtr]::Zero) { throw 'FAIL: Manager window absent' }
$priorDpi = [ManagerPaintProbe]::SetThreadDpiAwarenessContext([IntPtr]::new(-4))
$wasTopmost = ([ManagerPaintProbe]::GetWindowLong($hwnd, -20) -band 8) -ne 0
$bitmap = $null
$graphics = $null
try {
    if ([ManagerPaintProbe]::IsIconic($hwnd)) { [void][ManagerPaintProbe]::ShowWindow($hwnd, 9) }
    if (-not [ManagerPaintProbe]::SetWindowPos($hwnd, [IntPtr]::new(-1), 0, 0, 0, 0, 0x53)) {
        throw 'FAIL: cannot expose Manager window for native capture'
    }
    Start-Sleep -Milliseconds 400
    $rect = New-Object ManagerPaintProbe+Rect
    $point = New-Object ManagerPaintProbe+Point
    if (-not [ManagerPaintProbe]::GetClientRect($hwnd, [ref]$rect) -or
        -not [ManagerPaintProbe]::ClientToScreen($hwnd, [ref]$point)) {
        throw 'FAIL: cannot read Manager client bounds'
    }
    $bitmap = New-Object System.Drawing.Bitmap($rect.Right, $rect.Bottom)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.CopyFromScreen($point.X, $point.Y, 0, 0, $bitmap.Size)
    $absoluteOutput = [IO.Path]::GetFullPath($OutputPath)
    [void][IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($absoluteOutput))
    $bitmap.Save($absoluteOutput, [System.Drawing.Imaging.ImageFormat]::Png)
    $white = 0
    $samples = 0
    for ($y = 8; $y -lt [Math]::Min(380, $bitmap.Height); $y += 16) {
        for ($x = 8; $x -lt $bitmap.Width; $x += 16) {
            $pixel = $bitmap.GetPixel($x, $y)
            if ($pixel.R -gt 245 -and $pixel.G -gt 245 -and $pixel.B -gt 245) { $white++ }
            $samples++
        }
    }
    $ratio = $white / [Math]::Max(1, $samples)
    Write-Host ('Native client {0}x{1}; white ratio {2:P1}' -f $bitmap.Width, $bitmap.Height, $ratio)
    if ($ratio -gt 0.95) { throw 'FAIL: Manager client area is blank white' }
    Write-Host 'PASS: native Manager client is painted'
} finally {
    if ($graphics) { $graphics.Dispose() }
    if ($bitmap) { $bitmap.Dispose() }
    if (-not $wasTopmost) {
        [void][ManagerPaintProbe]::SetWindowPos($hwnd, [IntPtr]::new(-2), 0, 0, 0, 0, 0x13)
    }
    if ($priorDpi -ne [IntPtr]::Zero) { [void][ManagerPaintProbe]::SetThreadDpiAwarenessContext($priorDpi) }
}
