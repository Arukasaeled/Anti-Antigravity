(function () {
  'use strict';

  var DEFAULT_WALLPAPER = 'file:///C:/Users/35074/Pictures/E78978FCDE46412C17F90AAF7813EC8D.jpg';
  var script = document.currentScript;
  var config = window.__2agConfig || {};
  var backgroundId = '2ag-dream-skin-bg';
  var overlayId = '2ag-dream-skin-overlay';

  function attr(name, fallback) {
    var value = script && script.getAttribute(name);
    return value === null || value === '' ? fallback : value;
  }

  function numberAttr(name, fallback) {
    var value = Number(attr(name, fallback));
    return isFinite(value) ? value : fallback;
  }

  function ensureBackground() {
    var background = document.getElementById(backgroundId);
    if (!background) {
      background = document.createElement('div');
      background.id = backgroundId;
      background.innerHTML = '<div id="' + overlayId + '"></div>';
      (document.body || document.documentElement).appendChild(background);
    }
    var overlay = document.getElementById(overlayId);
    var wallpaper = config.wallpaper || attr('data-2ag-wallpaper', DEFAULT_WALLPAPER);
    var blur = config.blur || numberAttr('data-2ag-blur', 20);
    var opacity = config.opacity === undefined ? numberAttr('data-2ag-opacity', 0.55) : config.opacity;

    background.style.position = 'fixed';
    background.style.top = '0';
    background.style.left = '0';
    background.style.width = '100vw';
    background.style.height = '100vh';
    background.style.zIndex = '0';
    background.style.pointerEvents = 'none';
    background.style.backgroundImage = 'url("' + String(wallpaper).replace(/"/g, '\\"') + '")';
    background.style.backgroundSize = 'cover';
    background.style.backgroundPosition = 'center';
    background.style.backgroundRepeat = 'no-repeat';

    if (overlay) {
      overlay.style.width = '100%';
      overlay.style.height = '100%';
      overlay.style.backdropFilter = 'blur(' + blur + 'px)';
      overlay.style.webkitBackdropFilter = 'blur(' + blur + 'px)';
      overlay.style.backgroundColor = 'rgba(0, 0, 0, ' + opacity + ')';
    }
  }

  function loadStylesheet() {
    var cssPath = attr('data-2ag-css', config.cssPath || '');
    if (!cssPath || document.querySelector('link[data-2ag-dream-skin="1"]')) {
      return;
    }
    var link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = cssPath;
    link.setAttribute('data-2ag-dream-skin', '1');
    document.head.appendChild(link);
  }

  function applyConfig(next) {
    config = Object.assign({}, config, next || {});
    ensureBackground();
  }

  function boot() {
    ensureBackground();
    loadStylesheet();
    window.addEventListener('2ag:skin-config', function (event) {
      applyConfig(event.detail || {});
    });
    window.addEventListener('message', function (event) {
      if (event && event.data && event.data.type === '2ag:skin-config') {
        applyConfig(event.data.config || {});
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot, { once: true });
  } else {
    boot();
  }
})();
