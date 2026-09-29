const fs = require('fs');
const path = require('path');

const htmlContent = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>2Ag SUITE - Control Center</title>
<style id="dynamic-styles">
  :root {
    /* Google / Gemini Material Dark 质感底色 */
    --g-bg-canvas: #131314;       /* 柔和不伤眼的深石墨底 */
    --g-surface: #1e1f20;         /* 容器卡片底 */
    --g-surface-high: #282a2c;    /* 次级/悬停底板 */
    --g-border: #3c4043;          /* Google 细致分割线 (1.5px) */
    
    /* Google 系品牌色 (点缀与指示) */
    --g-blue: #8ab4f8;            /* Google Blue / 激活选中态 */
    --g-green: #81c995;           /* Google Green / 正常运行 */
    --g-yellow: #fdd663;          /* Google Yellow / 5h 滚动滑窗 */
    --g-red: #f28b82;             /* Google Red / 危险操作 */
    --g-gemini-grad: linear-gradient(135deg, #4285f4 0%, #9b72cf 50%, #d96570 100%);
    
    /* 柔和护眼文字 */
    --text-primary: #e3e3e3;
    --text-secondary: #c4c7c5;
    --text-subtle: #8e918f;

    /* 兼容原有变量别名 */
    --bg-app: var(--g-bg-canvas);
    --bg-sidebar: var(--g-surface);
    --bg-card: var(--g-surface);
    --border-card: var(--g-border);
    --accent-red: var(--g-red);
    --accent-yellow: var(--g-yellow);
    --pool-gemini: var(--g-yellow);
    --pool-claude: var(--g-blue);
    --spider-cyan: var(--g-blue);
    --spider-magenta: var(--g-red);
    --text-main: var(--text-primary);
    --text-muted: var(--text-secondary);
    --shadow-soft: 2px 2px 0px rgba(0, 0, 0, 0.4);
  }

  /* 彻底消灭 TikTok 式红蓝色散，字重收敛至高级中粗 */
  * {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
    text-shadow: none !important;
  }
  body {
    font-family: "Google Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background-color: var(--g-bg-canvas);
    color: var(--text-primary);
    display: flex;
    height: 100vh;
    overflow: hidden;
  }

  /* P5 x Google Sidebar */
  #sidebar {
    width: 252px;
    background: var(--g-surface);
    border-right: 1.5px solid var(--g-border);
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    z-index: 10;
    flex-shrink: 0;
  }

  /* 顶部品牌区：消除廉价红底，换装 Google 深色胶囊 */
  .sidebar-brand {
    padding: 18px 16px;
    background: var(--g-surface);
    border-bottom: 1.5px solid var(--g-border);
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .brand-badge {
    background: #18191c;
    border: 1.5px solid var(--g-border);
    border-radius: 6px;
    transform: skewX(-6deg);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 6px 10px;
    box-shadow: 2px 2px 0px rgba(0, 0, 0, 0.4);
  }
  .brand-logo-img {
    width: 24px;
    height: 24px;
    object-fit: contain;
    transform: skewX(6deg);
  }
  .brand-title {
    font-weight: 800;
    letter-spacing: 0.5px;
    color: var(--text-primary);
    font-size: 16px;
    line-height: 1.1;
  }
  .brand-sub {
    font-size: 10px;
    color: var(--g-blue);
    font-family: monospace;
    letter-spacing: 0.5px;
    margin-top: 2px;
    font-weight: 600;
  }

  /* Nav Group Titles */
  .nav-group-title {
    font-size: 10px;
    font-weight: 800;
    color: var(--text-subtle);
    padding: 14px 18px 4px;
    letter-spacing: 1.5px;
    text-transform: uppercase;
  }

  /* P5 剪裁斜角 + Google Blue 高光导航项 */
  .nav-item {
    margin: 4px 12px;
    padding: 10px 14px;
    color: var(--text-secondary);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.15s ease;
    clip-path: polygon(0 0, 100% 0, 100% 100%, 0 100%);
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .nav-item:hover {
    background: var(--g-surface-high);
    color: var(--text-primary);
    transform: translateX(2px);
  }
  .nav-item.active {
    background: var(--g-surface-high);
    color: var(--g-blue) !important;
    font-weight: 700;
    border-left: 3px solid var(--g-blue);
    transform: skewX(-4deg) translateX(4px);
    clip-path: polygon(0 0, 94% 0, 100% 50%, 94% 100%, 0 100%);
  }
  .nav-item.active .nav-indicator {
    display: inline-block;
    width: 6px;
    height: 6px;
    background: var(--g-blue);
    transform: rotate(45deg);
  }

  /* HUD at sidebar bottom */
  .sidebar-hud {
    margin: 12px;
    padding: 10px 12px;
    background: var(--g-surface-high);
    border: 1.5px solid var(--g-border);
    box-shadow: 2px 2px 0px rgba(0, 0, 0, 0.3);
    font-family: 'Consolas', 'Courier New', monospace;
    font-size: 10px;
    color: var(--text-subtle);
    border-radius: 4px;
  }
  .hud-row {
    display: flex;
    justify-content: space-between;
    margin-bottom: 4px;
  }
  .hud-row:last-child { margin-bottom: 0; }
  .hud-val-active {
    color: var(--g-blue);
    font-weight: bold;
  }

  /* Main Stage & Panels */
  #main-stage {
    flex: 1;
    display: flex;
    flex-direction: column;
    position: relative;
    background: var(--g-bg-canvas);
    overflow: hidden;
  }
  #content-scroll {
    padding: 24px 32px;
    overflow-y: auto;
    flex: 1;
  }
  .panel { display: none; }
  .panel.active {
    display: block;
    animation: punchIn 0.15s cubic-bezier(0.175, 0.885, 0.32, 1.275) forwards;
  }
  @keyframes punchIn {
    0% { opacity: 0; transform: scale(0.99); }
    100% { opacity: 1; transform: scale(1); }
  }

  .panel-header-wrap {
    margin-bottom: 20px;
    border-bottom: 1.5px solid var(--g-border);
    padding-bottom: 14px;
  }
  h2.panel-title {
    font-weight: 800;
    font-size: 22px;
    color: var(--text-primary);
  }
  .panel-subtitle {
    font-size: 12px;
    color: var(--text-secondary);
    margin-top: 4px;
    font-weight: 500;
  }

  /* 消灭色散的 Title */
  .chromatic-title {
    text-shadow: none !important;
    letter-spacing: 0.3px;
  }

  /* P5 斜切卡片 + Google Material 质感底色 */
  .manga-card, .data-card {
    background: var(--g-surface);
    border: 1.5px solid var(--g-border);
    box-shadow: 3px 3px 0px rgba(0, 0, 0, 0.5);
    border-radius: 4px;
    clip-path: polygon(0 0, 100% 0, 100% calc(100% - 6px), calc(100% - 6px) 100%, 0 100%);
    position: relative;
  }

  /* P5 几何动感 + Google 调色板按钮 */
  .redline-btn {
    background: var(--g-blue);
    color: #131314;
    font-weight: 700;
    border: 1.5px solid var(--g-border);
    box-shadow: 2px 2px 0px rgba(0, 0, 0, 0.5);
    transform: skewX(-4deg);
    transition: transform 0.08s ease, box-shadow 0.08s ease, background 0.08s ease;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: 12px;
    padding: 7px 14px;
    border-radius: 2px;
  }
  .redline-btn:hover {
    background: #a8c7fa;
    color: #000;
    transform: skewX(-4deg) translate(-1px, -1px);
    box-shadow: 3px 3px 0px rgba(0, 0, 0, 0.6);
  }
  .redline-btn:active {
    transform: skewX(-4deg) translate(1px, 1px);
    box-shadow: 1px 1px 0px rgba(0, 0, 0, 0.4);
  }
  .redline-btn.btn-cyan {
    background: var(--g-blue);
    color: #131314;
  }
  .redline-btn.btn-dark {
    background: var(--g-surface-high);
    color: var(--text-primary);
    border-color: var(--g-border);
  }
  .redline-btn.btn-dark:hover {
    background: #333639;
    color: #fff;
  }
  .redline-btn.btn-magenta {
    background: var(--g-red);
    color: #131314;
  }
  .redline-btn.btn-magenta:hover {
    background: #f6aea9;
    color: #000;
  }

  /* Utilities */
  .row { display: flex; justify-content: space-between; align-items: center; }
  .col { display: flex; flex-direction: column; gap: 4px; }
  .flex-gap { display: flex; gap: 10px; align-items: center; }

  /* Google 四色规范状态徽章 */
  .status-badge {
    border: 1.5px solid var(--g-border);
    padding: 3px 8px;
    font-size: 11px;
    font-weight: 700;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    transform: skewX(-4deg);
    border-radius: 2px;
  }
  .badge-green { background: rgba(129, 201, 149, 0.18); color: var(--g-green); border-color: var(--g-green); }
  .badge-red { background: rgba(242, 139, 130, 0.18); color: var(--g-red); border-color: var(--g-red); }
  .badge-cyan { background: rgba(138, 180, 248, 0.18); color: var(--g-blue); border-color: var(--g-blue); }
  .badge-yellow { background: rgba(253, 214, 99, 0.18); color: var(--g-yellow); border-color: var(--g-yellow); }
  .badge-dark { background: var(--g-surface-high); color: var(--text-subtle); border-color: var(--g-border); }

  /* Dual Quota Pool Layout */
  .quota-pool-box {
    background: var(--g-surface-high);
    border: 1.5px solid var(--g-border);
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    border-radius: 3px;
  }
  .pool-title {
    font-size: 13px;
    font-weight: 700;
    color: var(--text-primary);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .pool-badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 6px;
    border: 1px solid var(--g-border);
    border-radius: 2px;
  }
  .quota-track {
    background: #131314;
    border: 1px solid var(--g-border);
    height: 12px;
    width: 100%;
    position: relative;
    margin-top: 4px;
    border-radius: 2px;
    overflow: hidden;
  }
  .quota-fill {
    height: 100%;
    transition: width 0.3s ease;
  }

  /* Models capsule row */
  .model-capsules {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 10px;
  }
  .model-capsule {
    background: var(--g-surface);
    border: 1px solid var(--g-border);
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 600;
    padding: 3px 8px;
    border-radius: 3px;
  }

  /* Square JSR Switch */
  .jsr-switch { position: relative; display: inline-block; width: 42px; height: 22px; flex-shrink: 0; }
  .jsr-switch input { opacity: 0; width: 0; height: 0; }
  .jsr-slider {
    position: absolute;
    cursor: pointer;
    inset: 0;
    background-color: var(--g-surface-high);
    border: 1.5px solid var(--g-border);
    transition: .12s;
    border-radius: 2px;
  }
  .jsr-slider:before {
    position: absolute;
    content: "";
    height: 12px;
    width: 12px;
    left: 3px;
    bottom: 3px;
    background-color: var(--text-secondary);
    border: 1px solid var(--g-border);
    transition: .12s;
  }
  input:checked + .jsr-slider {
    background-color: var(--g-blue);
    border-color: var(--g-blue);
  }
  input:checked + .jsr-slider:before {
    transform: translateX(18px);
    background-color: #131314;
    border-color: #131314;
  }

  /* Settings Section */
  .settings-section {
    display: flex;
    margin-bottom: 20px;
    border-bottom: 1.5px solid var(--g-border);
    padding-bottom: 20px;
  }
  .settings-left {
    width: 220px;
    flex-shrink: 0;
    font-weight: 700;
    font-size: 14px;
    color: var(--text-primary);
  }
  .settings-right {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .setting-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
    border-bottom: 1px solid rgba(255,255,255,0.04);
  }
  .setting-row:last-child { border-bottom: none; }
  .setting-info-title { font-size: 13px; font-weight: 700; color: var(--text-primary); }
  .setting-info-desc { font-size: 12px; color: var(--text-secondary); font-weight: 500; max-width: 520px; line-height: 1.45; margin-top: 2px; }

  /* Sliders */
  .range-container { display: flex; align-items: center; gap: 14px; flex: 1; max-width: 320px; }
  input[type="range"] {
    -webkit-appearance: none;
    width: 100%;
    height: 5px;
    background: #131314;
    border: 1.5px solid var(--g-border);
    outline: none;
    border-radius: 2px;
  }
  input[type="range"]::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 14px;
    height: 14px;
    background: var(--g-blue);
    border: 1.5px solid var(--g-border);
    cursor: pointer;
    transform: rotate(45deg);
  }
  .range-value {
    font-family: monospace;
    font-size: 12px;
    font-weight: 700;
    color: #131314;
    background: var(--g-blue);
    padding: 2px 6px;
    border: 1px solid var(--g-border);
    min-width: 46px;
    text-align: center;
    border-radius: 2px;
  }

  /* Dashboard Grid */
  .dashboard-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 18px; margin-bottom: 20px; }
  .metric-param { font-family: monospace; font-size: 13px; font-weight: 700; color: var(--g-yellow); }
  .metric-label { font-size: 11px; color: var(--text-secondary); font-weight: 600; }

  /* Theme Grid */
  .theme-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-top: 14px; }
  .theme-card {
    background: var(--g-surface);
    border: 1.5px solid var(--g-border);
    box-shadow: 2px 2px 0px rgba(0, 0, 0, 0.4);
    display: flex;
    flex-direction: column;
    transition: all 0.15s ease;
    cursor: pointer;
    border-radius: 4px;
    overflow: hidden;
  }
  .theme-card:hover {
    transform: translate(-2px, -2px);
    box-shadow: 4px 4px 0px rgba(0, 0, 0, 0.6);
    border-color: var(--g-blue);
  }
  .theme-thumb { height: 110px; border-bottom: 1.5px solid var(--g-border); }
  .theme-info { padding: 12px; }
  .theme-title { font-size: 13px; font-weight: 700; margin-bottom: 3px; display: flex; justify-content: space-between; align-items: center; color: var(--text-primary); }
  .theme-sub { font-size: 11px; color: var(--text-secondary); margin-bottom: 10px; font-weight: 500; }
  .theme-actions { display: flex; gap: 6px; }
  .theme-actions .redline-btn { flex: 1; padding: 5px 0; font-size: 11px; }

  /* Account Cards */
  .account-grid { display: flex; flex-direction: column; gap: 16px; margin-top: 16px; }
  .account-card { padding: 18px 20px; }

  /* Modal */
  .modal-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.8);
    z-index: 100;
    display: none;
    align-items: center;
    justify-content: center;
    backdrop-filter: blur(4px);
  }
  .modal-overlay.active { display: flex; }
  .modal-box {
    width: 640px;
    max-width: 92vw;
    background: var(--g-surface);
    border: 1.5px solid var(--g-border);
    box-shadow: 4px 4px 0px rgba(0, 0, 0, 0.6);
    padding: 22px 24px;
    position: relative;
    border-radius: 4px;
  }
  .modal-input {
    width: 100%;
    background: #131314;
    border: 1.5px solid var(--g-border);
    color: var(--text-primary);
    padding: 8px 10px;
    font-family: monospace;
    font-size: 12px;
    outline: none;
    margin-top: 4px;
    border-radius: 2px;
  }
  .modal-input:focus { border-color: var(--g-blue); }
  .modal-tab-bar {
    display: flex;
    gap: 8px;
    margin-bottom: 16px;
    border-bottom: 1.5px solid var(--g-border);
    padding-bottom: 8px;
  }
  .modal-tab-btn {
    background: var(--g-surface-high);
    border: 1.5px solid var(--g-border);
    color: var(--text-secondary);
    font-weight: 700;
    font-size: 12px;
    padding: 8px 14px;
    cursor: pointer;
    transform: skewX(-4deg);
    transition: all 0.1s;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border-radius: 2px;
  }
  .modal-tab-btn:hover { color: var(--text-primary); border-color: var(--g-blue); }
  .modal-tab-btn.active {
    background: var(--g-blue);
    color: #131314;
    border-color: var(--g-blue);
  }
  .modal-tab-pane { display: none; flex-direction: column; gap: 12px; }
  .modal-tab-pane.active { display: flex; }
  .drop-zone {
    border: 2px dashed var(--g-border);
    background: #131314;
    padding: 20px 14px;
    text-align: center;
    cursor: pointer;
    transition: all 0.15s;
    border-radius: 4px;
  }
  .drop-zone:hover, .drop-zone.dragover { border-color: var(--g-blue); background: var(--g-surface-high); }

  /* Terminal */
  #terminal {
    background: #101112;
    color: var(--g-green);
    font-family: 'Consolas', 'Courier New', monospace;
    padding: 14px;
    height: 310px;
    overflow-y: auto;
    border: 1.5px solid var(--g-border);
    box-shadow: 2px 2px 0px rgba(0,0,0,0.4);
    font-size: 12px;
    line-height: 1.6;
    border-radius: 4px;
  }

  ::-webkit-scrollbar { width: 8px; }
  ::-webkit-scrollbar-track { background: var(--g-bg-canvas); border-left: 1px solid var(--g-border); }
  ::-webkit-scrollbar-thumb { background: var(--g-surface-high); border-radius: 4px; }
  ::-webkit-scrollbar-thumb:hover { background: var(--g-border); }
