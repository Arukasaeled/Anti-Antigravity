const fs = require('fs');
let code = fs.readFileSync('internal/patcher/injected_hub.js', 'utf8');

const translationsRegex = /const translations = \{[\s\S]*?\};\n  const ipcPending/;
const newTranslations = `const translations = {
    'en': {
      capsule: '2Ag', console: '2Ag Console', close: 'Close', skin: 'Skin', network: 'Network', rules: 'Rules', plugins: 'Plugins', diagnostics: 'Diagnostics',
      dreamSkin: 'Dream Skin', blur: 'Blur', darkness: 'Darkness', wallpaper: 'Wallpaper', chooseImage: 'Choose local image', imagePlaceholder: 'Image URL or file:/// path', localImageHint: 'The selected image is stored in this profile.', presets: 'Presets',
      darkDream: 'Dark Dream', cyberpunk: 'Cyberpunk', cleanGlass: 'Clean Glass', networkModel: 'Network & Model', hostNotLoaded: 'Host diagnostics not loaded', proxyNotLoaded: 'Proxy status not loaded', proxyDisabled: 'Proxy disabled in 2ag.json', readingProxy: 'Reading local proxy status...', proxyActive: 'Proxy active', proxyUnavailable: 'Proxy status unavailable', requests: 'requests', blocked: 'blocked', last: 'last', localImageSelected: 'Local image selected', officialEndpoints: 'Official endpoints (no overrides)', httpHint: 'HTTP requests can use endpoint overrides. HTTPS CONNECT remains opaque and is forwarded without TLS interception.',
      testGateway: 'Test gateway', probeRunning: 'Testing gateway...', probeOK: 'Gateway online', probeFailed: 'Gateway unavailable', rulesPlaceholder: 'Rules applied to configured JSON endpoints', saveRules: 'Save rules', rulesHint: 'Rules are persisted in this browser profile; HTTP JSON rewriting requires a matching target.', extensionsSidecar: 'Extensions', noPlugins: 'No plugins discovered', pluginRunning: 'Running', pluginStopped: 'Stopped', diagnosticsTitle: 'Diagnostics', openDevtools: 'Open DevTools (F12)', hotReload: 'Reload skin', resetTheme: 'Reset theme', exportConfig: 'Export config', diagHint: 'Use F12, Ctrl+Shift+I, Alt+A, or Ctrl+Shift+A while debugging the host.', hostPID: 'Host PID', cdp: 'CDP', env: 'env', language: 'Language', saved: 'Saved', reset: 'Theme reset', exported: 'Config exported', hotReloaded: 'Skin reloaded',
      modalHint: 'Settings and dialogs stay isolated from the transparent workspace.'
    },
    'zh-CN': {
      capsule: '2Ag', console: '2Ag 管理中枢', close: '关闭', skin: '视觉工坊', network: '网关', rules: '规则', plugins: '扩展生态', diagnostics: '环境诊断',
      dreamSkin: '视觉工坊 (Dream Studio)', blur: '背景模糊度 (Blur)', darkness: '遮罩暗度 (Darkness)', wallpaper: '壁纸设定 (Wallpaper)', chooseImage: '选择本地图片', imagePlaceholder: '图片 URL 或 file:/// 路径', localImageHint: '您选择的壁纸将持久化保存。', presets: '预设风格',
      darkDream: '深邃极客', cyberpunk: '赛博暗涌', cleanGlass: '极光冷灰', networkModel: '网络与模型', hostNotLoaded: '宿主诊断未加载', proxyNotLoaded: '网关状态未加载', proxyDisabled: '本地代理未开启', readingProxy: '正在读取探针...', proxyActive: '网关运行中', proxyUnavailable: '网关不可用', requests: '请求', blocked: '拦截', last: '最后', localImageSelected: '已应用本地壁纸', officialEndpoints: '官方直连端点', httpHint: '可自定义 HTTP 拦截和改写规则。',
      testGateway: '伴生网关探针', probeRunning: '探测中...', probeOK: '网关就绪', probeFailed: '网关离线', rulesPlaceholder: '匹配并改写目标 JSON', saveRules: '保存规则', rulesHint: '改写规则实时生效。', extensionsSidecar: '扩展生态 (Extensions)', noPlugins: '工作区无活动插件', pluginRunning: '运行中', pluginStopped: '已停止', diagnosticsTitle: '环境诊断 (Diagnostics)', openDevtools: '呼出宿主 DevTools (F12)', hotReload: '重载注入补丁 (Hot Reload)', resetTheme: '恢复默认主题', exportConfig: '导出运行配置 (JSON)', diagHint: '您还可以随时按下 F12 进入原生审查模式。', hostPID: '宿主 PID', cdp: 'CDP 端口', env: '环境变量', language: '界面语言', saved: '已保存', reset: '已恢复默认', exported: '配置已导出', hotReloaded: 'UI 热重载完成',
      modalHint: '模态弹窗遮蔽度 (Modal Glass Opacity)'
    }
  };
  const ipcPending`;
code = code.replace(translationsRegex, newTranslations);

code = code.replace(/const currentLanguage = INITIAL_CONFIG\.language \|\| 'en';/, "const currentLanguage = INITIAL_CONFIG.language || 'zh-CN';");

fs.writeFileSync('internal/patcher/injected_hub.js', code);
console.log('done');
