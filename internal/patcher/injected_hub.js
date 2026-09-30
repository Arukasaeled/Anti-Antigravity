(() => {
  'use strict';

  const INITIAL_CONFIG = __2AG_INITIAL_CONFIG__;
  const BG_ID = '2ag-dream-skin-bg';
  const OVERLAY_ID = '2ag-dream-skin-overlay';
  const SHADOW_HOST_ID = 'twoag-injected-root';
  const STORAGE_KEY = '2ag.skin.config.v1';
  const SUBSYSTEM_STORAGE_KEY = '2ag.subsystems.config.v1';
  const POS_STORAGE_KEY = '2ag.hub.position.v1';
  const OBSERVER_KEY = '__2ag_hub_observer';
  const TIMER_KEY = '__2ag_hub_timer';

  // 1. 基础状态初始化
  const state = Object.assign({
    wallpaper: '',
    blur: 28,
    opacity: 0.6,
    preset: 'Dark Dream',
    language: 'zh-CN'
  }, INITIAL_CONFIG || {});

  // 子系统配置（默认配置符合需求规范）
  let subsystems = {
    force_dispatch: true,    // 强制发送通道 (Ctrl + Shift + Enter)
    state_healer: true,      // 输入状态自愈
    overlay_stripper: true,  // 过渡遮罩粉碎
    text_fallback: false     // 纯文本降级模式
  };
  try {
    const savedSubsystems = JSON.parse(localStorage.getItem(SUBSYSTEM_STORAGE_KEY) || '{}');
    subsystems = Object.assign(subsystems, savedSubsystems);
  } catch (_) {}

  // 2. 浮标吸附位置持久化管理
  let hubPos = { side: 'right', topRatio: 0.45 };
  try {
    const savedPos = JSON.parse(localStorage.getItem(POS_STORAGE_KEY) || '{}');
    if (savedPos && (savedPos.side === 'left' || savedPos.side === 'right')) {
      hubPos.side = savedPos.side;
    }
    if (typeof savedPos.topRatio === 'number' && !isNaN(savedPos.topRatio)) {
      hubPos.topRatio = Math.max(0.06, Math.min(0.92, savedPos.topRatio));
    }
  } catch (_) {}

  function saveHubPosition() {
    try {
      localStorage.setItem(POS_STORAGE_KEY, JSON.stringify(hubPos));
    } catch (_) {}
  }

  function saveSubsystems() {
    try {
      localStorage.setItem(SUBSYSTEM_STORAGE_KEY, JSON.stringify(subsystems));
    } catch (_) {}
  }

  // 3. 矢量 SVG 图标定义（严禁 Emoji，统一采用 Google 原色与极细单线）
  const SVG_ICONS = {
    googleG: `
      <svg viewBox="0 0 24 24" width="20" height="20" style="display:block;flex-shrink:0;">
        <path fill="#4285F4" d="M23.745 12.27c0-.7-.06-1.4-.19-2.07H12v4.51h6.6c-.29 1.52-1.14 2.8-2.4 3.66v3.04h3.88c2.28-2.09 3.66-5.18 3.66-9.14z"/>
        <path fill="#34A853" d="M12 24c3.24 0 5.95-1.08 7.93-2.91l-3.88-3.04c-1.08.72-2.45 1.16-4.05 1.16-3.12 0-5.77-2.1-6.72-4.93H1.27v3.13C3.26 21.36 7.33 24 12 24z"/>
        <path fill="#FBBC05" d="M5.28 14.28c-.25-.72-.38-1.49-.38-2.28s.13-1.56.38-2.28V6.59H1.27C.46 8.21 0 10.05 0 12s.46 3.79 1.27 5.41l4.01-3.13z"/>
        <path fill="#EA4335" d="M12 4.75c1.77 0 3.35.61 4.6 1.8l3.42-3.42C17.95 1.19 15.24 0 12 0 7.33 0 3.26 2.64 1.27 6.59l4.01 3.13c.95-2.83 3.6-4.97 6.72-4.97z"/>
      </svg>
    `,
    close: `
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <line x1="18" y1="6" x2="6" y2="18"></line>
        <line x1="6" y1="6" x2="18" y2="18"></line>
      </svg>
    `,
    lightning: `
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
      </svg>
    `,
    reload: `
      <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2"/>
      </svg>
    `
  };

  // 生成象限圆环 SVG (外径 17px, 线宽 2.5px)
  // Active: 精确分为四段弧线（左上黄 #FBBC05、右上绿 #34A853、右下蓝 #4285F4、左下红 #EA4335）
  // Inactive: 外径 17px, 线宽 2.5px 的纯暗灰空心细环 #43474e
  function renderQuadrantRing(active) {
    if (active) {
      return `
        <svg width="17" height="17" viewBox="0 0 17 17" fill="none" class="quadrant-svg active">
          <!-- 右上: 绿 #34A853 -->
          <path d="M 9.13 1.28 A 7.25 7.25 0 0 1 15.72 7.87" stroke="#34A853" stroke-width="2.5" stroke-linecap="round"/>
          <!-- 右下: 蓝 #4285F4 -->
          <path d="M 15.72 9.13 A 7.25 7.25 0 0 1 9.13 15.72" stroke="#4285F4" stroke-width="2.5" stroke-linecap="round"/>
          <!-- 左下: 红 #EA4335 -->
          <path d="M 7.87 15.72 A 7.25 7.25 0 0 1 1.28 9.13" stroke="#EA4335" stroke-width="2.5" stroke-linecap="round"/>
          <!-- 左上: 黄 #FBBC05 -->
          <path d="M 1.28 7.87 A 7.25 7.25 0 0 1 7.87 1.28" stroke="#FBBC05" stroke-width="2.5" stroke-linecap="round"/>
        </svg>
      `;
    }
    return `
      <svg width="17" height="17" viewBox="0 0 17 17" fill="none" class="quadrant-svg inactive">
        <circle cx="8.5" cy="8.5" r="7.25" stroke="#43474e" stroke-width="2.5" fill="none"/>
      </svg>
    `;
  }

  // 4. Shadow DOM 内部独立样式（杜绝全局 CSS 污染）
  const SHADOW_CSS = `
    :host {
      all: initial;
      font-family: 'Google Sans', 'Roboto', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
      -webkit-font-smoothing: antialiased;
      text-rendering: optimizeLegibility;
      font-size: 13px;
      color: #e3e3e3;
    }
    *, *::before, *::after {
      box-sizing: border-box;
      user-select: none;
      -webkit-user-drag: none;
    }

    /* --- G-Hub 悬浮浮标 --- */
    .ghub-beacon {
      position: fixed;
      width: 44px;
      height: 44px;
      border-radius: 50%;
      z-index: 2147483647;
      display: flex;
      align-items: center;
      justify-content: center;
      background: conic-gradient(from 45deg, #4285F4, #34A853, #FBBC05, #EA4335, #4285F4);
      box-shadow: 0 6px 20px rgba(0, 0, 0, 0.65), 0 0 12px rgba(66, 133, 244, 0.25);
      cursor: grab;
      touch-action: none;
      transition: box-shadow 0.25s ease, transform 0.25s ease;
    }
    .ghub-beacon:active {
      cursor: grabbing;
    }
    .ghub-beacon:hover {
      box-shadow: 0 8px 26px rgba(0, 0, 0, 0.8), 0 0 16px rgba(66, 133, 244, 0.4);
    }
    .ghub-inner {
      width: 41px;
      height: 41px;
      border-radius: 50%;
      background: #13151a;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: background 0.2s ease;
    }
    .ghub-beacon:hover .ghub-inner {
      background: #191c22;
    }

    /* --- G-Cockpit 战术控制面板 --- */
    .cockpit-panel {
      position: fixed;
      width: 330px;
      background: #131519;
      border: 1.2px solid #2b2f38;
      border-radius: 12px;
      box-shadow: 0 20px 50px rgba(0, 0, 0, 0.75), 0 6px 16px rgba(0, 0, 0, 0.5);
      z-index: 2147483646;
      overflow: hidden;
      display: flex;
      flex-direction: column;
      opacity: 0;
      pointer-events: none;
      transform: scale(0.95);
      transition: opacity 0.22s cubic-bezier(0.2, 0.8, 0.2, 1), transform 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
    }
    .cockpit-panel[data-open="true"] {
      opacity: 1;
      pointer-events: auto;
      transform: scale(1);
    }

    /* 顶部 2.5px Google 四色导光线 */
    .cockpit-light-bar {
      width: 100%;
      height: 2.5px;
      background: linear-gradient(90deg, #4285F4 0%, #34A853 33%, #FBBC05 66%, #EA4335 100%);
      flex-shrink: 0;
    }

    .cockpit-body {
      padding: 16px 18px 18px;
      display: flex;
      flex-direction: column;
    }

    /* 头部 */
    .cockpit-header {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      margin-bottom: 14px;
    }
    .brand-wrap {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .brand-title {
      font-size: 15px;
      font-weight: 700;
      color: #ffffff;
      letter-spacing: -0.2px;
    }
    .status-line {
      display: flex;
      align-items: center;
      gap: 6px;
      margin-top: 3px;
      font-size: 11px;
      color: #9aa0a6;
    }
    .pulse-dot {
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: #34A853;
      box-shadow: 0 0 6px #34A853;
      animation: pulseAnim 2s infinite ease-in-out;
    }
    @keyframes pulseAnim {
      0%, 100% { transform: scale(1); opacity: 0.8; }
      50% { transform: scale(1.3); opacity: 1; }
    }
    .close-btn {
      background: transparent;
      border: none;
      color: #80868b;
      cursor: pointer;
      padding: 4px;
      border-radius: 4px;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: color 0.15s, background 0.15s;
    }
    .close-btn:hover {
      color: #ffffff;
      background: rgba(255, 255, 255, 0.08);
    }

    /* 额度水位条 */
    .quota-box {
      margin-bottom: 16px;
    }
    .quota-meta {
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-size: 11px;
      margin-bottom: 6px;
    }
    .quota-label {
      color: #bdc1c6;
    }
    .quota-value {
      font-weight: 600;
      letter-spacing: 0.1px;
      transition: color 0.3s ease;
    }
    .quota-track {
      width: 100%;
      height: 5px;
      border-radius: 3px;
      background: rgba(255, 255, 255, 0.08);
      overflow: hidden;
    }
    .quota-fill {
      height: 100%;
      border-radius: 3px;
      transition: width 0.4s ease, background 0.4s ease;
    }

    /* 分组标题 */
    .section-tag {
      font-size: 10px;
      font-weight: 700;
      color: #6a717a;
      letter-spacing: 0.6px;
      margin-bottom: 10px;
      text-transform: uppercase;
    }

    /* 子系统列表项 */
    .subsystem-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
      margin-bottom: 16px;
    }
    .subsystem-item {
      display: flex;
      justify-content: space-between;
      align-items: center;
      cursor: pointer;
      padding: 2px 0;
    }
    .item-text {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }
    .item-name {
      font-size: 13px;
      font-weight: 600;
      color: #e3e3e3;
    }
    .item-desc {
      font-size: 11px;
      color: #80868b;
    }
    .quadrant-btn {
      background: transparent;
      border: none;
      padding: 0;
      margin: 0;
      display: flex;
      align-items: center;
      justify-content: center;
      cursor: pointer;
      outline: none;
      transition: transform 0.15s ease;
    }
    .quadrant-btn:hover {
      transform: scale(1.1);
    }
    .quadrant-svg {
      display: block;
    }

    /* 操作按钮 */
    .actions-wrap {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .action-btn {
      width: 100%;
      padding: 9px 12px;
      border-radius: 6px;
      font-size: 12px;
      font-weight: 500;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      cursor: pointer;
      outline: none;
      transition: all 0.18s ease;
    }
    .btn-force {
      background: rgba(66, 133, 244, 0.12);
      border: 1.2px solid #4285F4;
      color: #8ab4f8;
    }
    .btn-force:hover {
      background: rgba(66, 133, 244, 0.22);
      color: #ffffff;
      box-shadow: 0 0 10px rgba(66, 133, 244, 0.35);
    }
    .btn-force:active {
      transform: scale(0.98);
    }
    .btn-reload {
      background: rgba(255, 255, 255, 0.05);
      border: 1.2px solid #2b2f38;
      color: #bdc1c6;
    }
    .btn-reload:hover {
      background: rgba(255, 255, 255, 0.1);
      border-color: #3c4043;
      color: #e3e3e3;
    }
    .btn-reload:active {
      transform: scale(0.98);
    }

    /* --- Toast 轻量提示条 --- */
    .cockpit-toast {
      position: fixed;
      top: 24px;
      left: 50%;
      transform: translateX(-50%) translateY(-20px);
      background: #181a20;
      border: 1.2px solid #4285F4;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.7), 0 0 14px rgba(66, 133, 244, 0.3);
      padding: 8px 18px;
      border-radius: 100px;
      font-size: 12px;
      font-weight: 500;
      color: #e3e3e3;
      display: flex;
      align-items: center;
      gap: 8px;
      z-index: 2147483647;
      opacity: 0;
      pointer-events: none;
      transition: all 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
    }
    .cockpit-toast[data-show="true"] {
      opacity: 1;
      transform: translateX(-50%) translateY(0);
    }
    .toast-dot {
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: #4285F4;
    }
  `;

  // 5. 宿主原生全局样式（仅限背景壁纸与 880px 排版注入，保持原生 DOM 纯净）
  const NATIVE_STYLE_ID = '2ag-native-core-style';
  function ensureNativeStyles() {
    let el = document.getElementById(NATIVE_STYLE_ID);
    if (!el) {
      el = document.createElement('style');
      el.id = NATIVE_STYLE_ID;
      el.textContent = `
        /* 880px 黄金视距排版重塑 */
        div[class*="chat-scroll"], div[class*="conversation-view"], div[class*="chat-stream"], div[class*="conversation-container"], [class*="chat-scroll-container"], .conversation-container, main[role="main"] > div, [class*="chat-session"], [class*="messages-container"], [class*="chat-history"], [class*="conversation-layout"], div[class*="conversation-content"], div[class*="chat-layout"] {
          max-width: 880px !important;
          margin: 0 auto !important;
          width: 100% !important;
          box-sizing: border-box !important;
        }
        pre, code, .code-block, [class*="code-block"], [class*="code-container"] {
          font-family: 'Google Sans Code', 'Consolas', 'Roboto Mono', 'Fira Code', monospace !important;
          line-height: 1.65 !important;
          font-size: 13.5px !important;
        }
        p, [class*="message-content"], [class*="markdown-body"] {
          line-height: 1.6 !important;
          font-family: 'Google Sans', 'Roboto', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif !important;
        }
        /* 宿主背景透明化以显现底层高斯模糊壁纸 */
        :root, :host, html, body, .dark, [class*="dark"], .theme-dark, .theme-standalone {
          --background: transparent !important;
          --color-background: transparent !important;
        }
        html, body, #root, #app, main, body > div:not([id="${SHADOW_HOST_ID}"]):not([id="${BG_ID}"]) {
          background-color: transparent !important;
        }
      `;
      (document.head || document.documentElement).appendChild(el);
    }
  }

  // 6. Dream-Skin 原版高斯模糊背景层
  function ensureDreamSkin() {
    if (!document.body) return;
    let bg = document.getElementById(BG_ID);
    const wallpaper = (INITIAL_CONFIG && INITIAL_CONFIG.wallpaper) || state.wallpaper || "";
    const blurVal = state.blur || 28;
    const opacityVal = state.opacity || 0.6;

    if (!bg) {
      bg = document.createElement('div');
      bg.id = BG_ID;
      bg.innerHTML = `<div id="${OVERLAY_ID}"></div>`;
      bg.style.cssText = `
        position: fixed !important;
        top: 0 !important;
        left: 0 !important;
        width: 100vw !important;
        height: 100vh !important;
        z-index: 0 !important;
        pointer-events: none !important;
        ${wallpaper ? `background-image: url("${wallpaper}") !important;` : ''}
        background-size: cover !important;
        background-position: center !important;
        filter: blur(${blurVal}px) !important;
        opacity: ${opacityVal} !important;
      `;
      document.body.prepend(bg);
    } else {
      if (bg.parentNode !== document.body || document.body.firstElementChild !== bg) {
        document.body.prepend(bg);
      }
      if (wallpaper && (!bg.style.backgroundImage || !bg.style.backgroundImage.includes('data:image'))) {
        bg.style.setProperty('background-image', `url("${wallpaper}")`, 'important');
      }
      bg.style.setProperty('position', 'fixed', 'important');
      bg.style.setProperty('top', '0', 'important');
      bg.style.setProperty('left', '0', 'important');
      bg.style.setProperty('width', '100vw', 'important');
      bg.style.setProperty('height', '100vh', 'important');
      bg.style.setProperty('z-index', '0', 'important');
      bg.style.setProperty('pointer-events', 'none', 'important');
      bg.style.setProperty('filter', `blur(${blurVal}px)`, 'important');
      bg.style.setProperty('opacity', `${opacityVal}`, 'important');
    }
  }

  // 7. Toast 提示引擎
  let toastTimer = null;
  function showToast(message) {
    const root = document.getElementById(SHADOW_HOST_ID);
    if (!root || !root.shadowRoot) return;
    const toast = root.shadowRoot.getElementById('twoag-toast');
    if (!toast) return;
    const msgEl = toast.querySelector('.toast-msg');
    if (msgEl) msgEl.textContent = message;
    toast.dataset.show = 'true';
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toast.dataset.show = 'false';
    }, 1500);
  }

  // 8. 模块 3：“强制发送”穿透引擎 (Force Dispatch Engine)
  function executeForceDispatch() {
    console.log('[2AG_FORCE_SEND_TRIGGERED]', {
      timestamp: Date.now(),
      subsystems: Object.assign({}, subsystems)
    });

    // Step 1: 动态寻址定位当前活跃的输入容器
    let target = document.activeElement;
    const isInputElement = (el) => {
      if (!el) return false;
      const tag = el.tagName;
      return tag === 'TEXTAREA' || tag === 'INPUT' || el.isContentEditable || el.classList.contains('monaco-editor') || el.closest('.monaco-editor');
    };

    if (!isInputElement(target)) {
      target = document.querySelector('textarea:not([disabled]), [contenteditable="true"], textarea, div[class*="chat-input"] textarea, div[class*="input-container"] textarea, div.monaco-editor [contenteditable="true"]');
    }

    if (target) {
      target.removeAttribute('disabled');
      target.setAttribute('aria-disabled', 'false');
      target.style.setProperty('pointer-events', 'auto', 'important');
      if (typeof target.focus === 'function') target.focus();
    }

    // 强行解除所有发送按钮及其容器的 disabled / pointer-events 阻断
    const sendButtons = document.querySelectorAll(
      'button[aria-label*="Send" i], button[aria-label*="发送"], button.send-button, [data-tooltip*="Send" i], [data-tooltip*="发送"], [data-testid*="send" i], [class*="send-button"], [class*="send_button"], button[type="submit"]'
    );
    sendButtons.forEach(btn => {
      btn.removeAttribute('disabled');
      btn.setAttribute('aria-disabled', 'false');
      btn.style.setProperty('pointer-events', 'auto', 'important');
      btn.style.setProperty('cursor', 'pointer', 'important');
      btn.style.setProperty('z-index', '9999', 'important');
    });

    // Step 2: 向当前输入焦点按时序严格派发冒泡键盘事件
    // 负向验证 B: 带上 customEventFlag: true 与 __2ag_synthetic: true，杜绝递归捕获死锁
    if (target) {
      const keyOpts = {
        key: 'Enter',
        code: 'Enter',
        keyCode: 13,
        which: 13,
        bubbles: true,
        cancelable: true,
        composed: true
      };

      const evDown = new KeyboardEvent('keydown', keyOpts);
      evDown.__2ag_synthetic = true;
      evDown.customEventFlag = true;

      const evPress = new KeyboardEvent('keypress', keyOpts);
      evPress.__2ag_synthetic = true;
      evPress.customEventFlag = true;

      const evUp = new KeyboardEvent('keyup', keyOpts);
      evUp.__2ag_synthetic = true;
      evUp.customEventFlag = true;

      target.dispatchEvent(evDown);
      target.dispatchEvent(evPress);
      target.dispatchEvent(evUp);
    }

    // Step 3: 若宿主界面仍未提交，抓取 DOM 树内临近的发送图标/按钮并触发原生 .click()
    setTimeout(() => {
      for (const btn of sendButtons) {
        const rect = btn.getBoundingClientRect();
        if (rect.width > 0 && rect.height > 0) {
          btn.click();
          break;
        }
      }
    }, 35);

    // 触发成功反馈 Toast
    showToast('[2Ag] 强制发送已派发 (Bypassed Frontend Lock)');
  }

  // 9. 输入状态自愈 (State Healer) 与 过渡遮罩粉碎 (Overlay Stripper) 守卫
  function performSubsystemGuards() {
    if (!document.body) return;

    // Guard 1: 输入状态自愈
    if (subsystems.state_healer) {
      const inputs = document.querySelectorAll('textarea[disabled], input[disabled], [contenteditable="false"][class*="editor"]');
      inputs.forEach(el => {
        el.removeAttribute('disabled');
        el.setAttribute('aria-disabled', 'false');
        el.style.setProperty('pointer-events', 'auto', 'important');
      });
    }

    // Guard 2: 过渡遮罩粉碎 (清理阻挡输入的无用幽灵遮罩)
    if (subsystems.overlay_stripper) {
      const potentialMasks = document.querySelectorAll('div[class*="backdrop"], div[class*="mask"], div[class*="overlay"], div[class*="shield"]');
      potentialMasks.forEach(mask => {
        if (mask.id === BG_ID || mask.id === SHADOW_HOST_ID) return;
        const style = window.getComputedStyle(mask);
        if (style.position === 'absolute' || style.position === 'fixed') {
          const rect = mask.getBoundingClientRect();
          if (rect.bottom > window.innerHeight - 180) {
            if (style.opacity === '0' || style.visibility === 'hidden' || style.backgroundColor === 'transparent' || style.backgroundColor === 'rgba(0, 0, 0, 0)') {
              if (!mask.querySelector('input, textarea, button, [contenteditable="true"]')) {
                mask.style.setProperty('pointer-events', 'none', 'important');
              }
            }
          }
        }
      });
    }
  }

  // 10. 全局快捷键监听（严格满足负向断言）
  if (!window.__2ag_dispatch_shortcut_installed) {
    window.__2ag_dispatch_shortcut_installed = true;
    window.addEventListener('keydown', (event) => {
      // 负向验证 B: 防止事件死循环，若为自身派发的合成事件，直接放行
      if (event.__2ag_synthetic || event.customEventFlag) {
        return;
      }

      // 负向验证 A: 普通 Enter 或 Shift + Enter 静默放行，绝对不拦截
      const isCtrlOrMeta = event.ctrlKey || event.metaKey;
      if (isCtrlOrMeta && event.shiftKey && (event.key === 'Enter' || event.keyCode === 13)) {
        if (subsystems.force_dispatch) {
          event.preventDefault();
          event.stopPropagation();
          executeForceDispatch();
        }
      }
    }, true);
  }

  // 11. Shadow DOM 核心构建与 G-Hub / G-Cockpit 挂载
  let isPanelOpen = false;

  function mountShadowUI() {
    if (!document.body && !document.documentElement) return;
    let rootHost = document.getElementById(SHADOW_HOST_ID);
    if (rootHost && rootHost.shadowRoot) {
      return; // 已经就绪
    }

    if (!rootHost) {
      rootHost = document.createElement('div');
      rootHost.id = SHADOW_HOST_ID;
      (document.body || document.documentElement).appendChild(rootHost);
    }

    const shadow = rootHost.attachShadow({ mode: 'open' });

    // 挂载独立样式
    const styleEl = document.createElement('style');
    styleEl.textContent = SHADOW_CSS;
    shadow.appendChild(styleEl);

    // 容器包装
    const container = document.createElement('div');
    container.className = 'twoag-scope';
    container.innerHTML = `
      <!-- G-Hub 悬浮浮标 -->
      <div id="twoag-ghub" class="ghub-beacon" title="anti-Antigravity G-Hub">
        <div class="ghub-inner">
          ${SVG_ICONS.googleG}
        </div>
      </div>

      <!-- G-Cockpit 战术控制面板 -->
      <div id="twoag-cockpit" class="cockpit-panel" data-open="false">
        <div class="cockpit-light-bar"></div>
        <div class="cockpit-body">
          <!-- 头部 -->
          <div class="cockpit-header">
            <div class="brand-wrap">
              ${SVG_ICONS.googleG}
              <div>
                <div class="brand-title">anti-Antigravity</div>
                <div class="status-line">
                  <span class="pulse-dot"></span>
                  <span class="status-text">宿主注入就绪 · 18ms</span>
                </div>
              </div>
            </div>
            <button id="btn-close-cockpit" class="close-btn" type="button" title="关闭面板">
              ${SVG_ICONS.close}
            </button>
          </div>

          <!-- 额度水位条 -->
          <div class="quota-box">
            <div class="quota-meta">
              <span class="quota-label">会话 Token 消耗水位</span>
              <span id="quota-val-text" class="quota-value">88.4% (接近阈值)</span>
            </div>
            <div class="quota-track">
              <div id="quota-fill-bar" class="quota-fill" style="width: 88.4%; background: #EA4335;"></div>
            </div>
          </div>

          <!-- GRAVITY BOOST SUBSYSTEMS 分组 -->
          <div class="section-tag">GRAVITY BOOST SUBSYSTEMS</div>
          <div class="subsystem-list">
            <div class="subsystem-item" data-sys="force_dispatch">
              <div class="item-text">
                <span class="item-name">强制发送通道</span>
                <span class="item-desc">穿透前端假死 (Ctrl + Shift + Enter)</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="force_dispatch">
                ${renderQuadrantRing(subsystems.force_dispatch)}
              </button>
            </div>

            <div class="subsystem-item" data-sys="state_healer">
              <div class="item-text">
                <span class="item-name">输入状态自愈</span>
                <span class="item-desc">自动清除 disabled 状态脱缰</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="state_healer">
                ${renderQuadrantRing(subsystems.state_healer)}
              </button>
            </div>

            <div class="subsystem-item" data-sys="overlay_stripper">
              <div class="item-text">
                <span class="item-name">过渡遮罩粉碎</span>
                <span class="item-desc">过滤 Loading / about:blank 幽灵图层</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="overlay_stripper">
                ${renderQuadrantRing(subsystems.overlay_stripper)}
              </button>
            </div>

            <div class="subsystem-item" data-sys="text_fallback">
              <div class="item-text">
                <span class="item-name">纯文本降级模式</span>
                <span class="item-desc">富文本彻底崩溃时兜底原生表单</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="text_fallback">
                ${renderQuadrantRing(subsystems.text_fallback)}
              </button>
            </div>
          </div>

          <!-- EMERGENCY ACTIONS 分组 -->
          <div class="section-tag">EMERGENCY ACTIONS</div>
          <div class="actions-wrap">
            <button id="btn-action-force-send" class="action-btn btn-force" type="button">
              ${SVG_ICONS.lightning}
              <span>立即触发强制发送 (Force Send)</span>
            </button>
            <button id="btn-action-reload" class="action-btn btn-reload" type="button">
              ${SVG_ICONS.reload}
              <span>无损重载宿主 Webview (CDP Reload)</span>
            </button>
          </div>
        </div>
      </div>

      <!-- Toast 提示 -->
      <div id="twoag-toast" class="cockpit-toast" data-show="false">
        <span class="toast-dot"></span>
        <span class="toast-msg">[2Ag] 强制发送已派发 (Bypassed Frontend Lock)</span>
      </div>
    `;
    shadow.appendChild(container);

    const ghubEl = shadow.getElementById('twoag-ghub');
    const cockpitEl = shadow.getElementById('twoag-cockpit');
    const closeBtn = shadow.getElementById('btn-close-cockpit');
    const forceSendBtn = shadow.getElementById('btn-action-force-send');
    const reloadBtn = shadow.getElementById('btn-action-reload');

    // 计算与布局更新
    function updateGHubLayout(animated) {
      if (!ghubEl) return;
      const winW = window.innerWidth;
      const winH = window.innerHeight;
      const targetTop = Math.max(12, Math.min(winH - 56, hubPos.topRatio * (winH - 44)));
      const targetLeft = hubPos.side === 'left' ? 12 : (winW - 44 - 12);

      if (animated) {
        ghubEl.style.transition = 'top 0.35s cubic-bezier(0.2, 0.8, 0.2, 1), left 0.35s cubic-bezier(0.2, 0.8, 0.2, 1)';
      } else {
        ghubEl.style.transition = 'none';
      }
      ghubEl.style.top = targetTop + 'px';
      ghubEl.style.left = targetLeft + 'px';

      updateCockpitLayout();
    }

    function updateCockpitLayout() {
      if (!cockpitEl || !ghubEl) return;
      const ghubRect = ghubEl.getBoundingClientRect();
      const winW = window.innerWidth;
      const winH = window.innerHeight;
      const panelW = 330;
      const panelH = cockpitEl.offsetHeight || 420;

      // 贴靠浮标垂直位置并 clamp
      let panelTop = ghubRect.top;
      if (panelTop + panelH > winH - 16) {
        panelTop = Math.max(16, winH - panelH - 16);
      }
      panelTop = Math.max(16, panelTop);

      cockpitEl.style.top = panelTop + 'px';

      // 贴靠浮标水平位置（右侧吸附时面板在左侧，左侧吸附时面板在右侧）
      if (hubPos.side === 'right') {
        cockpitEl.style.left = Math.max(12, ghubRect.left - panelW - 12) + 'px';
      } else {
        cockpitEl.style.left = (ghubRect.right + 12) + 'px';
      }
    }

    function toggleCockpit(open) {
      isPanelOpen = typeof open === 'boolean' ? open : !isPanelOpen;
      cockpitEl.dataset.open = isPanelOpen ? 'true' : 'false';
      if (isPanelOpen) {
        updateCockpitLayout();
      }
    }

    // 自由拖拽与 5px 死区判定
    let startX = 0;
    let startY = 0;
    let initialLeft = 0;
    let initialTop = 0;
    let isDragging = false;

    ghubEl.addEventListener('pointerdown', (e) => {
      startX = e.clientX;
      startY = e.clientY;
      const rect = ghubEl.getBoundingClientRect();
      initialLeft = rect.left;
      initialTop = rect.top;
      isDragging = false;
      ghubEl.style.transition = 'none';
    });

    window.addEventListener('pointermove', (e) => {
      if (e.buttons === 0 && isDragging) {
        finishDrag(e);
        return;
      }
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;

      if (!isDragging) {
        if (Math.hypot(dx, dy) >= 5) {
          isDragging = true;
          try { ghubEl.setPointerCapture(e.pointerId); } catch (_) {}
          if (isPanelOpen) toggleCockpit(false);
        }
      }

      if (isDragging) {
        const winW = window.innerWidth;
        const winH = window.innerHeight;
        const newLeft = Math.max(0, Math.min(winW - 44, initialLeft + dx));
        const newTop = Math.max(0, Math.min(winH - 44, initialTop + dy));
        ghubEl.style.left = newLeft + 'px';
        ghubEl.style.top = newTop + 'px';
      }
    });

    function finishDrag(e) {
      if (!isDragging) {
        // 位移 < 5px 视为点击
        toggleCockpit();
        return;
      }

      isDragging = false;
      try { ghubEl.releasePointerCapture(e.pointerId); } catch (_) {}

      // 弹性吸附就近边缘
      const winW = window.innerWidth;
      const winH = window.innerHeight;
      const currentRect = ghubEl.getBoundingClientRect();
      const centerX = currentRect.left + 22;

      hubPos.side = centerX < winW / 2 ? 'left' : 'right';
      hubPos.topRatio = Math.max(0.06, Math.min(0.92, currentRect.top / (winH - 44)));

      saveHubPosition();
      updateGHubLayout(true);
    }

    ghubEl.addEventListener('pointerup', finishDrag);
    ghubEl.addEventListener('pointercancel', finishDrag);

    // 面板内部控制
    closeBtn.addEventListener('click', () => toggleCockpit(false));

    // 象限圆环开关点击事件（事件委托）
    shadow.querySelectorAll('.subsystem-item').forEach(item => {
      item.addEventListener('click', (e) => {
        const key = item.dataset.sys;
        if (!key || subsystems[key] === undefined) return;
        subsystems[key] = !subsystems[key];
        saveSubsystems();

        // 重新渲染当前项开关圆环
        const btn = item.querySelector('.quadrant-btn');
        if (btn) {
          btn.innerHTML = renderQuadrantRing(subsystems[key]);
        }
        performSubsystemGuards();
      });
    });

    // 应急按钮 1：立即触发强制发送
    forceSendBtn.addEventListener('click', () => {
      executeForceDispatch();
    });

    // 应急按钮 2：无损重载宿主 Webview
    reloadBtn.addEventListener('click', () => {
      showToast('[2Ag] 正在执行无损 Webview 重载...');
      setTimeout(() => {
        window.location.reload();
      }, 300);
    });

    // 窗口尺寸自适应
    window.addEventListener('resize', () => {
      updateGHubLayout(false);
    });

    // 初始化布局
    updateGHubLayout(false);

    // 轮询配额水位 (语义化风险警示色阶: 0~70% 绿, 70~85% 黄, 85~100% 极危红)
    function updateQuotaDisplay(percent) {
      const p = Math.max(0, Math.min(100, Number(percent) || 0));
      const textEl = shadow.getElementById('quota-val-text');
      const barEl = shadow.getElementById('quota-fill-bar');
      if (!textEl || !barEl) return;

      let color = '#34A853'; // 绿
      let label = `${p.toFixed(1)}% (健康)`;
      if (p >= 85) {
        color = '#EA4335'; // 极危红
        label = `${p.toFixed(1)}% (接近阈值)`;
      } else if (p >= 70) {
        color = '#FBBC05'; // 黄
        label = `${p.toFixed(1)}% (告警)`;
      }

      textEl.textContent = label;
      textEl.style.color = color;
      barEl.style.width = `${p}%`;
      barEl.style.background = color;
    }

    async function pollTokenQuota() {
      const ports = [28470, 28471];
      for (const p of ports) {
        try {
          const res = await fetch(`http://127.0.0.1:${p}/api/v1/accounts/active`, { cache: 'no-store' });
          if (!res.ok) continue;
          const data = await res.json();
          const g5h = data.gemini_5h_percent !== undefined ? data.gemini_5h_percent : (data.active_account ? data.active_account.gemini_5h_percent : 88.4);
          updateQuotaDisplay(g5h);
          return;
        } catch (_) {}
      }
      // 网关未连接时保持优雅默认值
      updateQuotaDisplay(88.4);
    }

    pollTokenQuota();
    setInterval(pollTokenQuota, 4000);

    // 关键挂载断言日志（按规范格式输出）
    console.log('[2AG_UI_MOUNTED]', { version: '2.0', root: '#' + SHADOW_HOST_ID });
  }

  // 12. 统一初始化守护巡检流水线
  function tick() {
    try { ensureNativeStyles(); } catch (_) {}
    try { ensureDreamSkin(); } catch (_) {}
    try { mountShadowUI(); } catch (_) {}
    try { performSubsystemGuards(); } catch (_) {}
  }

  tick();

  // MutationObserver 监听确保在 SPA 路由重构或 DOM 重建时组件依旧存活
  if (window[OBSERVER_KEY]) {
    try { window[OBSERVER_KEY].disconnect(); } catch (_) {}
  }
  window[OBSERVER_KEY] = new MutationObserver(() => {
    if (window.__2ag_guard) return;
    window.__2ag_guard = true;
    try {
      tick();
    } finally {
      window.__2ag_guard = false;
    }
  });
  window[OBSERVER_KEY].observe(document.documentElement || document.body, { childList: true, subtree: true });

  if (window[TIMER_KEY]) {
    try { window.clearInterval(window[TIMER_KEY]); } catch (_) {}
  }
  window[TIMER_KEY] = window.setInterval(tick, 2000);

})();