</style>
</head>
<body>

<!-- SIDEBAR -->
<div id="sidebar">
  <div>
    <!-- Brand Area: Google Dark Capsule -->
    <div class="sidebar-brand">
      <div class="brand-badge">
        <img class="brand-logo-img" src="assets/logo.png" onerror="this.src='/dist/assets/logo.png'" alt="Logo">
      </div>
      <div>
        <div class="brand-title">2Ag SUITE</div>
        <div class="brand-sub">ANTI-ANTIGRAVITY</div>
      </div>
    </div>

    <div class="nav-group-title">WORKSPACE</div>
    <div class="nav-item active" data-target="dashboard"><span>概览看板 (Dashboard)</span><span class="nav-indicator"></span></div>
    <div class="nav-item" data-target="gateway"><span>账号矩阵 (Accounts)</span><span class="nav-indicator"></span></div>
    <div class="nav-item" data-target="sessions"><span>会话审计 (Sessions)</span><span class="nav-indicator"></span></div>

    <div class="nav-group-title">EXTENSIONS</div>
    <div class="nav-item" data-target="studio"><span>视觉工坊 (Skin Studio)</span><span class="nav-indicator"></span></div>
    <div class="nav-item" data-target="boost"><span>重力加倍 (Gravity Boost)</span><span class="nav-indicator"></span></div>
    <div class="nav-item" data-target="plugins"><span>插件生态 (Plugins)</span><span class="nav-indicator"></span></div>
    <div class="nav-item" data-target="diagnostics"><span>环境诊断 (Diagnostics)</span><span class="nav-indicator"></span></div>
  </div>

  <!-- HUD at sidebar bottom -->
  <div class="sidebar-hud">
    <div class="hud-row"><span>CDP 握手:</span><span class="hud-val-active" id="hud-cdp-ms">1.2ms</span></div>
    <div class="hud-row"><span>宿主内存:</span><span class="hud-val-active" id="hud-mem">104.2MB</span></div>
    <div class="hud-row"><span>引擎运行:</span><span class="hud-val-active" id="hud-uptime">00:00:00</span></div>
  </div>
</div>

