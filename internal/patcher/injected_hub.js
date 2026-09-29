(() => {
  'use strict';

  const INITIAL_CONFIG = __2AG_INITIAL_CONFIG__;
  const BG_ID = '2ag-dream-skin-bg';
  const OVERLAY_ID = '2ag-dream-skin-overlay';
  const STYLE_ID = '2ag-penetrate-css';
  const HUB_ID = '2ag-hub-root';
  const DRAWER_ID = '2ag-hub-drawer';
  const STORAGE_KEY = '2ag.skin.config.v1';
  const OBSERVER_KEY = '__2ag_hub_observer';
  const TIMER_KEY = '__2ag_hub_timer';
  const DICT = {
    'en-US': {
      capsule: '2Ag', console: '2Ag Console', close: 'Close', skin: 'Appearance', network: 'Network', rules: 'Global Rules', plugins: 'Extensions', diagnostics: 'Diagnostics',
      dreamSkin: 'Dream Skin', blur: 'Blur', darkness: 'Darkness', wallpaper: 'Wallpaper', chooseImage: 'Choose local image', imagePlaceholder: 'Image URL or file:/// path', localImageHint: 'The selected image is stored in this profile.', presets: 'Presets',
      darkDream: 'Dark Dream', cyberpunk: 'Cyberpunk', cleanGlass: 'Clean Glass', networkModel: 'Network & Model', hostNotLoaded: 'Host diagnostics not loaded', proxyNotLoaded: 'Proxy status not loaded', proxyDisabled: 'Proxy disabled in 2ag.json', readingProxy: 'Reading local proxy status…', proxyActive: 'Proxy active', proxyUnavailable: 'Proxy status unavailable', requests: 'requests', blocked: 'blocked', last: 'last', localImageSelected: 'Local image selected', officialEndpoints: 'Official endpoints (no overrides)', httpHint: 'HTTP requests can use endpoint overrides. HTTPS CONNECT remains opaque and is forwarded without TLS interception.',
      testGateway: 'Test gateway', probeRunning: 'Testing gateway…', probeOK: 'Gateway online', probeFailed: 'Gateway unavailable', rulesPlaceholder: 'Rules applied to configured JSON endpoints', saveRules: 'Save rules', rulesHint: 'Rules are persisted in this browser profile; HTTP JSON rewriting requires a matching target.', extensionsSidecar: 'Extensions & Sidecar', noPlugins: 'No plugins discovered', pluginRunning: 'Running', pluginStopped: 'Stopped', diagnosticsTitle: 'Diagnostics', openDevtools: 'Open DevTools (F12)', hotReload: 'Reload skin', resetTheme: 'Reset theme', exportConfig: 'Export config', diagHint: 'Use F12, Ctrl+Shift+I, Alt+A, or Ctrl+Shift+A while debugging the host.', hostPID: 'Host PID', cdp: 'CDP', env: 'env', language: 'Language', saved: 'Saved', reset: 'Theme reset', exported: 'Config exported', hotReloaded: 'Skin reloaded',
      modalHint: 'Settings and dialogs stay isolated from the transparent workspace.'
    },
    'zh-CN': {
      capsule: '2Ag', console: '2Ag 控制台', close: '关闭', skin: '外观', network: '网络端点', rules: '全局规则', plugins: '扩展插件', diagnostics: '系统诊断',
      dreamSkin: 'Dream Skin', blur: '毛玻璃模糊度', darkness: '遮罩暗度', wallpaper: '壁纸', chooseImage: '选择本地图片', imagePlaceholder: '图片 URL 或 file:/// 路径', localImageHint: '所选图片会保存到当前配置档。', presets: '主题预设',
      darkDream: '暗夜梦境', cyberpunk: '赛博朋克', cleanGlass: '清透玻璃', networkModel: '网络与模型', hostNotLoaded: '宿主诊断尚未加载', proxyNotLoaded: '代理状态尚未加载', proxyDisabled: '2ag.json 中未启用本地代理', readingProxy: '正在读取本地代理状态…', proxyActive: '代理运行中', proxyUnavailable: '代理状态不可用', requests: '请求', blocked: '已阻断', last: '最近', localImageSelected: '已选择本地图片', officialEndpoints: '官方端点（无重定向）', httpHint: 'HTTP 请求支持端点重定向；HTTPS CONNECT 保持透明转发，不解密 TLS。',
      testGateway: '测试网关', probeRunning: '正在测试网关…', probeOK: '网关在线', probeFailed: '网关不可用', rulesPlaceholder: '应用到已配置 JSON 端点的规则', saveRules: '保存规则', rulesHint: '规则保存在当前浏览器配置中；只有匹配的目标才会改写 HTTP JSON。', extensionsSidecar: '扩展与伴生进程', noPlugins: '未发现插件', pluginRunning: '运行中', pluginStopped: '已停止', diagnosticsTitle: '系统诊断', openDevtools: '打开开发者工具（F12）', hotReload: '重新加载皮肤', resetTheme: '恢复默认主题', exportConfig: '导出配置', diagHint: '调试宿主时可使用 F12、Ctrl+Shift+I、Alt+A 或 Ctrl+Shift+A。', hostPID: '宿主 PID', cdp: 'CDP', env: '环境', language: '语言', saved: '已保存', reset: '主题已重置', exported: '配置已导出', hotReloaded: '皮肤已重载',
      modalHint: '设置与弹窗使用实体磨砂层，与透明工作区严格隔离。'
    }
  };
  const ipcPending = new Map();
  let ipcSequence = 0;
  const extensionTabs = new Map();
  const loadedPluginScripts = new Set();

  const state = Object.assign({
    wallpaper: '',
    blur: 20,
    opacity: 0.55,
    preset: 'Dark Dream',
    plugins: {  },
    network: {},
    global_rules: '',
    language: 'zh-CN'
  }, INITIAL_CONFIG || {});
  state.plugins = Object.assign({  }, state.plugins || {});

  const HUB_CSS = `
    [id="2ag-hub-root"] { position: fixed; top: 14px; right: 18px; z-index: 2147483000; font: 13px/1.4 system-ui, -apple-system, Segoe UI, sans-serif; color: rgba(255,255,255,.94); -webkit-app-region: no-drag; }
    [id="2ag-hub-toggle"] { display:inline-flex; align-items:center; gap:7px; border: 1px solid rgba(177,205,255,.38); border-radius: 999px; padding: 7px 12px; color: rgba(255,255,255,.98); background: linear-gradient(135deg, rgba(91,125,204,.9), rgba(38,42,61,.86)); box-shadow: 0 8px 24px rgba(0,0,0,.3), 0 0 0 1px rgba(255,255,255,.04) inset; cursor: pointer; backdrop-filter: blur(14px); transition: transform .16s ease, box-shadow .16s ease, filter .16s ease; }
    [id="2ag-hub-toggle"]:hover { filter: brightness(1.12); transform: translateY(-1px); box-shadow: 0 10px 28px rgba(0,0,0,.34), 0 0 22px rgba(128,169,255,.28); }
    [id="2ag-hub-toggle"] .ag-logo { width:18px; height:18px; object-fit:contain; transform: translateY(-1px); filter: drop-shadow(0 0 5px rgba(153,193,255,.55)); }
    .ag-badge { display:inline-flex; align-items:center; height:16px; padding:0 5px; border-radius:999px; color:rgba(220,232,255,.94); background:rgba(255,255,255,.12); font-size:10px; letter-spacing:.04em; }
    [id="2ag-hub-drawer"] { position: fixed; top: 56px; right: 18px; width: 360px; max-height: calc(100vh - 74px); overflow: auto; padding: 16px; border: 1px solid rgba(203,220,255,.25); border-radius: 16px; background: rgba(20,22,28,.96); box-shadow: 0 18px 60px rgba(0,0,0,.65), 0 0 0 1px rgba(255,255,255,.04) inset; backdrop-filter: blur(30px) saturate(135%); -webkit-backdrop-filter: blur(30px) saturate(135%); opacity: 0; transform: translateY(-8px) scale(.98); pointer-events: none; transition: opacity .16s ease, transform .16s ease; }
    [id="2ag-hub-root"][data-anchor="bottom"] [id="2ag-hub-drawer"] { top: auto; right: auto; bottom: 56px; left: 18px; transform-origin: bottom left; }
    [id="2ag-hub-drawer"][data-open="true"] { opacity: 1; transform: translateY(0) scale(1); pointer-events: auto; }
    [id="2ag-hub-drawer"] h2 { margin: 0; font-size: 15px; letter-spacing: .02em; }
    [id="2ag-hub-drawer"] h3 { margin: 18px 0 8px; font-size: 12px; color: rgba(255,255,255,.66); text-transform: uppercase; letter-spacing: .08em; }
    [id="2ag-hub-drawer"] .ag-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 10px 0; }
    [id="2ag-hub-drawer"] label { color: rgba(255,255,255,.86); }
    [id="2ag-hub-drawer"] input[type="range"] { width: 160px; accent-color: #9bbcff; appearance:none; height:5px; border-radius:999px; background:linear-gradient(90deg,#9bbcff 0 50%,rgba(255,255,255,.16) 50%); outline:none; }
    [id="2ag-hub-drawer"] input[type="range"]::-webkit-slider-thumb { appearance:none; width:14px; height:14px; border-radius:50%; border:2px solid rgba(232,241,255,.94); background:#769eff; box-shadow:0 0 12px rgba(126,164,255,.75); }
    [id="2ag-hub-drawer"] output { min-width:42px; padding:2px 7px; border:1px solid rgba(166,194,255,.24); border-radius:999px; color:rgba(224,234,255,.96); background:rgba(95,133,211,.2); text-align:center; font-size:11px; }
    [id="2ag-hub-drawer"] input[type="text"] { box-sizing: border-box; width: 100%; padding: 8px 9px; border: 1px solid rgba(255,255,255,.14); border-radius: 8px; color: #fff; background: rgba(0,0,0,.26); outline: none; }
    [id="2ag-hub-drawer"] button { border: 1px solid rgba(255,255,255,.14); border-radius: 8px; padding: 7px 9px; color: rgba(255,255,255,.92); background: rgba(255,255,255,.08); cursor: pointer; }
    [id="2ag-hub-drawer"] button:hover { background: rgba(255,255,255,.16); box-shadow:0 0 14px rgba(128,169,255,.12); }
    [id="2ag-hub-drawer"] .ag-presets { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; }
    [id="2ag-hub-drawer"] .ag-presets button { transition: transform .12s ease, background .12s ease; }
    [id="2ag-hub-drawer"] .ag-presets button:active { transform:scale(.96); }
    [id="2ag-hub-drawer"] .ag-presets button[data-active="true"] { border-color: rgba(155,188,255,.8); background: linear-gradient(135deg,rgba(94,132,214,.42),rgba(94,132,214,.18)); box-shadow:0 0 16px rgba(109,151,238,.16); }
    [id="2ag-hub-drawer"] .ag-file { display: flex; gap: 6px; }
    [id="2ag-hub-drawer"] .ag-file button { flex: 0 0 auto; }
    [id="2ag-hub-drawer"] .ag-muted { color: rgba(255,255,255,.54); font-size: 11px; }
    [id="2ag-hub-drawer"] input[type="checkbox"] { position:absolute; opacity:0; width:1px; height:1px; pointer-events:none; }
    [id="2ag-hub-drawer"] .ag-toggle { display:inline-flex; align-items:center; flex:0 0 auto; }
    [id="2ag-hub-drawer"] .ag-toggle-track { display:inline-flex; width:36px; height:20px; padding:2px; box-sizing:border-box; border-radius:999px; background:rgba(255,255,255,.18); border:1px solid rgba(255,255,255,.18); transition:background .16s ease, border-color .16s ease; cursor:pointer; }
    [id="2ag-hub-drawer"] .ag-toggle-thumb { width:14px; height:14px; border-radius:50%; background:#d9e5ff; box-shadow:0 2px 5px rgba(0,0,0,.35); transition:transform .16s ease, background .16s ease; }
    [id="2ag-hub-drawer"] input[type="checkbox"]:checked + .ag-toggle-track { background:linear-gradient(90deg,#759eff,#a9c5ff); border-color:rgba(198,218,255,.7); }
    [id="2ag-hub-drawer"] input[type="checkbox"]:checked + .ag-toggle-track .ag-toggle-thumb { transform:translateX(16px); background:#fff; }
    [id="2ag-hub-drawer"] .ag-tabs { display: grid; grid-template-columns: repeat(5, 1fr); gap: 5px; margin: 14px 0; padding-bottom:3px; border-bottom:1px solid rgba(255,255,255,.08); }
    [id="2ag-hub-drawer"] .ag-tabs button { padding: 6px 4px; font-size: 11px; position:relative; border-color:transparent; background:transparent; }
    [id="2ag-hub-drawer"] .ag-tabs button[data-active="true"] { color:#dce8ff; }
    [id="2ag-hub-drawer"] .ag-tabs button[data-active="true"]::after { content:""; position:absolute; left:12%; right:12%; bottom:-4px; height:2px; border-radius:2px; background:linear-gradient(90deg,#83aaff,#d0ddff); box-shadow:0 0 10px rgba(131,170,255,.7); }
    [id="2ag-hub-drawer"] .ag-panel { display: none; }
    [id="2ag-hub-drawer"] .ag-panel[data-active="true"] { display: block; }
    [id="2ag-hub-drawer"] .ag-status { margin: 8px 0; padding: 8px; border: 1px solid rgba(255,255,255,.1); border-radius: 8px; background: rgba(0,0,0,.2); }
    [id="2ag-hub-drawer"] textarea { box-sizing: border-box; width: 100%; min-height: 120px; resize: vertical; padding: 8px 9px; border: 1px solid rgba(255,255,255,.14); border-radius: 8px; color: #fff; background: rgba(0,0,0,.26); outline: none; }
    [id="2ag-hub-drawer"] .ag-plugin { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 0; border-bottom: 1px solid rgba(255,255,255,.08); }
    :root, :host, html, body, .dark, [class*="dark"], .theme-dark, .theme-standalone {
      --background: transparent !important;
      --color-background: transparent !important;
      --sidebar: transparent !important;
      --color-sidebar: transparent !important;
      --sidebar-background: transparent !important;
      --sidebar-muted: transparent !important;
      --color-sidebar-muted: transparent !important;
      --sidebar-secondary: transparent !important;
      --color-sidebar-secondary: transparent !important;
      --vscode-editor-background: transparent !important;
      --vscode-sideBar-background: transparent !important;
      --vscode-activityBar-background: transparent !important;
      --vscode-panel-background: transparent !important;
      --vscode-editorPane-background: transparent !important;
      --vscode-statusBar-background: transparent !important;
      --vscode-editorGutter-background: transparent !important;
      --vscode-breadcrumb-background: transparent !important;
    }
    html, body, #root, #app, main,
    body > div,
    [data-testid="app"],
    [class*="session"], [class*="conversation"], [class*="chat-stream"],
    .monaco-workbench,
    .monaco-workbench .part.editor,
    .monaco-workbench .part.sidebar,
    .monaco-workbench .part.titlebar,
    .monaco-workbench .part.activitybar,
    .monaco-workbench .part.statusbar,
    .monaco-workbench .part.panel,
    .bg-background, [class*="bg-background"],
    .bg-sidebar, [class*="bg-sidebar"],
    div[class*="workspace"], div[class*="layout"],
    div[class*="container"], div[class*="main"],
    div[class*="content"], div[class*="editor"],
    div.h-screen.w-screen,
    div.h-full.w-full {
      position: relative;
      z-index: 1;
      background: transparent !important;
      background-color: transparent !important;
    }
    #root > div, #app > div, main > div, [class*="workspace"] > div, [class*="layout"] > div {
      background: transparent !important;
      background-color: transparent !important;
    }
    div[role="dialog"], div[class*="modal"], div[class*="dialog"], div[class*="settings"], div[class*="Settings"], div[class*="popup"], div[class*="popover"], section[class*="settings"] { background:rgba(22,22,26,.95) !important; background-color:rgba(22,22,26,.95) !important; backdrop-filter:blur(36px) !important; -webkit-backdrop-filter:blur(36px) !important; border:1px solid rgba(255,255,255,.12) !important; box-shadow:0 20px 50px rgba(0,0,0,.8) !important; z-index:2147482000 !important; }
    div[class*="backdrop"], div[class*="overlay"]:not([id="2ag-dream-skin-bg"]) { background:rgba(0,0,0,.6) !important; backdrop-filter:blur(6px) !important; -webkit-backdrop-filter:blur(6px) !important; }
    code, pre, .monaco-editor, .view-lines { color:rgba(245,247,255,.94); }
    ::-webkit-scrollbar { width:9px; height:9px; } ::-webkit-scrollbar-thumb { background:rgba(255,255,255,.2); border-radius:9px; border:2px solid transparent; background-clip:padding-box; } ::-webkit-scrollbar-track { background:rgba(0,0,0,.12); }
    input, textarea, [contenteditable="true"] { background-color: rgba(30,30,30,.75) !important; }
    [id="2ag-dream-skin-bg"] { position: fixed !important; inset: 0 !important; z-index: -99999 !important; pointer-events: none !important; background-size: cover !important; background-position: center !important; background-repeat: no-repeat !important; }
    [id="2ag-dream-skin-overlay"] { width: 100% !important; height: 100% !important; }
  `;

  function ipc(method, params) {
    return new Promise((resolve, reject) => {
      if (typeof window.__2AG_IPC__ !== 'function') { reject(new Error('2Ag IPC bridge is unavailable')); return; }
      const id = 'ipc-' + (++ipcSequence);
      ipcPending.set(id, { resolve, reject });
      try { window.__2AG_IPC__(JSON.stringify({ id, method, params: params || {} })); } catch (error) { ipcPending.delete(id); reject(error); }
    });
  }

  window.__2AG_IPC_DELIVER__ = function (payload) {
    let message; try { message = typeof payload === 'string' ? JSON.parse(payload) : payload; } catch (_) { return; }
    const pending = ipcPending.get(message && message.id); if (!pending) return;
    ipcPending.delete(message.id); if (message.error) pending.reject(new Error(message.error)); else pending.resolve(message.result);
  };

  function currentLanguage() { return state.language === 'en-US' ? 'en-US' : 'zh-CN'; }
  function t(key, fallback) { const table = DICT[currentLanguage()] || DICT['zh-CN']; return table[key] || DICT['en-US'][key] || fallback || key; }
  function applyLanguage() {
    document.querySelectorAll('[id="2ag-hub-root"] [data-i18n]').forEach((node) => {
      const key = node.getAttribute('data-i18n'); node.textContent = t(key, node.textContent);
    });
    document.querySelectorAll('[id="2ag-hub-root"] [data-i18n-placeholder]').forEach((node) => { node.placeholder = t(node.getAttribute('data-i18n-placeholder'), node.placeholder); });
    document.querySelectorAll('[id="2ag-hub-root"] [data-i18n-aria]').forEach((node) => { node.setAttribute('aria-label', t(node.getAttribute('data-i18n-aria'), node.getAttribute('aria-label'))); });
    const toggle = document.getElementById('2ag-language-toggle'); if (toggle) toggle.textContent = currentLanguage() === 'zh-CN' ? '中 / EN' : 'EN / 中';
    renderValues(); refreshNetworkStatus(); refreshPluginStatus();
  }
  function setLanguage(language) {
    state.language = language === 'en-US' ? 'en-US' : 'zh-CN';
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch (_) {}
    ipc('core.config.set', { patch: { language: state.language } }).catch(() => {});
    applyLanguage();
  }

  function mountExtensionTab(tab) {
    const drawer = document.getElementById(DRAWER_ID); if (!drawer || !tab || !tab.id) return;
    const tabs = drawer.querySelector('.ag-tabs'); const panelHost = drawer.querySelector('.ag-extension-panels');
    if (!tabs || !panelHost || drawer.querySelector('[data-tab="ext:' + tab.id + '"]')) return;
    const button = document.createElement('button'); button.type = 'button'; button.dataset.tab = 'ext:' + tab.id; button.textContent = tab.title || tab.id;
    button.addEventListener('click', () => showTab('ext:' + tab.id)); tabs.appendChild(button);
    const panel = document.createElement('div'); panel.className = 'ag-panel'; panel.dataset.panel = 'ext:' + tab.id; panelHost.appendChild(panel);
    try { tab.render(panel); } catch (error) { panel.textContent = 'Plugin panel error: ' + error.message; }
  }

  window.__2AG__ = window.__2AG__ || {};
  window.__2AG__.ipc = ipc;
  window.__2AG__.registerTab = function (tab) { if (!tab || !tab.id || typeof tab.render !== 'function') throw new Error('registerTab requires id and render'); extensionTabs.set(tab.id, tab); mountExtensionTab(tab); return () => extensionTabs.delete(tab.id); };
  if (!window.__2ag_keydown_handler) {
    window.__2ag_keydown_handler = true;
    window.addEventListener('keydown', (event) => {
      if (window.__2ag_devtools_dispatching) return;
      if ((event.altKey && String(event.key).toLowerCase() === 'a') || (event.ctrlKey && event.shiftKey && String(event.key).toLowerCase() === 'a')) {
        const drawer = document.getElementById(DRAWER_ID); if (drawer) { event.preventDefault(); drawer.dataset.open = drawer.dataset.open === 'true' ? 'false' : 'true'; }
        return;
      }
      if (event.key === 'F12' || (event.ctrlKey && event.shiftKey && String(event.key).toLowerCase() === 'i')) { event.preventDefault(); ipc('core.diagnostics.devtools', {}).catch(() => {}); }
    }, true);
  }

  function number(value, fallback, min, max) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? Math.min(max, Math.max(min, parsed)) : fallback;
  }

  function loadState() {
    try {
      const configuredPlugins = Object.assign({}, state.plugins || {});
      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
      Object.assign(state, stored);
      if (/^https?:\/\/127\.0\.0\.1:\d+\/bg\.jpg$/i.test(String(state.wallpaper || ''))) state.wallpaper = INITIAL_CONFIG.wallpaper || state.wallpaper;
      state.blur = number(state.blur, 20, 0, 40);
      state.opacity = number(state.opacity, 0.55, 0.1, 0.9);
      state.language = state.language === 'en-US' ? 'en-US' : 'zh-CN';
      state.plugins = Object.assign({  }, configuredPlugins, state.plugins || {});
    } catch (_) {}
    if (INITIAL_CONFIG.privacy && INITIAL_CONFIG.privacy.block_beacons && !window.__2ag_beacon_guard) {
      try {
        const originalBeacon = navigator.sendBeacon;
        navigator.sendBeacon = function (url) {
          const blockedHosts = INITIAL_CONFIG.privacy.blocked_hosts || [];
          const value = String(url || '').toLowerCase();
          if (blockedHosts.some((host) => value.indexOf(String(host).toLowerCase()) >= 0)) return true;
          return typeof originalBeacon === 'function' ? originalBeacon.apply(this, arguments) : false;
        };
        window.__2ag_beacon_guard = true;
      } catch (_) {}
    }
  }

  function saveState() {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); } catch (_) {}
    const patch = { blur: state.blur, opacity: state.opacity, language: currentLanguage(), global_rules: state.global_rules || '' };
    if (/^[A-Za-z]:[\\/]/.test(String(state.wallpaper || ''))) patch.wallpaper_path = state.wallpaper;
    ipc('core.config.set', { patch }).catch(() => {});
    window.dispatchEvent(new CustomEvent('2ag:config-changed', { detail: Object.assign({}, state) }));
  }

  function imageValue() {
    const value = state.wallpaper || INITIAL_CONFIG.wallpaper || 'http://127.0.0.1:18082/bg.jpg';
    if (INITIAL_CONFIG.wallpaper_path && String(value).toLowerCase() === String(INITIAL_CONFIG.wallpaper_path).toLowerCase()) return INITIAL_CONFIG.wallpaper || value;
    if (/^file:\/\//i.test(value)) return value.replace(/\\/g, '/');
    if (/^[A-Za-z]:[\\/]/.test(value)) return 'file:///' + value.replace(/\\/g, '/');
    return value;
  }

  function ensureSkin() {
    const body = document.body;
    if (!body) return;
    let bg = document.getElementById(BG_ID);
    if (!bg) { bg = document.createElement('div'); bg.id = BG_ID; body.prepend(bg); }
    else if (bg.parentNode !== body || body.firstElementChild !== bg) body.prepend(bg);
    const bgStyle = 'position:fixed !important;inset:0 !important;z-index:-99999 !important;pointer-events:none !important;background-image:url("' + String(imageValue()).replace(/"/g, '\\"') + '") !important;background-size:cover !important;background-position:center !important;background-repeat:no-repeat !important;';
    if (bg.style.cssText !== bgStyle) bg.style.cssText = bgStyle;
    let overlay = document.getElementById(OVERLAY_ID);
    if (!overlay || overlay.parentNode !== bg) { overlay = document.createElement('div'); overlay.id = OVERLAY_ID; bg.appendChild(overlay); }
    overlay.style.cssText = 'width:100%;height:100%;backdrop-filter:blur(' + state.blur + 'px);-webkit-backdrop-filter:blur(' + state.blur + 'px);background:rgba(0,0,0,' + state.opacity + ');';
    let style = document.getElementById(STYLE_ID);
    const host = document.head || document.documentElement;
    if (host) {
      if (!style) { style = document.createElement('style'); style.id = STYLE_ID; }
      if (style.parentNode !== host || host.lastElementChild !== style) host.appendChild(style);
      if (style.textContent !== HUB_CSS) style.textContent = HUB_CSS;
    }
  }

  function renderValues() {
    const blur = document.getElementById('2ag-blur');
    const blurValue = document.getElementById('2ag-blur-value');
    if (blur) blur.value = String(state.blur);
    if (blurValue) blurValue.textContent = state.blur + ' px';
    const opacity = document.getElementById('2ag-opacity');
    const opacityValue = document.getElementById('2ag-opacity-value');
    if (opacity) opacity.value = String(state.opacity);
    if (opacityValue) opacityValue.textContent = Math.round(state.opacity * 100) + '%';
    if (blur) blur.style.background = 'linear-gradient(90deg,#9bbcff ' + (Number(state.blur) / 40 * 100) + '%,rgba(255,255,255,.16) ' + (Number(state.blur) / 40 * 100) + '%)';
    if (opacity) opacity.style.background = 'linear-gradient(90deg,#9bbcff ' + ((Number(state.opacity) - .1) / .8 * 100) + '%,rgba(255,255,255,.16) ' + ((Number(state.opacity) - .1) / .8 * 100) + '%)';
    const path = document.getElementById('2ag-wallpaper-path');
    if (path && !path.matches(':focus')) {
      const value = String(state.wallpaper || '');
      path.value = /^data:image\//i.test(value) ? t('localImageSelected') : (value === INITIAL_CONFIG.wallpaper ? (INITIAL_CONFIG.wallpaper_path || value) : value);
    }
    document.querySelectorAll('[id="2ag-hub-drawer"] [data-preset]').forEach((button) => {
      button.dataset.active = button.dataset.preset === state.preset ? 'true' : 'false';
    });
    const rules = document.getElementById('2ag-global-rules');
    if (rules && document.activeElement !== rules) rules.value = state.global_rules || '';
    document.querySelectorAll('[id="2ag-hub-drawer"] [data-plugin-name]').forEach((row) => {
      const value = state.plugins[row.dataset.pluginName];
      const toggle = row.querySelector('input[type="checkbox"]');
      if (toggle) toggle.checked = Boolean(value && value.enabled !== undefined ? value.enabled : value);
    });
  }

  function showTab(name) {
    document.querySelectorAll('[id="2ag-hub-drawer"] [data-tab]').forEach((button) => { button.dataset.active = button.dataset.tab === name ? 'true' : 'false'; });
    document.querySelectorAll('[id="2ag-hub-drawer"] [data-panel]').forEach((panel) => { panel.dataset.active = panel.dataset.panel === name ? 'true' : 'false'; });
    if (name === 'network') refreshNetworkStatus();
    if (name === 'plugins') refreshPluginStatus();
  }

  function refreshNetworkStatus() {
    const status = document.getElementById('2ag-network-status');
    if (!status) return;
    const host = document.getElementById('2ag-host-status');
    if (host) {
      const env = INITIAL_CONFIG.env_overrides || {};
      const names = Object.keys(env);
      host.textContent = t('hostPID') + ' ' + (INITIAL_CONFIG.host_pid || '—') + ' · ' + t('cdp') + ' 127.0.0.1:' + (INITIAL_CONFIG.cdp_port || '—') + (names.length ? ' · ' + t('env') + ': ' + names.join(', ') : '');
    }
    const overrides = document.getElementById('2ag-network-overrides');
    if (overrides) {
      const items = (state.network && state.network.endpoint_overrides) || [];
      overrides.textContent = items.length ? items.map((item) => item.host + ' → ' + item.url).join('\n') : t('officialEndpoints');
    }
    if (!INITIAL_CONFIG.proxy_url) { status.textContent = t('proxyDisabled'); return; }
    status.textContent = t('readingProxy');
    fetch(INITIAL_CONFIG.proxy_url + '/status', { cache: 'no-store' }).then((response) => response.json()).then((value) => {
      status.textContent = t('proxyActive') + ' · ' + value.requests + ' ' + t('requests') + ' · ' + value.blocked + ' ' + t('blocked') + ' · ' + t('last') + ' ' + value.last_ms + ' ms';
    }).catch(() => { status.textContent = t('proxyUnavailable'); });
  }

  function probeNetwork() {
    const status = document.getElementById('2ag-network-status'); if (!status) return;
    status.textContent = t('probeRunning');
    ipc('core.diagnostics.probe', {}).then((result) => {
      const latency = result && result.latency_ms !== undefined ? result.latency_ms + ' ms' : '—';
      status.textContent = result && result.ok ? t('probeOK') + ' · ' + (result.status || 'OK') + ' · ' + latency : t('probeFailed') + ' · ' + ((result && result.status) || '—') + ' · ' + latency;
    }).catch((error) => { status.textContent = t('probeFailed') + ' · ' + error.message; });
  }

  function refreshPluginStatus() {
    if (!INITIAL_CONFIG.plugin_url) return;
    fetch(INITIAL_CONFIG.plugin_url + '/plugins', { cache: 'no-store' }).then((response) => response.json()).then((items) => {
      items.forEach((item) => {
        const row = Array.from(document.querySelectorAll('[data-plugin-name]')).find((candidate) => candidate.dataset.pluginName === item.name);
        const toggle = row && row.querySelector('input[type="checkbox"]');
        if (toggle) toggle.checked = Boolean(item.running);
        const status = row && row.querySelector('[data-plugin-status]'); if (status) status.textContent = item.running ? t('pluginRunning') : t('pluginStopped');
      });
    }).catch(() => {});
  }

  function loadPluginScripts() {
    Object.keys(state.plugins || {}).forEach((name) => {
      const plugin = state.plugins[name];
      const sourceURL = plugin && typeof plugin === 'object' ? plugin.uiURL : '';
      if (!sourceURL || loadedPluginScripts.has(sourceURL)) return;
      loadedPluginScripts.add(sourceURL);
      fetch(sourceURL, { cache: 'no-store' })
        .then((response) => { if (!response.ok) throw new Error('HTTP ' + response.status); return response.text(); })
        .then((source) => { (new Function(source + '\n//# sourceURL=' + sourceURL))(); })
        .catch((error) => { console.warn('[2ag] plugin UI load failed for ' + name + ':', error); });
    });
  }

  function applyState(persist) {
    state.blur = number(state.blur, 20, 0, 40);
    state.opacity = number(state.opacity, 0.55, 0.1, 0.9);
    ensureSkin(); renderValues(); if (persist) saveState();
  }

  function hotReload() { ensureSkin(); ensureHub(); applyLanguage(); const status = document.getElementById('2ag-diag-status'); if (status) status.textContent = t('hotReloaded'); }
  function resetTheme() { state.wallpaper = INITIAL_CONFIG.wallpaper_path || INITIAL_CONFIG.wallpaper || ''; state.blur = 20; state.opacity = .55; state.preset = 'Dark Dream'; applyState(true); const status = document.getElementById('2ag-diag-status'); if (status) status.textContent = t('reset'); }
  function exportConfig() { ipc('core.config.get', {}).then((value) => { const blob = new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' }); const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = '2ag-config.json'; link.click(); setTimeout(() => URL.revokeObjectURL(link.href), 1000); const status = document.getElementById('2ag-diag-status'); if (status) status.textContent = t('exported'); }).catch(() => {}); }

  function preset(name) {
    const presets = {
      'Dark Dream': { blur: 20, opacity: 0.55 },
      Cyberpunk: { blur: 28, opacity: 0.35 },
      'Clean Glass': { blur: 12, opacity: 0.2 }
    };
    Object.assign(state, presets[name] || presets['Dark Dream']); state.preset = name; applyState(true);
  }

  function createDrawer() {
    if (document.getElementById(HUB_ID)) return;
    const root = document.createElement('div'); root.id = HUB_ID;
    root.innerHTML = '<button id="2ag-hub-toggle" type="button" data-i18n-aria="console"><img class="ag-logo" alt=""><span data-i18n="capsule">2Ag</span><span class="ag-badge">v1.0</span></button>' +
      '<section id="2ag-hub-drawer" aria-label="2Ag settings" data-open="false">' +
      '<div style="display:flex;justify-content:space-between;align-items:center"><div style="display:flex;align-items:center;gap:8px"><img class="ag-drawer-logo" alt="" style="width:20px;height:20px;object-fit:contain;filter:drop-shadow(0 0 6px rgba(153,193,255,.6))"><h2 data-i18n="console">2Ag Console</h2></div><div style="display:flex;align-items:center;gap:6px"><button id="2ag-language-toggle" type="button" aria-label="Language">中 / EN</button><button id="2ag-hub-close" type="button" data-i18n="close">Close</button></div></div>' +
      '<div class="ag-tabs"><button type="button" data-tab="skin" data-i18n="skin">Skin</button><button type="button" data-tab="network" data-i18n="network">Network</button><button type="button" data-tab="rules" data-i18n="rules">Rules</button><button type="button" data-tab="plugins" data-i18n="plugins">Plugins</button><button type="button" data-tab="diagnostics" data-i18n="diagnostics">Diag</button></div>' +
      '<div class="ag-extension-panels">' +
      '<div class="ag-panel" data-panel="skin"><h3 data-i18n="dreamSkin">Dream Skin</h3>' +
      '<div class="ag-row"><label for="2ag-blur" data-i18n="blur">Blur</label><output id="2ag-blur-value"></output></div><input id="2ag-blur" type="range" min="0" max="40" step="1">' +
      '<div class="ag-row" style="margin-top:14px"><label for="2ag-opacity" data-i18n="darkness">Darkness</label><output id="2ag-opacity-value"></output></div><input id="2ag-opacity" type="range" min="0.1" max="0.9" step="0.01">' +
      '<div class="ag-row" style="margin-top:14px"><label data-i18n="wallpaper">Wallpaper</label></div><div class="ag-file"><input id="2ag-wallpaper-file" type="file" accept="image/*" style="display:none"><button id="2ag-wallpaper-pick" type="button" data-i18n="chooseImage">Choose local image</button></div>' +
      '<input id="2ag-wallpaper-path" type="text" data-i18n-placeholder="imagePlaceholder" placeholder="Image URL or file:/// path" style="margin-top:7px"><div class="ag-muted" data-i18n="localImageHint">The selected image is stored in this profile.</div>' +
      '<h3 data-i18n="presets">Presets</h3><div class="ag-presets"><button type="button" data-preset="Dark Dream" data-i18n="darkDream">Dark Dream</button><button type="button" data-preset="Cyberpunk" data-i18n="cyberpunk">Cyberpunk</button><button type="button" data-preset="Clean Glass" data-i18n="cleanGlass">Clean Glass</button></div></div>' +
      '<div class="ag-panel" data-panel="network"><h3 data-i18n="networkModel">Network &amp; Model</h3><div id="2ag-host-status" class="ag-status" data-i18n="hostNotLoaded">Host diagnostics not loaded</div><div id="2ag-network-status" class="ag-status" data-i18n="proxyNotLoaded">Proxy status not loaded</div><div id="2ag-network-overrides" class="ag-status"></div><button id="2ag-network-probe" type="button" data-i18n="testGateway">Test gateway</button><div class="ag-muted" data-i18n="httpHint">HTTP requests can use endpoint overrides.</div></div>' +
      '<div class="ag-panel" data-panel="rules"><h3 data-i18n="rules">Global Rules</h3><textarea id="2ag-global-rules" data-i18n-placeholder="rulesPlaceholder" placeholder="Rules applied to configured JSON endpoints"></textarea><button id="2ag-rules-save" type="button" data-i18n="saveRules" style="margin-top:8px">Save rules</button><div class="ag-muted" data-i18n="rulesHint">Rules are persisted in this browser profile.</div></div>' +
      '<div class="ag-panel" data-panel="plugins"><h3 data-i18n="extensionsSidecar">Extensions &amp; Sidecar</h3><div id="2ag-plugin-list"></div></div></div>' +
      '<div class="ag-panel" data-panel="diagnostics"><h3 data-i18n="diagnosticsTitle">Diagnostics</h3><button id="2ag-devtools" type="button" data-i18n="openDevtools">Open DevTools (F12)</button><div style="display:flex;gap:6px;margin-top:8px"><button id="2ag-hot-reload" type="button" data-i18n="hotReload">Reload skin</button><button id="2ag-reset-theme" type="button" data-i18n="resetTheme">Reset theme</button><button id="2ag-export-config" type="button" data-i18n="exportConfig">Export config</button></div><div id="2ag-diag-status" class="ag-muted" style="margin-top:8px" data-i18n="diagHint">Use F12, Ctrl+Shift+I, Alt+A, or Ctrl+Shift+A while debugging the host.</div></div>' +
      '</section>';
    document.body.appendChild(root);
    const logo = root.querySelector('.ag-logo'); if (logo) logo.src = INITIAL_CONFIG.logo_url || '';
    const drawerLogo = root.querySelector('.ag-drawer-logo'); if (drawerLogo) drawerLogo.src = INITIAL_CONFIG.logo_url || '';
    const drawer = root.querySelector('[id="2ag-hub-drawer"]');
    root.querySelector('[id="2ag-hub-toggle"]').addEventListener('click', () => { drawer.dataset.open = drawer.dataset.open !== 'true' ? 'true' : 'false'; });
    root.querySelector('[id="2ag-hub-close"]').addEventListener('click', () => { drawer.dataset.open = 'false'; });
    root.querySelector('[id="2ag-language-toggle"]').addEventListener('click', () => setLanguage(currentLanguage() === 'zh-CN' ? 'en-US' : 'zh-CN'));
    root.querySelectorAll('[data-tab]').forEach((button) => button.addEventListener('click', () => showTab(button.dataset.tab)));
    root.querySelector('[id="2ag-blur"]').addEventListener('input', (event) => { state.blur = Number(event.target.value); applyState(true); });
    root.querySelector('[id="2ag-opacity"]').addEventListener('input', (event) => { state.opacity = Number(event.target.value); applyState(true); });
    root.querySelector('[id="2ag-wallpaper-path"]').addEventListener('change', (event) => { state.wallpaper = event.target.value.trim(); applyState(true); });
    root.querySelector('[id="2ag-wallpaper-pick"]').addEventListener('click', () => {
      // Chromium's native file input opens the OS picker directly. Do not
      // invoke PowerShell, which flashes a console and can open a second
      // Explorer window before the actual selection dialog.
      root.querySelector('[id="2ag-wallpaper-file"]').click();
    });
    root.querySelector('[id="2ag-wallpaper-file"]').addEventListener('change', (event) => {
      const file = event.target.files && event.target.files[0]; if (!file) return;
      const reader = new FileReader(); reader.onload = () => { state.wallpaper = String(reader.result); applyState(true); }; reader.readAsDataURL(file);
    });
    root.querySelectorAll('[data-preset]').forEach((button) => button.addEventListener('click', () => preset(button.dataset.preset)));
    root.querySelector('[id="2ag-network-probe"]').addEventListener('click', probeNetwork);
    root.querySelector('[id="2ag-rules-save"]').addEventListener('click', () => {
      state.global_rules = root.querySelector('[id="2ag-global-rules"]').value; saveState();
      if (INITIAL_CONFIG.proxy_url) fetch(INITIAL_CONFIG.proxy_url + '/config/global-rules', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ global_rules: state.global_rules }) }).catch(() => {});
    });
    root.querySelector('[id="2ag-devtools"]').addEventListener('click', () => { ipc('core.diagnostics.devtools', {}).catch(() => {}); });
    root.querySelector('[id="2ag-hot-reload"]').addEventListener('click', hotReload);
    root.querySelector('[id="2ag-reset-theme"]').addEventListener('click', resetTheme);
    root.querySelector('[id="2ag-export-config"]').addEventListener('click', exportConfig);
    const pluginList = root.querySelector('[id="2ag-plugin-list"]');
    Object.keys(state.plugins).forEach((name) => {
      const value = state.plugins[name] || {};
      const rowContainer = document.createElement('div'); 
      rowContainer.className = 'ag-plugin'; 
      rowContainer.dataset.pluginName = name;
      rowContainer.style.display = 'block';

      const label = (value && value.displayName) || name;
      const description = value && value.description ? '<small class="ag-muted">' + String(value.description).replace(/[<>&"']/g, '') + '</small>' : '';
      
      const header = document.createElement('div');
      header.style.display = 'flex';
      header.style.justifyContent = 'space-between';
      header.style.alignItems = 'center';
      header.innerHTML = '<span style="display:flex;flex-direction:column;gap:2px"><strong>' + String(label).replace(/[<>&"']/g, '') + '</strong>' + description + '<small class="ag-muted" data-plugin-status>' + t('pluginStopped') + '</small></span><label class="ag-toggle" style="cursor:pointer"><input type="checkbox"><span class="ag-toggle-track"><span class="ag-toggle-thumb"></span></span></label>';
      
      const toggle = header.querySelector('input');
      toggle.checked = Boolean(value && value.enabled !== undefined ? value.enabled : value);
      toggle.addEventListener('change', () => {
        const enabled = toggle.checked;
        state.plugins[name] = Object.assign({}, value, { enabled }); saveState();
        if (INITIAL_CONFIG.plugin_url) {
          fetch(INITIAL_CONFIG.plugin_url + '/plugins/' + encodeURIComponent(name), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled }) }).catch(() => { toggle.checked = !enabled; state.plugins[name] = Object.assign({}, value, { enabled: !enabled }); saveState(); });
        }
      });
      rowContainer.appendChild(header);

      if (value.ui && value.ui.schema && value.ui.schema.fields) {
        const fieldsBox = document.createElement('div');
        fieldsBox.style.marginTop = '10px';
        fieldsBox.style.paddingTop = '10px';
        fieldsBox.style.borderTop = '1px solid rgba(255,255,255,0.05)';
        
        value.ui.schema.fields.forEach(f => {
          const fRow = document.createElement('div');
          fRow.style.display = 'flex';
          fRow.style.justifyContent = 'space-between';
          fRow.style.alignItems = 'center';
          fRow.style.marginTop = '8px';
          fRow.innerHTML = '<span style="font-size:12px">' + (f.label || f.key) + '</span>';
          
          const configVal = value.config && value.config[f.key] !== undefined ? value.config[f.key] : f.default;
          
          if (f.type === 'boolean') {
            const toggleWrapper = document.createElement('label');
            toggleWrapper.className = 'ag-toggle';
            toggleWrapper.style.cursor = 'pointer';
            toggleWrapper.innerHTML = '<input type="checkbox"' + (configVal ? ' checked' : '') + '><span class="ag-toggle-track"><span class="ag-toggle-thumb"></span></span>';
            toggleWrapper.querySelector('input').addEventListener('change', function() {
              ipc('core.config.plugin.set', { plugin: name, key: f.key, value: this.checked }).catch(()=>{});
            });
            fRow.appendChild(toggleWrapper);
          } else {
            const inputEl = document.createElement('input');
            inputEl.type = f.type === 'number' ? 'number' : 'text';
            inputEl.value = configVal || '';
            inputEl.placeholder = f.default || '';
            inputEl.style.cssText = 'width: 120px; background: rgba(0,0,0,0.2); border: 1px solid rgba(255,255,255,0.1); color: #fff; padding: 4px 6px; border-radius: 4px; font-size: 11px;';
            inputEl.addEventListener('change', function() {
              let val = this.value;
              if(f.type === 'number') val = Number(val);
              ipc('core.config.plugin.set', { plugin: name, key: f.key, value: val }).catch(()=>{});
            });
            fRow.appendChild(inputEl);
          }
          fieldsBox.appendChild(fRow);
        });
        rowContainer.appendChild(fieldsBox);
      }
      
      pluginList.appendChild(rowContainer);
    });
    renderValues();
    showTab('skin');
    applyLanguage();
    extensionTabs.forEach((tab) => mountExtensionTab(tab));
    positionHub(root);
  }

  function positionHub(root) {
    if (!root) return;
    const candidates = Array.from(document.querySelectorAll('button, [role="button"], a'));
    const install = candidates.find((node) => /install\s+ide/i.test((node.textContent || '').trim()));
    const settings = candidates.find((node) => /^settings$/i.test((node.textContent || '').trim()));
    const anchor = install || settings;
    if (!anchor) return;
    const rect = anchor.getBoundingClientRect();
    if (install) {
      root.dataset.anchor = 'top';
      root.style.top = Math.max(8, rect.top) + 'px';
      root.style.right = Math.max(18, window.innerWidth - rect.left + 8) + 'px';
    } else {
      root.dataset.anchor = 'bottom';
      root.style.left = Math.max(18, rect.left) + 'px';
      root.style.bottom = Math.max(18, window.innerHeight - rect.bottom) + 'px';
      root.style.top = 'auto';
      root.style.right = 'auto';
    }
  }

  function ensureHub() {
    if (!document.body) return;
    if (!document.getElementById(HUB_ID)) createDrawer();
    positionHub(document.getElementById(HUB_ID));
  }

  loadState(); loadPluginScripts(); applyState(false); ensureHub();
  if (!window[OBSERVER_KEY]) {
    window[OBSERVER_KEY] = new MutationObserver(() => {
      if (window.__2ag_hub_guard) return;
      window.__2ag_hub_guard = true;
      try { ensureSkin(); ensureHub(); } finally { window.__2ag_hub_guard = false; }
    });
    window[OBSERVER_KEY].observe(document, { childList: true, subtree: true });
  }
  if (!window[TIMER_KEY]) {
    let count = 0;
    window[TIMER_KEY] = window.setInterval(() => {
      ensureSkin();
      ensureHub();
      if (++count > 20) {
        window.clearInterval(window[TIMER_KEY]);
        window[TIMER_KEY] = window.setInterval(() => { ensureSkin(); ensureHub(); }, 1500);
      }
    }, 500);
  }
})();