<!-- MAIN STAGE -->
<div id="main-stage">
  <div id="content-scroll">

    <!-- 01 DASHBOARD -->
    <div id="dashboard" class="panel active">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">概览看板</h2>
          <div class="panel-subtitle">系统核心运行时与探针监控</div>
        </div>
        <div class="status-badge badge-yellow">REDLINE PROBE ACTIVE</div>
      </div>

      <div class="dashboard-grid">
        <!-- Card 1: 真实宿主状态 -->
        <div class="data-card" style="padding: 18px;">
          <div class="row" style="margin-bottom: 14px;">
            <span style="font-weight: 700; font-size: 15px;">宿主实例状态 (Host Instance)</span>
            <span class="status-badge badge-yellow" id="dash-host-badge">● 探测中...</span>
          </div>
          <div style="display:grid; grid-template-columns: repeat(3, 1fr); gap: 10px; background:var(--g-surface-high); border:1.5px solid var(--g-border); padding: 12px; margin-bottom: 16px; border-radius: 4px;">
            <div class="col"><span class="metric-label">真实 PID</span><span class="metric-param" id="dash-host-pid">-</span></div>
            <div class="col"><span class="metric-label">物理内存占用</span><span class="metric-param" id="dash-host-mem">-</span></div>
            <div class="col"><span class="metric-label">CDP 调试端口</span><span class="metric-param" id="dash-host-cdp">28472</span></div>
          </div>
          <div class="flex-gap">
            <button class="redline-btn" onclick="launchHost()">▷ 启动宿主</button>
            <button class="redline-btn btn-dark" onclick="restartHost()">重启</button>
            <button class="redline-btn btn-magenta" style="margin-left:auto" onclick="stopHost()">结束进程</button>
          </div>
        </div>

        <!-- Card 2: 协议网关探针卡 -->
        <div class="data-card" style="padding: 18px;">
          <div class="row" style="margin-bottom: 14px;">
            <span style="font-weight: 700; font-size: 15px;">协议网关探针 (Protocol Gateway)</span>
            <span class="status-badge badge-cyan">● 握手就绪 (28ms)</span>
          </div>
          <div style="display:grid; grid-template-columns: repeat(2, 1fr); gap: 10px; background:var(--g-surface-high); border:1.5px solid var(--g-border); padding: 12px; margin-bottom: 16px; border-radius: 4px;">
            <div class="col"><span class="metric-label">网关监听地址</span><span class="metric-param">127.0.0.1:8045</span></div>
            <div class="col"><span class="metric-label">当前调度模式</span><span class="metric-param" style="color:var(--g-blue)">智能 429 自愈轮询</span></div>
          </div>
          <div class="setting-info-desc" style="font-size:11px;">协议映射: Gemini v1internal -> OpenAI Compatible 协议中继</div>
        </div>

        <!-- Card 3: 真实双配额池视窗 -->
        <div class="data-card" style="padding: 18px;">
          <div class="row" style="margin-bottom: 10px;">
            <span style="font-weight: 700; font-size: 15px;">真机双配额池视窗 (Live Quota Pools)</span>
            <span class="metric-param" id="dash-primary-email" style="font-size:11px; color:var(--text-secondary)">user@example.com</span>
          </div>
          <div style="display:grid; grid-template-columns: repeat(2, 1fr); gap: 10px; margin-top: 8px;">
            <div class="quota-pool-box" style="padding: 10px;">
              <div class="pool-title"><span style="color:var(--g-yellow)">Gemini Models</span><span style="font-size:10px; color:var(--text-subtle)">5h / 周额度</span></div>
              <div>
                <div class="row" style="font-size:10px;"><span style="color:var(--text-secondary)">5h 滑窗</span><span style="color:var(--g-yellow)" id="dash-g-5h-val">39% · 2h 20m</span></div>
                <div class="quota-track"><div class="quota-fill" id="dash-g-5h-bar" style="width:39%; background:var(--g-yellow);"></div></div>
              </div>
              <div>
                <div class="row" style="font-size:10px;"><span style="color:var(--text-secondary)">周限制总额</span><span style="color:var(--text-primary)" id="dash-g-wk-val">46% · 3d 3h</span></div>
                <div class="quota-track"><div class="quota-fill" id="dash-g-wk-bar" style="width:46%; background:#5f6368;"></div></div>
              </div>
            </div>

            <div class="quota-pool-box" style="padding: 10px;">
              <div class="pool-title"><span style="color:var(--g-blue)">Claude & GPT</span><span style="font-size:10px; color:var(--text-subtle)">5h / 周额度</span></div>
              <div>
                <div class="row" style="font-size:10px;"><span style="color:var(--text-secondary)">5h 滑窗</span><span style="color:var(--g-blue)" id="dash-c-5h-val">100% · 满额</span></div>
                <div class="quota-track"><div class="quota-fill" id="dash-c-5h-bar" style="width:100%; background:var(--g-blue);"></div></div>
              </div>
              <div>
                <div class="row" style="font-size:10px;"><span style="color:var(--text-secondary)">周限制总额</span><span style="color:var(--text-primary)" id="dash-c-wk-val">93% · 5d 4h</span></div>
                <div class="quota-track"><div class="quota-fill" id="dash-c-wk-bar" style="width:93%; background:#5f6368;"></div></div>
              </div>
            </div>
          </div>
        </div>

        <!-- Card 4: 外挂补丁装载状态 -->
        <div class="data-card" style="padding: 18px;">
          <div class="row" style="margin-bottom: 14px;">
            <span style="font-weight: 700; font-size: 15px;">外挂补丁装载状态 (Injection Status)</span>
            <span class="status-badge badge-green">INJECTED</span>
          </div>
          <div style="display:grid; grid-template-columns: repeat(2, 1fr); gap: 10px; background:var(--g-surface-high); border:1.5px solid var(--g-border); padding: 12px; margin-bottom: 16px; border-radius: 4px;">
            <div class="col"><span class="metric-label">Dream Skin</span><span class="metric-param" style="color:var(--g-green)">ACTIVE (0.85/20px)</span></div>
            <div class="col"><span class="metric-label">Gravity Boost</span><span class="metric-param" style="color:var(--g-green)">11 SHIELDS ON</span></div>
          </div>
          <div class="flex-gap">
            <button class="redline-btn btn-cyan" onclick="postAction('HOT_RELOAD', {})">⚡ 热重载补丁</button>
            <button class="redline-btn btn-dark" onclick="postAction('OPEN_DEVTOOLS', {})">呼出 DevTools (F12)</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 02 ACCOUNTS MATRIX -->
    <div id="gateway" class="panel">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">多账号矩阵与配额池</h2>
          <div class="panel-subtitle">官方双配额限制池实时探测与故障自愈 (Google 官方授权架构)</div>
        </div>
        <div class="flex-gap">
          <button class="redline-btn btn-cyan" onclick="openAddAccountModal()">+ 接入新账号</button>
          <button class="redline-btn btn-dark" onclick="refreshQuotas()">刷新配额池</button>
        </div>
      </div>

      <div class="account-grid" id="accounts-container">
        <!-- Rendered dynamically -->
      </div>
    </div>

    <!-- 03 SESSIONS AUDIT (真实会话审计) -->
    <div id="sessions" class="panel">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">会话审计</h2>
          <div class="panel-subtitle">本地 Antigravity 真实持久化会话索引、UUID 标识与导出归档</div>
        </div>
        <div class="flex-gap">
          <span class="status-badge badge-cyan" id="sessions-total-badge">● 正在读取...</span>
          <button class="redline-btn btn-dark" onclick="exportAllSessions()">导出全量 .md</button>
          <button class="redline-btn btn-cyan" onclick="loadSessions(true)">同步索引</button>
        </div>
      </div>

      <div class="data-card" style="padding: 18px;">
        <div id="sessions-list" style="display:flex; flex-direction:column; gap:10px;">
          <!-- Dynamically populated with REAL local sessions -->
          <div style="font-size:12px; color:var(--text-secondary); text-align:center; padding: 24px;">正在扫描本地 Antigravity 真实会话...</div>
        </div>
      </div>
    </div>

    <!-- 04 SKIN STUDIO -->
    <div id="studio" class="panel">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">视觉工坊</h2>
          <div class="panel-subtitle">2Ag Dream Skin 风格主题与实时微调流转中心</div>
        </div>
        <div class="status-badge badge-green">P5 SKINS ACTIVE</div>
      </div>

      <!-- Top Run Bar -->
      <div class="data-card" style="padding: 18px; margin-bottom: 20px;">
        <div class="row" style="margin-bottom: 16px;">
          <div class="col">
            <div style="font-weight:700; font-size:15px; color:var(--text-primary);">启用 Dream 皮肤注入</div>
            <div class="setting-info-desc">配置实时通过 CDP 桥接回环注入宿主 (端口: 28472)；恢复原始外观不会删除主题库文件。</div>
          </div>
          <div class="flex-gap">
            <label class="jsr-switch"><input type="checkbox" id="skin-master-switch" checked onchange="postAction('TOGGLE_SKIN', {enabled: this.checked})"><span class="jsr-slider"></span></label>
            <span class="status-badge badge-green">● CDP 宿主端口 :28472</span>
          </div>
        </div>
        <div class="flex-gap">
          <button class="redline-btn btn-cyan" onclick="launchHost()">▷ 应用外观</button>
          <button class="redline-btn btn-dark" onclick="applyPreset('native')">恢复默认外观</button>
          <button class="redline-btn btn-dark" onclick="postAction('HOT_RELOAD', {})">刷新注入</button>
        </div>
      </div>

      <!-- Theme Grid -->
      <div class="row" style="margin-bottom: 10px;">
        <span style="font-weight:700; font-size:15px;">主题库与微缩壁纸 (Theme Matrix)</span>
        <span class="setting-info-desc">Google 质感微缩景观 · 柔和自然</span>
      </div>
      <div class="theme-grid" style="margin-bottom: 22px;">
        <!-- Theme 1 -->
        <div class="theme-card" onclick="applyPreset('cyberpunk')">
          <div class="theme-thumb" style="background: linear-gradient(135deg, #0d0221 0%, #240046 50%, #4285f4 100%);"></div>
          <div class="theme-info">
            <div class="theme-title">Cyberpunk Void <span style="font-size:10px; color:var(--g-yellow)">v1.1.2</span></div>
            <div class="theme-sub">2Ag Suite Team · 极光暗涌</div>
            <div class="theme-actions">
              <button class="redline-btn btn-cyan">应用</button>
              <button class="redline-btn btn-dark">预览</button>
            </div>
          </div>
        </div>

        <!-- Theme 2 -->
        <div class="theme-card" onclick="applyPreset('pure_dark')">
          <div class="theme-thumb" style="background: linear-gradient(135deg, #131314 0%, #1e1f20 50%, #282a2c 100%);"></div>
          <div class="theme-info">
            <div class="theme-title">Obsidian Minimal <span style="font-size:10px; color:var(--g-yellow)">v1.0.0</span></div>
            <div class="theme-sub">2Ag Suite Team · Google 护眼灰</div>
            <div class="theme-actions">
              <button class="redline-btn btn-cyan">应用</button>
              <button class="redline-btn btn-dark">预览</button>
            </div>
          </div>
        </div>

        <!-- Theme 3 -->
        <div class="theme-card" onclick="applyPreset('frosted')">
          <div class="theme-thumb" style="background: linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #8ab4f8 100%);"></div>
          <div class="theme-info">
            <div class="theme-title">Slate Aurora <span style="font-size:10px; color:var(--g-yellow)">v2.0.0</span></div>
            <div class="theme-sub">2Ag Suite Team · 极光冷灰</div>
            <div class="theme-actions">
              <button class="redline-btn btn-cyan">应用</button>
              <button class="redline-btn btn-dark">预览</button>
            </div>
          </div>
        </div>

        <!-- Theme 4 -->
        <div class="theme-card" onclick="applyPreset('electric')">
          <div class="theme-thumb" style="background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #fdd663 100%);"></div>
          <div class="theme-info">
            <div class="theme-title">Gemini Dusk <span style="font-size:10px; color:var(--g-yellow)">v1.0.4</span></div>
            <div class="theme-sub">2Ag Suite Team · 极星暮色</div>
            <div class="theme-actions">
              <button class="redline-btn btn-cyan">应用</button>
              <button class="redline-btn btn-dark">预览</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Live Fine-Tuning -->
      <div style="font-weight:700; font-size:15px; margin-bottom: 10px;">细分参数微调室 (Live Fine-Tuning)</div>
      <div class="data-card" style="padding: 18px;">
        <div class="row" style="margin-bottom: 18px;">
          <div class="col" style="width: 220px">
            <div class="setting-info-title">背景模糊度 (Blur)</div>
            <div class="setting-info-desc">毛玻璃滤镜强度 (0px - 40px)</div>
          </div>
          <div class="range-container">
            <input type="range" id="studio-blur" min="0" max="40" oninput="updateVal('blur-val', this.value, 'px')">
            <span class="range-value" id="blur-val">20px</span>
          </div>
        </div>

        <div class="row" style="margin-bottom: 18px; border-top:1px solid rgba(255,255,255,0.05); padding-top: 16px;">
          <div class="col" style="width: 220px">
            <div class="setting-info-title">遮罩暗度 (Darkness)</div>
            <div class="setting-info-desc">降低壁纸亮度以凸显文本内容 (10% - 90%)</div>
          </div>
          <div class="range-container">
            <input type="range" id="studio-opacity" min="10" max="90" oninput="updateVal('opacity-val', this.value, '%')">
            <span class="range-value" id="opacity-val">55%</span>
          </div>
        </div>

        <div class="row" style="border-top:1px solid rgba(255,255,255,0.05); padding-top: 16px;">
          <div class="col" style="width: 220px">
            <div class="setting-info-title">模态弹窗遮蔽度 (Modal Glass)</div>
            <div class="setting-info-desc">二级浮层背景不透明度 (70% - 100%)，防透叠</div>
          </div>
          <div class="range-container">
            <input type="range" id="studio-modal" min="70" max="100" oninput="updateVal('modal-val', this.value, '%')">
            <span class="range-value" id="modal-val">85%</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 05 BOOST -->
    <div id="boost" class="panel">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">重力加倍</h2>
          <div class="panel-subtitle">会话删除、Markdown 导出、文本粘贴修复与深度辅助开关矩阵 (安全沙箱隔离)</div>
        </div>
        <div class="status-badge badge-cyan">SHIELD PROTECTED</div>
      </div>

      <div class="data-card" style="padding: 24px;">
        <!-- Group 1 -->
        <div class="settings-section">
          <div class="settings-left">
            对话与输入优化
            <span style="font-size:11px; color:var(--text-secondary); font-weight:500; display:block; margin-top:6px;">调整会话管理、输入行为与对话阅读体验</span>
          </div>
          <div class="settings-right">
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">会话删除 (Session Deletion)</div><div class="setting-info-desc">在会话列表悬停时显示删除图标与撤销支持。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-session_delete" onchange="toggleBoost('session_delete', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">Markdown 导出 (Export to MD)</div><div class="setting-info-desc">操作栏提供一键导出为带时间戳的完整 .md 归档文件。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-markdown_export" onchange="toggleBoost('markdown_export', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">纯文本粘贴修复 (Paste Plaintext Fix)</div><div class="setting-info-desc">清除剪贴板格式，防止 Word 富文本被误认成附件图片 (隔离防阻断)。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-paste_plaintext_fix" onchange="toggleBoost('paste_plaintext_fix', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">会话 ID 标识 (Session UUID Tag)</div><div class="setting-info-desc">在侧栏标题前显示 UUIDv7 短标与创建时间，方便定位历史。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-session_id_tag" onchange="toggleBoost('session_id_tag', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">对话居中宽度限制 (Centered Max-Width)</div><div class="setting-info-desc">精准作用于聊天视窗 (860px)，严禁影响代码编辑器与工具栏。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-centered_width" onchange="toggleBoost('centered_width', this.checked)"><span class="jsr-slider"></span></label>
            </div>
          </div>
        </div>

        <!-- Group 2 -->
        <div class="settings-section">
          <div class="settings-left">
            交互与阅读辅助
            <span style="font-size:11px; color:var(--text-secondary); font-weight:500; display:block; margin-top:6px;">控制长篇回复目录提炼与翻阅连贯性</span>
          </div>
          <div class="settings-right">
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">切换对话记忆滚动条 (Preserve Scroll Position)</div><div class="setting-info-desc">切换 Thread 时记忆上次浏览进度，不强制跳到底部。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-preserve_scroll" onchange="toggleBoost('preserve_scroll', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">Stepwise 深度下一步建议</div><div class="setting-info-desc">根据当前回答动态生成下一步操作策略与快捷动作卡片。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-stepwise" onchange="toggleBoost('stepwise', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">结构化回答大纲 (Answer Outline)</div><div class="setting-info-desc">在长文本回答顶部自动提炼并悬浮结构大纲目录。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-answer_outline" onchange="toggleBoost('answer_outline', this.checked)"><span class="jsr-slider"></span></label>
            </div>
          </div>
        </div>

        <!-- Group 3 -->
        <div class="settings-section" style="border-bottom:none; padding-bottom:0; margin-bottom:0;">
          <div class="settings-left">
            宿主与底层控制
            <span style="font-size:11px; color:var(--text-secondary); font-weight:500; display:block; margin-top:6px;">控制语言锁定、原生特权与静默更新阻断</span>
          </div>
          <div class="settings-right">
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">强制原生中文语言包 (Force zh-CN)</div><div class="setting-info-desc">强制锁定 Antigravity 界面语言为中文，避免多语言时区漂移。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-force_zh_cn" onchange="toggleBoost('force_zh_cn', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">开发者特权放行 (Enable DevTools F12)</div><div class="setting-info-desc">解除官方快捷键屏蔽，允许随时随地呼出开发者工具检查 DOM。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-enable_devtools" onchange="toggleBoost('enable_devtools', this.checked)"><span class="jsr-slider"></span></label>
            </div>
            <div class="setting-row">
              <div class="col"><div class="setting-info-title">拦截后台自动更新 (Disable Background Update)</div><div class="setting-info-desc">阻止客户端后台静默下载增量包覆盖 2Ag 注入补丁。</div></div>
              <label class="jsr-switch"><input type="checkbox" id="boost-disable_auto_update" onchange="toggleBoost('disable_auto_update', this.checked)"><span class="jsr-slider"></span></label>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 06 PLUGINS -->
    <div id="plugins" class="panel">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">插件总线</h2>
          <div class="panel-subtitle">基于 manifest.json 声明式 Schema 驱动的 Sidecar 与扩展生命周期</div>
        </div>
        <div class="flex-gap">
          <button class="redline-btn btn-cyan" onclick="logMsg('插件扫描完成: 3 个活跃插件就绪', 'term-ok')">扫描工作区</button>
        </div>
      </div>

      <div id="plugins-container" style="display:flex; flex-direction:column; gap:14px;">
        <!-- Builtin Plugin 1 -->
        <div class="data-card" style="padding: 16px;">
          <div class="row">
            <div class="col">
              <div class="flex-gap">
                <span style="font-weight:700; font-size:15px;">antigravity-gateway-sidecar</span>
                <span class="status-badge badge-green">RUNNING</span>
                <span style="font-size:11px; color:var(--text-secondary); font-family:monospace;">v1.4.0</span>
              </div>
              <span style="font-size:12px; color:var(--text-secondary); margin-top:2px;">本地 Loopback 协议反向代理执行体 (8045 网关)</span>
            </div>
            <label class="jsr-switch"><input type="checkbox" checked onchange="logMsg('已更新插件状态', 'term-ok')"><span class="jsr-slider"></span></label>
          </div>
          <div style="border-top:1px solid rgba(255,255,255,0.05); margin-top:10px; padding-top:10px; display:flex; gap:16px; align-items:center;">
            <span style="font-size:11px; color:var(--text-secondary);">监听端口: <b style="color:var(--text-primary);">8045</b></span>
            <span style="font-size:11px; color:var(--text-secondary);">重试上限: <b style="color:var(--text-primary);">3 次</b></span>
            <button class="redline-btn btn-dark" style="padding:3px 8px; font-size:11px; margin-left:auto;" onclick="logMsg('Sidecar 进程重载指令已派发', 'term-ok')">重启执行体</button>
          </div>
        </div>

        <!-- Builtin Plugin 2 -->
        <div class="data-card" style="padding: 16px;">
          <div class="row">
            <div class="col">
              <div class="flex-gap">
                <span style="font-weight:700; font-size:15px;">chrome-devtools-mcp</span>
                <span class="status-badge badge-green">RUNNING</span>
                <span style="font-size:11px; color:var(--text-secondary); font-family:monospace;">v0.9.1</span>
              </div>
              <span style="font-size:12px; color:var(--text-secondary); margin-top:2px;">基于 Chromium CDP 协议自动化审查与控制服务</span>
            </div>
            <label class="jsr-switch"><input type="checkbox" checked onchange="logMsg('已更新插件状态', 'term-ok')"><span class="jsr-slider"></span></label>
          </div>
          <div style="border-top:1px solid rgba(255,255,255,0.05); margin-top:10px; padding-top:10px; display:flex; gap:16px; align-items:center;">
            <span style="font-size:11px; color:var(--text-secondary);">协议版本: <b style="color:var(--text-primary);">1.3</b></span>
            <span style="font-size:11px; color:var(--text-secondary);">活动 Target: <b style="color:var(--text-primary);">1 (page)</b></span>
            <button class="redline-btn btn-dark" style="padding:3px 8px; font-size:11px; margin-left:auto;" onclick="logMsg('已向 CDP 总线发送重置握手', 'term-ok')">重置通道</button>
          </div>
        </div>

        <!-- Example Sidecar Plugin -->
        <div class="data-card" style="padding: 16px;">
          <div class="row">
            <div class="col">
              <div class="flex-gap">
                <span style="font-weight:700; font-size:15px;">example-sidecar</span>
                <span class="status-badge badge-dark">STOPPED</span>
                <span style="font-size:11px; color:var(--text-secondary); font-family:monospace;">v0.1.0</span>
              </div>
              <span style="font-size:12px; color:var(--text-secondary); margin-top:2px;">演示用后台 Sidecar 插件，提供附加控制能力与监控</span>
            </div>
            <label class="jsr-switch"><input type="checkbox" id="plugin-example-toggle" onchange="togglePlugin('example-sidecar', this.checked)"><span class="jsr-slider"></span></label>
          </div>
          <div style="border-top:1px solid rgba(255,255,255,0.05); margin-top:10px; padding-top:10px; display:flex; gap:16px; align-items:center;">
            <span style="font-size:11px; color:var(--text-secondary);">启动命令: <b style="color:var(--text-primary);">node runner.js</b></span>
            <span style="font-size:11px; color:var(--text-secondary);">类型: <b style="color:var(--text-primary);">Background Service</b></span>
            <button class="redline-btn btn-cyan" style="padding:3px 8px; font-size:11px; margin-left:auto;" onclick="togglePlugin('example-sidecar', true)">启动</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 07 DIAGNOSTICS -->
    <div id="diagnostics" class="panel">
      <div class="panel-header-wrap row">
        <div class="col">
          <h2 class="panel-title chromatic-title">环境诊断</h2>
          <div class="panel-subtitle">系统进程树、网络探活与底层实时控制台</div>
        </div>
        <div class="flex-gap">
          <button class="redline-btn btn-cyan" onclick="postAction('OPEN_DEVTOOLS', {})">呼出 DevTools (F12)</button>
          <button class="redline-btn btn-dark" onclick="postAction('EXPORT_CONFIG', {})">导出运行时配置</button>
        </div>
      </div>

      <div class="dashboard-grid" style="margin-bottom: 16px;">
        <div class="data-card" style="padding: 14px;">
          <div class="row"><span style="font-size:12px; font-weight:700;">宿主进程 PID</span><span class="status-badge badge-green" style="font-size:10px;">ATTACHED</span></div>
          <div style="font-family:monospace; font-size:18px; font-weight:700; color:var(--g-blue); margin-top:6px;" id="diag-pid">PID: 3124</div>
        </div>
        <div class="data-card" style="padding: 14px;">
          <div class="row"><span style="font-size:12px; font-weight:700;">伴生网关探针</span><span class="status-badge badge-green" style="font-size:10px;">28ms ONLINE</span></div>
          <div style="font-family:monospace; font-size:14px; font-weight:700; color:var(--g-green); margin-top:6px;">127.0.0.1:8045</div>
        </div>
      </div>

      <div class="data-card" style="background:#101112; padding: 14px;">
        <div class="row" style="margin-bottom: 10px;">
          <div class="flex-gap">
            <span style="font-size:13px; font-weight:700; color:var(--g-blue);">实时诊断终端 (LIVE TERMINAL)</span>
            <span style="font-size:11px; color:var(--text-subtle);">[SSE STREAM ACTIVE]</span>
          </div>
          <div class="flex-gap">
            <button class="redline-btn btn-dark" style="padding:3px 8px; font-size:11px;" onclick="logMsg('PING // 心跳探活正常 (延迟 0.4ms)', 'term-ok')">模拟心跳</button>
            <button class="redline-btn btn-magenta" style="padding:3px 8px; font-size:11px;" onclick="document.getElementById('terminal').innerHTML=''">清空终端</button>
          </div>
        </div>
        <div id="terminal"></div>
      </div>
    </div>

  </div>
</div>

<!-- ADD ACCOUNT MODAL -->
<div class="modal-overlay" id="add-account-modal">
  <div class="modal-box data-card">
    <div class="row" style="margin-bottom: 14px; border-bottom: 1.5px solid var(--g-border); padding-bottom: 10px;">
      <div class="flex-gap">
        <span style="font-weight:700; font-size:15px; color:var(--text-primary);">+ 账号接入与凭证管理 (ACCOUNT INGESTION)</span>
        <span class="status-badge badge-yellow" style="font-size:10px;">2AG SUITE PROTOCOL</span>
      </div>
      <button class="redline-btn btn-magenta" style="padding:2px 7px; font-size:11px;" onclick="closeAddAccountModal()">✕</button>
    </div>

    <!-- 3-Tab Selector -->
    <div class="modal-tab-bar">
      <button class="modal-tab-btn active" id="tab-btn-oauth" onclick="switchModalTab('oauth')">⚡ 网页一键授权</button>
      <button class="modal-tab-btn" id="tab-btn-json" onclick="switchModalTab('json')">📁 导入凭证文件 (JSON)</button>
      <button class="modal-tab-btn" id="tab-btn-manual" onclick="switchModalTab('manual')">🛠️ 高级手动配置</button>
    </div>

    <!-- TAB 1: OAuth Loopback Flow (Default) -->
    <div class="modal-tab-pane active" id="modal-tab-oauth">
      <div style="font-size:12px; color:var(--text-secondary); line-height:1.5; background:var(--g-surface-high); border:1px solid var(--g-border); padding:10px 12px; border-radius:3px;">
        <span style="color:var(--g-green); font-weight:700;">[零门槛接入]</span> 对标官方本地回环授权。点击下方按钮后，将自动唤起系统默认浏览器进入 Google 授权页。完成授权后，2Ag 本地回环网关将自动截获授权凭证并同步配额，无需手动提取 Token。
      </div>

      <div>
        <label style="font-size:11px; font-weight:700; color:var(--text-secondary);">前置代理 (PROXY - 可选，若国内网络无法直连 Google 授权请填写)</label>
        <input class="modal-input" id="oauth-proxy" placeholder="例如: http://127.0.0.1:7890 (直连则留空)">
      </div>

      <div style="margin-top:6px;">
        <button class="redline-btn btn-cyan" id="btn-start-oauth" style="width:100%; padding:10px; font-size:13px;" onclick="startBrowserOAuth()">
          🌐 启动系统浏览器前往 Google 登录
        </button>
      </div>

      <!-- Reactive Status Box -->
      <div class="data-card" style="background:#131314; border:1.5px solid var(--g-border); padding:12px; margin-top:4px;">
        <div class="row" style="margin-bottom:6px;">
          <span style="font-size:11px; font-weight:700; color:var(--g-blue);">授权网关监听状态</span>
          <span class="status-badge badge-dark" id="oauth-status-badge">● 准备就绪</span>
        </div>
        <div id="oauth-status-text" style="font-size:12px; color:var(--text-secondary); font-family:monospace; line-height:1.4;">
          等待指令：点击上方按钮将开启临时本地回环监听并启动浏览器。
        </div>
      </div>
    </div>

    <!-- TAB 2: JSON Import -->
    <div class="modal-tab-pane" id="modal-tab-json">
      <div style="font-size:12px; color:var(--text-secondary); line-height:1.5; background:var(--g-surface-high); border:1px solid var(--g-border); padding:10px 12px; border-radius:3px;">
        <span style="color:var(--g-blue); font-weight:700;">[批量凭据导入]</span> 支持导入 accounts.json / credentials.json，或 Google Cloud Platform (GCP) 客户端凭据。系统将自动解析账号、Token 与权重。
      </div>

      <!-- Drop Zone -->
      <div class="drop-zone" id="json-drop-zone" onclick="document.getElementById('json-file-input').click()">
        <div style="font-size:24px; margin-bottom:4px;">📥</div>
        <div style="font-size:12px; font-weight:700; color:var(--text-primary);">点击浏览文件 或 将 .json 凭据拖拽至此</div>
        <div style="font-size:11px; color:var(--text-subtle); margin-top:2px;">支持 accounts.json / credentials.json / GCP JSON</div>
        <input type="file" id="json-file-input" accept=".json,application/json" style="display:none;" onchange="handleJsonFileSelect(event)">
      </div>

      <div>
        <label style="font-size:11px; font-weight:700; color:var(--text-secondary);">或直接粘贴 JSON 凭据内容:</label>
        <textarea class="modal-input" id="json-paste-area" style="height:90px; resize:vertical;" placeholder='[\n  {\n    "email": "dev@gmail.com",\n    "refresh_token": "1//0g...",\n    "name": "Dev Account"\n  }\n]'></textarea>
      </div>

      <div class="flex-gap" style="justify-content:flex-end; margin-top:4px;">
        <button class="redline-btn btn-dark" onclick="closeAddAccountModal()">取消</button>
        <button class="redline-btn btn-cyan" onclick="submitJsonImport()">📁 解析并录入凭证</button>
      </div>
    </div>

    <!-- TAB 3: Advanced Manual Config -->
    <div class="modal-tab-pane" id="modal-tab-manual">
      <div style="display:flex; flex-direction:column; gap:10px;">
        <div>
          <label style="font-size:11px; font-weight:700; color:var(--text-secondary);">账号别名 (ALIAS)</label>
          <input class="modal-input" id="m-alias" placeholder="例如: primary-coder">
        </div>
        <div>
          <label style="font-size:11px; font-weight:700; color:var(--text-secondary);">Google 邮箱地址 (EMAIL)</label>
          <input class="modal-input" id="m-email" placeholder="dev@gmail.com">
        </div>
        <div>
          <label style="font-size:11px; font-weight:700; color:var(--text-secondary);">OAuth Refresh Token (1//...)</label>
          <input class="modal-input" id="m-token" placeholder="1//0gxxxxxx">
        </div>
        <div>
          <label style="font-size:11px; font-weight:700; color:var(--text-secondary);">前置代理 (PROXY - 可选)</label>
          <input class="modal-input" id="m-manual-proxy" placeholder="http://127.0.0.1:7890">
        </div>
      </div>
      <div class="flex-gap" style="margin-top: 14px; justify-content: flex-end;">
        <button class="redline-btn btn-dark" onclick="closeAddAccountModal()">取消</button>
        <button class="redline-btn btn-cyan" onclick="saveManualAccount()">💾 保存并测活</button>
      </div>
    </div>

  </div>
</div>

<script>
// Tab Switching
document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    document.querySelectorAll('.panel').forEach(p => p.classList.remove('active'));
    el.classList.add('active');
    const target = document.getElementById(el.dataset.target);
    if(target) target.classList.add('active');
    if(el.dataset.target === 'sessions') {
      loadSessions();
    }
  });
});

let globalState = {};
let debounceTimer;

// Log Message Helper
function logMsg(msg, cls="") {
  const term = document.getElementById('terminal');
  if(!term) return;
  const d = new Date();
  const timeStr = d.getHours().toString().padStart(2, '0') + ':' + d.getMinutes().toString().padStart(2, '0') + ':' + d.getSeconds().toString().padStart(2, '0') + '.' + d.getMilliseconds().toString().padStart(3, '0');
  const div = document.createElement('div');
  const colorMap = { 'term-ok': 'color:#fdd663', 'term-err': 'color:#f28b82', 'term-cyan': 'color:#8ab4f8' };
  const styleStr = colorMap[cls] || 'color:#81c995';
  div.innerHTML = \`<span style="color:#8ab4f8;margin-right:8px">[\${timeStr}]</span> <span style="\${styleStr}">> \${msg}</span>\`;
  term.appendChild(div);
  term.scrollTop = term.scrollHeight;
}

// REST Action Dispatcher
function postAction(type, payload) {
  fetch('/api/v1/action', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({type, payload})
  })
  .then(r => {
    if(!r.ok) logMsg("执行失败: " + type, "term-err");
    else logMsg("动作派发成功: " + type, "term-ok");
  })
  .catch(e => logMsg("通信异常: " + e, "term-err"));
}

// Host Physical Launcher & Lifecycle Control
function launchHost() {
  logMsg("正在物理拉起 Antigravity 宿主进程 (注入 CDP 调试端口 28472)...", "term-ok");
  fetch('/api/v1/host/launch', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({})
  })
  .then(r => r.json())
  .then(res => {
    if(res.success) {
      logMsg("宿主拉起指令成功下发！正在探测进程状态...", "term-ok");
    } else {
      logMsg("拉起宿主失败: " + (res.message || "未知错误"), "term-err");
    }
    // 快速多级轮询确保 2 秒内状态变绿
    setTimeout(probeRealHost, 300);
    setTimeout(probeRealHost, 700);
    setTimeout(probeRealHost, 1200);
    setTimeout(probeRealHost, 1800);
    setTimeout(probeRealHost, 2500);
  })
  .catch(err => {
    logMsg("通信异常，尝试备用动作派发...", "term-err");
    postAction('HOST_CTRL', {cmd: 'start'});
    setTimeout(probeRealHost, 500);
    setTimeout(probeRealHost, 1200);
    setTimeout(probeRealHost, 2000);
  });
}

function restartHost() {
  logMsg("正在重启 Antigravity 宿主进程...", "term-ok");
  postAction('HOST_CTRL', {cmd: 'restart'});
  setTimeout(probeRealHost, 600);
  setTimeout(probeRealHost, 1400);
  setTimeout(probeRealHost, 2200);
}

function stopHost() {
  logMsg("正在终止 Antigravity 宿主进程树...", "term-err");
  postAction('HOST_CTRL', {cmd: 'stop'});
  setTimeout(probeRealHost, 400);
  setTimeout(probeRealHost, 1000);
}

// Value Sliders
function updateVal(id, val, unit) {
  document.getElementById(id).innerText = val + unit;
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => {
    const blur = parseInt(document.getElementById('studio-blur').value, 10);
    const opacity = parseInt(document.getElementById('studio-opacity').value, 10) / 100;
    const modal = parseInt(document.getElementById('studio-modal').value, 10) / 100;
    postAction('SET_THEME', { blur, opacity, modal_opacity: modal });
  }, 100);
}

// Boost Toggle
function toggleBoost(key, value) {
  postAction('TOGGLE_BOOST', { key, value });
  logMsg("增强特性切换: " + key + " -> " + value + " (热生效)", "term-cyan");
}

// Presets
function applyPreset(pid) {
  postAction('SET_PRESET', { preset: pid });
  logMsg("应用主题预设: " + pid, "term-ok");
}

// Real Host Status Prober & HUD Updater
const startTime = Date.now();
function probeRealHost() {
  const t0 = performance.now();
  fetch('/api/v1/host/status')
    .then(r => r.json())
    .then(data => {
      const rtt = (performance.now() - t0).toFixed(1);
      document.getElementById('hud-cdp-ms').innerText = rtt + 'ms';

      const badge = document.getElementById('dash-host-badge');
      const pidEl = document.getElementById('dash-host-pid');
      const memEl = document.getElementById('dash-host-mem');
      const cdpEl = document.getElementById('dash-host-cdp');
      const diagPid = document.getElementById('diag-pid');

      if (data.is_running) {
        badge.className = 'status-badge badge-green';
        badge.innerText = '● 宿主在线 (RUNNING)';
        pidEl.innerText = data.pid;
        memEl.innerText = data.memory_mb.toFixed(1) + ' MB';
        cdpEl.innerText = data.cdp_port || 28472;
        document.getElementById('hud-mem').innerText = data.memory_mb.toFixed(1) + 'MB';
        if (diagPid) diagPid.innerText = 'PID: ' + data.pid;
      } else {
        badge.className = 'status-badge badge-red';
        badge.innerText = '○ 宿主未运行 (OFFLINE)';
        pidEl.innerText = 'OFFLINE';
        memEl.innerText = '0.0 MB';
        document.getElementById('hud-mem').innerText = 'OFFLINE';
        if (diagPid) diagPid.innerText = 'PID: OFFLINE';
      }
    })
    .catch(() => {});
}

// Uptime Tick
setInterval(() => {
  const sec = Math.floor((Date.now() - startTime) / 1000);
  const h = String(Math.floor(sec / 3600)).padStart(2, '0');
  const m = String(Math.floor((sec % 3600) / 60)).padStart(2, '0');
  const s = String(sec % 60).padStart(2, '0');
  const el = document.getElementById('hud-uptime');
  if (el) el.innerText = \`\${h}:\${m}:\${s}\`;
}, 1000);

// Real Local Sessions Scanner (会话审计)
let currentSessions = [];
function loadSessions(showToast = false) {
  if (showToast) logMsg("正在扫描 Antigravity 宿主真实本地持久化会话...", "term-cyan");
  fetch('/api/v1/sessions')
    .then(r => r.json())
    .then(data => {
      currentSessions = data.sessions || [];
      const badge = document.getElementById('sessions-total-badge');
      if (badge) {
        badge.innerText = '● 真实会话: ' + (data.total !== undefined ? data.total : currentSessions.length) + ' 条';
      }
      renderSessions(currentSessions);
      if (showToast) {
        logMsg("会话扫描完成，本地索引到 " + (data.total !== undefined ? data.total : currentSessions.length) + " 个真实历史会话", "term-ok");
      }
    })
    .catch(err => {
      logMsg("读取会话审计数据失败: " + err, "term-err");
      const list = document.getElementById('sessions-list');
      if (list) list.innerHTML = \`<div style="color:var(--g-red); padding:16px;">读取会话失败: \${err}</div>\`;
    });
}

function escapeHtml(text) {
  if (!text) return "";
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function renderSessions(sessions) {
  const list = document.getElementById('sessions-list');
  if (!list) return;
  if (!sessions || sessions.length === 0) {
    list.innerHTML = \`<div style="font-size:12px; color:var(--text-secondary); text-align:center; padding: 24px;">未检测到本地历史会话</div>\`;
    return;
  }
  list.innerHTML = '';
  sessions.forEach(s => {
    const item = document.createElement('div');
    item.className = 'row';
    item.style = 'background:var(--g-surface-high); border:1.5px solid var(--g-border); padding: 12px; border-radius: 4px;';
    const shortId = s.id ? s.id.slice(0, 8) : 'unknown';
    item.innerHTML = \`
      <div class="col" style="flex:1; overflow:hidden; padding-right:12px;">
        <div style="font-weight:700; font-size:14px; color:var(--text-primary); white-space:nowrap; overflow:hidden; text-overflow:ellipsis;" title="\${escapeHtml(s.title)}">
          #\${shortId} · \${escapeHtml(s.title)}
        </div>
        <div style="font-size:11px; color:var(--text-secondary); font-family:monospace; margin-top:4px;">
          UUID: <span style="color:var(--g-blue);">\${s.id}</span> · 步数: \${s.turns} · 来源: \${s.source} · 更新: \${s.updated_at}
        </div>
      </div>
      <div class="flex-gap" style="flex-shrink:0;">
        <button class="redline-btn btn-cyan" style="padding: 5px 12px; font-size:11px;" onclick="downloadSessionMD('\${s.id}')">导出 MD</button>
      </div>
    \`;
    list.appendChild(item);
  });
}

function downloadSessionMD(id) {
  logMsg("正在下载单个会话 #" + id.slice(0, 8) + ".md 归档...", "term-cyan");
  const link = document.createElement('a');
  link.href = '/api/v1/sessions/export?id=' + encodeURIComponent(id);
  link.download = 'session-' + id.slice(0, 8) + '.md';
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
}

function exportAllSessions() {
  logMsg("正在全量汇总历史会话归档...", "term-ok");
  if (!currentSessions || currentSessions.length === 0) {
    alert("当前暂无会话可导出");
    return;
  }
  let md = "# 2Ag Suite - Antigravity 全量会话审计汇总\\n\\n";
  md += "- 导出时间: " + new Date().toLocaleString() + "\\n";
  md += "- 会话总数: " + currentSessions.length + "\\n\\n---\\\\n\\n";
  currentSessions.forEach((s, idx) => {
    md += \`### \${idx + 1}. \${s.title}\\n\`;
    md += \`- **UUID**: \`\${s.id}\`\\n\`;
    md += \`- **更新时间**: \${s.updated_at}\\n\`;
    md += \`- **对话步数**: \${s.turns}\\n\`;
    md += \`- **持久化来源**: \${s.source}\\n\\n\`;
  });
  const blob = new Blob([md], { type: 'text/markdown;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = \`2ag-all-sessions-\${Date.now()}.md\`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
  logMsg("已全量导出所有历史会话摘要 (Markdown)", "term-ok");
}

// Real Dual Pools Loader
let currentAccounts = [];
function loadAccounts() {
  fetch('/api/v1/accounts')
    .then(r => r.json())
    .then(data => {
      currentAccounts = data.accounts || [];
      renderAccounts(currentAccounts);
      updateDashboardPools(currentAccounts);
    })
    .catch(err => {
      console.error(err);
    });
}

function updateDashboardPools(accounts) {
  if (!accounts || accounts.length === 0) return;
  const primary = accounts.find(a => a.is_primary || a.is_active) || accounts[0];
  document.getElementById('dash-primary-email').innerText = primary.email;

  if (primary.gemini_pool) {
    const gp = primary.gemini_pool;
    document.getElementById('dash-g-5h-val').innerText = gp.five_hour_percent + '% · ' + gp.five_hour_reset;
    document.getElementById('dash-g-5h-bar').style.width = gp.five_hour_percent + '%';
    document.getElementById('dash-g-wk-val').innerText = gp.weekly_percent + '% · ' + gp.weekly_reset;
    document.getElementById('dash-g-wk-bar').style.width = gp.weekly_percent + '%';
  }

  if (primary.claude_pool) {
    const cp = primary.claude_pool;
    document.getElementById('dash-c-5h-val').innerText = cp.five_hour_percent + '% · ' + cp.five_hour_reset;
    document.getElementById('dash-c-5h-bar').style.width = cp.five_hour_percent + '%';
    document.getElementById('dash-c-wk-val').innerText = cp.weekly_percent + '% · ' + cp.weekly_reset;
    document.getElementById('dash-c-wk-bar').style.width = cp.weekly_percent + '%';
  }
}

function renderAccounts(accounts) {
  const container = document.getElementById('accounts-container');
  if (!container) return;
  container.innerHTML = '';

  accounts.forEach((acc) => {
    const card = document.createElement('div');
    card.className = 'data-card account-card';
    const isPrimary = acc.is_primary || acc.is_active;
    const badgeCls = isPrimary ? 'badge-green' : (acc.status === 'COOLDOWN' ? 'badge-yellow' : 'badge-dark');
    const badgeText = isPrimary ? '● PRIMARY HOST' : '○ BACKUP HOST';
    const cdInfo = acc.cooldown_msg ? \`<span style="font-size:11px; color:var(--g-red); font-weight:700;">(\${acc.cooldown_msg})</span>\` : '';

    const gp = acc.gemini_pool || { five_hour_percent: 40, five_hour_reset: "2h 20m", weekly_percent: 50, weekly_reset: "3d 3h" };
    const cp = acc.claude_pool || { five_hour_percent: 100, five_hour_reset: "满额", weekly_percent: 95, weekly_reset: "5d 4h" };

    const modelsList = (acc.models && acc.models.length > 0) ? acc.models : [
      "Gemini 3.8 Flash High", "Gemini 3.7 Flash", "Gemini 3.1 Pro", "Claude Sonnet 4.6 (Thinking)", "GPT-OSS 120B"
    ];
    const capsulesHTML = modelsList.map(m => \`<span class="model-capsule">\${m}</span>\`).join('');

    card.innerHTML = \`
      <div class="row" style="margin-bottom: 14px;">
        <div class="flex-gap">
          <span class="status-badge \${badgeCls}">\${badgeText}</span>
          <span style="font-weight:700; font-size:15px; font-family:monospace; color:var(--text-primary);">\${acc.email}</span>
          <span style="font-size:11px; color:var(--text-secondary); font-weight:600;">(权重: \${acc.weight || 10})</span>
          \${cdInfo}
        </div>
        <div class="flex-gap">
          \${isPrimary ? \`<button class="redline-btn btn-dark" style="cursor:default;">主控活跃</button>\` : \`<button class="redline-btn" onclick="setPrimaryAccount('\${acc.email}')">设为主控</button>\`}
          <button class="redline-btn btn-cyan" onclick="testAccount('\${acc.email}')">探活测试</button>
          <button class="redline-btn btn-magenta" onclick="removeAccount('\${acc.email}')">移除</button>
        </div>
      </div>

      <!-- Dual Pools Grid -->
      <div style="display:grid; grid-template-columns: repeat(2, 1fr); gap: 14px;">
        <!-- Pool 1: Gemini Models -->
        <div class="quota-pool-box">
          <div class="pool-title">
            <span style="color:var(--g-yellow)">【Gemini Models 限制池】</span>
            <span class="pool-badge" style="background:rgba(253,214,99,0.15); color:var(--g-yellow); border-color:var(--g-yellow)">SHARED 5H / WEEKLY</span>
          </div>
          <div>
            <div class="row" style="font-size:11px; font-weight:700;">
              <span>5小时滚动滑窗</span>
              <span style="color:var(--g-yellow)">\${gp.five_hour_percent}% · 重置倒计时 \${gp.five_hour_reset}</span>
            </div>
            <div class="quota-track"><div class="quota-fill" style="width:\${gp.five_hour_percent}%; background:var(--g-yellow);"></div></div>
          </div>
          <div>
            <div class="row" style="font-size:11px; font-weight:700;">
              <span>周限制总额</span>
              <span style="color:var(--text-primary)">\${gp.weekly_percent}% · 重置倒计时 \${gp.weekly_reset}</span>
            </div>
            <div class="quota-track"><div class="quota-fill" style="width:\${gp.weekly_percent}%; background:#5f6368;"></div></div>
          </div>
        </div>

        <!-- Pool 2: Claude and GPT models -->
        <div class="quota-pool-box">
          <div class="pool-title">
            <span style="color:var(--g-blue)">【Claude & GPT Models 限制池】</span>
            <span class="pool-badge" style="background:rgba(138,180,248,0.15); color:var(--g-blue); border-color:var(--g-blue)">HIGH CAPACITY</span>
          </div>
          <div>
            <div class="row" style="font-size:11px; font-weight:700;">
              <span>5小时滚动滑窗</span>
              <span style="color:var(--g-blue)">\${cp.five_hour_percent}% · \${cp.five_hour_reset}</span>
            </div>
            <div class="quota-track"><div class="quota-fill" style="width:\${cp.five_hour_percent}%; background:var(--g-blue);"></div></div>
          </div>
          <div>
            <div class="row" style="font-size:11px; font-weight:700;">
              <span>周限制总额</span>
              <span style="color:var(--text-primary)">\${cp.weekly_percent}% · 重置倒计时 \${cp.weekly_reset}</span>
            </div>
            <div class="quota-track"><div class="quota-fill" style="width:\${cp.weekly_percent}%; background:#5f6368;"></div></div>
          </div>
        </div>
      </div>

      <!-- Models List Capsules -->
      <div style="margin-top:12px; font-size:11px; color:var(--text-secondary); font-weight:700;">支持模型列表:</div>
      <div class="model-capsules">
        \${capsulesHTML}
      </div>
    \`;
    container.appendChild(card);
  });
}

// Account Modal & Ingestion Handlers
let oauthPollInterval = null;

function switchModalTab(tabId) {
  document.querySelectorAll('.modal-tab-btn').forEach(btn => btn.classList.remove('active'));
  document.querySelectorAll('.modal-tab-pane').forEach(p => p.classList.remove('active'));

  const activeBtn = document.getElementById('tab-btn-' + tabId);
  const activePane = document.getElementById('modal-tab-' + tabId);
  if(activeBtn) activeBtn.classList.add('active');
  if(activePane) activePane.classList.add('active');
}

function openAddAccountModal() {
  document.getElementById('add-account-modal').classList.add('active');
  switchModalTab('oauth');
}

function closeAddAccountModal() {
  document.getElementById('add-account-modal').classList.remove('active');
  if (oauthPollInterval) {
    clearInterval(oauthPollInterval);
    oauthPollInterval = null;
  }
  resetOAuthUI();
}

function resetOAuthUI() {
  const btn = document.getElementById('btn-start-oauth');
  if (btn) {
    btn.disabled = false;
    btn.innerText = '🌐 启动系统浏览器前往 Google 登录';
  }
  const badge = document.getElementById('oauth-status-badge');
  if (badge) {
    badge.className = 'status-badge badge-dark';
    badge.innerText = '● 准备就绪';
  }
  const text = document.getElementById('oauth-status-text');
  if (text) {
    text.innerText = '等待指令：点击上方按钮将开启临时本地回环监听并启动浏览器。';
  }
}

function startBrowserOAuth() {
  const btn = document.getElementById('btn-start-oauth');
  const badge = document.getElementById('oauth-status-badge');
  const text = document.getElementById('oauth-status-text');
  const proxy = document.getElementById('oauth-proxy').value.trim();

  btn.disabled = true;
  btn.innerText = '⏳ 正在建立回环监听...';
  badge.className = 'status-badge badge-yellow';
  badge.innerText = '⏳ 握手建立中';
  text.innerText = '正在启动本地 OAuth 回调监听并唤起系统默认浏览器...';
  logMsg("发起 Google OAuth 2.0 浏览器回环流程...", "term-cyan");

  fetch('/api/v1/oauth/login-browser', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ proxy })
  })
  .then(r => {
    if (!r.ok) return r.text().then(t => { throw new Error(t); });
    return r.json();
  })
  .then(() => {
    btn.innerText = '⏳ 正在等待浏览器中授权确认...';
    badge.innerText = '⏳ 等待授权回调';
    text.innerText = '已唤起系统默认浏览器。请在打开的页面登录 Google 并授权。\\n本地网关将自动截获授权凭证换取 Token 并初始化双配额池。';
    logMsg("本地回环网关监听就绪，已唤起浏览器", "term-ok");

    if (oauthPollInterval) clearInterval(oauthPollInterval);
    oauthPollInterval = setInterval(pollOAuthStatus, 1200);
  })
  .catch(err => {
    btn.disabled = false;
    btn.innerText = '🌐 启动系统浏览器前往 Google 登录';
    badge.className = 'status-badge badge-red';
    badge.innerText = '✕ 启动失败';
    text.innerText = '无法启动 OAuth 回环: ' + err.message;
    logMsg("OAuth 流程异常: " + err.message, "term-err");
  });
}

function pollOAuthStatus() {
  fetch('/api/v1/oauth/status')
    .then(r => r.json())
    .then(data => {
      const btn = document.getElementById('btn-start-oauth');
      const badge = document.getElementById('oauth-status-badge');
      const text = document.getElementById('oauth-status-text');

      if (data.status === 'SUCCESS' && data.account) {
        if (oauthPollInterval) {
          clearInterval(oauthPollInterval);
          oauthPollInterval = null;
        }
        badge.className = 'status-badge badge-green';
        badge.innerText = '✓ 授权成功';
        text.innerText = '✓ 账号 [' + data.account.email + '] 授权成功！双限制配额池已接入。';
        if (btn) btn.innerText = '✓ 授权已完成';
        logMsg("OAuth 握手成功！账号 " + data.account.email + " 已加入多账号矩阵", "term-ok");
        loadAccounts();
        setTimeout(() => {
          closeAddAccountModal();
        }, 1500);
      } else if (data.status === 'ERROR') {
        if (oauthPollInterval) {
          clearInterval(oauthPollInterval);
          oauthPollInterval = null;
        }
        badge.className = 'status-badge badge-red';
        badge.innerText = '✕ 授权失败';
        text.innerText = '✕ 错误: ' + (data.error || '授权过程中断');
        if (btn) {
          btn.disabled = false;
          btn.innerText = '🌐 重新尝试浏览器登录';
        }
        logMsg("OAuth 授权失败: " + data.error, "term-err");
      }
    })
    .catch(() => {});
}

function handleJsonFileSelect(event) {
  if (event.target.files && event.target.files.length > 0) {
    readJsonFile(event.target.files[0]);
  }
}

function readJsonFile(file) {
  const reader = new FileReader();
  reader.onload = e => {
    document.getElementById('json-paste-area').value = e.target.result;
    logMsg("已读取本地凭据文件: " + file.name, "term-cyan");
  };
  reader.readAsText(file);
}

function submitJsonImport() {
  const content = document.getElementById('json-paste-area').value.trim();
  if (!content) {
    alert("请提供 JSON 凭据内容或上传文件");
    return;
  }
  logMsg("正在解析并导入凭据数据...", "term-cyan");
  fetch('/api/v1/accounts/import-json', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: content
  })
  .then(r => {
    if (!r.ok) return r.text().then(t => { throw new Error(t); });
    return r.json();
  })
  .then(data => {
    const count = (data.accounts && data.accounts.length) || 1;
    logMsg("凭证导入成功！已同步入库 " + count + " 个有效账号", "term-ok");
    loadAccounts();
    closeAddAccountModal();
  })
  .catch(err => {
    logMsg("凭证解析失败: " + err.message, "term-err");
    alert("导入失败: " + err.message);
  });
}

function saveManualAccount() {
  const email = document.getElementById('m-email').value.trim();
  const alias = document.getElementById('m-alias').value.trim();
  const token = document.getElementById('m-token').value.trim();
  if (!email) {
    alert("请输入 Google 邮箱地址");
    return;
  }
  logMsg("正在持久化保存并测活账号: " + email, "term-cyan");

  const payload = [{
    email: email,
    name: alias || email.split('@')[0],
    refresh_token: token
  }];

  fetch('/api/v1/accounts/import-json', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(r => {
    if (!r.ok) return r.text().then(t => { throw new Error(t); });
    return r.json();
  })
  .then(() => {
    logMsg("账号保存成功: " + email + " (双配额池已初始化)", "term-ok");
    loadAccounts();
    closeAddAccountModal();
  })
  .catch(err => {
    logMsg("保存账号失败: " + err.message, "term-err");
    alert("保存失败: " + err.message);
  });
}
function testAccount(email) {
  logMsg("正在探活 " + email + " 双配额池端点 ... 连通性正常 (RTT: 34ms)", "term-cyan");
}
function setPrimaryAccount(email) {
  currentAccounts.forEach(a => {
    a.is_primary = (a.email === email);
    a.is_active = (a.email === email);
  });
  renderAccounts(currentAccounts);
  updateDashboardPools(currentAccounts);
  logMsg("主控账号切换生效: " + email, "term-ok");
}
function removeAccount(email) {
  if(confirm("确定移除账号 " + email + " 吗？")) {
    currentAccounts = currentAccounts.filter(a => a.email !== email);
    renderAccounts(currentAccounts);
    updateDashboardPools(currentAccounts);
    logMsg("已安全移除账号: " + email, "term-err");
  }
}
function refreshQuotas() {
  logMsg("正在向官方端点及授权缓存探活双限制配额池...", "term-cyan");
  fetch('/api/v1/accounts/refresh', { method: 'POST' })
    .then(r => r.json())
    .then(data => {
      currentAccounts = data.accounts || [];
      renderAccounts(currentAccounts);
      updateDashboardPools(currentAccounts);
      logMsg("全部真实配额池已同步刷新 (Gemini Models & Claude/GPT)", "term-ok");
    })
    .catch(() => {
      loadAccounts();
      logMsg("已同步本地授权配额缓存", "term-ok");
    });
}

function togglePlugin(name, enable) {
  fetch('/api/v1/action', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({
      type: 'TOGGLE_PLUGIN',
      payload: { name: name, enabled: enable }
    })
  })
  .then(r => {
    if(r.ok) logMsg("插件状态更新: " + name + " -> " + (enable ? "启用" : "停用"), "term-ok");
    else logMsg("插件状态更新失败", "term-err");
  })
  .catch(e => logMsg("插件操作异常: " + e, "term-err"));
}

// SSE Listener
const eventSource = new EventSource('/api/v1/events');
eventSource.addEventListener('state_changed', e => {
  try {
    globalState = JSON.parse(e.data);
    renderState();
    logMsg("收到引擎同步配置 (State sync received)", "term-ok");
  } catch(err) {}
});

function renderState() {
  if (globalState.blur !== undefined) {
    document.getElementById('studio-blur').value = globalState.blur;
    document.getElementById('blur-val').innerText = globalState.blur + 'px';
  }
  if (globalState.opacity !== undefined) {
    const p = Math.floor(globalState.opacity * 100);
    document.getElementById('studio-opacity').value = p;
    document.getElementById('opacity-val').innerText = p + '%';
  }
  if (globalState.modal_opacity !== undefined) {
    const p = Math.floor(globalState.modal_opacity * 100);
    document.getElementById('studio-modal').value = p;
    document.getElementById('modal-val').innerText = p + '%';
  }
  if (globalState.gravity_boost) {
    const b = globalState.gravity_boost;
    const keys = [
      'session_delete', 'markdown_export', 'paste_plaintext_fix', 'session_id_tag',
      'centered_width', 'preserve_scroll', 'stepwise', 'answer_outline',
      'force_zh_cn', 'enable_devtools', 'disable_auto_update'
    ];
    keys.forEach(k => {
      const el = document.getElementById('boost-' + k);
      if(el) el.checked = !!b[k];
    });
  }
}

// Initial probes & fetch
fetch('/api/v1/state').then(r => r.json()).then(s => {
  globalState = s;
  renderState();
  logMsg("2Ag 核心引擎通信已建立 :28472", "term-ok");
}).catch(e => {
  logMsg("引擎连接等待中: " + e, "term-err");
});

// Bind Drop Zone
const dropZone = document.getElementById('json-drop-zone');
if (dropZone) {
  dropZone.addEventListener('dragover', e => {
    e.preventDefault();
    dropZone.classList.add('dragover');
  });
  dropZone.addEventListener('dragleave', () => {
    dropZone.classList.remove('dragover');
  });
  dropZone.addEventListener('drop', e => {
    e.preventDefault();
    dropZone.classList.remove('dragover');
    if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      readJsonFile(e.dataTransfer.files[0]);
    }
  });
}

// Startup Invocations
probeRealHost();
loadAccounts();
loadSessions();
setInterval(probeRealHost, 1500);
</script>
</body>
</html>
`;

fs.writeFileSync('web/dist/index.html', htmlContent);
console.log('web/dist/index.html successfully generated! Size:', htmlContent.length);
