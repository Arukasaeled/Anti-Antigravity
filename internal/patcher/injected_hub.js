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
  // 跨实例接管的握手槽：存放「上一个实例的 disposeHub」。
  const PREV_DISPOSE_KEY = '__2ag_ui_dispose';
  // 本次求值的实例指纹。每次求值都是全新 IIFE，故它天然唯一。
  // 用途：给 shadow 宿主节点盖章 —— 节点上的章与当前实例不一致，说明那棵树是别的实例
  // 建的，必须丢弃重建（见 mountShadowUI 里的 stillUsable 判定）。
  const HUB_INSTANCE_ID = Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 8);

  // 「哪些面板此刻是展开的」的窗口级记录槽。
  // 用途：热重载（以及任何配置变更后的重注入）会在同一文档里跑出一个新实例，旧实例
  // 的整棵 UI 树被 dispose 拆除重建，新实例的 isPanelOpen / 抽屉 data-open 都从默认的
  // 收起态开始 —— 于是用户正在看的控制舱、正在拖的微调滑块会凭空消失。
  // 面板自身的操作恰好就是这条路径（拖微调滑块 → SET_THEME → Manager 热重载），
  // 所以这一格必须记、必须恢复。
  // 只记在 window 上而不写 localStorage：同一个文档内跨实例存活（够用），
  // 而整页跳转时窗口对象是全新的，面板自然回到收起态，
  // 不会在用户刚打开的新页面上突然弹出一个控制舱。
  const UI_OPEN_KEY = '__2ag_ui_open';

  // 读取 / 写入上面那格记录。写成独立函数是因为三个写入点（控制舱、微调抽屉、
  // 账号手风琴）分布在文件的不同层，且都要容忍宿主把 window 换成代理对象之类的情况。
  function readUiOpenState() {
    try {
      const raw = window[UI_OPEN_KEY];
      return (raw && typeof raw === 'object') ? raw : {};
    } catch (_) {
      return {};
    }
  }
  function rememberUiOpenState(key, value) {
    try {
      const cur = readUiOpenState();
      cur[key] = !!value;
      window[UI_OPEN_KEY] = cur;
    } catch (_) {}
  }

  // 0. 接管旧实例（本文件每次被求值都是一个全新 IIFE 实例）。
  // 求值时机有两类：①Page.addScriptToEvaluateOnNewDocument 在「每个新文档」注入一次；
  // ②/api/v1/hot/reload 与配置变更推送会在「当前文档」再求值一次。
  // 每个实例各有自己的 state、闭包与 shadow 引用。若不先请旧实例退场，旧实例的
  // 2s 巡检 / 4s 配额轮询 / 窗口级 pointerdown·keydown 监听会继续驱动它自己那棵
  // 早已过期的 Shadow DOM —— 实测表现是「热重载返回 injected:1 成功，界面纹丝不动」：
  // 新实例只跑完了不依赖挂载的那几步（原生样式、模态玻璃变量），
  // 而 mountShadowUI() 因为「旧宿主节点仍在 body 上且浮标可见」直接早退，
  // 第 6 个调参芯片、微调抽屉、凭据快照行永远不落 DOM。
  // 这里把自毁注册到窗口级命名槽，新实例启动第一件事就是关掉旧实例 ——
  // 与 installDomObserver 的同一条设计口径（新实例必须顶掉旧实例）。
  // 回退链里带上 window.__2ag.dispose：本文件历史上就暴露过这个入口，
  // 因此即便页面上跑的是「还没有握手槽的旧版本」，也能被本行收掉
  // （否则旧实例的 2s 巡检 / 4s 配额轮询 / 窗口级监听会一直空转到页面关闭）。
  try {
    let prevDispose = window[PREV_DISPOSE_KEY];
    if (typeof prevDispose !== 'function' && window.__2ag && typeof window.__2ag.dispose === 'function') {
      prevDispose = window.__2ag.dispose;
    }
    if (typeof prevDispose === 'function') prevDispose();
  } catch (_) {}

  // 1. 基础状态初始化
  // modalOpacity 与 Go 端 config.Config.ModalOpacity 对应；补丁背景层只用 blur/opacity，
  // 但换肤时三个值必须整套记录，才能与主窗口的 SET_PRESET 分支保持同一份语义。
  const state = Object.assign({
    wallpaper: '',
    blur: 28,
    opacity: 0.6,
    modalOpacity: 0.9,
    preset: 'Dark Dream',
    language: 'zh-CN'
  }, INITIAL_CONFIG || {});

  // 1.05 INITIAL_CONFIG 用的是后端 config.Config 的 snake_case 字段名（modal_opacity），
  // 而本文件内部一律用 modalOpacity —— 上面那句 Object.assign 于是只会多出一个
  // 永远没人读的 modal_opacity 键，state.modalOpacity 永远停在默认 0.9。
  // 实测后果：在主窗口把「模态弹窗遮蔽度」从 90% 拖到 80%，宿主遮罩毫无变化。
  // 这个问题此前一直没被发现，是因为出厂的用户配置值恰好就是 0.9 —— 巧合掩盖，不是没坏。
  if (INITIAL_CONFIG && typeof INITIAL_CONFIG.modal_opacity === 'number' && isFinite(INITIAL_CONFIG.modal_opacity)) {
    state.modalOpacity = INITIAL_CONFIG.modal_opacity;
  }

  // 1.5 GravityBoost 真实开关（来源：2ag.json → config.Config.GravityBoost → HubConfig.GravityBoost
  // → __2AG_INITIAL_CONFIG__.gravity_boost）。历史上这 9 个字段一路写进状态机却没有任何消费方，
  // 六个开关在界面上装作"已生效"。这里建立唯一的消费入口，让开关真正驱动宿主行为。
  const boost = Object.assign({
    session_delete: false,
    markdown_export: false,
    paste_plaintext_fix: false,
    session_id_tag: false,
    centered_width: true,
    preserve_scroll: true,
    force_zh_cn: true,
    enable_devtools: true,
    disable_auto_update: false
  }, (INITIAL_CONFIG && INITIAL_CONFIG.gravity_boost) || {});

  // 1.6 区域语言锁定（force_zh_cn 的真实落点之一）
  // 宿主是 Electron/Chromium，界面语言由 localStorage 里已有的 locale 键决定，
  // 仅靠启动参数无法覆盖"曾经被写成 en"的既有 profile。这里在不破坏已有值的前提下补齐。
  function enforceHostLocale() {
    if (!boost.force_zh_cn) return;
    const localeKeys = [
      'language', 'locale', 'vscode-nls-locale', 'i18n.locale',
      'antigravity.locale', 'user.language', 'workbench.locale'
    ];
    try {
      for (const key of localeKeys) {
        const current = localStorage.getItem(key);
        if (current === null) continue;               // 宿主没写过这个键，不擅自新增
        const lower = String(current).toLowerCase();
        if (lower.startsWith('zh')) continue;          // 已经是中文，不动
        localStorage.setItem(key, 'zh-CN');
      }
    } catch (_) {}
    try {
      // 早先这里写的是「只在 lang 缺失时才补」—— 那是一条从未触发过的死分支：
      // 宿主 index.html 是静态的 <html lang="en">，属性永远存在，于是「区域语言锁定」
      // 在属性这一侧从来没有生效过（真机实测 document.documentElement.lang 恒为 "en"）。
      // 改为直接锁定：这个开关的名字就是「强制锁定为 zh-CN」，不是「缺失时兜底」。
      // 改动风险已实测：宿主 bundle 与 jetbox.css / compiled_tailwind.css 里
      // :lang(...) 伪类命中数均为 0，全 bundle 对 lang 属性的读取只有一处，
      // 且是把它当遥测里的 locale 上报 —— 锁成 zh-CN 正是那个字段该有的值。
      if (document.documentElement.getAttribute('lang') !== 'zh-CN') {
        document.documentElement.setAttribute('lang', 'zh-CN');
      }
    } catch (_) {}
  }

  // 1.6.1 宿主界面词典层 —— force_zh_cn 的第二个落点，也是「汉化」真正会发生的地方。
  //
  // 在此之前，这个开关的汉化能力只有 enforceHostLocale()：它把 localStorage 里已有的
  // locale 键改成 zh-CN。但上游 bundle 是纯英文硬编码 —— 9.5 MB 的 main.js 里
  // useTranslation / translations / zh-CN 的命中数全是 0，连一个 CJK 字符都没有，
  // 界面语言根本没有「切到中文」这个选项。要真的看到中文，只剩运行时 DOM 文本替换一条路。
  //
  // 词典与引擎放在这个自成一体的段里，刻意不掺进 mountShadowUI 那一大坨中枢 UI 逻辑：
  // 上游改版时最可能发生的事是「某些词条不再出现」，那时希望的结果是翻译静默失效，
  // 而不是把宿主或中枢一起带崩。对外只有 installHostI18n() 一个入口，内部全程 try/catch。
  //
  // 三条硬边界（都是实测出来的，不是保守估计）：
  //   1. 只替换「整个文本节点精确等于词条」的情况，绝不做子串替换。
  //      宿主正文是一棵巨大的 Markdown 树，任何子串规则都会把用户写的英文句子改成中文。
  //   2. 整棵会话视图（[data-testid="conversation-view"]）不参与替换。
  //      精确匹配也挡不住「用户恰好只发了一个 Copy」这种极端情形，
  //      而漏译几个消息工具条按钮的代价，远小于改写用户正文或模型输出。
  //   3. 输入框（contenteditable / Lexical）与代码块（pre/code）整块跳过。
  //      注意输入框里那句占位文本不是可编辑内容，所以它照样会被翻译。
  const HOST_I18N_SKIP_SELECTOR = [
    'script', 'style', 'noscript', 'textarea', 'input', 'select', 'option',
    'pre', 'code', 'kbd', 'samp', 'var',
    '[contenteditable]:not([contenteditable="false"])',
    '[data-lexical-editor]', '.monaco-editor',
    '[data-testid="conversation-view"]',
    '[data-testid="pending-user-messages"]',
    '[data-testid^="markdown-"]', '.markdown-frontmatter'
  ].join(',');

  const HOST_I18N_ATTRS = ['aria-label', 'placeholder', 'title', 'data-title'];

  // 编辑区本身要跳过，但它的 aria-label（「Message input」之类）是界面词、不是内容词。
  // 于是给这几个宿主单独开一档：翻译它自己的属性，但绝不进它的子树 ——
  // 子树里正是用户正在打的字。
  const HOST_I18N_ATTR_ONLY_SELECTOR = [
    '[contenteditable]:not([contenteditable="false"])',
    '[data-lexical-editor]', '.monaco-editor'
  ].join(',');

  // 词典：左列每一条都来自对上游 bundle 的逐字核验（在 main.js 里以 "字面量" 形式命中才算数），
  // 没有一条是按语感猜的。那些看起来该有、实测在上游并不存在的译法
  // （Show Sidebar / New Chat / Language / Privacy / Log out / Documentation / Font Size …）
  // 一律不收 —— 收录它们只会制造「词典里有、界面上永远不出现」的假覆盖。
  const HOST_I18N_DICT = {
    // 侧栏与导航（真机 DOM 上以 data-testid 标记的那几块）
    'New Conversation': '新建对话',
    'Conversation History': '对话历史',
    'Scheduled Tasks': '计划任务',
    'Projects': '项目',
    'Conversations': '对话',
    'Settings': '设置',
    'No Project': '无项目',
    'More actions': '更多操作',
    'Create New Project': '新建项目',
    'Sidebar': '侧边栏',
    'Toggle Sidebar': '切换侧边栏',
    'Display Options': '显示选项',

    // 输入区
    'Ask anything, @ to mention, / for actions': '随便问点什么 — @ 提及上下文，/ 唤起操作',
    'Message input': '消息输入框',
    'Add context': '添加上下文',
    'Send message': '发送消息',
    'Record voice memo': '录制语音备忘',
    'Typeahead menu': '候选菜单',

    // 标题栏菜单
    'File': '文件',
    'View': '视图',
    'Window': '窗口',
    'Help': '帮助',
    'Notifications': '通知',
    'Go Back': '后退',
    'Go Forward': '前进',

    // 通用动作
    'Cancel': '取消',
    'Confirm': '确认',
    'Save': '保存',
    'Close': '关闭',
    'Retry': '重试',
    'Copy code': '复制代码',
    'Delete': '删除',
    'Rename': '重命名',
    'Search': '搜索',
    'Restore': '恢复',
    'Pin': '固定',
    'Unpin': '取消固定',
    'Archive': '归档',
    'Archive Conversation': '归档对话',
    'Export': '导出',
    'Share': '分享',
    'Feedback': '反馈',
    'About': '关于',
    'Reload': '重新加载',
    'Reload Window': '重新加载窗口',
    'Refresh': '刷新',
    'Try Again': '重试',
    'Try again': '重试',
    'Undo': '撤销',
    'Select All': '全选',
    'Find': '查找',
    'Zoom In': '放大',
    'Zoom Out': '缩小',
    'Minimize': '最小化',
    'Maximize': '最大化',
    'Restart': '重启',
    'Enable': '启用',
    'Disable': '禁用',

    // 设置与弹层
    'Account': '账号',
    'Appearance': '外观',
    'General': '通用',
    'Advanced': '高级',
    'Terminal': '终端',
    'Extensions': '扩展',
    'Plugins': '插件',
    'MCP Servers': 'MCP 服务',
    'Model': '模型',
    'Models': '模型',
    'Context': '上下文',
    'Usage': '用量',
    'Version': '版本',
    'Update': '更新',
    'Restart to Update': '重启以更新',
    'Check for Updates': '检查更新',
    'Download': '下载',
    'Install': '安装',
    'Installed': '已安装',
    'Enabled': '已启用',
    'Disabled': '已禁用',
    'Allow': '允许',
    'Deny': '拒绝',
    'Approve': '批准',
    'Reject': '驳回',
    'Continue': '继续',
    'Skip': '跳过',
    'Stop': '停止',
    'Running': '运行中',
    'Completed': '已完成',
    'Error': '错误',
    'Warning': '警告',

    // 工作区 / 企业向
    'Workspace': '工作区',
    'Skills': '技能',
    'Rules': '规则',
    'Subagents': '子代理',
    'Automations': '自动化',
    'Scheduled': '已计划',
    'Schedule': '计划',
    'Run Now': '立即运行',
    'View Logs': '查看日志',
    'Manage Permissions': '管理权限',
    'Permissions': '权限',
    'Network': '网络',
    'Experimental': '实验性',
    'Preview': '预览',
    'New Project': '新建项目',
    'Project': '项目',
    'Open File': '打开文件',

    // 模型档位（model-selector-effort-option；真机上是模型选择器内的独立文本节点）
    'High': '高',
    'Medium': '中',
    'Low': '低',

    // 真机全站扫描后剩下的英文：品牌名 Antigravity 与模型名 Gemini 3.8 Flash 刻意不译（它们是数据不是界面词）
    'Install IDE': '安装 IDE',

    // 其余通用
    'Command Palette': '命令面板',
    'Theme': '主题',
    'Light': '浅色',
    'Dark': '深色',
    'System': '跟随系统',
    'Something went wrong': '出错了',
    'Got it': '知道了',
    'Next': '下一步',
    'Back': '返回',
    'Done': '完成',
    'Finish': '结束',
    'Apply': '应用',
    'Reset': '重置',
    'No models available': '无可用模型',
    'Learn more': '了解更多',
    'Recording': '录制中',
    'Stop recording': '停止录制',
    'Loading': '加载中',
    'Loading...': '加载中…',
    'No results': '无结果',
    'Sign in': '登录',
    'Sign out': '退出登录',
    'Mark all as read': '全部标为已读',
    'Clear all': '全部清除',
    'Dismiss': '忽略'
  };

  // 模板型 aria-label：冒号后面是模型名 / 项目名，属动态数据，必须原样保留。
  // 这两条来自上游 bundle 里的模板字面量，原文是
  //   aria-label={`Select model, current: ${t}${v ? ` ${v}` : ''}`}
  //   aria-label={`Select project, current: ${A}`}
  // 用全等匹配永远叫不醒它们，只能按前缀识别。
  const HOST_I18N_PATTERNS = [
    [/^Select model, current: (.+)$/, '选择模型，当前：'],
    [/^Select project, current: (.+)$/, '选择项目，当前：']
  ];

  const I18N_OBSERVER_KEY = '__2ag_i18n_observer';
  let hostI18nQueue = new Set();
  let hostI18nTimer = null;
  let hostI18nPrimed = false;
  let hostI18nScans = 0;
  let hostI18nHits = 0;

  // 首尾空白必须留着：相邻文本节点常靠一个空格分词，吞掉会把两个词粘在一起。
  function hostI18nTextValue(raw) {
    const source = String(raw);
    const trimmed = source.trim();
    if (!trimmed) return null;
    if (!Object.prototype.hasOwnProperty.call(HOST_I18N_DICT, trimmed)) return null;
    const at = source.indexOf(trimmed);
    return source.slice(0, at) + HOST_I18N_DICT[trimmed] + source.slice(at + trimmed.length);
  }

  function hostI18nAttrValue(raw) {
    if (!raw) return null;
    const exact = hostI18nTextValue(raw);
    if (exact !== null) return exact;
    const source = String(raw);
    const trimmed = source.trim();
    if (!trimmed) return null;
    const lead = source.slice(0, source.indexOf(trimmed));
    for (const pair of HOST_I18N_PATTERNS) {
      // 两种写法都要认：英文原文（首译），以及「已经被本层译过一遍」的形式。
      // 后者不是锦上添花 —— 热重载会重新注入一份新补丁，若只认英文原文，
      // 那就等于「第一次注入译得对，第二次注入之后动态位永久留在英文」：
      // 属性一旦变成「选择项目，当前：No Project」就再也匹配不上 /^Select project, current: /。
      // 幂等在这里是正确性要求，不是优化。
      const matched = trimmed.match(pair[0]);
      const dynamic = matched
        ? matched[1]
        : (trimmed.indexOf(pair[1]) === 0 ? trimmed.slice(pair[1].length) : null);
      if (dynamic === null) continue;
      // 捕获组本来是动态值（模型名 / 项目名），原则上原样保留。
      // 但 'No Project' 这类本身就是界面固定词、只是恰好落在动态位上，仍应跟词典走；
      // 词典里没有的真实项目名与真实模型名自然保持原样。
      const known = Object.prototype.hasOwnProperty.call(HOST_I18N_DICT, dynamic)
        ? HOST_I18N_DICT[dynamic]
        : dynamic;
      return lead + pair[1] + known;
    }
    return null;
  }

  function hostI18nApplyText(node) {
    const next = hostI18nTextValue(node.nodeValue);
    if (next === null || next === node.nodeValue) return;
    node.nodeValue = next;
    hostI18nHits += 1;
  }

  function hostI18nApplyAttrs(el) {
    if (typeof el.hasAttribute !== 'function') return;
    for (const name of HOST_I18N_ATTRS) {
      if (!el.hasAttribute(name)) continue;
      const current = el.getAttribute(name);
      const next = hostI18nAttrValue(current);
      if (next === null || next === current) continue;
      el.setAttribute(name, next);
      hostI18nHits += 1;
    }
  }

  // 单次遍历。用 TreeWalker 而不是递归：宿主会话树可能上万节点，
  // 而 FILTER_REJECT 在 TreeWalker 上的语义正好是「连同整棵子树一起跳过」——
  // 命中 pre / contenteditable / 会话视图时整块不再往下走，这是性能与安全的同一个开关。
  function hostI18nWalk(root) {
    if (!root) return;
    if (root.nodeType === 3) { hostI18nApplyText(root); return; }
    if (root.nodeType !== 1 && root.nodeType !== 9 && root.nodeType !== 11) return;
    if (root.nodeType === 1) {
      if (typeof root.matches === 'function' && root.matches(HOST_I18N_SKIP_SELECTOR)) {
        if (root.matches(HOST_I18N_ATTR_ONLY_SELECTOR)) hostI18nApplyAttrs(root);
        return;
      }
      hostI18nApplyAttrs(root);
    }
    const walker = document.createTreeWalker(
      root,
      NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT,
      {
        acceptNode(node) {
          if (node.nodeType === 3) return NodeFilter.FILTER_ACCEPT;   // 3 = TEXT_NODE（不是 SHOW_TEXT 掩码 4）
          if (typeof node.matches === 'function' && node.matches(HOST_I18N_SKIP_SELECTOR)) {
            if (node.matches(HOST_I18N_ATTR_ONLY_SELECTOR)) hostI18nApplyAttrs(node);
            return NodeFilter.FILTER_REJECT;
          }
          return NodeFilter.FILTER_ACCEPT;
        }
      }
    );
    hostI18nScans += 1;
    let node = walker.nextNode();
    while (node) {
      if (node.nodeType === 3) hostI18nApplyText(node);
      else hostI18nApplyAttrs(node);
      node = walker.nextNode();
    }
  }

  function hostI18nDrain() {
    hostI18nTimer = null;
    const batch = Array.from(hostI18nQueue);
    hostI18nQueue = new Set();
    if (hubDisposed || !boost.force_zh_cn) return;
    for (const node of batch) {
      try { hostI18nWalk(node); } catch (_) {}
    }
  }

  function hostI18nSchedule(node) {
    if (!node) return;
    // Set 而不是数组：React 重渲染会在一帧里反复产出同一棵子树。
    hostI18nQueue.add(node);
    // 上游流式输出会持续改 characterData。不合并的话每个 token 都要重走一遍子树，
    // 那是把「翻译一下界面」做成一个常驻 CPU 负担。
    if (hostI18nTimer) return;
    hostI18nTimer = window.setTimeout(hostI18nDrain, 120);
  }

  // 唯一入口，被 tick() 每 2s 调用一次，因此必须幂等。
  function installHostI18n() {
    if (hubDisposed) return;
    // 开关关掉时停止观察，但不做「把已经译成中文的再译回英文」的回滚：
    // 文本节点会被 React 随时替换，逐个回滚既不可靠也没意义。
    // 重新打开开关（或刷新页面）时，词典层按当前 DOM 重新生效。
    if (!boost.force_zh_cn) {
      if (window[I18N_OBSERVER_KEY]) {
        try { window[I18N_OBSERVER_KEY].disconnect(); } catch (_) {}
        window[I18N_OBSERVER_KEY] = null;
      }
      hostI18nPrimed = false;
      return;
    }
    if (document.documentElement && !window[I18N_OBSERVER_KEY]) {
      window[I18N_OBSERVER_KEY] = new MutationObserver((records) => {
        if (hubDisposed || !boost.force_zh_cn) return;
        for (const record of records) {
          if (record.type === 'childList') {
            for (const added of record.addedNodes) hostI18nSchedule(added);
          } else {
            hostI18nSchedule(record.target);
          }
        }
      });
      try {
        // subtree 是硬要求：上游的菜单、弹层、下拉都走 base-ui portal 挂到 body 末尾，
        // 不在「当前面板」这棵子树里。
        window[I18N_OBSERVER_KEY].observe(document.documentElement, {
          childList: true, subtree: true,
          characterData: true,
          attributes: true, attributeFilter: HOST_I18N_ATTRS
        });
      } catch (_) {}
    }
    // 首次全站扫描要等 body 真的存在（document-start 注入时它还是 null）。
    // 单独用一个旗标而不是「装了观察器就算完」—— 否则 body 迟到的那次扫描永远不会发生。
    if (!hostI18nPrimed && document.body) {
      hostI18nPrimed = true;
      try { hostI18nWalk(document.body); } catch (_) {}
    }
  }

  // 1.7 剪贴板纯文本净化（paste_plaintext_fix 的真实落点）
  // 宿主会话常把富文本 HTML 一并塞进输入框，导致 Token 浪费与排版崩坏。
  // 捕获阶段拦截 paste，用纯文本重写剪贴板内容后再放行。
  function installPastePlaintextFix() {
    if (!boost.paste_plaintext_fix) return;
    if (window.__2ag_paste_fix_installed) return;
    window.__2ag_paste_fix_installed = true;
    window.addEventListener('paste', (e) => {
      if (!boost.paste_plaintext_fix) return;
      if (!e.clipboardData) return;
      const text = e.clipboardData.getData('text/plain');
      if (typeof text !== 'string' || text.length === 0) return;
      const html = e.clipboardData.getData('text/html');
      if (!html) return;   // 本来就是纯文本，无需干预
      e.preventDefault();
      e.stopPropagation();
      const target = e.target;
      if (target && typeof target.value === 'string') {
        // 受控输入：走原生 setter + input 事件，让框架 state 与服务端保持一致
        writeValueIntoInput(target, text);
      } else {
        document.execCommand('insertText', false, text);
      }
    }, true);
  }

  // 1.8 DevTools 穿透（enable_devtools 的真实落点）
  // 宿主自身会在捕获阶段吞掉 F12；这里在自己的捕获监听里抢先放行，
  // 让按键回到 Chromium 原生处理链，从而真正弹出 DevTools。
  function installDevtoolsPassthrough() {
    if (window.__2ag_devtools_passthrough_installed) return;
    window.__2ag_devtools_passthrough_installed = true;
    window.addEventListener('keydown', (e) => {
      if (!boost.enable_devtools) return;
      if (e.key !== 'F12' && e.keyCode !== 123) return;
      // 不 preventDefault：默许 Chromium 的原生 DevTools 快捷键生效，
      // 只切断宿主上层可能存在的吞噬逻辑。
      e.stopImmediatePropagation();
    }, true);
  }

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
  // text_fallback 没有宿主侧消费方（见其 UI 处的说明）。历史上它可被点亮并写进 localStorage，
  // 于是老用户的 localStorage 里可能残留 true —— 那会让置灰的圆环仍然显示为「已开启」。
  // 这里强制归位并回写存储，保证内存状态、持久化状态、界面三者呈现同一个事实：
  // 该能力不存在。（saveSubsystems 是函数声明，声明提升，此处调用安全。）
  subsystems.text_fallback = false;
  saveSubsystems();

  // 2. 浮标自由停靠位置持久化管理
  // 历史模型是 { side: 'left'|'right', topRatio }：只能表达"贴左/贴右"两列，
  // 配合松手时的弹性吸附算法，用户拖到屏幕正中也会被强行弹回边缘。
  // 现在改为自由坐标 —— 存「浮标左上角占可用空间的百分比」：
  // 既跨分辨率/跨窗口尺寸稳定（同一百分比落在同一相对位置），
  // 又不对位置做任何再解释：拖到哪里就停在哪里。
  const clamp01 = (v) => (v < 0 ? 0 : v > 1 ? 1 : v);
  let hubPos = { xRatio: 0.98, yRatio: 0.45 };
  try {
    const savedPos = JSON.parse(localStorage.getItem(POS_STORAGE_KEY) || '{}');
    if (typeof savedPos.xRatio === 'number' && !isNaN(savedPos.xRatio)) {
      hubPos.xRatio = clamp01(savedPos.xRatio);
    } else if (savedPos.side === 'left' || savedPos.side === 'right') {
      // 老版本遗留的 { side, topRatio } 就地迁移：贴左→0.02，贴右→0.98。
      hubPos.xRatio = savedPos.side === 'left' ? 0.02 : 0.98;
    }
    if (typeof savedPos.yRatio === 'number' && !isNaN(savedPos.yRatio)) {
      hubPos.yRatio = clamp01(savedPos.yRatio);
    } else if (typeof savedPos.topRatio === 'number' && !isNaN(savedPos.topRatio)) {
      hubPos.yRatio = clamp01(savedPos.topRatio);
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

  // 2.1 主题库（与 Go 端 internal/core/state.go 的 SET_PRESET 分支逐项对齐）。
  // 这里的三个数值必须与后端分支完全一致，否则「舱内换肤」与「主窗口换肤」会给出
  // 两套不同外观 —— 用户会看到同一个主题名对应两种风格。
  //   native    → Blur 0  / Opacity 0.1  / Modal 0.1 → 见 state.go 的 native 分支
  //   pure_dark → Blur 10 / Opacity 0.90 / Modal 0.95
  //   cyberpunk → Blur 15 / Opacity 0.70 / Modal 0.90
  //   frosted   → Blur 30 / Opacity 0.40 / Modal 0.80
  //   electric  → Blur 24 / Opacity 0.65 / Modal 0.88
  const THEME_PRESETS = [
    { id: 'native',    short: 'Default',      title: '恢复默认外观（关闭壁纸与模糊）', swatch: 'linear-gradient(135deg, #202124 0%, #2d2e30 100%)', blur: 0,  opacity: 0.1,  modalOpacity: 1.0 },
    { id: 'spectrum',  short: '2Ag Spectrum', title: '2Ag Spectrum v1.0.0 · 深色底 + 四色柔和辉光', swatch: 'linear-gradient(135deg, #0e1116 0%, #16202b 46%, #1b2b26 76%, #2a2119 100%)', blur: 18, opacity: 0.78, modalOpacity: 0.92 },
    { id: 'pure_dark', short: 'Obsidian',     title: 'Obsidian Minimal v1.0.0 · Google 护眼灰', swatch: 'linear-gradient(135deg, #131314 0%, #1e1f20 50%, #282a2c 100%)', blur: 10, opacity: 0.90, modalOpacity: 0.95 },
    { id: 'frosted',   short: 'Slate Aurora', title: 'Slate Aurora v2.0.0 · 极光冷灰',        swatch: 'linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #8ab4f8 100%)', blur: 30, opacity: 0.40, modalOpacity: 0.80 },
    { id: 'cyberpunk', short: 'Cyberpunk',    title: 'Cyberpunk Void v1.1.2 · 极光暗涌',      swatch: 'linear-gradient(135deg, #0d0221 0%, #240046 50%, #4285f4 100%)', blur: 15, opacity: 0.70, modalOpacity: 0.90 },
    { id: 'electric',  short: 'Gemini Dusk',  title: 'Gemini Dusk v1.0.4 · 极星暮色',         swatch: 'linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #fdd663 100%)', blur: 24, opacity: 0.65, modalOpacity: 0.88 }
  ];

  // ── 账号邮箱脱敏 ────────────────────────────────────────────────
  // 邮箱默认以「首字符 + 星号 + 尾字符」露出，点击旁边的眼睛才显示全文。默认隐藏是刻意的：
  // 截图、录屏、直播、共享屏幕时不必先手动打码。脱敏发生在**渲染层**，不是丢数据 ——
  // 完整邮箱仍然在后端与按钮的 data-email 里，切换等操作不受影响。
  //
  // 掩码策略：只脱敏 local-part（首字符 + 星号 + 尾字符），domain 完整保留。
  // local-part 是身份标识，domain 是公开信息（gmail.com / outlook.com 谁都知道），
  // 保留 domain 既能让用户一眼认出这是邮箱、又能快速区分多个账号。
  // 星号固定 3 个而非按真实长度铺开 —— 长度本身也不外泄。
  function maskEmail(raw) {
    const s = String(raw === undefined || raw === null ? '' : raw);
    if (!s) return '';
    const at = s.lastIndexOf('@');
    if (at <= 0) return s;                       // 无 @ 或 @ 开头 ⇒ 不是邮箱，原样返回
    const local = s.slice(0, at);
    const domain = s.slice(at + 1);
    if (!domain) return local;                   // "a@" 这种残缺输入只返回 local
    // 1~2 字符的 local-part 信息量本来就极低，保留原样即可。
    let masked;
    if (local.length === 1) masked = local;
    else if (local.length === 2) masked = local.charAt(0) + '*';
    else masked = local.charAt(0) + '***' + local.charAt(local.length - 1);
    return masked + '@' + domain;
  }

  // 哪些账号的邮箱被用户显式展开过。
  //
  // 状态必须是模块级的、以邮箱为 key 的集合，不能只挂在 DOM 上：
  // refreshAccountList 每次都会整体重写列表 innerHTML，宿主重建时也会重跑
  // paintAccountHead —— 状态若只活在 dataset 里，用户的展开动作撑不过一次刷新。
  // 以邮箱（而非元素）为 key，则同一账号在头部与列表中共享同一个展开态。
  const revealedEmails = new Set();

  function isEmailRevealed(rawEmail) {
    return revealedEmails.has(String(rawEmail || ''));
  }

  // 眼睛按钮：与文本并列渲染，点击只切换同一行内的可见性。
  // 用 button 而不是 span —— 键盘可聚焦，且能挂 aria-pressed 表达真实状态。
  function eyeBtnHtml(shown, label) {
    const title = shown ? '隐藏邮箱' : '显示完整邮箱';
    return (
      '<button class="acct-eye" type="button" data-revealed="' + (shown ? 'true' : 'false') + '"' +
      ' aria-pressed="' + (shown ? 'true' : 'false') + '"' +
      ' aria-label="' + title + '" title="' + title + '">' +
      '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">' +
      '<path class="eye-open" d="M12 5c-5 0-9 4.5-9 7s4 7 9 7 9-4.5 9-7-4-7-9-7zm0 11a4 4 0 110-8 4 4 0 010 8zm0-6a2 2 0 100 4 2 2 0 000-4z"/>' +
      '<path class="eye-off" d="M3 3l18 18M10.6 10.7a3 3 0 004.2 4.2M6.5 7.1C4.2 8.6 3 11 3 12c0 2.5 4 7 9 7 1.6 0 3.1-.5 4.4-1.3M17.7 16.6C19.8 15.1 21 12.9 21 12c0-2.5-4-7-9-7-.9 0-1.8.2-2.6.5"/>' +
      '</svg></button>'
    );
  }

  // 一行邮箱 = 文本 + 眼睛。revealed 状态由 revealedEmails 决定，同一账号处处一致。
  //
  // 状态为什么不放在 DOM 上：refreshAccountList 会整体重写 acct-list 的 innerHTML，
  // 隐藏/展开如果只活在 dataset 里，任何一次列表刷新都会把用户的展开动作抹掉。
  // 以邮箱为 key 而不是以元素为 key —— 手风琴头部与列表行显示的是同一个账号，
  // 展开一次应当两处都展开（toggleHeadEmailReveal 也读写同一个集合）。
  function emailCellHtml(rawEmail, revealed) {
    const shown = revealed === undefined ? isEmailRevealed(rawEmail) : revealed === true;
    const full = escapeHtml(rawEmail || '(未知账号)');
    const masked = shown ? full : escapeHtml(maskEmail(rawEmail) || '(未知账号)');
    return (
      '<span class="acct-mail" data-full="' + full + '" data-raw="' + escapeHtml(String(rawEmail || '')) + '" data-shown="' + (shown ? 'true' : 'false') + '">' + masked + '</span>' +
      eyeBtnHtml(shown, full)
    );
  }

  // 眼睛点击的统一处理：切换 data-shown 并重写文本，不触碰 DOM 结构。
  function toggleEmailReveal(btn) {
    if (!btn) return;
    const row = btn.parentNode;
    const span = row ? row.querySelector('.acct-mail') : null;
    if (!span) return;
    const show = span.dataset.shown !== 'true';
    // 先落状态再改 DOM：下次 renderAccountList 会经 emailCellHtml → isEmailRevealed
    // 读回同一个状态，用户展开的账号不会在列表刷新后自己收起来。
    const raw = span.dataset.raw || unescapeHtml(span.dataset.full);
    if (show) revealedEmails.add(String(raw));
    else revealedEmails.delete(String(raw));
    applyEmailReveal(span, btn, show);
  }

  // 手风琴头部那颗眼睛：文本节点是 #acct-head-mail（不在 .acct-mail 之内），
  // 且文本由 paintAccountHead 以 textContent 写入 —— 这里必须同样走 textContent，
  // 因为 dataset.full 存的是**已转义**的 HTML 片段，textContent 会原样显示实体。
  // 因此头部单独走一条「原始值」通路：dataset.raw 里再存一份原文。
  function toggleHeadEmailReveal(btn) {
    const span = shadow.getElementById('acct-head-mail');
    if (!span) return;
    const nowShown = span.dataset.shown !== 'true';
    const raw = span.dataset.raw || '';
    if (nowShown) revealedEmails.add(String(raw));
    else revealedEmails.delete(String(raw));
    span.dataset.shown = nowShown ? 'true' : 'false';
    span.textContent = nowShown ? raw : maskEmail(raw);
    syncEyeState(btn, nowShown);
  }

  // 统一的「状态落地」：文本、dataset、aria、title 一次写完，避免四处各写一半。
  function applyEmailReveal(span, btn, show) {
    span.dataset.shown = show ? 'true' : 'false';
    // data-full 写入时已 escapeHtml 过一次，直接回填不会二次转义；
    // 脱敏后的文本必须再转义一次。
    span.innerHTML = show ? span.dataset.full : escapeHtml(maskEmail(unescapeHtml(span.dataset.full)));
    syncEyeState(btn, show);
  }

  function syncEyeState(btn, show) {
    if (!btn) return;
    btn.dataset.revealed = show ? 'true' : 'false';
    btn.setAttribute('aria-pressed', show ? 'true' : 'false');
    const title = show ? '隐藏邮箱' : '显示完整邮箱';
    btn.setAttribute('title', title);
    btn.setAttribute('aria-label', title);
  }

  // data-full 里存的是「已转义」的文本；脱敏前必须先还原成原文，
  // 否则 &amp; 之类的实体会被当成三段字符参与掩码计算。
  function unescapeHtml(value) {
    return String(value === undefined || value === null ? '' : value)
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")
      .replace(/&amp;/g, '&');
  }

  // 账号邮箱/名称来自磁盘上的账号 JSON（用户可自行编辑），拼进 innerHTML 前必须转义。
  // 全文件此前没有这个函数，账号列表又必须走 innerHTML 渲染 —— 不转义就是一个注入面。
  function escapeHtml(value) {
    return String(value === undefined || value === null ? '' : value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  // 2.2 皮肤配置持久化。键名早已存在（STORAGE_KEY）却从未被读写 —— 舱内换肤必须真的
  // 落盘，否则宿主重载后皮肤回到旧值，用户会以为「换了没用」。
  function loadSkinConfig() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
      if (!saved || typeof saved !== 'object') return null;
      const preset = THEME_PRESETS.some((t) => t.id === saved.preset) ? saved.preset : null;
      const blur = (typeof saved.blur === 'number' && isFinite(saved.blur) && saved.blur >= 0) ? saved.blur : null;
      const opacity = (typeof saved.opacity === 'number' && isFinite(saved.opacity) && saved.opacity >= 0) ? saved.opacity : null;
      const modalOpacity = (typeof saved.modalOpacity === 'number' && isFinite(saved.modalOpacity) && saved.modalOpacity >= 0) ? saved.modalOpacity : null;
      if (preset === null && blur === null && opacity === null) return null;
      return { preset, blur, opacity, modalOpacity, updatedAt: Number(saved.updatedAt) || 0 };
    } catch (_) {
      return null;
    }
  }
  function saveSkinConfig(preset) {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({
        preset: preset,
        blur: state.blur,
        opacity: state.opacity,
        modalOpacity: state.modalOpacity,
        updatedAt: Date.now()
      }));
    } catch (_) {}
  }

  // 2.3 主题卡高亮同步（顶层函数，按 SHADOW_HOST_ID 现场寻址）。
  // 为什么不能写成某一次挂载闭包里的函数：主窗口每次改配置，Go 侧都会「先重注入补丁、
  // 再推送状态」。重注入产生的新实例走到 mountShadowUI 时会发现旧 UI 仍然可用而提前返回
  // （这是有意为之，避免闪烁重建），于是新实例永远不会执行到它自己的事件绑定段。若高亮
  // 同步绑死在「最新实例的闭包指针」上，主窗口换主题后面板高亮就会永远停在旧值 ——
  // 又一个看着能用、其实不动的假功能。这里改成从 DOM 现场取节点，任何实例调用都一致。
  function syncThemeChipsFromDom() {
    const root = document.getElementById(SHADOW_HOST_ID);
    const row = root && root.shadowRoot ? root.shadowRoot.getElementById('theme-row') : null;
    if (!row) return;
    // 高亮口径按「数值优先」裁决，而不是只信主题名：
    // 后端 cdp_injector.go 的 BuildHubConfig() 把 Preset 恒写为字面量 "Dark Dream"
    // （config.Config 里根本没有 preset 字段，主题只以 blur/opacity/modal_opacity 三个
    //  数值落盘），而 "Dark Dream" 并不在 THEME_PRESETS 白名单里。若只比对名字，高亮
    // 永远落空；若只信 localStorage，用户在主窗口换了主题后高亮就是错的。
    // 真正可靠的一致来源是数值 —— 谁的外观都一样，谁就该被点亮。
    const isKnown = (id) => THEME_PRESETS.some((t) => t.id === id);
    const matched = THEME_PRESETS.filter(
      (t) => t.blur === state.blur && Math.abs(t.opacity - state.opacity) < 0.001
    )[0];
    let activePreset = matched ? matched.id : (isKnown(state.preset) ? state.preset : null);
    if (!activePreset) {
      const saved = loadSkinConfig();
      if (saved && saved.preset && isKnown(saved.preset)) activePreset = saved.preset;
    }
    row.querySelectorAll('.theme-chip').forEach((chip) => {
      chip.dataset.active = (activePreset && chip.dataset.preset === activePreset) ? 'true' : 'false';
    });
  }

  // 2.4 端内微调抽屉（Live Fine-Tuning Drawer · P2 单元三）的顶层工具函数。
  // 与 2.3 同理：必须从 DOM 现场寻址，不能绑死在某一次挂载闭包里 —— 重注入后的新实例
  // 会因为「旧 UI 仍可用」而提前返回，闭包版本永远不会被重新绑定。
  // 抽屉里的三个滑块与主窗口「材质与视觉」三个滑块同一口径、同一落点：
  //   Blur 0–40px  → state.blur       → Dream-Skin 背景层 filter
  //   Darkness 10–90% → state.opacity → Dream-Skin 背景层 opacity
  //   Modal Glass 70–100% → state.modalOpacity → 宿主遮罩层 background-color 的 alpha
  // 自定义数值一旦偏离预设，主题就不再是「某个预设」，用一个不在白名单里的哨兵值顶掉
  // state.preset —— 否则 syncThemeChipsFromDom 的「主题名回退」会把某个芯片错误点亮。
  const TUNE_SENTINEL_PRESET = '__custom__';
  const tuneNum = (v, lo, hi, fallback) => {
    const n = typeof v === 'number' && isFinite(v) ? v : fallback;
    return n < lo ? lo : (n > hi ? hi : n);
  };
  function tuneDrawerParts() {
    const root = document.getElementById(SHADOW_HOST_ID);
    const shadow = root && root.shadowRoot ? root.shadowRoot : null;
    if (!shadow) return null;
    return {
      shadow: shadow,
      drawer: shadow.getElementById('live-tune-drawer'),
      chip: shadow.getElementById('theme-chip-tune'),
      blur: shadow.getElementById('tune-blur'),
      dark: shadow.getElementById('tune-dark'),
      modal: shadow.getElementById('tune-modal'),
      blurVal: shadow.getElementById('tune-blur-val'),
      darkVal: shadow.getElementById('tune-dark-val'),
      modalVal: shadow.getElementById('tune-modal-val')
    };
  }
  // 把当前 state 的三个数值反填到滑块与右侧数值文本（预设反哺滑块的落点）
  function syncTuneSliders() {
    const p = tuneDrawerParts();
    if (!p) return;
    const blur = Math.round(tuneNum(state.blur, 0, 40, 28));
    const dark = Math.round(tuneNum(state.opacity, 0.1, 0.9, 0.6) * 100);
    const modal = Math.round(tuneNum(state.modalOpacity, 0.7, 1, 0.9) * 100);
    if (p.blur) p.blur.value = String(blur);
    if (p.dark) p.dark.value = String(dark);
    if (p.modal) p.modal.value = String(modal);
    if (p.blurVal) p.blurVal.textContent = blur + 'px';
    if (p.darkVal) p.darkVal.textContent = dark + '%';
    if (p.modalVal) p.modalVal.textContent = modal + '%';
  }
  // 模态弹窗遮蔽度：宿主所有弹窗 / 抽屉 / 菜单的遮罩背景都由 Tailwind 的 bg-black/NN
  // 类上色（实测档位 /20 /25 /30 /40 /45 /50 /80），因此统一收束到一个 CSS 变量上。
  // 用变量而不是重写样式表，是为了让拖动滑块时的更新只改一个属性值 —— 毫秒级随动。
  function applyModalGlass(value) {
    const v = tuneNum(value, 0.7, 1, 0.9);
    const el = document.documentElement;
    if (el && el.style) el.style.setProperty('--2ag-modal-glass', String(v));
  }
  // 展开/收起抽屉。展开时若账号手风琴是展开态，先把它收起（互斥保护）：
  // 抽屉 240px + 手风琴 168px 列表在 441px 窄视口下会一起把控制舱撑破。
  function setTuneDrawer(open) {
    const p = tuneDrawerParts();
    if (!p || !p.drawer) return;
    const next = typeof open === 'boolean' ? open : (p.drawer.dataset.open !== 'true');
    p.drawer.dataset.open = next ? 'true' : 'false';
    if (p.chip) p.chip.dataset.active = next ? 'true' : 'false';
    rememberUiOpenState('tune', next);
    if (next) {
      const acc = p.shadow.getElementById('acct-acc');
      if (acc && acc.dataset.open === 'true') {
        acc.dataset.open = 'false';
        rememberUiOpenState('acct', false);
      }
      syncTuneSliders();
    }
  }

  // 2.5 发送按钮寻址与"终止语义"门禁
  //
  // 寻址分三层，按「信号是否由标准语义给出」排序，而不是按类名猜测：
  //   第 1 层 语义层：aria-label / data-testid / data-tooltip / role 这类
  //           无障碍属性或测试契约。官方重命名 CSS 类不会动它们；
  //           即使改版，改的也是值而不是「这里有没有一个叫 Send 的按钮」。
  //   第 2 层 结构层：button[type="submit"]。这是 HTML 规范，不是样式约定。
  //           放在语义层之后，因为一个页面里可能有多份表单。
  //   第 3 层 历史层：明文的类名猜测（send-button / send_button）。
  //           保留它是因为本项目曾在这批类名上跑通过真实宿主，删掉是纯粹的
  //           能力损失；但它降级为兜底 —— 前两层命中就轮不到它。
  //
  // 此前只有第 3 层与散落的语义属性混在一个字符串里，靠 querySelectorAll
  // 的文档顺序决定谁先被点到。文档顺序与「哪个才是真正的发送键」无关。
  const SEND_SELECTORS_TIERED = [
    'button[aria-label*="Send" i]',
    'button[aria-label*="发送"]',
    '[role="button"][aria-label*="Send" i]',
    '[role="button"][aria-label*="发送"]',
    'button[data-testid*="send" i]',
    '[data-testid*="send" i]',
    'button[data-tooltip*="Send" i]',
    'button[data-tooltip*="发送"]',
    'button[type="submit"]',
    'button.send-button',
    '[class*="send-button"]',
    '[class*="send_button"]',
  ];
  // 正向语义：与终止语义互斥。命中它的候选在同层内优先。
  const SEND_LABEL_RE = /(send|submit|发送|提交)/i;
  // 终止语义：命中即"坚决不点"。宿主在提交后常把这颗按钮原地复用为「停止生成」，
  // 回退点击若盲点上去，就会把用户刚发出的请求自己取消掉。
  const STOP_LABEL_RE = /(stop|停止|cancel|abort)/i;
  // 回退点击只用于等宿主完成一次同步 flush，不再信任派发瞬间抓到的静态节点快照
  const FALLBACK_CLICK_DELAY_MS = 35;
  // 窗口级句柄命名槽：与 OBSERVER_KEY / TIMER_KEY 同一惯例，保证重挂载幂等（不再线性泄漏）
  const HOOKS_KEY = '__2ag_hub_hooks';

  // 2.6 窗口级句柄：跨重挂载只装一次，回调通过下面两个"指向最新一次挂载"的闭包转发
  let hubLayoutRefresher = null;   // → 最新一次挂载的 updateGHubLayout(false)
  let hubQuotaRefresher = null;    // → 最新一次挂载的 refreshQuotas
  // → 最新一次挂载的「面板关合裁决器」：窗口级 pointerdown(capture) 与 keydown(capture)
  //   共用同一条转发通道。面板与浮标节点属于具体某一次挂载，窗口监听却只能装一次，
  //   因此监听器本体留在窗口级、裁决逻辑由这个指针转发到当前挂载的闭包。
  let hubPanelDismisser = null;
  // → 最新一次挂载的 syncThemeChips：主窗口改了主题时，舱内主题卡的高亮要跟着走
  let hubThemeSyncer = null;
  let hubDisposed = false;         // dispose() 之后彻底停摆，不再自愈/响应快捷键

  function clearHubRuntimeHandles() {
    const installed = window[HOOKS_KEY];
    if (!installed) return;
    try { window.removeEventListener('resize', installed.resizeHandler); } catch (_) {}
    try { window.clearInterval(installed.quotaTimer); } catch (_) {}
    if (installed.dismissPointer) {
      try { window.removeEventListener('pointerdown', installed.dismissPointer, true); } catch (_) {}
    }
    if (installed.dismissKey) {
      try { window.removeEventListener('keydown', installed.dismissKey, true); } catch (_) {}
    }
    window[HOOKS_KEY] = null;
  }

  // 幂等安装：重挂载时若句柄已存在则直接复用，绝不叠加第二份
  function ensureHubRuntimeHooks() {
    const installed = window[HOOKS_KEY];
    if (installed && installed.resizeHandler && installed.quotaTimer && installed.dismissPointer && installed.dismissKey) return;
    clearHubRuntimeHandles();
    const resizeHandler = () => {
      if (hubDisposed) return;
      if (typeof hubLayoutRefresher === 'function') hubLayoutRefresher();
    };
    window.addEventListener('resize', resizeHandler);
    const quotaTimer = window.setInterval(() => {
      if (hubDisposed) return;
      if (typeof hubQuotaRefresher === 'function') hubQuotaRefresher();
    }, 4000);
    // capture:true —— 宿主自己的 pointerdown/keydown 处理器可能 stopPropagation，
    // 冒泡阶段注册会在那些页面上失效；捕获阶段先过我们这道闸。
    const dismissPointer = (e) => {
      if (hubDisposed) return;
      if (typeof hubPanelDismisser === 'function') hubPanelDismisser(e);
    };
    window.addEventListener('pointerdown', dismissPointer, true);
    const dismissKey = (e) => {
      if (hubDisposed) return;
      if (typeof hubPanelDismisser === 'function') hubPanelDismisser(e);
    };
    window.addEventListener('keydown', dismissKey, true);
    window[HOOKS_KEY] = { resizeHandler, quotaTimer, dismissPointer, dismissKey };
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

  // 生成象限圆环 SVG (外径 17px, 线宽 2.5px) —— 2Ag 的签名状态控件。
  //
  // 三态，与 Manager 侧 .google-ring-toggle 的 [data-state] 是同一套语义：
  //   on（默认）  : 四段弧线全亮（右上绿 #34A853、右下蓝 #4285F4、
  //                 左下红 #EA4335、左上黄 #FBBC05）—— 这个功能真的在跑
  //   experimental: 一整圈半透明黄 —— 界面在这里，但宿主侧没有消费方，
  //                 切换不会改变任何行为。黄不是「警告」而是「未完成」，
  //                 与 Manager 侧 #ring-preserve_scroll 用的是同一个信号
  //   off         : 纯暗灰空心细环 #43474e —— 已关闭
  //
  // 三态必须是三种不同的图形而不是同一种图形换颜色：色盲用户、低亮度屏幕、
  // 以及「四色全亮」这种本身就是多色的状态，都让「颜色」不足以独立承担区分。
  function renderQuadrantRing(active, state) {
    if (state === 'experimental') {
      return `
        <svg width="17" height="17" viewBox="0 0 17 17" fill="none" class="quadrant-svg experimental">
          <circle cx="8.5" cy="8.5" r="7.25" stroke="#FBBC05" stroke-width="2.5" fill="none" opacity="0.42"/>
        </svg>
      `;
    }
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
    /* === 2Ag Design Token（与 2Ag Manager 的 <style id="2ag-design-system"> 同源） ===

       注入面板与 Manager 是同一个产品的两个面：一个是宿主里常驻的浮层，一个是窗口
       里的控制台。此前两边各写各的颜色，面板看起来像「黑色外挂工具」，控制台看起来
       像「深色后台」，同屏并排就能看出是两套东西。这里把 Manager 那套 token 原样
       搬进来（数值逐项对齐，不重新调色），从此改一处就是改两处。

       配比仍然是 80% 中性面 / 15% 语义色 / 5% 品牌光谱：四色只允许出现在光谱轨道、
       状态环、以及极少量辉光上，绝不平铺到卡片边框与按钮底色。 */
    :host {
      all: initial;
      font-family: 'Google Sans', 'Roboto', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
      -webkit-font-smoothing: antialiased;
      text-rendering: optimizeLegibility;
      font-size: 13px;
      color: #e8eaed;

      /* Surface 四级：面与面之间靠明度差分层，不靠描边堆叠 */
      --2ag-surface-base: #0e0f11;
      --2ag-surface-1: #16181b;
      --2ag-surface-2: #1d2024;
      --2ag-surface-3: #24282d;
      --2ag-surface-4: #2b3037;
      --2ag-outline: #2c3036;
      --2ag-outline-variant: #22262b;

      /* 语义四色（深色界面调制版，非纯品牌 RGB） */
      --2ag-blue: #8ab4f8;
      --2ag-blue-strong: #a8c7fa;
      --2ag-blue-dim: #669df6;
      --2ag-blue-container: rgba(138, 180, 248, 0.14);
      --2ag-blue-border: rgba(138, 180, 248, 0.32);
      --2ag-green: #81c995;
      --2ag-green-container: rgba(129, 201, 149, 0.14);
      --2ag-green-border: rgba(129, 201, 149, 0.30);
      --2ag-yellow: #fdd663;
      --2ag-yellow-container: rgba(253, 214, 102, 0.13);
      --2ag-yellow-border: rgba(253, 214, 102, 0.28);
      --2ag-red: #f28b82;
      --2ag-red-container: rgba(242, 139, 130, 0.13);
      --2ag-red-border: rgba(242, 139, 130, 0.28);

      --2ag-text-primary: #e8eaed;
      --2ag-text-secondary: #bdc1c6;
      --2ag-text-tertiary: #8e918f;
      --2ag-text-disabled: #5f6368;
      --2ag-text-on-accent: #062e6f;

      /* Radius：面板比卡片大一档，卡片比 chip 大一档 */
      --2ag-r-xs: 8px;
      --2ag-r-sm: 12px;
      --2ag-r-md: 16px;
      --2ag-r-lg: 20px;
      --2ag-r-pill: 100px;

      /* Motion：快、软、有物理感，不做弹跳 */
      --2ag-dur-fast: 120ms;
      --2ag-dur-base: 200ms;
      --2ag-dur-slow: 320ms;
      --2ag-ease: cubic-bezier(0.2, 0, 0, 1);
      --2ag-ease-emph: cubic-bezier(0.05, 0.7, 0.1, 1);

      --2ag-e1: 0 1px 2px rgba(0, 0, 0, 0.35);
      --2ag-e2: 0 2px 8px rgba(0, 0, 0, 0.40);
      --2ag-e3: 0 8px 28px rgba(0, 0, 0, 0.50);

      /* 品牌光谱：只在「一眼认得出是 2Ag」的地方出现。
         两端透明是为了让它像一束光，而不是一条贴上去的彩带。 */
      --2ag-spectrum: linear-gradient(90deg,
        rgba(138, 180, 248, 0) 0%,
        rgba(138, 180, 248, 0.95) 8%,
        rgba(138, 180, 248, 0.95) 24%,
        rgba(234, 67, 53, 0.90) 29%,
        rgba(234, 67, 53, 0.90) 48%,
        rgba(251, 188, 5, 0.90) 53%,
        rgba(251, 188, 5, 0.90) 72%,
        rgba(52, 168, 83, 0.95) 77%,
        rgba(52, 168, 83, 0.95) 92%,
        rgba(52, 168, 83, 0) 100%);
    }
    *, *::before, *::after {
      box-sizing: border-box;
      user-select: none;
      -webkit-user-drag: none;
    }

    /* --- 全局滚动条净化（无死角）---
       Windows/Chromium 的原生滚动条是 17px 粗灰白条。历史实现只给 .acct-list 写了
       ::-webkit-scrollbar 伪元素样式，但手风琴展开后真正撑破高度、发生 overflow 的是
       更外层的 .cockpit-body ⇒ 那一层没有伪元素声明，直接退回原生条，于是深色面板
       被一道贯穿顶底的白条切穿。修法不是再补一个选择器，而是在 Shadow Root 顶层做
       一次性无死角声明：任何一层发生 overflow 都走微光细条。

       两套写法在 Chromium 上是**互斥**的，且这里是实测数据（真机 CDP，量「元素让出了
       多少像素给滚动条」= offsetWidth - clientWidth - 左右边框）：
         · scrollbar-width: thin + scrollbar-color 非 auto  ⇒ 10px（Chromium 自绘 thin）
         · scrollbar-width: auto + scrollbar-color 非 auto  ⇒ 16px（几乎就是原生粗条）
         · scrollbar-width: thin + scrollbar-color: auto    ⇒ 10px
         · scrollbar-width: auto + scrollbar-color: auto    ⇒ 4px ← 只有这一组让伪元素生效
       即：**任何非 auto 的 scrollbar-width / scrollbar-color 都会让 ::-webkit-scrollbar
       的 width 声明作废**。历史实现同时写了 thin + 半透明 color，于是 4px 从未生效，
       实际显示的是 10px 的 Chromium thin 条 —— 不是 17px 原生白条（底线守住了），
       但也不是设计要求的 4px 微光细条。要 4px 就必须把标准属性留在 auto。
       这里仍显式写 auto !important：scrollbar-width / scrollbar-color 是可继承属性，
       宿主页面若在 html/body 上设了 thin 或配色，不显式压回 auto 就会被继承进来，
       4px 规则同样失效。 */
    :host,
    :host *,
    .cockpit-panel,
    .cockpit-body,
    .acct-list,
    * {
      scrollbar-width: auto !important;
      scrollbar-color: auto !important;
    }
    ::-webkit-scrollbar {
      width: 4px !important;
      height: 4px !important;
    }
    ::-webkit-scrollbar-track {
      background: transparent !important;
    }
    ::-webkit-scrollbar-thumb {
      background: rgba(255, 255, 255, 0.15) !important;
      border-radius: 2px !important;
    }
    ::-webkit-scrollbar-thumb:hover {
      background: rgba(255, 255, 255, 0.3) !important;
    }
    ::-webkit-scrollbar-corner {
      background: transparent !important;
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
    /* 拖拽抖动根治：浮标内部所有图形一律退出命中测试，
       指针事件永远落在 .ghub-beacon 本体上，
       杜绝命中目标在 svg / path / .ghub-inner 之间切换产生的抓取偏移跳变。 */
    .ghub-beacon svg,
    .ghub-beacon svg *,
    .ghub-beacon path,
    .ghub-beacon circle,
    .ghub-beacon img,
    .ghub-beacon span,
    .ghub-beacon .ghub-inner {
      pointer-events: none !important;
    }
    .ghub-inner {
      width: 41px;
      height: 41px;
      border-radius: 50%;
      background: var(--2ag-surface-1);
      display: flex;
      align-items: center;
      justify-content: center;
      transition: background var(--2ag-dur-base) var(--2ag-ease);
    }
    .ghub-beacon:hover .ghub-inner {
      background: var(--2ag-surface-2);
    }

    /* --- G-Cockpit 战术控制面板 --- */
    .cockpit-panel {
      position: fixed;
      width: 330px;
      background: var(--2ag-surface-1);
      border: 1px solid var(--2ag-outline-variant);
      border-radius: var(--2ag-r-lg);
      box-shadow: 0 20px 50px rgba(0, 0, 0, 0.75), 0 6px 16px rgba(0, 0, 0, 0.5);
      z-index: 2147483646;
      overflow: hidden;
      /* 高度硬上限：面板内容（主题库 + 账号列表 + 额度条 + 子系统 + 应急按钮）
         实测约 668px，在矮视口（如 441px 高）里若不设上限必然纵向溢出到屏外，
         连关闭按钮都点不到。上限 = 视口高 - 上下各 12px 边距。 */
      max-height: calc(100vh - 24px);
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

    /* 顶部光谱束（soft spectrum beam）。
       这里是全产品唯一允许「一眼看到四色」的地方，所以它必须是这一处 ——
       浮层的第一眼身份标签。旧版是一条 2.5px 的纯色段带，四段硬切、无渐隐，
       看起来像贴上去的彩虹条；现在改为：两端渐隐（让它像一束光而不是一条边），
       高度降到 2px，下面垫一层同色极低不透明度的 6px 辉光带，
       于是它是「发光」而不是「涂色」。 */
    .cockpit-light-bar {
      position: relative;
      width: 100%;
      height: 2px;
      background: var(--2ag-spectrum);
      flex-shrink: 0;
      opacity: 0.9;
    }
    .cockpit-light-bar::after {
      content: '';
      position: absolute;
      left: 0;
      right: 0;
      top: 0;
      height: 6px;
      background: var(--2ag-spectrum);
      opacity: 0.16;
      filter: blur(4px);
      pointer-events: none;
    }

    .cockpit-body {
      padding: 14px 16px 16px;
      display: flex;
      flex-direction: column;
      /* 面板被 max-height 压住后，滚动必须发生在正文区（顶部导光条固定不动），
         否则内容会顶破圆角边框。min-height:0 是 flex 子项允许收缩的必要条件。 */
      overflow-y: auto;
      min-height: 0;
    }

    /* 头部：品牌 Hero。
       注入面板的第一屏只有两行字（名字 + 状态），但它决定了用户对「这是什么东西」
       的判断，所以给它真正的层级：品牌名最大，状态行读起来像一行遥测。 */
    .cockpit-header {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      margin-bottom: 14px;
    }
    .brand-wrap {
      display: flex;
      align-items: center;
      gap: 9px;
      min-width: 0;
    }
    .brand-title {
      font-size: 16px;
      font-weight: 700;
      color: var(--2ag-text-primary);
      letter-spacing: -0.2px;
    }
    /* 品牌名下方那行「宿主注入就绪」：等宽小字 + 状态点，
       和 Manager 侧的 .live-indicator 说的是同一件事（同一个真实探针驱动的活体状态）。 */
    .status-line {
      display: flex;
      align-items: center;
      gap: 6px;
      margin-top: 3px;
      font-size: 10.5px;
      font-family: 'Cascadia Mono', Consolas, monospace;
      color: var(--2ag-text-tertiary);
    }
    .pulse-dot {
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: var(--2ag-green);
      box-shadow: 0 0 6px rgba(129, 201, 149, 0.85);
      animation: pulseAnim 2s infinite ease-in-out;
    }
    @keyframes pulseAnim {
      0%, 100% { transform: scale(1); opacity: 0.8; }
      50% { transform: scale(1.3); opacity: 1; }
    }
    .close-btn {
      background: transparent;
      border: none;
      color: var(--2ag-text-tertiary);
      cursor: pointer;
      padding: 5px;
      border-radius: var(--2ag-r-xs);
      display: flex;
      align-items: center;
      justify-content: center;
      transition: color var(--2ag-dur-fast) var(--2ag-ease), background var(--2ag-dur-fast) var(--2ag-ease);
    }
    .close-btn:hover {
      color: var(--2ag-text-primary);
      background: rgba(255, 255, 255, 0.07);
    }

    /* 配额池：按「池 × 窗口」展开成 2×2 四个桶 —— Gemini 5h / Gemini 周 /
       Claude 5h / Claude 周，标签一律带池名前缀，不存在任何需要猜测归属的数字。
       历史实现把池与窗口交叉配对了（Gemini 只显示 5h、Claude 只显示周限额），
       于是「剩余最少」的那两个桶在面板上根本不可见：用户拿 Claude 的周百分比
       去比 Manager 里 Gemini 的周百分比，必然对不上，而真正告急的桶一次都没露面。
       本块只做展示，任何百分比都来自后端授权缓存；available=false 时整池置灰并
       如实标注，绝不回填默认值或历史值。 */
    .quota-caps {
      display: flex;
      flex-direction: column;
      gap: 6px;
      margin-bottom: 6px;
    }
    /* 单池 = 标题行 + 两个窗口桶。圆环取代了旧的水平进度条：剩余额度直接映射为
       可见弧长，额度减少时环上的弧随之缩短，比一根横条更容易一眼判断「还剩多少」。

       这里刻意不描边：面板整体是 surface-1，池块是 surface-2，两档明度差已经足够
       把「这是两个并列的池」说清楚。此前每层都加 1.2px 边框，加上面板边框与账号行
       边框，一个 330px 宽的浮层里能数出四层矩形框 —— 这是 Manager 侧同一个毛病，
       两边现在一起改成「靠明度分层，不靠边框套娃」。 */
    .quota-pool {
      display: flex;
      flex-direction: column;
      gap: 7px;
      padding: 9px 11px;
      background: var(--2ag-surface-2);
      border-radius: var(--2ag-r-sm);
      transition: opacity var(--2ag-dur-base) var(--2ag-ease);
    }
    /* 未载入：整池降透明度，传达「这不是一个可用读数」，而不是拿空环充数 */
    .quota-pool[data-available="false"] {
      opacity: 0.5;
    }
    .quota-pool-head {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 8px;
      min-width: 0;
    }
    .quota-pool-name {
      font-size: 10.5px;
      font-weight: 600;
      letter-spacing: 0.2px;
      color: var(--2ag-text-secondary);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    /* 「剩余额度」写在池标题右侧而不是每个环里：两个环并排时环心只放得下一个数字，
       语义靠这里一次性说清，避免把「剩余」缩成看不懂的单字。 */
    .quota-pool-hint {
      flex-shrink: 0;
      font-size: 9px;
      color: var(--2ag-text-disabled);
    }
    .quota-buckets {
      display: flex;
      gap: 10px;
    }
    .quota-bucket {
      display: flex;
      align-items: center;
      gap: 7px;
      flex: 1;
      min-width: 0;
    }
    .quota-ring-wrap {
      position: relative;
      flex-shrink: 0;
      width: 38px;
      height: 38px;
    }
    .quota-ring {
      display: block;
      width: 100%;
      height: 100%;
    }
    .quota-ring-track {
      fill: none;
      stroke: rgba(255, 255, 255, 0.10);
      stroke-width: 4;
    }
    /* 进度弧：stroke-dasharray 固定为 viewBox 里的圆周长，stroke-dashoffset 由 JS
       按剩余比例设置。dashoffset = 0 ⇒ 整圈可见（满额）；= 周长 ⇒ 一圈不可见（耗尽）。
       rotate(-90deg) 把起点移到 12 点方向，于是弧「从上往下」流失，与水位直觉一致。
       注意：.quota-ring-wrap 缩到 38px 只是等比缩放整个 viewBox，r 与周长都不变 ——
       dasharray 与 JS 里的 QUOTA_RING_C 仍按 46 单位的 r=18 计算。 */
    .quota-ring-arc {
      fill: none;
      stroke: var(--2ag-green);
      stroke-width: 4;
      stroke-linecap: round;
      stroke-dasharray: 113.097;
      stroke-dashoffset: 113.097;
      transform: rotate(-90deg);
      transform-origin: 50% 50%;
      transition: stroke-dashoffset var(--2ag-dur-slow) var(--2ag-ease-emph), stroke var(--2ag-dur-slow) var(--2ag-ease);
    }
    .quota-ring-center {
      position: absolute;
      inset: 0;
      display: flex;
      align-items: center;
      justify-content: center;
      pointer-events: none;
    }
    .quota-bucket-value {
      font-size: 10.5px;
      font-weight: 700;
      letter-spacing: 0.1px;
      line-height: 1;
      font-variant-numeric: tabular-nums;
      color: var(--2ag-text-tertiary);
      transition: color var(--2ag-dur-base) var(--2ag-ease);
    }
    .quota-bucket-text {
      flex: 1;
      min-width: 0;
      display: flex;
      flex-direction: column;
      gap: 3px;
    }
    .quota-bucket-label {
      font-size: 9.5px;
      font-weight: 600;
      color: var(--2ag-text-secondary);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .quota-bucket-reset {
      font-size: 8.5px;
      color: var(--2ag-text-tertiary);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    /* 配额出处行：把「这份数字是什么时候采的、从哪来」写在面板上。
       没有它，一个 9 小时前的旧快照与一个刚刚刷新的读数长得一模一样。
       与 Manager 侧 #dash-quota-source 是同一套语义、同一套配色。 */
    .quota-source {
      font-size: 9px;
      line-height: 1.45;
      color: var(--2ag-text-tertiary);
      margin-bottom: 14px;
      word-break: break-word;
    }
    /* 采样时间超过 5h 滑窗自身长度 ⇒ 连「5h 还剩多少」这个问题的答案都已经过期。
       这不是「可能不准」的暗示，而是一条确定的失效判据，所以用告警色标明。 */
    .quota-source[data-stale="true"] {
      color: var(--2ag-yellow);
    }

    /* --- 舱内主题库（THEME MATRIX · Material 3 胶囊芯片）--- */
    /* 历史实现是 5 个带 26px 渐变缩略图的方块卡片，在 330px 面板里既占高又不像
       控制台。改为 M3 风格的药丸芯片：左侧 10px 双色渐变圆点承担色板语义，
       右侧直接写主题名，整行可换行排布。 */
    .theme-row {
      display: flex;
      gap: 6px;
      margin-bottom: 16px;
      flex-wrap: wrap;
    }
    .theme-chip {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      height: 32px;
      padding: 0 12px 0 8px;
      background: var(--2ag-surface-2);
      border: 1px solid transparent;
      border-radius: var(--2ag-r-pill);
      cursor: pointer;
      transition: background var(--2ag-dur-base) var(--2ag-ease), border-color var(--2ag-dur-base) var(--2ag-ease), transform var(--2ag-dur-fast) var(--2ag-ease);
      user-select: none;
      white-space: nowrap;
    }
    .theme-chip:hover {
      background: var(--2ag-surface-3);
    }
    .theme-chip:active {
      transform: scale(0.97);
    }
    /* 选中态：与 Manager 侧 .filter-pill.active 同一套语言 ——
       蓝色容器底 + 蓝色描边 + 提亮文字，而不是「细白描边」。
       白色描边在深色面上会被读成「聚焦」，蓝色才是「已选中」。 */
    .theme-chip[data-active="true"] {
      border-color: var(--2ag-blue-border);
      background: var(--2ag-blue-container);
    }
    .theme-dot {
      flex-shrink: 0;
      width: 10px;
      height: 10px;
      border-radius: 50%;
      border: 1px solid rgba(255, 255, 255, 0.14);
    }
    .theme-name {
      font-size: 11px;
      font-weight: 600;
      color: var(--2ag-text-secondary);
      line-height: 1;
    }
    .theme-chip[data-active="true"] .theme-name {
      color: var(--2ag-blue-strong);
    }
    /* 第 6 个芯片「调参」：齿轮用 Google 四色渐变 fill（渐变定义在 shadow root
       顶部的 <defs> 里，见 ag-g4-gear），文字与其它芯片一致。
       它不参与预设高亮裁决（data-preset 缺席 ⇒ syncThemeChipsFromDom 不会碰它），
       只在自己展开抽屉时点亮描边。

       尺寸 14px 而不是 12px：齿轮是带中心孔的实心填充图形，比图标字体里的细线
       图形更吃像素；12px 下四条色带各占约 3px，加上抗锯齿的相邻混色，四种品牌色
       会互相糊成一团。14px 时每条色带约 3.6px 且中心孔留出呼吸空间，
       四色可辨。 */
    .theme-chip-tune .theme-gear {
      flex-shrink: 0;
      width: 14px;
      height: 14px;
      display: block;
    }
    .theme-chip-tune[data-active="true"] {
      border-color: var(--2ag-blue-border);
      background: var(--2ag-blue-container);
    }

    /* --- 端内微调抽屉（Live Fine-Tuning Drawer · P2 单元三）---
       紧贴 THEME MATRIX 下方滑出。折叠用 max-height 过渡（抽屉内容高度固定，
       不需要测量），展开时把上方账号手风琴收起（见 setTuneDrawer 的互斥逻辑），
       保证 441px 窄视口下控制舱不被内容撑破。 */
    .live-tune-drawer {
      max-height: 0;
      opacity: 0;
      overflow: hidden;
      margin-bottom: 0;
      /* flex-shrink: 0 是承重件，不是装饰。
         .cockpit-body 是 height 受限（面板 max-height: calc(100vh - 24px)）的
         column flex 容器，而本抽屉带 overflow: hidden ⇒ 它的自动最小尺寸被算作 0
         ⇒ 其余子项都有 min-height: auto 兜底、唯独它可被无限压缩，于是正文区的
         全部溢出量都被它一个人吸收，展开时高度被压成 0。表现为：data-open="true"、
         max-height 计算值确为 240px、getBoundingClientRect 照样报出内容坐标，
         但 offsetHeight === 0 —— 元素仍占着 layout rect 却不可命中，
         鼠标事件穿透到下方元素（实测命中的是 THEME MATRIX 的 section-tag），
         按下滑块会被外部点击监听当成「点了抽屉外面」而立即关闭抽屉。 */
      flex-shrink: 0;
      transition: max-height 0.28s cubic-bezier(0.2, 0.8, 0.2, 1), opacity 0.2s ease, margin-bottom 0.28s ease;
    }
    .live-tune-drawer[data-open="true"] {
      max-height: 240px;
      opacity: 1;
      margin-bottom: 16px;
    }
    .tune-row + .tune-row {
      margin-top: 12px;
    }
    .tune-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 6px;
    }
    .tune-label {
      font-size: 11px;
      font-weight: 600;
      color: var(--2ag-text-secondary);
    }
    .tune-val {
      font-size: 10.5px;
      font-weight: 600;
      font-family: 'Cascadia Mono', Consolas, monospace;
      color: var(--2ag-text-primary);
      background: var(--2ag-surface-3);
      padding: 2px 8px;
      border-radius: var(--2ag-r-pill);
      min-width: 46px;
      text-align: center;
      font-variant-numeric: tabular-nums;
    }
    /* 滑轨 4px 深色，滑块 14px 浅蓝圆点（--2ag-blue）。
       与 Manager 侧 input[type=range] 的滑轨/滑块规格对齐。 */
    .tune-range {
      -webkit-appearance: none;
      appearance: none;
      display: block;
      width: 100%;
      height: 4px;
      border-radius: 2px;
      background: rgba(255, 255, 255, 0.07);
      outline: none;
      cursor: pointer;
    }
    .tune-range::-webkit-slider-thumb {
      -webkit-appearance: none;
      appearance: none;
      width: 14px;
      height: 14px;
      border-radius: 50%;
      background: var(--2ag-blue);
      border: none;
      cursor: pointer;
      box-shadow: 0 0 0 3px var(--2ag-blue-container);
      transition: transform var(--2ag-dur-fast) var(--2ag-ease);
    }
    .tune-range::-webkit-slider-thumb:hover {
      transform: scale(1.12);
    }
    .tune-range::-moz-range-thumb {
      width: 14px;
      height: 14px;
      border-radius: 50%;
      background: var(--2ag-blue);
      border: none;
      cursor: pointer;
    }
    .tune-range::-moz-range-track {
      height: 4px;
      border-radius: 2px;
      background: rgba(255, 255, 255, 0.07);
    }

    /* --- 舱内账号轮转（ACCOUNT ROTATION · 单行手风琴）--- */
    /* 折叠态只留一行「当前活跃主账号」，展开态才是可滚动的备选列表 —— 面板纵向
       空间有限，把 6 个账号常驻铺开会把主题库与应急按钮全挤出视口。 */
    .acct-acc {
      margin-bottom: 16px;
    }
    .acct-head {
      position: relative;
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 9px 10px;
      background: var(--2ag-surface-2);
      border: 1px solid transparent;
      border-radius: var(--2ag-r-sm);
      cursor: pointer;
      user-select: none;
      overflow: hidden;
      transition: border-color var(--2ag-dur-base) var(--2ag-ease), background var(--2ag-dur-base) var(--2ag-ease);
    }
    .acct-head:hover {
      background: var(--2ag-surface-3);
    }
    /* 展开态：底部一条光谱细线。这是「当前账号」在面板里的品牌标记，
       与 Manager 侧 .acct-surface.is-primary::before 是同一条线（1px，两端渐隐）。 */
    .acct-head::after {
      content: '';
      position: absolute;
      left: 0;
      right: 0;
      bottom: 0;
      height: 1px;
      background: var(--2ag-spectrum);
      opacity: 0;
      transition: opacity var(--2ag-dur-base) var(--2ag-ease);
      pointer-events: none;
    }
    .acct-acc[data-open="true"] .acct-head {
      background: var(--2ag-surface-3);
    }
    .acct-acc[data-open="true"] .acct-head::after {
      opacity: 0.75;
    }
    /* 微光绿点：当前有活跃主控账号的信号灯 */
    .acct-dot {
      flex-shrink: 0;
      width: 7px;
      height: 7px;
      border-radius: 50%;
      background: var(--2ag-green);
      box-shadow: 0 0 6px rgba(129, 201, 149, 0.85);
    }
    .acct-dot[data-off="true"] {
      background: var(--2ag-text-disabled);
      box-shadow: none;
    }
    .acct-head-mail {
      flex: 1;
      min-width: 0;
      font-size: 11.5px;
      font-weight: 600;
      color: var(--2ag-text-primary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    /* 「主账号」胶囊：与 Manager 侧 .acct-badge-primary 同一套绿容器配色 */
    .acct-pill {
      flex-shrink: 0;
      font-size: 9px;
      font-weight: 700;
      letter-spacing: 0.3px;
      padding: 2px 7px;
      border-radius: var(--2ag-r-pill);
      background: var(--2ag-green-container);
      color: var(--2ag-green);
    }
    .acct-caret {
      flex-shrink: 0;
      font-size: 10px;
      line-height: 1;
      color: var(--2ag-text-tertiary);
      transition: transform var(--2ag-dur-base) var(--2ag-ease);
    }
    .acct-acc[data-open="true"] .acct-caret {
      transform: rotate(180deg);
    }
    /* 展开区：默认折叠（display:none），避免 6 行账号常驻占用面板高度 */
    .acct-body {
      display: none;
      margin-top: 6px;
    }
    .acct-acc[data-open="true"] .acct-body {
      display: block;
    }
    .acct-list {
      display: flex;
      flex-direction: column;
      gap: 6px;
      max-height: 168px;
      overflow-y: auto;
      padding-right: 2px;
      /* 滚动条外观统一由文件顶部的「全局滚动条净化」块声明（:host, * 全穿透），
         此处不再单独写 ::-webkit-scrollbar —— 单点声明覆盖不到外层容器，
         正是上一版白条失守的成因。 */
    }
    .acct-empty {
      font-size: 11px;
      color: var(--2ag-text-tertiary);
      padding: 8px 2px;
      line-height: 1.5;
    }
    /* 一行账号 = 两段：第一段是「邮箱 + 标签 + 切换按钮」（横排，与旧版一致），
       第二段是该账号自己的双池配额微读（见 .acct-quota）。
       改成竖排容器而不是把配额塞进第一段：面板宽度只有 330px，邮箱本身就常被
       省略号截断，再往同一行里挤两个配额读数会把邮箱压到只剩几个字符。

       无边框：列表本身已经在 surface 上，行再描边就是第三层矩形。悬停给面，
       当前账号给绿容器 —— 用色与明度表达状态，不用线框。 */
    .acct-row {
      display: flex;
      flex-direction: column;
      align-items: stretch;
      gap: 5px;
      padding: 8px 10px;
      background: rgba(255, 255, 255, 0.025);
      border-radius: var(--2ag-r-sm);
      transition: background var(--2ag-dur-base) var(--2ag-ease);
    }
    .acct-row:hover {
      background: var(--2ag-surface-3);
    }
    .acct-row[data-active="true"] {
      background: var(--2ag-green-container);
    }
    .acct-line {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      min-width: 0;
    }
    .acct-meta {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-width: 0;
      flex: 1;
    }
    /* --- 每账号配额微读（展开列表逐行）---
       为什么每行都要显示：面板顶部的两条大圆环只反映「当前活跃账号」的额度，
       而「活跃账号是谁」由系统凭据决定（机器级，所有沙箱共享），用户看到的邮箱
       可能和他以为在用的那个不是同一个。于是展开列表里每个账号都必须带上它
       自己的真实读数，用户不必去猜测某条额度属于谁。
       数值全部来自 /api/v1/accounts 里该账号自己的 gemini_pool / claude_pool，
       不存在任何跨账号取数；读不到就照实显示「--」灰环，绝不拿活跃账号的值顶替。 */
    .acct-quota {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 4px 10px;
    }
    /* 每个池一组：池名标签 + 该池的两个窗口。分组不是为了好看 —— 四个读数平铺
       成一串时，「92%」这类数字没有任何途径说明自己属于哪个池，用户只能靠位置
       去猜；而位置恰恰是历史实现里出错的地方（Gemini 只显 5h、Claude 只显周，
       另外两个桶干脆不出现）。分组后每个数字都带着自己的池名出场。 */
    .acct-quota-pool {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 0;
    }
    .acct-quota-pool-tag {
      font-size: 8.5px;
      font-weight: 600;
      letter-spacing: 0.2px;
      color: var(--2ag-text-tertiary);
      white-space: nowrap;
    }
    .acct-quota-item {
      display: flex;
      align-items: center;
      gap: 4px;
      min-width: 0;
    }
    .acct-mini-ring {
      flex-shrink: 0;
      width: 13px;
      height: 13px;
      display: block;
    }
    .acct-mini-track {
      fill: none;
      stroke: rgba(255, 255, 255, 0.12);
      stroke-width: 2.2;
    }
    .acct-mini-arc {
      fill: none;
      stroke: var(--2ag-green);
      stroke-width: 2.2;
      stroke-linecap: round;
      /* r = 5.5 ⇒ 周长 34.5575，与 JS 里的 ACCT_RING_C 必须一致 */
      stroke-dasharray: 34.5575;
      stroke-dashoffset: 34.5575;
      transform: rotate(-90deg);
      transform-origin: 50% 50%;
      transition: stroke-dashoffset 0.5s cubic-bezier(0.2, 0.8, 0.2, 1), stroke 0.4s ease;
    }
    .acct-quota-label {
      font-size: 9.5px;
      color: var(--2ag-text-tertiary);
      white-space: nowrap;
    }
    .acct-quota-val {
      font-size: 9.5px;
      font-weight: 700;
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
    }
    .acct-mail {
      font-size: 11.5px;
      font-weight: 600;
      color: var(--2ag-text-primary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    /* 眼睛按钮：与邮箱同一行，尺寸对齐 11.5px 文本的行高。
       默认低对比，悬停与「已展开」时才提亮 —— 展开态必须一眼可辨。 */
    .acct-eye {
      flex: 0 0 auto;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 18px;
      height: 18px;
      padding: 0;
      margin-left: 2px;
      border: none;
      background: transparent;
      border-radius: 4px;
      cursor: pointer;
      color: var(--2ag-text-tertiary);
      vertical-align: middle;
      transition: color .15s ease, background-color .15s ease;
    }
    .acct-eye:hover {
      color: var(--2ag-text-primary);
      background: var(--2ag-surface-2);
    }
    .acct-eye:focus-visible {
      outline: 2px solid var(--2ag-blue);
      outline-offset: 1px;
    }
    .acct-eye svg {
      width: 13px;
      height: 13px;
      display: block;
    }
    .acct-eye svg path {
      fill: currentColor;
      stroke: currentColor;
      stroke-width: 1.6;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    /* 睁眼 = 已展开（显示完整邮箱）；闭眼 = 已隐藏。两者互斥。 */
    .acct-eye .eye-off {
      display: none;
    }
    .acct-eye[data-revealed="false"] .eye-open {
      display: none;
    }
    .acct-eye[data-revealed="false"] .eye-off {
      display: block;
    }
    .acct-eye[data-revealed="true"] {
      color: var(--2ag-text-primary);
    }
    /* 邮箱文本与眼睛必须成组，否则 flex 布局会把它们拆到两端 */
    .acct-mail-row2 {
      display: flex;
      align-items: center;
      min-width: 0;
    }
    .acct-sub {
      font-size: 10px;
      color: var(--2ag-text-tertiary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .acct-tag {
      font-size: 9.5px;
      font-weight: 700;
      letter-spacing: 0.3px;
      color: var(--2ag-green);
      flex-shrink: 0;
    }
    .acct-switch {
      flex-shrink: 0;
      font-size: 10.5px;
      font-weight: 600;
      padding: 4px 9px;
      border-radius: 5px;
      background: var(--2ag-blue-container);
      border: 1px solid var(--2ag-blue-border);
      color: var(--2ag-blue-strong);
      cursor: pointer;
      transition: background 0.18s ease, color 0.18s ease;
    }
    .acct-switch:hover {
      background: rgba(138, 180, 248, 0.24);
      color: var(--2ag-text-primary);
    }
    .acct-switch:disabled {
      opacity: 0.45;
      cursor: not-allowed;
    }
    /* --- 凭据快照入口（CREDENTIAL VAULT · P2 单元四）---
       样式克制：比备选账号行更轻，不与「切换」按钮争视觉权重 —— 它是一条
       低频的存档操作，不是主流程。 */
    .acct-vault {
      display: flex;
      align-items: center;
      gap: 6px;
      margin-top: 2px;
      padding: 7px 9px;
      background: rgba(255, 255, 255, 0.02);
      border: 1px dashed var(--2ag-blue-border);
      border-radius: 7px;
      font-size: 11px;
      font-weight: 600;
      color: var(--2ag-blue);
      cursor: pointer;
      transition: background 0.18s ease, border-color 0.18s ease;
    }
    .acct-vault:hover {
      background: rgba(138, 180, 248, 0.10);
      border-color: rgba(138, 180, 248, 0.5);
    }
    .acct-vault[data-busy="true"] {
      opacity: 0.55;
      cursor: progress;
    }

    /* --- 平滑重启遮罩 ---
       切换账号时盖住整个页面。文字用 --2ag-text-*，圆环保留四色 —— 这是一次
       明确的品牌时刻（产品正在做一件需要用户等待的事），四色在这里出现是「这是
       2Ag 在换号」，不是装饰。 */
    .ghub-loading {
      position: fixed;
      inset: 0;
      z-index: 2147483647;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 14px;
      background: rgba(9, 10, 12, 0.82);
      backdrop-filter: blur(6px);
      -webkit-backdrop-filter: blur(6px);
      opacity: 0;
      pointer-events: none;
      transition: opacity var(--2ag-dur-base) var(--2ag-ease);
      font-family: 'Google Sans', 'Roboto', -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
    }
    .ghub-loading[data-show="true"] {
      opacity: 1;
      pointer-events: auto;
    }
    .ghub-spinner {
      width: 34px;
      height: 34px;
      border-radius: 50%;
      border: 2.5px solid rgba(255, 255, 255, 0.14);
      border-top-color: #4285F4;
      border-right-color: #34A853;
      border-bottom-color: #FBBC05;
      border-left-color: #EA4335;
      animation: spinAnim 0.9s linear infinite;
    }
    @keyframes spinAnim {
      to { transform: rotate(360deg); }
    }
    .ghub-loading-text {
      font-size: 13px;
      font-weight: 600;
      color: var(--2ag-text-primary);
      letter-spacing: 0.2px;
    }
    .ghub-loading-sub {
      font-size: 11px;
      color: var(--2ag-text-tertiary);
      margin-top: -6px;
    }

    /* 分组标题：与 Manager 侧的 .title-latin / .summary-key-latin 同一套规格
       （10px、700、宽字距、全大写、disabled 色）—— 它是分隔符，不是内容。 */
    .section-tag {
      font-size: 10px;
      font-weight: 700;
      color: var(--2ag-text-disabled);
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
      gap: 12px;
      cursor: pointer;
      padding: 2px 0;
    }
    .item-text {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-width: 0;
    }
    .item-name {
      font-size: 13px;
      font-weight: 600;
      color: var(--2ag-text-primary);
    }
    .item-desc {
      font-size: 11px;
      line-height: 1.45;
      color: var(--2ag-text-tertiary);
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
      transition: transform var(--2ag-dur-fast) var(--2ag-ease);
    }
    .quadrant-btn:hover {
      transform: scale(1.1);
    }
    .quadrant-btn:active {
      transform: scale(0.94);
    }
    .quadrant-svg {
      display: block;
    }
    /* 实验性条目：整行降到 0.62，圆环再降一档 —— 与 Manager 侧
       .setting-row.is-disabled 的数值逐项一致（同一套「未完成」表达）。 */
    .subsystem-item.is-disabled {
      cursor: not-allowed;
    }
    .subsystem-item.is-disabled .item-name,
    .subsystem-item.is-disabled .item-desc {
      opacity: 0.62;
    }
    .subsystem-item.is-disabled .quadrant-btn {
      cursor: not-allowed;
      opacity: 0.42;
    }
    .subsystem-item.is-disabled .quadrant-btn:hover {
      transform: none;
    }
    /* 英文副名：与 Manager 侧 .setting-latin / .title-latin 同一规格。
       中文主名占主层级，英文只是技术气质，不能和中文一样重。 */
    .item-latin {
      margin-left: 7px;
      font-size: 10px;
      font-weight: 500;
      letter-spacing: 0.3px;
      color: var(--2ag-text-disabled);
    }
    /* 「实验性 · 开发中」标签。这个类在 markup 里一直存在，但此前从没写过对应的
       CSS 规则 —— 它退化成一个裸 <span>，直接贴在前一个 span 后面，
       在面板上渲染成「纯文本降级模式 Text Fallback实验性 · 开发中」这种连体字符串。
       规格与 Manager 侧 .tag-experimental 对齐，两端读起来才是同一个产品。 */
    .tag-experimental {
      display: inline-block;
      margin-left: 8px;
      padding: 1px 7px;
      border-radius: var(--2ag-r-pill);
      background: var(--2ag-yellow-container);
      border: 1px solid var(--2ag-yellow-border);
      color: var(--2ag-yellow);
      font-size: 10px;
      font-weight: 600;
      white-space: nowrap;
    }
    /* 操作按钮：与 Manager 侧 .google-btn / .google-btn-tonal 同一套分级 ——
       主操作实心蓝，次操作面底无边框。旧版两个按钮都是「描边 + 透明底」，
       主次只差一个色相，在暗面上几乎分不出来。 */
    .actions-wrap {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .action-btn {
      width: 100%;
      min-height: 36px;
      padding: 9px 12px;
      border-radius: var(--2ag-r-pill);
      font-size: 12px;
      font-weight: 600;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      cursor: pointer;
      outline: none;
      transition: background var(--2ag-dur-base) var(--2ag-ease), border-color var(--2ag-dur-base) var(--2ag-ease), color var(--2ag-dur-base) var(--2ag-ease);
    }
    .btn-force {
      background: var(--2ag-blue);
      border: 1px solid transparent;
      color: var(--2ag-text-on-accent);
    }
    .btn-force:hover {
      background: var(--2ag-blue-strong);
      color: var(--2ag-text-on-accent);
      box-shadow: var(--2ag-e1);
    }
    .btn-force:active {
      transform: scale(0.98);
    }
    .btn-reload {
      background: var(--2ag-surface-3);
      border: 1px solid transparent;
      color: var(--2ag-text-secondary);
    }
    .btn-reload:hover {
      background: var(--2ag-surface-4);
      color: var(--2ag-text-primary);
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
      background: var(--2ag-surface-2);
      border: 1px solid var(--2ag-blue-border);
      box-shadow: var(--2ag-e3);
      padding: 8px 18px;
      border-radius: var(--2ag-r-pill);
      font-size: 12px;
      font-weight: 500;
      color: var(--2ag-text-primary);
      display: flex;
      align-items: center;
      gap: 8px;
      z-index: 2147483647;
      opacity: 0;
      pointer-events: none;
      transition: opacity var(--2ag-dur-base) var(--2ag-ease), transform var(--2ag-dur-base) var(--2ag-ease);
    }
    .cockpit-toast[data-show="true"] {
      opacity: 1;
      transform: translateX(-50%) translateY(0);
    }
    .toast-dot {
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: var(--2ag-blue);
    }

    /* ---------- 键盘焦点 ---------------------------------------------------
       上面 .tune-range / .quadrant-btn / .action-btn 各写了 outline: none，
       那是为了掐掉浏览器给鼠标点击画的那圈方框，不是「不要焦点提示」。
       结果是整块面板键盘走过一遍完全看不出落点 —— 而这个面板本身是键盘
       驱动的（Ctrl + Shift + Enter、ESC 都是它的入口），矛盾尤其刺眼。
       这里按 Manager 侧的同一条规则补回来：只在 :focus-visible（键盘/辅助
       技术导航）时显示，鼠标点击不会看到它。 */
    .quadrant-btn:focus-visible,
    .action-btn:focus-visible,
    .theme-chip:focus-visible,
    .theme-chip-tune:focus-visible,
    .acct-head:focus-visible,
    .acct-switch:focus-visible,
    .acct-vault:focus-visible,
    .close-btn:focus-visible {
      outline: 2px solid var(--2ag-blue);
      outline-offset: 2px;
    }
    /* 滑块没有矩形盒可描边（轨道只有 4px 高），焦点落在 6px 的 thumb 上：
       给它一圈蓝色光环，与 hover 时那圈 container 色区分开。 */
    .tune-range:focus-visible::-webkit-slider-thumb {
      box-shadow: 0 0 0 4px var(--2ag-blue-container), 0 0 0 5.5px var(--2ag-blue);
    }
    .tune-range:focus-visible::-moz-range-thumb {
      box-shadow: 0 0 0 4px var(--2ag-blue-container), 0 0 0 5.5px var(--2ag-blue);
    }
    /* 实验性开关被 disabled 挡在 Tab 序列外，但万一被程序聚焦，也不能没有落点。 */
    .subsystem-item.is-disabled .quadrant-btn:focus-visible {
      outline-color: var(--2ag-yellow);
    }
  `;

  // 5. 宿主原生全局样式（仅限背景壁纸与 880px 排版注入，保持原生 DOM 纯净）
  const NATIVE_STYLE_ID = '2ag-native-core-style';
  // 880px 黄金视距排版块：由 GravityBoost.centered_width 真实控制。
  // 历史实现无条件注入，开关只是装饰品；现在抽成常量，开关切换时整块增删。
  const CENTERED_WIDTH_CSS = `
        /* 880px 黄金视距排版重塑 */
        div[class*="chat-scroll"], div[class*="conversation-view"], div[class*="chat-stream"], div[class*="conversation-container"], [class*="chat-scroll-container"], .conversation-container, main[role="main"] > div, [class*="chat-session"], [class*="messages-container"], [class*="chat-history"], [class*="conversation-layout"], div[class*="conversation-content"], div[class*="chat-layout"] {
          max-width: 880px !important;
          margin: 0 auto !important;
          width: 100% !important;
          box-sizing: border-box !important;
        }
  `;
  // 保持常驻的字体、背景透明化与透光覆写块
  const BASE_NATIVE_CSS = `
        pre, code, .code-block, [class*="code-block"], [class*="code-container"] {
          font-family: 'Google Sans Code', 'Consolas', 'Roboto Mono', 'Fira Code', monospace !important;
          line-height: 1.65 !important;
          font-size: 13.5px !important;
        }
        p, [class*="message-content"], [class*="markdown-body"] {
          line-height: 1.6 !important;
          font-family: 'Google Sans', 'Roboto', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif !important;
        }
        /* 模态弹窗遮蔽度 (Modal Glass) 的真实落点。
           宿主所有「整屏遮罩」—— 弹窗背景板、加载遮罩、右键菜单背景板、抽屉背板 ——
           都是全屏元素 + Tailwind 的 bg-black/NN 工具类（实测档位 /20 /25 /30 /40 /45 /50 /80）。
           这类规则的源码是 color-mix(in srgb, #000 NN%, transparent)，NN 烘死在类名里，
           逐条覆盖不现实；统一收束到一个 CSS 变量上：变量值来自用户滑块（0.7–1.0），
           拖动时只改一个属性值，宿主随即重绘 —— 这是「毫秒级随动」的实现路径。
           作用域刻意加 [class~="inset-0"] 限定为「铺满整屏」的层：
           tooltip（absolute top-full ... bg-black/80）这类小面积深色块不是遮罩，
           跟着变会污染宿主自己的对比度设计。
           用 rgb(0 0 0 / alpha) 而不是 color-mix()，是为了避开 @supports 回退分支带来的
           双写歧义 —— 单个声明、单个语义。 */
        [class~="inset-0"][class*="bg-black/"] {
          background-color: rgb(0 0 0 / var(--2ag-modal-glass, 0.85)) !important;
        }
  `;
  // Dream-Skin 透光级联（Transparency Cascade）。
  //
  // 它存在的唯一目的是「让底下的壁纸透出来」——把宿主自己的侧边栏 / 顶栏 / 输入框
  // 全部改成半透明并加 backdrop-filter。因此它是**壁纸相关**的样式，必须随壁纸一起启停。
  //
  // 历史实现把它并进 BASE_NATIVE_CSS，于是清除壁纸后宿主整个页面依然是透明的，
  // 而透明背后已经没有任何东西 —— 屏幕一片空。那比「图没清掉」更糟：用户会以为程序坏了。
  // 现在它只由 ensureDreamSkin 在有壁纸时挂上、在 Native 时摘掉。
  const DREAM_SKIN_CSS = `        /* 宿主背景透明化以显现底层高斯模糊壁纸 */
        :root, :host, html, body, .dark, [class*="dark"], .theme-dark, .theme-standalone {
          --background: transparent !important;
          --color-background: transparent !important;
        }
        html, body, #root, #app, main, body > div:not([id="${SHADOW_HOST_ID}"]):not([id="${BG_ID}"]) {
          background-color: transparent !important;
        }

        /* ------------------------------------------------------------------
           透光级联（Transparency Cascade）
           以下选择器全部来自对运行中宿主的真实 DOM 探查，不是猜的类名：
             · 左侧导航栏本体  <div class="h-full w-full flex flex-col pb-2 bg-sidebar">   256x867
             · 顶栏           <div class="flex items-center gap-1 px-2 h-full bg-sidebar w-full"> 1400x35
             · 输入框外容器    <div class="relative flex flex-col p-px rounded-2xl bg-card-border"> 768x90
             · 输入框内层      <div class="... rounded-[calc(theme(borderRadius.2xl)-1px)] bg-card"> 766x80
           配色不硬写类规则，而是改宿主自己的 Tailwind 主题变量 —— 编译产物里
             .bg-sidebar      { background-color: var(--color-sidebar); }
             .bg-sidebar-muted{ background-color: var(--color-sidebar-muted); }
             .bg-card-border  { background-color: var(--color-card-border); }
             .bg-card         { background-color: var(--color-card); }
           所以覆盖变量既精准又不会与宿主的样式升级打架。
           [class~="..."] 用「空白分隔的完整 token」匹配，因此
           [class~="bg-sidebar"] 只命中真正的侧边栏/顶栏外壳，
           不会误伤 bg-sidebar-secondary / bg-sidebar-muted 那些内嵌条目。
           ------------------------------------------------------------------ */

        /* 左侧导航栏 + 顶栏：实心 #101010 换成 65% 透明深板岩 + 16px 高斯模糊，
           让底下的壁纸透出来。顶栏与侧边栏同色同变量，一并处理才不会露出突兀的色块。 */
        :root, html, body {
          --sidebar: rgba(18, 20, 26, 0.65) !important;
          --color-sidebar: rgba(18, 20, 26, 0.65) !important;
        }
        [class~="bg-sidebar"] {
          backdrop-filter: blur(16px) !important;
          -webkit-backdrop-filter: blur(16px) !important;
        }

        /* 输入框「悬浮空气岛」：
           外层 p-px 的 1px padding 就是视觉描边 —— 给它一道细微高光；
           内层 bg-card 才是可见面板 —— 把它改成 72% 透明毛玻璃。
           --card 的覆盖写在外层元素上（而不是 :root），这样只影响这一个输入框，
           宿主的弹窗 / 下拉菜单仍然保持原有的不透明背景，不会连可读性一起牺牲。 */
        [class~="bg-card-border"] {
          --card: rgba(28, 30, 38, 0.72) !important;
          --color-card: rgba(28, 30, 38, 0.72) !important;
          background-color: rgba(255, 255, 255, 0.09) !important;
          backdrop-filter: blur(18px) !important;
          -webkit-backdrop-filter: blur(18px) !important;
        }

`;

  // native style 元素内容的**唯一派生点**。
  //
  // 三个片段按需拼装：
  //   CENTERED_WIDTH_CSS  受 GravityBoost 开关控制，与壁纸无关；
  //   BASE_NATIVE_CSS     字体 + 模态遮罩变量，与壁纸无关；
  //   DREAM_SKIN_CSS      透光级联，**只在与壁纸共存时有意义**。
  //
  // 为什么必须是函数而不是各写各的：ensureNativeStyles() 由 2s 巡检调用（不知道壁纸状态），
  // ensureDreamSkin() 由状态变更调用（知道）。两个写入者各拼各的字符串，只要一处漏掉
  // DREAM_SKIN_CSS，界面就会每 2 秒在有/无透光之间闪一次。
  function nativeStyleText() {
    const custom = effectiveWallpaper() !== '';
    return (boost.centered_width ? CENTERED_WIDTH_CSS : '')
      + BASE_NATIVE_CSS
      + (custom ? DREAM_SKIN_CSS : '');
  }
  function ensureNativeStyles() {
    // 模态遮罩变量必须每次都跟着 state 走：tick 每 2s 跑一次，主窗口改配置后
    // （__2ag_onStateUpdate 也会调本函数）宿主遮罩层的实际透明度才算真的同步了。
    // 只在 style 元素缺省时才写，否则「滑块拖了但宿主遮罩没变」又会变成假功能。
    try { applyModalGlass(state.modalOpacity); } catch (_) {}
    let el = document.getElementById(NATIVE_STYLE_ID);
    if (!el) {
      el = document.createElement('style');
      el.id = NATIVE_STYLE_ID;
      el.textContent = nativeStyleText();
      (document.head || document.documentElement).appendChild(el);
      return;
    }
    // 已存在：按当前开关/壁纸状态重算（幂等，可反复调用）。
    const want = nativeStyleText();
    if (el.textContent !== want) el.textContent = want;
  }

  // 6. Dream-Skin 原版高斯模糊背景层
  //
  // lastAppliedWallpaper 记住「上一次真正写进样式的壁纸值」。换壁纸这件事必须靠
  // 「值有没有变」来判断，不能用「当前值长什么样」来判断 —— 历史实现的判据是
  // `!bg.style.backgroundImage.includes('data:image')`，它与「值是否变化」毫无关系，
  // 于是两种模式各坏一头：
  //   · 图形模式（用户日常路径，走 supervisor.hubConfigFromConfig 的 base64 data URL）
  //     首次写入后判据恒假 ⇒ 换壁纸后宿主外观永远是旧图，配置落盘了、界面也报成功；
  //   · CLI 模式（走 loopback 的 http://127.0.0.1:18082/bg.jpg）判据恒真 ⇒ 每次
  //     tick 都重写一遍（幂等，但纯属浪费）。
  //
  // 这个值必须跨实例存活（记在 window 上，理由同 UI_OPEN_KEY）：热重载会在同一个
  // 文档里跑出新实例，模块级变量随之归零，新实例就判断不出「这张图刚才是否已经写
  // 进去了」，只能把同一个 134KB 的 data URL 再写一遍 —— 浏览器要重新解析、重新
  // 解码，于是每次改滑块（现在都会触发热重载）壁纸都闪一下。
  const WALLPAPER_SRC_KEY = '__2ag_wallpaper_src';
  let lastAppliedWallpaper = '';
  try {
    const prevWallpaper = window[WALLPAPER_SRC_KEY];
    if (typeof prevWallpaper === 'string') lastAppliedWallpaper = prevWallpaper;
  } catch (_) {}

  // 写入「已应用的壁纸值」。刻意做成单一入口：内存变量与窗口级记录必须同步更新，
  // 只改其中一个就会出现「本实例以为没写过、于是重写一遍（闪烁）」或
  // 「下一个实例以为写过了、于是该换的图不换（静默失效）」。
  function markWallpaperApplied(value) {
    lastAppliedWallpaper = value;
    try { window[WALLPAPER_SRC_KEY] = value; } catch (_) {}
  }

  // 11.3 增量推送带来的壁纸，优先级高于 INITIAL_CONFIG.wallpaper。
  //
  // 为什么需要这一层：INITIAL_CONFIG 是「上一次把整段补丁注入进来时」随源码一起送达的
  // 一份快照，它的 wallpaper 字段从此不再变化。若换壁纸只走增量推送（快路径），
  // INITIAL_CONFIG.wallpaper 依然是旧图，直接取它就会把新图盖回去 —— 用户点了换图，
  // 界面报成功，宿主外观纹丝不动。因此推送必须能顶掉这份快照。
  //
  // 落点选 window 而非模块级变量，理由同 WALLPAPER_SRC_KEY：热重载会在同一文档里
  // 跑出新实例，模块级变量归零，而「用户最近一次推过来的壁纸」必须跨实例存活，
  // 否则热重载后的新实例又去读那份过期的 INITIAL_CONFIG。
  const PUSHED_WALLPAPER_KEY = '__2ag_wallpaper_pushed';
  // 三态，缺一不可：
  //   null  「从未收到过推送」—— 此时才允许回落到注入快照
  //   ''    「明确要求回到 Native」—— 这是一个指令，绝不能被快照盖回去
  //   'data:…' / 'http…'  推送过来的图
  //
  // 这里曾经用 '' 同时表示前两者，于是 ensureDreamSkin 的 `pushedWallpaper || 快照`
  // 会把刚采纳的清空指令当成 falsy 跳过，背景层继续贴上一张图 ——
  // 实测症状就是「后端 wallpaper_path 已经为空，DOM 里旧图还在」。
  let pushedWallpaper = null;
  try {
    const prevPushed = window[PUSHED_WALLPAPER_KEY];
    if (typeof prevPushed === 'string') pushedWallpaper = prevPushed;
  } catch (_) {}

  function adoptPushedWallpaper(value) {
    pushedWallpaper = value;
    try { window[PUSHED_WALLPAPER_KEY] = value; } catch (_) {}
  }

  // 当前应当渲染的背景值：**唯一取值点**。
  //
  // 优先级：推送过来的值（含空串）> 注入快照里的值 > 补丁内部状态。
  // 判据必须是「是否收到过推送」（!== null）而不是「值是否非空」——
  // 空串是一次真实的指令。早期写成 `pushedWallpaper || …` 时，
  // 「清除壁纸」会在这一跳被快照里的旧图盖回去。
  function effectiveWallpaper() {
    if (pushedWallpaper !== null) return pushedWallpaper;
    const snap = INITIAL_CONFIG && INITIAL_CONFIG.wallpaper;
    if (typeof snap === 'string') return snap;
    if (typeof state.wallpaper === 'string') return state.wallpaper;
    return '';
  }

  // 壁纸层的唯一渲染入口。三态收敛在这里，别处不得直接操作 bg 层。
  //
  //   wallpaper 非空 → Custom：确保背景层存在、贴图、尺寸/模糊/透明度就位，
  //                     并确保透光级联（DREAM_SKIN_CSS）挂着。
  //   wallpaper 为空 → Native：**拆除**而不是「清空」。见下。
  //
  // Native 为什么必须拆除而不是留个空层：
  //   ① 空的 bg 层（100vw/100vh、z-index:0）虽然看不见，却仍然占着 body 的第一个子元素，
  //      宿主对 body > div 的任何结构性判断都会把它算进去；
  //   ② 更要命的是 DREAM_SKIN_CSS —— 它把侧边栏 / 顶栏 / 输入框全改成半透明，
  //      好让底下的壁纸透出来。壁纸没了还留着它，宿主就变成「整体透明、背后什么都没有」，
  //      整个界面一片空。那比「图没清掉」更糟：用户会以为程序坏了。
  //   ③ 两个 window 级记账键（__2ag_wallpaper_src / __2ag_wallpaper_pushed）必须归零，
  //      否则下次设壁纸会被判成「值没变」而跳过重绘。
  function ensureDreamSkin() {
    if (!document.body) return;
    let bg = document.getElementById(BG_ID);
    // 取值收敛到 effectiveWallpaper()：空串（Native）必须能穿过这条链。
    const wallpaper = effectiveWallpaper();
    // 严禁用 `state.blur || 28` 这种写法：blur = 0 是「恢复默认外观」的合法取值
    // （native 预设就要 0 模糊），而 0 是 falsy，会被静默改写成 28px ——
    // 于是用户点「恢复默认外观」后壁纸依旧糊成一片，界面却报成功。
    // 只有「未定义 / 非法值」才允许回落默认。
    const blurVal = (typeof state.blur === 'number' && isFinite(state.blur) && state.blur >= 0) ? state.blur : 28;
    const opacityVal = (typeof state.opacity === 'number' && isFinite(state.opacity) && state.opacity >= 0) ? state.opacity : 0.6;
    const styleEl = document.getElementById(NATIVE_STYLE_ID);

    if (!wallpaper) {
      if (bg && bg.parentNode) bg.parentNode.removeChild(bg);
      // 只重写内容，不删 <style> 元素：字体规则与模态遮罩变量与壁纸无关，必须留着。
      // 内容由 nativeStyleText() 派生 —— 此时 wallpaper 已为空，它自然会去掉透光级联。
      if (styleEl) {
        const baseOnly = nativeStyleText();
        if (styleEl.textContent !== baseOnly) styleEl.textContent = baseOnly;
      }
      // 记账归零必须发生在拆除之后：置空让下一次写入任何非空值都满足
      // `wallpaper !== lastAppliedWallpaper`，必定重绘。
      markWallpaperApplied('');
      adoptPushedWallpaper('');
      return;
    }

    // Custom：透光级联与背景层都在场。
    if (styleEl) {
      const want = nativeStyleText();
      if (styleEl.textContent !== want) styleEl.textContent = want;
    }

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
        background-image: url("${wallpaper}") !important;
        background-size: cover !important;
        background-position: center !important;
        filter: blur(${blurVal}px) !important;
        opacity: ${opacityVal} !important;
      `;
      document.body.prepend(bg);
      markWallpaperApplied(wallpaper);
      return;
    }

    if (bg.parentNode !== document.body || document.body.firstElementChild !== bg) {
      document.body.prepend(bg);
    }
    // 只在壁纸值「与前一次写入的不同」时重写 —— 判据是值的变化，不是值的形状。
    // 后面那半条 `!bg.style.backgroundImage` 不是历史坏判据的回归：它问的是
    // 「这个层现在到底有没有图」，而不是「这串 URL 长什么样」。层还活着却根本没图
    // （例如宿主清过内联样式）时，无论记录怎么说都必须补上。
    if (wallpaper !== lastAppliedWallpaper || !bg.style.backgroundImage) {
      bg.style.setProperty('background-image', `url("${wallpaper}")`, 'important');
      markWallpaperApplied(wallpaper);
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

  // 8.1 受控输入写值链路：绕过 React 的 _valueTracker
  // 直接 `el.value = x` 会同步更新框架的 value tracker，随后的 input 事件被
  // updateValueIfChanged 判定为"无变化"→ onChange 不触发 → 宿主 state 仍是旧值。
  // 必须走原型链上的原生 setter，让 tracker 与 DOM 产生真实差异，再用 InputEvent 通知框架。
  function writeValueIntoInput(el, text) {
    if (!el || typeof text !== 'string') return false;
    try {
      const proto = el.tagName === 'TEXTAREA' ? window.HTMLTextAreaElement.prototype
        : el.tagName === 'INPUT' ? window.HTMLInputElement.prototype : null;
      const desc = proto && Object.getOwnPropertyDescriptor(proto, 'value');
      if (desc && typeof desc.set === 'function') {
        desc.set.call(el, text);
      } else if (el.isContentEditable) {
        el.textContent = text;
      } else {
        el.value = text;
      }
      // contenteditable 走 beforeinput/input 双发，普通表单控件只需 input
      el.dispatchEvent(new InputEvent('input', {
        bubbles: true,
        cancelable: false,
        composed: true,
        inputType: 'insertText',
        data: text
      }));
      return true;
    } catch (_) {
      return false;
    }
  }

  // 8.2 发送按钮语义门禁
  // 返回 'stop' 表示命中终止语义（坚决不点）；返回 'send' 表示可安全回退点击。
  function sendButtonLabel(btn) {
    return [
      btn.getAttribute('aria-label') || '',
      btn.getAttribute('data-tooltip') || '',
      btn.getAttribute('title') || '',
      btn.getAttribute('data-testid') || '',
      btn.textContent || ''
    ].join(' ');
  }
  function classifySendButton(btn) {
    return STOP_LABEL_RE.test(sendButtonLabel(btn)) ? 'stop' : 'send';
  }
  // 正向语义分：同层候选之间用它排序。命中「发送/提交」字样的排前面，
  // 这样即便某一层同时返回多颗按钮，也不会按文档顺序随便挑一颗。
  function sendSemanticScore(btn, label) {
    let score = 0;
    if (SEND_LABEL_RE.test(label)) score += 4;
    if (btn.getAttribute('type') === 'submit') score += 2;
    if (btn.tagName === 'BUTTON') score += 1;
    return score;
  }

  // 8.3 逐层寻址：先语义层，命中即停；整层无有效候选才下沉。
  // 返回 { btn, tier, locked } —— tier 一并带出，是为了让"最终用的是哪一层"可见，
  // 官方改版导致寻址下沉时能在控制台直接看到，而不是等到点击彻底失效才发现。
  //
  // 关于 disabled：实测宿主（Antigravity 2.18.1）的发送键在输入框为空时**常态就是
  // disabled**（aria-label="Send message"、data-testid="send-button"、28x28 可见）。
  // 因此绝不能把 disabled 当成"这颗按钮不存在"——那样一来，日常状态下寻址会整体落空，
  // 兼容性自检也会把"一切正常"误报成"功能性失效"。
  // 处置与输入框一致：照常选中并标 locked，真正要点击时再解阻断（见 8.4）。
  function findSendButtonTiered() {
    for (let tier = 0; tier < SEND_SELECTORS_TIERED.length; tier++) {
      let nodes;
      try {
        nodes = document.querySelectorAll(SEND_SELECTORS_TIERED[tier]);
      } catch (_) {
        continue;
      }
      let best = null;
      // 初值必须是 -Infinity 而不是 -1：加了"锁着的扣 8 分"之后，
      // 一颗正常的 locked 发送键（4+1-8 = -3）会低于 -1，于是被静默丢弃、
      // 整层寻址落空 —— 实测踩过这个坑（detectSend 恒为 -1）。
      let bestScore = -Infinity;
      let bestLocked = false;
      for (const btn of nodes) {
        if (!btn.isConnected) continue;        // 卸载重建后留下的孤儿：事件冒泡不到宿主根
        if (classifySendButton(btn) === 'stop') continue;   // 终止语义：坚决不点
        const rect = btn.getBoundingClientRect();
        if (rect.width <= 0 || rect.height <= 0) continue;
        const locked = !!btn.disabled;
        let score = sendSemanticScore(btn, sendButtonLabel(btn));
        // 同层内未被锁的优先；全层都被锁时才退而取之（那正是本子系统要救的情形）。
        if (locked) score -= 8;
        if (score > bestScore) {
          bestScore = score;
          best = btn;
          bestLocked = locked;
        }
      }
      if (best) return { btn: best, tier: tier, locked: bestLocked };
    }
    return { btn: null, tier: -1, locked: false };
  }

  // 8.4 回退点击：必须在"点击那一刻"重新寻址，并对终止语义做最后一道否决
  //
  // 关于解阻断：宿主把发送键标 disabled 是因为它自己的状态机认为"不能发"
  // （输入为空，或仍在生成中）。摘掉 disabled 属性只解决浏览器层面的 no-op，
  // **改变不了宿主框架内部的判断**——若 React 状态说不能发，点击仍会被它自己的
  // handler 丢弃。这一点必须如实记录：本函数提高的是"点得动"的概率，
  // 不是"宿主一定接受"。真正让宿主接受，靠的是 Step 1b 先写值、再派发键盘事件
  // 把它的受控状态带过去。
  function fallbackClickVisibleSendButton() {
    const found = findSendButtonTiered();
    if (!found.btn) return false;
    const btn = found.btn;
    if (found.locked) {
      try {
        btn.removeAttribute('disabled');
        btn.setAttribute('aria-disabled', 'false');
        btn.style.setProperty('pointer-events', 'auto', 'important');
      } catch (_) {}
    }
    try {
      btn.click();
      return true;
    } catch (_) {
      return false;
    }
  }

  // 8.5 输入容器寻址（同一套分层思想：语义 → 结构 → 历史类名 → 几何兜底）
  //
  // 旧实现把「textareta:not([disabled])」放在选择器串最前面，于是它按文档顺序
  // 命中的是第一颗可编辑元素 —— 在宿主里那可能是侧栏的搜索框、重命名输入框，
  // 甚至上一轮对话里残留的某个编辑域。类名那一档（chat-input / input-container）
  // 纯属猜测，官方改版即失效。
  const INPUT_SEMANTIC_RE = /(message|prompt|chat|ask|输入|发送|消息|提问|对话)/i;
  const INPUT_SEMANTIC_ATTRS = ['aria-label', 'placeholder', 'data-testid', 'data-tooltip', 'title'];

  function isEditableNode(el) {
    if (!el) return false;
    const tag = el.tagName;
    if (tag === 'TEXTAREA') return true;
    if (tag === 'INPUT') {
      // 只认文本型 input：复选框/单选框/文件框在语义上不是"输入消息的地方"
      const type = (el.getAttribute('type') || 'text').toLowerCase();
      return type === 'text' || type === 'search' || type === 'url' || type === '';
    }
    return !!el.isContentEditable;
  }

  // 注意：这里**刻意不把 disabled / readOnly 当成不可用**。
  // 本子系统存在的意义之一就是清除前端假死的 disabled 状态（Step 1 会摘掉它），
  // 若在此直接排除，正好把最需要救的那一类输入域排除掉了。
  // 它们只影响排序权重：同一层里可写的排前面，全层都不可写时才退而取之。
  function isLockedInput(el) {
    return !!(el.disabled || el.readOnly);
  }

  function isUsableInput(el) {
    if (!el || !el.isConnected) return false;
    // 被 aria-disabled 标死的输入仍可作为目标；这里只排除"物理上不可见"的节点。
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return false;
    const view = el.ownerDocument && el.ownerDocument.defaultView;
    if (view) {
      const style = view.getComputedStyle(el);
      if (style && (style.display === 'none' || style.visibility === 'hidden')) return false;
    }
    return true;
  }

  // 在一组候选里挑一个：优先未被锁的，其次同分取先出现的。
  function pickBestInput(candidates) {
    let fallback = null;
    for (const cand of candidates) {
      if (!isUsableInput(cand)) continue;
      if (!isLockedInput(cand)) return cand;
      if (!fallback) fallback = cand;
    }
    return fallback;
  }

  function inputSemanticText(el) {
    const parts = [];
    for (const attr of INPUT_SEMANTIC_ATTRS) {
      const v = el.getAttribute && el.getAttribute(attr);
      if (v) parts.push(v);
    }
    return parts.join(' ');
  }

  // 结构层：如果发送按钮找到了，就沿它的祖先往上找"同时含有输入框和这颗按钮"的
  // 最小容器 —— 这是宿主自己的结构关系，不是我们的猜测。找不到按钮时才退化为几何。
  function findInputNearSendButton(sendBtn) {
    if (!sendBtn || !sendBtn.isConnected) return null;
    let node = sendBtn.parentElement;
    for (let hops = 0; node && hops < 6; hops++) {
      const editables = node.querySelectorAll('textarea, input, [contenteditable="true"]');
      const picked = pickBestInput(Array.from(editables).filter(isEditableNode));
      if (picked) return picked;
      node = node.parentElement;
    }
    return null;
  }

  // 几何兜底：宿主工作台的输入区永远贴在页面底部。取视口下半部分里
  // 最靠下的一块可见可编辑区域 —— 它几乎不可能是别的东西。
  // 锁死的候选在这里被扣一档：宁可取稍高一点但没被锁的，也不要取最底下一块点不动的。
  function findBottomMostInput() {
    let best = null;
    let bestRank = -Infinity;
    let nodes;
    try {
      nodes = document.querySelectorAll('textarea, input, [contenteditable="true"]');
    } catch (_) {
      return null;
    }
    const floor = window.innerHeight * 0.5;
    for (const cand of nodes) {
      if (!isEditableNode(cand) || !isUsableInput(cand)) continue;
      const rect = cand.getBoundingClientRect();
      if (rect.top < floor) continue;
      const rank = rect.top - (isLockedInput(cand) ? 100000 : 0);
      if (rank > bestRank) {
        bestRank = rank;
        best = cand;
      }
    }
    return best;
  }

  function findInputTargetTiered(sendBtn) {
    // 第 1 层：语义属性自称是「消息 / 提示 / 对话」输入域
    let all;
    try {
      all = document.querySelectorAll('textarea, input, [contenteditable="true"]');
    } catch (_) {
      all = [];
    }
    const semantic = [];
    for (const cand of all) {
      if (!isEditableNode(cand) || !isUsableInput(cand)) continue;
      if (INPUT_SEMANTIC_RE.test(inputSemanticText(cand))) semantic.push(cand);
    }
    if (semantic.length) return { el: pickBestInput(semantic), tier: 0 };
    // 第 2 层：与发送按钮同一容器（结构线索）
    const nearBtn = findInputNearSendButton(sendBtn);
    if (nearBtn) return { el: nearBtn, tier: 1 };
    // 第 3 层：历史类名猜测（保留旧能力，但降级为兜底）
    let legacy = null;
    try {
      legacy = document.querySelector('div[class*="chat-input"] textarea, div[class*="input-container"] textarea, .monaco-editor [contenteditable="true"]');
    } catch (_) {}
    if (legacy && isEditableNode(legacy) && isUsableInput(legacy)) return { el: legacy, tier: 2 };
    // 第 4 层：几何兜底（视口下半部最靠下的可编辑区域）
    const bottom = findBottomMostInput();
    if (bottom) return { el: bottom, tier: 3 };
    return { el: null, tier: -1 };
  }

  // 8.6 宿主 DOM 兼容性自检。
  //
  // 上游改版时，本补丁最可能的失效形态是「静默下沉」：语义层全丢，靠历史类名或几何
  // 兜底仍然点得动，于是功能看起来正常，直到兜底层也失效的那天一起爆掉。
  // 这个函数把「现在到底靠哪一层在工作」变成可随时查询的事实：
  //   detectSend / detectInput 都是从语义层算起的层号，数字越大越危险，
  //   tier_ok=false 表示连兜底层都没找到 —— 那就是功能性失效，不是降级。
  function describeHostDom() {
    const sendFound = findSendButtonTiered();
    const inputFound = findInputTargetTiered(sendFound.btn);
    const sendLabel = sendFound.btn ? sendButtonLabel(sendFound.btn).trim().slice(0, 80) : '';
    const inputDesc = inputFound.el
      ? ((inputFound.el.tagName || '') + (inputFound.el.getAttribute && inputFound.el.getAttribute('placeholder')
        ? '[placeholder=' + String(inputFound.el.getAttribute('placeholder')).slice(0, 40) + ']'
        : '')).slice(0, 120)
      : '';
    // 兼容性是「能不能干活」，不是「用哪条路干活」：只要两层里有一层找到了节点，
    // 强制发送就仍然可用；层号只用来提示退化程度。
    return {
      version: '2.2',
      detectSend: sendFound.tier,
      detectInput: inputFound.tier,
      sendLabel: sendLabel,
      inputDesc: inputDesc,
      // 宿主平时就把发送键标成 disabled（输入为空时就是常态），
      // 所以"锁着"是正常态、不是故障信号 —— 单独报出来免得被误读成失败。
      sendLocked: !!sendFound.locked,
      tier_ok: !!(sendFound.btn || inputFound.el),
      // 语义层（0/1）完全失守时把这条标出来，便于一眼判断是否该更新选择器表。
      semantic_ok: sendFound.tier >= 0 && sendFound.tier <= 1 && inputFound.tier >= 0 && inputFound.tier <= 1,
      hint: (sendFound.tier > 1 || inputFound.tier > 1)
        ? '宿主 DOM 与语义层不匹配，正在使用兜底寻址；请核对上游是否需要更新选择器表'
        : ''
    };
  }

  // ── 9.5 Capability Probe（上游兼容性自检） ─────────────────────────────────
  //
  // 回答的问题不是「上游版本是多少」（版本号只是 fingerprint，不是兼容性的判据），
  // 而是「这个宿主现在还支撑得起哪些能力」。每一项都**真去 DOM/API 上试一次**，
  // 不读版本号、不读配置开关。
  //
  // 四态（与任务书 §3 一致，刻意不再造第二套词汇）：
  //   OK           探针命中，功能可用
  //   DEGRADED     能用，但走的是兜底层（语义层失守）—— 上游大概改了 DOM
  //   UNSUPPORTED  宿主结构里根本没有这个东西（不是坏了，是不适用）
  //   FAILED       本该有，实测出错
  //
  // 为什么必须有这一层：上游改版时补丁不会「报错」，只会静默下沉到兜底层继续工作，
  // 直到某天兜底层也不灵了才爆。把「上游到底变了没有」变成一个随时可问的问题，
  // 而不是等用户来报告某个功能坏了。
  function probeCapabilities() {
    const cap = {};
    const put = (id, state, detail) => { cap[id] = { state: state, detail: detail || '' }; };

    // CDP_AVAILABLE：能从页面侧看到 __2ag（说明整段补丁确实跑起来了）。
    // 这一项实质上永远 OK —— 它跑不到就没人调用本函数 —— 但它让报告的第一行
    // 与「报告自身存在」这件事绑定，不会被误读成「什么都没探到」。
    put('CDP_AVAILABLE', 'OK', '补丁实例已在宿主文档中运行');

    // RUNTIME_INJECTION：本文件的 IIFE 跑完了，且 window.__2ag 已挂上。
    try {
      put('RUNTIME_INJECTION', (window.__2ag && typeof window.__2ag === 'object') ? 'OK' : 'FAILED',
        window.__2ag ? 'window.__2ag 可访问' : 'window.__2ag 缺失');
    } catch (e) { put('RUNTIME_INJECTION', 'FAILED', String(e).slice(0, 120)); }

    // WORKSPACE_TARGET：宿主页面自身是不是那个工作台（DOM 里有没有会话区）。
    try {
      const view = document.querySelector('[data-testid="conversation-view"]')
        || document.querySelector('[data-testid*="conversation" i]');
      const sidebar = document.querySelector('[data-testid="sidebar-toggle"]');
      if (view && sidebar) put('WORKSPACE_TARGET', 'OK', '会话区与侧栏均命中语义选择器');
      else if (view || sidebar) put('WORKSPACE_TARGET', 'DEGRADED',
        (view ? '会话区' : '侧栏') + '命中，另一项未命中');
      else put('WORKSPACE_TARGET', 'UNSUPPORTED', '未找到 conversation-view / sidebar-toggle');
    } catch (e) { put('WORKSPACE_TARGET', 'FAILED', String(e).slice(0, 120)); }

    // SKIN_ROOT / SKIN_BACKGROUND：皮肤层能不能画。
    try {
      const host = document.getElementById(SHADOW_HOST_ID);
      put('SKIN_ROOT', host && host.shadowRoot ? 'OK' : 'FAILED',
        host ? (host.shadowRoot ? 'Shadow 宿主与根均在场' : '宿主在但 shadowRoot 丢失（被抢注？）') : 'Shadow 宿主不存在');
    } catch (e) { put('SKIN_ROOT', 'FAILED', String(e).slice(0, 120)); }
    try {
      const styleEl = document.getElementById(NATIVE_STYLE_ID);
      const bgEl = document.getElementById(BG_ID);
      const wantWallpaper = effectiveWallpaper() !== '';
      if (!styleEl) put('SKIN_BACKGROUND', 'FAILED', 'native style 元素缺失');
      else if (wantWallpaper && !bgEl) put('SKIN_BACKGROUND', 'FAILED', '配置有壁纸但背景层不存在');
      else if (!wantWallpaper && bgEl) put('SKIN_BACKGROUND', 'DEGRADED', 'Native 形态却仍留着背景层');
      else put('SKIN_BACKGROUND', 'OK', wantWallpaper ? '背景层与透光级联均在' : 'Native 形态，背景层已拆除');
    } catch (e) { put('SKIN_BACKGROUND', 'FAILED', String(e).slice(0, 120)); }

    // SIDEBAR_HOOK：注入是否真的控制到了宿主侧栏（透光级联的落点）。
    try {
      const side = document.querySelector('[class~="bg-sidebar"]');
      if (!side) put('SIDEBAR_HOOK', 'UNSUPPORTED', '未找到 [class~="bg-sidebar"]（上游可能改了类名）');
      else {
        const bg = getComputedStyle(side).backgroundColor;
        const wantWallpaper = effectiveWallpaper() !== '';
        if (wantWallpaper && (bg === 'rgba(0, 0, 0, 0)' || bg.indexOf('rgba(18, 20, 26') === 0)) put('SIDEBAR_HOOK', 'OK', '侧栏已透光: ' + bg);
        else if (!wantWallpaper) put('SIDEBAR_HOOK', 'OK', 'Native 形态，侧栏保持宿主原色: ' + bg);
        else put('SIDEBAR_HOOK', 'DEGRADED', '侧栏仍在但未被透光: ' + bg);
      }
    } catch (e) { put('SIDEBAR_HOOK', 'FAILED', String(e).slice(0, 120)); }

    // LOCALIZATION_ROOT：词典层的挂载点（<html lang> 与可翻译文本节点）。
    try {
      const lang = document.documentElement.getAttribute('lang');
      const obs = window[I18N_OBSERVER_KEY];
      if (!obs) put('LOCALIZATION_ROOT', 'FAILED', '词典观察器未安装');
      else if (lang === 'zh-CN') put('LOCALIZATION_ROOT', 'OK', 'lang=zh-CN，观察器在场');
      else put('LOCALIZATION_ROOT', 'DEGRADED', '观察器在场但 lang=' + lang);
    } catch (e) { put('LOCALIZATION_ROOT', 'FAILED', String(e).slice(0, 120)); }

    // G_HUB：中枢自身可交互（浮标在、面板可开）。
    try {
      const root = document.getElementById(SHADOW_HOST_ID);
      const beacon = root && root.shadowRoot ? root.shadowRoot.getElementById('twoag-ghub') : null;
      const panel = root && root.shadowRoot ? root.shadowRoot.getElementById('twoag-cockpit') : null;
      if (beacon && panel) put('G_HUB', 'OK', '浮标与面板均在场');
      else put('G_HUB', 'DEGRADED', beacon ? '浮标在但面板缺失' : '浮标缺失');
    } catch (e) { put('G_HUB', 'FAILED', String(e).slice(0, 120)); }

    // PROMPT_HOOK / MODEL_SELECTOR：强制发送链路的两个语义锚点。
    try {
      const sendFound = findSendButtonTiered();
      if (sendFound.tier < 0) put('PROMPT_HOOK', 'UNSUPPORTED', '四层选择器全部未命中发送按钮');
      else if (sendFound.tier <= 1) put('PROMPT_HOOK', 'OK', '语义层命中（tier ' + sendFound.tier + '）');
      else put('PROMPT_HOOK', 'DEGRADED', '仅兜底层命中（tier ' + sendFound.tier + '）');
    } catch (e) { put('PROMPT_HOOK', 'FAILED', String(e).slice(0, 120)); }
    try {
      const model = document.querySelector('[data-testid*="model" i]')
        || document.querySelector('[aria-label*="model" i]')
        || document.querySelector('[aria-label*="模型"]');
      put('MODEL_SELECTOR', model ? 'OK' : 'UNSUPPORTED', model ? '命中: ' + (model.getAttribute('data-testid') || model.getAttribute('aria-label') || model.tagName) : '未找到模型选择器');
    } catch (e) { put('MODEL_SELECTOR', 'FAILED', String(e).slice(0, 120)); }

    // 汇总：只要有一项不是 OK 就如实报出来。刻意不做「全绿 = 兼容」这种判断 ——
    // 那等于把「四项不适用」与「四项都好」画成同一个数。
    let ok = 0, degraded = 0, unsupported = 0, failed = 0;
    for (const k of Object.keys(cap)) {
      const s = cap[k].state;
      if (s === 'OK') ok++; else if (s === 'DEGRADED') degraded++;
      else if (s === 'UNSUPPORTED') unsupported++; else failed++;
    }
    return { cap: cap, ok: ok, degraded: degraded, unsupported: unsupported, failed: failed };
  }
  function executeForceDispatch(text) {
    console.log('[2AG_FORCE_SEND_TRIGGERED]', {
      timestamp: Date.now(),
      subsystems: Object.assign({}, subsystems)
    });

    // Step 1: 动态寻址定位当前活跃的输入容器
    let target = document.activeElement;
    let targetTier = -1;
    const isInputElement = (el) => {
      if (!el) return false;
      const tag = el.tagName;
      return tag === 'TEXTAREA' || tag === 'INPUT' || el.isContentEditable || el.classList.contains('monaco-editor') || el.closest('.monaco-editor');
    };

    if (!isInputElement(target)) {
      // 先把发送按钮找出来：它既是第 2 层的结构线索，也避免后面重复寻址。
      const sendFound = findSendButtonTiered();
      const inputFound = findInputTargetTiered(sendFound.btn);
      target = inputFound.el;
      targetTier = inputFound.tier;
      // 寻址下沉到兜底层时如实播报：这不是错误，但用户/我们都需要知道
      // 「这次点的是哪一层」，否则官方改版导致语义层全失效时，界面上什么都看不出来。
      if (targetTier >= 2) {
        console.warn('[2AG_INPUT_TIER_FALLBACK]', {
          tier: targetTier,
          note: '语义层未命中，已下沉到' + (targetTier === 2 ? '历史类名' : '几何兜底') + '；宿主 DOM 可能已改版'
        });
      }
    } else {
      targetTier = -2; // 用户自己就在输入框里，直接用 activeElement
    }

    if (target) {
      target.removeAttribute('disabled');
      target.setAttribute('aria-disabled', 'false');
      target.style.setProperty('pointer-events', 'auto', 'important');
      if (typeof target.focus === 'function') target.focus();
    }

    // Step 1b: 受控输入（React/Vue）写值穿透
    // 仅在调用方显式给了文本时写入（快捷键路径不传文本 → 保持原有行为，绝不改写用户已输入的内容）
    if (target && typeof text === 'string' && text.length > 0) {
      writeValueIntoInput(target, text);
    }

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

    // Step 3: 若宿主界面仍未提交，等一次同步 flush 后重新寻址当前可见的发送按钮并触发原生 .click()
    // 已废除：静态 sendButtons 快照（:552 抓到的链表，35ms 后可能已全是孤儿或已复用为 Stop）
    setTimeout(() => {
      fallbackClickVisibleSendButton();
    }, FALLBACK_CLICK_DELAY_MS);

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
      if (hubDisposed) return;
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
      // 就绪判定必须包含"仍在 body 上 + 浮标仍真的可见"：
      // 宿主 div 自身没有布局（子元素全是 position:fixed），拿它量 rect 恒为 0，
      // 所以只能量 shadow 里的浮标。仅判 shadowRoot 存在会让宿主把 host 搬进
      // 隐藏容器 / content-visibility:hidden 祖先之后永远早退（静默哑火、不自愈）。
      const ghubEl = rootHost.shadowRoot.getElementById('twoag-ghub');
      const ghubRect = ghubEl ? ghubEl.getBoundingClientRect() : null;
      // rect 非零 ≠ 可见：content-visibility:hidden 的祖先、visibility:hidden、opacity:0
      // 都会保留下层布局（rect 照旧）却让浮标完全不可见 —— 这类"隐形哑火"必须判为不可用
      let cssVisible = true;
      if (ghubEl && typeof ghubEl.checkVisibility === 'function') {
        try {
          cssVisible = ghubEl.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true, contentVisibilityAuto: true });
        } catch (_) {}
      }
      const stillUsable = rootHost.isConnected
        && rootHost.parentNode === document.body
        && !!ghubEl
        && ghubRect.width > 0 && ghubRect.height > 0
        && cssVisible
        // 实例指纹必须一致。「浮标可见」只证明那棵树当年建得好，不证明它属于本实例：
        // 热重载在同一个文档里重新求值时，旧节点完全满足上面四条，于是新实例早退、
        // 新 UI 永不落 DOM（热重载"成功"而界面不变）。盖章比对让陈旧树必然被重建。
        && rootHost.dataset.hubInstance === HUB_INSTANCE_ID;
      if (stillUsable) {
        return; // 已经就绪
      }
      // 节点还活着但已不可用（被搬走 / 被藏起来）：shadow tree 无法二次 attachShadow，
      // 必须丢弃旧节点重建，否则会退化成"每 2s 都认为已完成"的永久哑火
      try { rootHost.remove(); } catch (_) {}
      rootHost = null;
    }

    if (!rootHost) {
      rootHost = document.createElement('div');
      rootHost.id = SHADOW_HOST_ID;
      (document.body || document.documentElement).appendChild(rootHost);
    }

    // 盖章必须在 attachShadow 之前。盖章是 stillUsable 的第六条判据：没有章的节点会被
    // 判为陈旧树、被拆掉重建，所以每条创建路径都得盖，漏一处就会多一次无谓重建。
    rootHost.dataset.hubInstance = HUB_INSTANCE_ID;

    let shadow;
    try {
      shadow = rootHost.attachShadow({ mode: 'open' });
    } catch (err) {
      // 该节点已被他人以 closed 模式抢注 shadow tree —— attachShadow 对此节点永久抛错。
      // 丢弃抢注节点、用同一 id 重建，而不是每 2s 抛一次被 try{}catch{} 吞掉（静默哑火）。
      console.warn('[2Ag] attachShadow 失败（疑被 closed-shadow 抢注），改用新建同 id 宿主重试：', err && err.message);
      try { rootHost.remove(); } catch (_) {}
      rootHost = document.createElement('div');
      rootHost.id = SHADOW_HOST_ID;
      rootHost.dataset.hubInstance = HUB_INSTANCE_ID; // 第二条创建路径，同样要盖章
      (document.body || document.documentElement).appendChild(rootHost);
      try {
        shadow = rootHost.attachShadow({ mode: 'open' });
      } catch (err2) {
        console.warn('[2Ag] attachShadow 在新建宿主上仍失败，本轮放弃挂载：', err2 && err2.message);
        return;
      }
    }

    // 挂载独立样式
    const styleEl = document.createElement('style');
    styleEl.textContent = SHADOW_CSS;
    shadow.appendChild(styleEl);

    // 容器包装
    const container = document.createElement('div');
    container.className = 'twoag-scope';
    container.innerHTML = `
      <!-- Google 四色渐变定义。必须挂在 shadow root 内部：<linearGradient> 的
           url(#id) 引用是同根解析的，放到宿主 document 里会解析不到（closed/open
           都取不到跨树引用）。宽高 0 且 absolute，不占布局。

           配色与停位是刻意选的，不要「顺手调柔和」：
           · 用 Google 品牌全饱和四色（蓝 #4285F4 / 红 #EA4335 / 黄 #FBBC05 /
             绿 #34A853），而不是浅色变体（#8AB4F8 等）。浅色在 14px 下会互相
             糊成一团粉黄，看起来像一个彩色 emoji，而不是「四色」。
           · 四个色带各占约 25%，色带之间只留 4% 的过渡。早先的写法是四个停位
             均匀铺开做连续插值，结果任意时刻可见区域都只有 1–2 个中间混色，
             四个品牌色一个都不纯。硬边色带保证任何尺寸下四色都在场。 -->
      <svg width="0" height="0" style="position: absolute;" aria-hidden="true">
        <defs>
          <linearGradient id="ag-g4-gear" x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stop-color="#4285F4" />
            <stop offset="23%" stop-color="#4285F4" />
            <stop offset="27%" stop-color="#EA4335" />
            <stop offset="48%" stop-color="#EA4335" />
            <stop offset="52%" stop-color="#FBBC05" />
            <stop offset="73%" stop-color="#FBBC05" />
            <stop offset="77%" stop-color="#34A853" />
            <stop offset="100%" stop-color="#34A853" />
          </linearGradient>
        </defs>
      </svg>

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
                  <!-- 「· 18ms」是历史写死的假延迟：全文件没有任何一处更新过这个节点，
                       它从挂载到卸载恒定显示 18ms，属于纯装饰。这里只保留可验证的事实
                       —— 补丁能渲染出这段 UI，就说明宿主注入确实已就绪。 -->
                  <span class="status-text">宿主注入就绪</span>
                </div>
              </div>
            </div>
            <button id="btn-close-cockpit" class="close-btn" type="button" title="关闭面板">
              ${SVG_ICONS.close}
            </button>
          </div>

          <!-- 主题库：点击即换肤。数值与 Go 端 internal/core/state.go 的 SET_PRESET 分支
               逐项对齐，保证「舱内换肤」与「2Ag 主窗口换肤」得到完全相同的外观。
               芯片为 Material 3 药丸形：左侧渐变圆点取 THEME_PRESETS[t].swatch（本身就是
               该主题主色调的 linear-gradient 字符串），右侧为名称。 -->
          <div class="section-tag">THEME MATRIX</div>
          <div class="theme-row" id="theme-row">
            ${THEME_PRESETS.map((t) => `
              <div class="theme-chip" data-preset="${t.id}" data-active="false" title="${t.title}">
                <span class="theme-dot" style="background: ${t.swatch};"></span>
                <span class="theme-name">${t.short}</span>
              </div>
            `).join('')}
            <!-- 第 6 个芯片：端内调参入口。齿轮必须是纯正 Google 四色渐变 ——
                 单色涂抹会与其余 5 个芯片的双色色板语义混淆。渐变定义放在
                 shadow root 内的 <defs>（fill="url(#ag-g4-gear)" 在同根内解析）。 -->
            <div class="theme-chip theme-chip-tune" id="theme-chip-tune" data-active="false" title="展开端内微调抽屉（模糊度 / 遮罩暗度 / 模态弹窗遮蔽度）">
              <svg class="theme-gear" viewBox="0 0 24 24" aria-hidden="true">
                <path fill="url(#ag-g4-gear)" d="M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.488.488 0 0 0-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54a.484.484 0 0 0-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z"/>
              </svg>
              <span class="theme-name">调参</span>
            </div>
          </div>

          <!-- 端内微调抽屉：与主窗口「材质与视觉」三个滑块同一口径、同一落点。
               拖动时毫秒级改 state + ensureDreamSkin（宿主内存，不落盘）；
               松手 300ms 后 POST /api/v1/action SET_THEME 持久化到 2ag.json。 -->
          <div class="live-tune-drawer" id="live-tune-drawer" data-open="false">
            <div class="tune-row">
              <div class="tune-head">
                <span class="tune-label">背景模糊度 (Blur)</span>
                <span class="tune-val" id="tune-blur-val">--</span>
              </div>
              <input class="tune-range" type="range" id="tune-blur" min="0" max="40" step="1" value="28">
            </div>
            <div class="tune-row">
              <div class="tune-head">
                <span class="tune-label">遮罩暗度 (Darkness)</span>
                <span class="tune-val" id="tune-dark-val">--</span>
              </div>
              <input class="tune-range" type="range" id="tune-dark" min="10" max="90" step="1" value="60">
            </div>
            <div class="tune-row">
              <div class="tune-head">
                <span class="tune-label">模态弹窗遮蔽度 (Modal Glass)</span>
                <span class="tune-val" id="tune-modal-val">--</span>
              </div>
              <input class="tune-range" type="range" id="tune-modal" min="70" max="100" step="1" value="90">
            </div>
          </div>

          <!-- 账号轮转：数据来自本机 2Ag API，切换走 switch-and-restart（含宿主沙箱重启）。
               默认折叠为单行手风琴，点击整行展开备选账号列表。 -->
          <div class="section-tag">ACCOUNT ROTATION</div>
          <div class="acct-acc" id="acct-acc" data-open="false">
            <div class="acct-head" id="acct-head" title="点击展开 / 收起备选账号">
              <span class="acct-dot" id="acct-dot" data-off="true"></span>
              <span class="acct-head-mail" id="acct-head-mail">正在读取本机账号…</span>
              <span class="acct-head-eye" id="acct-head-eye"></span>
              <span class="acct-pill" id="acct-head-pill">主账号</span>
              <span class="acct-caret">▾</span>
            </div>
            <div class="acct-body">
              <div class="acct-list" id="acct-list">
                <div class="acct-empty">正在读取本机账号…</div>
              </div>
            </div>
          </div>

          <!-- 配额池：一个池一块，池内两个窗口桶各占一个圆环。
               为什么必须四个桶全列：历史实现只显示 Gemini-5h 与 Claude-周这两个桶，
               另外两个（Gemini 周、Claude 5h）在面板上完全不可见 —— 而「剩余最少」
               的恰恰常是它们。用户拿 G-Hub 的「周 92%」（其实是 Claude 周）去比
               Manager 里的 Gemini 周，必然对不上，于是整块面板被判定成假数据。
               现在每个环旁边都写着「哪个池的哪个窗口」，任何数字都不需要猜归属。

               可用性按池标记（available=false 的池整块置灰并显示 --）：
               后端只会把「真的解析到该池的 5h / weekly 桶」标成可用，读不到就
               如实说「未载入」，绝不用默认值或另一个池的读数顶替。 -->
          <div class="quota-caps" id="quota-caps">
            <div class="quota-pool" id="quota-pool-gemini" data-available="false">
              <div class="quota-pool-head">
                <span class="quota-pool-name">Gemini Models</span>
                <span class="quota-pool-hint">剩余额度</span>
              </div>
              <div class="quota-buckets">
                <div class="quota-bucket" id="quota-bucket-gemini-5h">
                  <div class="quota-ring-wrap">
                    <svg class="quota-ring" viewBox="0 0 46 46" aria-hidden="true">
                      <circle class="quota-ring-track" cx="23" cy="23" r="18"></circle>
                      <circle class="quota-ring-arc" cx="23" cy="23" r="18"></circle>
                    </svg>
                    <div class="quota-ring-center">
                      <span class="quota-bucket-value">--</span>
                    </div>
                  </div>
                  <div class="quota-bucket-text">
                    <span class="quota-bucket-label">5h 滑窗</span>
                    <span class="quota-bucket-reset">--</span>
                  </div>
                </div>
                <div class="quota-bucket" id="quota-bucket-gemini-wk">
                  <div class="quota-ring-wrap">
                    <svg class="quota-ring" viewBox="0 0 46 46" aria-hidden="true">
                      <circle class="quota-ring-track" cx="23" cy="23" r="18"></circle>
                      <circle class="quota-ring-arc" cx="23" cy="23" r="18"></circle>
                    </svg>
                    <div class="quota-ring-center">
                      <span class="quota-bucket-value">--</span>
                    </div>
                  </div>
                  <div class="quota-bucket-text">
                    <span class="quota-bucket-label">周限额</span>
                    <span class="quota-bucket-reset">--</span>
                  </div>
                </div>
              </div>
            </div>
            <div class="quota-pool" id="quota-pool-claude" data-available="false">
              <div class="quota-pool-head">
                <span class="quota-pool-name">Claude &amp; GPT Models</span>
                <span class="quota-pool-hint">剩余额度</span>
              </div>
              <div class="quota-buckets">
                <div class="quota-bucket" id="quota-bucket-claude-5h">
                  <div class="quota-ring-wrap">
                    <svg class="quota-ring" viewBox="0 0 46 46" aria-hidden="true">
                      <circle class="quota-ring-track" cx="23" cy="23" r="18"></circle>
                      <circle class="quota-ring-arc" cx="23" cy="23" r="18"></circle>
                    </svg>
                    <div class="quota-ring-center">
                      <span class="quota-bucket-value">--</span>
                    </div>
                  </div>
                  <div class="quota-bucket-text">
                    <span class="quota-bucket-label">5h 滑窗</span>
                    <span class="quota-bucket-reset">--</span>
                  </div>
                </div>
                <div class="quota-bucket" id="quota-bucket-claude-wk">
                  <div class="quota-ring-wrap">
                    <svg class="quota-ring" viewBox="0 0 46 46" aria-hidden="true">
                      <circle class="quota-ring-track" cx="23" cy="23" r="18"></circle>
                      <circle class="quota-ring-arc" cx="23" cy="23" r="18"></circle>
                    </svg>
                    <div class="quota-ring-center">
                      <span class="quota-bucket-value">--</span>
                    </div>
                  </div>
                  <div class="quota-bucket-text">
                    <span class="quota-bucket-label">周限额</span>
                    <span class="quota-bucket-reset">--</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <!-- 出处与新鲜度：数据来自本机授权缓存，本工程只读、从不主动向官方端点握手。
               不写这一行，用户会把 9 小时前的旧快照当成实时读数 —— 两者在界面上完全一样。 -->
          <div class="quota-source" id="quota-source" data-stale="false">配额出处：读取中…</div>

          <!-- GRAVITY BOOST SUBSYSTEMS 分组 -->
          <div class="section-tag">GRAVITY BOOST SUBSYSTEMS</div>
          <div class="subsystem-list">
            <div class="subsystem-item" data-sys="force_dispatch">
              <div class="item-text">
                <span class="item-name">强制发送通道<span class="item-latin">Force Dispatch</span></span>
                <span class="item-desc">穿透前端假死 · Ctrl + Shift + Enter</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="force_dispatch" data-state="${subsystems.force_dispatch ? 'on' : 'off'}" role="switch" aria-checked="${subsystems.force_dispatch ? 'true' : 'false'}">
                ${renderQuadrantRing(subsystems.force_dispatch)}
              </button>
            </div>

            <div class="subsystem-item" data-sys="state_healer">
              <div class="item-text">
                <span class="item-name">输入状态自愈<span class="item-latin">State Healer</span></span>
                <span class="item-desc">自动清除 disabled 状态脱缰</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="state_healer" data-state="${subsystems.state_healer ? 'on' : 'off'}" role="switch" aria-checked="${subsystems.state_healer ? 'true' : 'false'}">
                ${renderQuadrantRing(subsystems.state_healer)}
              </button>
            </div>

            <div class="subsystem-item" data-sys="overlay_stripper">
              <div class="item-text">
                <span class="item-name">过渡遮罩粉碎<span class="item-latin">Overlay Stripper</span></span>
                <span class="item-desc">过滤 Loading / about:blank 幽灵图层</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="overlay_stripper" data-state="${subsystems.overlay_stripper ? 'on' : 'off'}" role="switch" aria-checked="${subsystems.overlay_stripper ? 'true' : 'false'}">
                ${renderQuadrantRing(subsystems.overlay_stripper)}
              </button>
            </div>

            <!-- text_fallback 是空头开关：subsystems.text_fallback 只在定义、这段 UI 与圆环渲染
                 中出现，全文件没有任何一处读取它去改变行为。历史实现在这里把它渲染成一个
                 可点亮的开关，用户以为「富文本崩溃时有了兜底」，实际不存在这条代码路径。
                 按与前端「实验性」开关同一原则处理：置灰、不可点、如实标注。
                 内联样式已移除，改由 .subsystem-item.is-disabled + 黄环（data-state="experimental"）
                 表达 —— 与 Manager 侧 #ring-preserve_scroll / #ring-disable_auto_update 同一套。 -->
            <div class="subsystem-item is-disabled" data-sys="text_fallback" title="实验性：宿主侧实现尚未挂钩，切换不会生效">
              <div class="item-text">
                <span class="item-name">纯文本降级模式<span class="item-latin">Text Fallback</span><span class="tag-experimental">实验性 · 开发中</span></span>
                <span class="item-desc">富文本彻底崩溃时兜底原生表单 —— 宿主侧尚未挂钩，切换不会生效</span>
              </div>
              <button class="quadrant-btn" type="button" data-key="text_fallback" data-state="experimental" role="switch" aria-checked="false" aria-disabled="true" disabled>
                ${renderQuadrantRing(subsystems.text_fallback, 'experimental')}
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

      <!-- 账号切换平滑重启遮罩 -->
      <div id="ghub-loading" class="ghub-loading" data-show="false">
        <div class="ghub-spinner"></div>
        <div class="ghub-loading-text">正在平滑重启并载入隔离沙箱...</div>
        <div class="ghub-loading-sub">宿主进程将被优雅终止并换用目标账号的 profile 目录重新拉起</div>
      </div>
    `;
    shadow.appendChild(container);

    const ghubEl = shadow.getElementById('twoag-ghub');
    const cockpitEl = shadow.getElementById('twoag-cockpit');
    const closeBtn = shadow.getElementById('btn-close-cockpit');
    const forceSendBtn = shadow.getElementById('btn-action-force-send');
    const reloadBtn = shadow.getElementById('btn-action-reload');
    const themeRow = shadow.getElementById('theme-row');
    const acctListEl = shadow.getElementById('acct-list');
    const loadingEl = shadow.getElementById('ghub-loading');

    // 计算与布局更新
    const HUB_SIZE = 44;            // 浮标直径（与 .ghub-beacon 的 width/height 一致）
    const COCKPIT_GAP = 4;          // 面板与浮标之间的水平间隙
    const DRAG_THRESHOLD_PX = 4;    // 位移死区：< 4px 判为点击，>= 4px 判为拖拽
    // 面板假定尺寸：打开瞬间 panel.offsetHeight 可能是 0（首次渲染尚未布局），
    // 四象限弹射需要先按假定值算，并逐帧用真实尺寸纠偏（见 updateCockpitLayout）。
    const PANEL_EST_W = 340;
    const PANEL_EST_H = 480;
    const VIEW_MARGIN = 12;         // 面板距视口边缘的安全留白

    // 浮标左上角坐标 ↔ 比例的双向换算。
    // 分母用 (可用空间)，而不是视口尺寸 —— 保证 ratio=1 时浮标右/下边缘
    // 恰好贴住视口内侧，ratio=0 时贴住左上角，缩放窗口时两端都不会跑出屏幕。
    function hubLeftForRatio(xRatio, winW) {
      const span = Math.max(1, winW - HUB_SIZE);
      return Math.max(0, Math.min(span, xRatio * span));
    }
    function hubTopForRatio(yRatio, winH) {
      const span = Math.max(1, winH - HUB_SIZE);
      return Math.max(0, Math.min(span, yRatio * span));
    }

    function updateGHubLayout(animated) {
      if (!ghubEl) return;
      const winW = window.innerWidth;
      const winH = window.innerHeight;
      const targetLeft = hubLeftForRatio(hubPos.xRatio, winW);
      const targetTop = hubTopForRatio(hubPos.yRatio, winH);

      if (animated) {
        ghubEl.style.transition = 'top 0.35s cubic-bezier(0.2, 0.8, 0.2, 1), left 0.35s cubic-bezier(0.2, 0.8, 0.2, 1)';
      } else {
        ghubEl.style.transition = 'none';
      }
      ghubEl.style.left = targetLeft + 'px';
      ghubEl.style.top = targetTop + 'px';

      // 窗口缩小后浮标可能已被推到新的更小空间里，用回写比例让持久化状态与实际一致。
      hubPos.xRatio = clamp01(winW - HUB_SIZE > 0 ? targetLeft / (winW - HUB_SIZE) : 0);
      hubPos.yRatio = clamp01(winH - HUB_SIZE > 0 ? targetTop / (winH - HUB_SIZE) : 0);

      updateCockpitLayout();
    }

    // 四象限自适应弹射：面板永远以浮标为锚点朝"空间更大的那一侧"展开，
    // 并且无论在哪个角落都能完整落在可视区内。
    function updateCockpitLayout() {
      if (!cockpitEl || !ghubEl) return;
      const ghubRect = ghubEl.getBoundingClientRect();
      const winW = window.innerWidth;
      const winH = window.innerHeight;

      // offsetWidth/Height 在首次打开时可能为 0（元素刚脱离 opacity:0/缩放态），
      // 用假定尺寸兜底，避免"面板被算到视口外"。
      // 高度还必须按视口再钳一次：CSS 的 max-height 已经压住了真实高度，但这里
      // 用于定位的 panelH 若取到未钳制的值，top 就会被算成一个把面板推出屏外的数。
      const panelW = cockpitEl.offsetWidth || PANEL_EST_W;
      const rawPanelH = cockpitEl.offsetHeight || PANEL_EST_H;
      const panelH = Math.min(rawPanelH, Math.max(VIEW_MARGIN, winH - 2 * VIEW_MARGIN));

      // --- 水平：优先向右展开，右侧放不下就向左 ---
      // 判据用浮标右边缘到视口右侧的真实剩余空间，而不是假设浮标贴在某个固定列。
      const spaceRight = winW - (ghubRect.left + HUB_SIZE);
      const spaceLeft = ghubRect.left;
      let left;
      if (spaceRight >= panelW + COCKPIT_GAP + VIEW_MARGIN) {
        left = ghubRect.left + HUB_SIZE + COCKPIT_GAP;            // 向右展开
      } else if (spaceLeft >= panelW + COCKPIT_GAP + VIEW_MARGIN) {
        left = ghubRect.left - panelW - COCKPIT_GAP;              // 向左展开
      } else {
        // 两侧都放不下（浮标停在屏幕正中的窄视口）：退化为「钉住可见区」，
        // 宁可让面板与浮标重叠，也不能让它出界。
        left = Math.max(VIEW_MARGIN, Math.min(winW - panelW - VIEW_MARGIN, ghubRect.left));
      }
      // 最终硬钳制：任何分支都不许把面板推出视口。
      left = Math.max(VIEW_MARGIN, Math.min(Math.max(VIEW_MARGIN, winW - panelW - VIEW_MARGIN), left));
      cockpitEl.style.left = left + 'px';
      cockpitEl.style.right = 'auto';

      // --- 垂直：优先与浮标顶部齐平，再钳进视口 ---
      const maxTop = Math.max(VIEW_MARGIN, winH - panelH - VIEW_MARGIN);
      const safeTop = Math.max(VIEW_MARGIN, Math.min(maxTop, ghubRect.top));
      cockpitEl.style.top = safeTop + 'px';

      // 视口比面板还窄时收缩宽度，保证横向不溢出。
      const available = winW - 2 * VIEW_MARGIN;
      cockpitEl.style.maxWidth = available > 0 ? Math.min(330, available) + 'px' : 'none';
    }

    function toggleCockpit(open) {
      isPanelOpen = typeof open === 'boolean' ? open : !isPanelOpen;
      cockpitEl.dataset.open = isPanelOpen ? 'true' : 'false';
      rememberUiOpenState('panel', isPanelOpen);
      if (isPanelOpen) {
        updateCockpitLayout();
        // 面板每次打开都刷新一次「账号列表 + 主题高亮」：账号可能在别处被切过，
        // 主题也可能被主窗口改过，面板里显示的必须是此刻的真值而不是上次渲染的残留。
        try { syncThemeChips(); } catch (_) {}
        try { refreshAccountList(); } catch (_) {}
      }
    }

    // --- 自由拖拽：抓取偏移锁定 + 5px 死区 ---
    let startX = 0;
    let startY = 0;
    // grabX / grabY = 鼠标相对浮标左上角的恒定偏移（pointerdown 时一次性锁定）。
    // 用它换算绝对坐标，指针全程停留在浮标内的同一相对位置，彻底根除抖动。
    let grabX = 0;
    let grabY = 0;
    let isDragging = false;
    // pointerActive = 「本次指针交互确实起于浮标上的 pointerdown」。
    // 这是拖拽语义的唯一合法凭据。没有它，窗口级 pointermove 会拿 startX/startY
    // （初值 0,0，或上一次拖拽遗留的旧起点）去和当前指针位置求距离，于是：
    //   · 用户从未碰过浮标，只是在页面上拖选一段文字
    //   · → pointermove 带着 buttons=1 到达 window
    //   · → dx = clientX - 0 直接是几百像素，远超 4px 死区
    //   · → isDragging 被误置为 true，浮标凭空飞到光标底下，面板同时被关掉
    // 真实用户从不需要「按住任意位置拖动来搬浮标」，所以必须用 pointerActive 一刀切断。
    let pointerActive = false;

    ghubEl.addEventListener('pointerdown', (e) => {
      startX = e.clientX;
      startY = e.clientY;
      pointerActive = true;
      // 用 getBoundingClientRect() 取整元素边界，严禁使用 e.offsetX / e.offsetY
      // —— offsetX/Y 的参考系随 event.target 漂移（命中 svg/path/inner 时基准不同），
      //    会直接产生几十像素的瞬移跳跃。
      const rect = ghubEl.getBoundingClientRect();
      grabX = e.clientX - rect.left;
      grabY = e.clientY - rect.top;
      isDragging = false;
      // 拖拽期间禁止 CSS 缓动与指针位移打架
      ghubEl.style.transition = 'none';
      try { ghubEl.setPointerCapture(e.pointerId); } catch (_) {}
    });

    window.addEventListener('pointermove', (e) => {
      // --- 凭据闸：与浮标无关的指针移动一律不参与拖拽判定 ---
      // 必须用 pointerActive（起于浮标本体的 pointerdown）而不是 startX/startY 做判据：
      // startX/startY 初值是 0,0，且一次拖拽结束后仍留着旧起点，拿它求 hypot 会让
      // 「用户按住鼠标在页面上拖选文字」这种完全无关的操作被误判成拖拽浮标 ——
      // 浮标会凭空飞到光标底下，面板同时被关掉。真实用户从不需要按住任意位置搬浮标。
      if (!pointerActive) return;
      // --- 悬停硬闸 ---
      // 指针仍在本页移动但鼠标键已经松开（在窗口外松手、或 pointerup 丢失）：
      // 收束为一次结束，绝不让它继续当作拖拽。
      if (e.buttons === 0) {
        if (isDragging) finishDrag(e);
        else pointerActive = false;
        return;
      }
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;

      if (!isDragging) {
        if (Math.hypot(dx, dy) >= DRAG_THRESHOLD_PX) {
          isDragging = true;
          if (isPanelOpen) toggleCockpit(false);
        }
      }

      if (isDragging) {
        // 空间脱节防线（每帧兜底）：拖拽进行中面板必须无条件保持关闭，
        // 杜绝「浮标被拖走、面板仍留在原地」的幽灵状态。
        if (isPanelOpen) toggleCockpit(false);
        const winW = window.innerWidth;
        const winH = window.innerHeight;
        // 严格基于抓取偏移计算浮标左上角，并与真实指针保持恒定相对位置
        const newLeft = Math.max(0, Math.min(winW - HUB_SIZE, e.clientX - grabX));
        const newTop = Math.max(0, Math.min(winH - HUB_SIZE, e.clientY - grabY));
        ghubEl.style.left = newLeft + 'px';
        ghubEl.style.top = newTop + 'px';
      }
    });

    function finishDrag(e) {
      // 无论走哪条分支，这次指针交互都已结束，凭据必须即刻作废 ——
      // 否则后续任何 buttons=1 的移动（例如用户拖选文字）会被当成拖拽的延续。
      pointerActive = false;

      if (!isDragging) {
        // 位移未越过 4px 死区 ⇒ 这是一次「点击」，不是拖拽：切换面板开合。
        // 点击绝不允许改动位置 —— 浮标停在原地，只有面板状态翻转。
        try { ghubEl.releasePointerCapture(e.pointerId); } catch (_) {}
        toggleCockpit();
        return;
      }

      isDragging = false;
      try { ghubEl.releasePointerCapture(e.pointerId); } catch (_) {}

      // 落点即终点：绝不吸附、绝不弹回边缘。
      // 只把当前像素坐标原样折算成比例存档，供下次启动/窗口缩放时复原。
      // 若这个位置此刻会超出可视区（窗口刚变小），由 updateGHubLayout 负责推回，
      // 但推回的目标就是钳制后的边界，不做任何"就近贴边"的再解释。
      const winW = window.innerWidth;
      const winH = window.innerHeight;
      const currentRect = ghubEl.getBoundingClientRect();
      const spanX = Math.max(1, winW - HUB_SIZE);
      const spanY = Math.max(1, winH - HUB_SIZE);
      hubPos.xRatio = clamp01(currentRect.left / spanX);
      hubPos.yRatio = clamp01(currentRect.top / spanY);

      saveHubPosition();
      // 无缓动回写：位置与松手瞬间逐像素一致，不产生任何回弹位移。
      updateGHubLayout(false);
    }

    ghubEl.addEventListener('pointerup', finishDrag);
    ghubEl.addEventListener('pointercancel', finishDrag);

    // --- 面板开合的绝对裁决权 ---
    // 铁律一：面板与浮标上绝不挂 mouseleave / pointerleave（悬停消失是低级 bug，
    //         且鼠标快速划过时 leave 会因指针脱离元素而误触发）。
    // 铁律二：关闭只能由三种明确意图触发 —— 再次点击浮标、点击面板外部、按下 ESC。
    //         绝不因为"鼠标移开了"而关闭。
    // 这里只负责给出裁决逻辑；真正的窗口监听由 ensureHubRuntimeHooks() 幂等安装一次，
    // 并转发到本闭包（否则每次重挂载都会多叠一对窗口监听器）。
    hubPanelDismisser = (e) => {
      if (!isPanelOpen) return;
      if (e.type === 'keydown') {
        // ESC 无条件关闭：不看面板是否可见、不看焦点在不在宿主输入框里，
        // 也不阻止默认行为与传播（宿主自己的 ESC 语义必须保持原样）。
        if (e.key === 'Escape') toggleCockpit(false);
        return;
      }
      // 事件从 Shadow DOM 内部冒出来时，e.target 会被重定向到宿主 <div>，
      // 光比对 target 永远认不出"点的是面板内部"。必须走 composedPath() 拿完整链路。
      if (typeof e.composedPath === 'function') {
        const path = e.composedPath();
        if (path.includes(ghubEl) || path.includes(cockpitEl)) return;
      } else if (e.target === ghubEl || e.target === cockpitEl) {
        return;
      }
      toggleCockpit(false);
    };

    // 面板内部控制
    closeBtn.addEventListener('click', () => toggleCockpit(false));

    // 象限圆环开关点击事件（事件委托）
    shadow.querySelectorAll('.subsystem-item').forEach(item => {
      item.addEventListener('click', (e) => {
        const key = item.dataset.sys;
        if (!key || subsystems[key] === undefined) return;
        // 标了 disabled 的条目是「实验性、宿主侧未挂钩」的空头开关：
        // 点击处理器挂在整行上（不只是圆环按钮），所以必须在入口显式拒绝，
        // 否则点文字区域依然能翻动状态并写进 localStorage —— 那就又变回假开关了。
        const ringBtn = item.querySelector('.quadrant-btn');
        if (ringBtn && ringBtn.disabled) return;
        subsystems[key] = !subsystems[key];
        saveSubsystems();

        // 重新渲染当前项开关圆环。data-state 与 class 必须一起更新 ——
        // 两者分工：data-state 说「这个功能现在是什么状态」，class 说「用哪种图形画」。
        // 只更新其中一个就会出现「灰环但 aria 说开着」这类自相矛盾的界面。
        const btn = item.querySelector('.quadrant-btn');
        if (btn) {
          btn.innerHTML = renderQuadrantRing(subsystems[key]);
          btn.setAttribute('data-state', subsystems[key] ? 'on' : 'off');
          btn.setAttribute('aria-checked', subsystems[key] ? 'true' : 'false');
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

    // ------------------------------------------------------------------
    // THEME MATRIX：舱内实时换肤
    // ------------------------------------------------------------------
    // 与主窗口「主题库」是同一套预设、同一套数值，只是改走补丁内联路径：
    // 直接改 state 的 blur/opacity/modalOpacity 再调 ensureDreamSkin()，
    // 后者在背景层已存在时走 setProperty 逐项回写 —— 无需重建节点，零延迟生效。
    // 薄委托：实现只有一份，就是顶层的 syncThemeChipsFromDom()。
    // 之所以不在闭包里再写一份，是因为闭包版本绑死在「本实例的 themeRow 句柄」上，
    // 而重注入后的新实例不会重新绑定（它发现旧 UI 可用就提前返回），那份实现会与
    // 顶层版本各自漂移。统一走顶层一条路径，任何实例调用的结果都一致。
    function syncThemeChips() {
      syncThemeChipsFromDom();
    }

    function applyThemePreset(presetId) {
      const preset = THEME_PRESETS.filter((t) => t.id === presetId)[0];
      // 白名单外的一律拒绝，绝不「就近取一个默认值」——那会让 UI 与实际外观脱节。
      if (!preset) return;

      // 1) 本机瞬时生效：改 state 后 ensureDreamSkin() 走 setProperty 逐项回写，
      //    不重建节点、不等网络 —— 这是「零延迟换肤」的实现路径。
      state.blur = preset.blur;
      state.opacity = preset.opacity;
      state.modalOpacity = preset.modalOpacity;
      state.preset = presetId;
      try { ensureDreamSkin(); } catch (_) {}
      try { ensureNativeStyles(); } catch (_) {}
      syncThemeChips();
      // 预设反哺滑块：抽屉里三个滑块的位置与右侧数值必须即刻跳到该主题的物理预设值，
      // 否则用户会看到「点了 Cyberpunk，滑块还停在上一套数值」的脱节读数。
      try { syncTuneSliders(); } catch (_) {}

      // 2) 写回 localStorage（舱内自己的记录，网关不可达时也能保持高亮一致）。
      saveSkinConfig(presetId);

      // 3) 同步落盘到 2ag 配置：这一步不能省。宿主重启（含本单元新加的账号切换重启、
      //    以及「无损重载」按钮）后 INITIAL_CONFIG 来自磁盘配置，若只改了 localStorage，
      //    用户会看到「换肤秒生效 → 重启后自己变回去了」，正是本项目要消灭的假功能。
      //    网络失败不再谎报成功：本机效果已生效，但如实告诉用户配置没落盘。
      fetchFirstOk('/api/v1/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'SET_PRESET', payload: { preset: presetId } })
      }).then(() => {
        showToast(`[2Ag] 已切换主题：${preset.title}（已写入配置）`);
      }).catch(() => {
        showToast(`[2Ag] 已切换主题：${preset.title}（仅当前会话，配置未落盘）`);
      });
    }

    if (themeRow) {
      themeRow.addEventListener('click', (e) => {
        const chip = e.target && e.target.closest ? e.target.closest('.theme-chip') : null;
        if (!chip) return;
        // 第 6 个芯片是「调参」，它没有 data-preset —— 不能走 applyThemePreset
        // （那会被白名单拒绝并静默什么都不做，用户点了没反应 = 假功能）。
        if (!chip.dataset.preset) {
          setTuneDrawer();
          return;
        }
        applyThemePreset(chip.dataset.preset);
      });
    }

    // ------------------------------------------------------------------
    // LIVE FINE-TUNING：端内微调抽屉（P2 单元三）
    // ------------------------------------------------------------------
    // 手感要求：拖动时毫秒级改宿主内存（不落盘、不等网络），松手 300ms 后才持久化。
    // 因此 input 事件与 change 事件是两条不同的路径，绝不能合并。
    if (typeof syncTuneSliders === 'function') {
      try { syncTuneSliders(); } catch (_) {}
    }

    // 抽屉里的滑块是 DOM 节点，重注入（新实例）时旧 UI 会被复用、闭包指针失效，
    // 因此所有取值都走 tuneDrawerParts() 现场寻址，与 2.3/2.4 的口径一致。
    const tuneChangeTimers = {};

    function tunePushToState() {
      // 三个滑块 → state 三数值。UI 是唯一真值来源（用户刚拖过它），
      // 因此这里不做「读取 state 再比对」的绕路。
      const p = tuneDrawerParts();
      if (!p) return;
      const blur = p.blur ? Number(p.blur.value) : NaN;
      const dark = p.dark ? Number(p.dark.value) : NaN;
      const modal = p.modal ? Number(p.modal.value) : NaN;
      if (isFinite(blur)) state.blur = Math.round(tuneNum(blur, 0, 40, 28));
      if (isFinite(dark)) state.opacity = tuneNum(dark / 100, 0.1, 0.9, 0.6);
      if (isFinite(modal)) state.modalOpacity = tuneNum(modal / 100, 0.7, 1, 0.9);
      // 数值一旦偏离预设，主题就不再是「某个预设」：用哨兵值顶掉 state.preset，
      // 否则 syncThemeChipsFromDom 的「主题名回退」分支会把某个芯片错误点亮。
      const matched = THEME_PRESETS.filter(
        (t) => t.blur === state.blur && Math.abs(t.opacity - state.opacity) < 0.001
      )[0];
      state.preset = matched ? matched.id : TUNE_SENTINEL_PRESET;
      if (p.blurVal && isFinite(blur)) p.blurVal.textContent = Math.round(tuneNum(blur, 0, 40, 28)) + 'px';
      if (p.darkVal && isFinite(dark)) p.darkVal.textContent = Math.round(tuneNum(dark, 10, 90, 60)) + '%';
      if (p.modalVal && isFinite(modal)) p.modalVal.textContent = Math.round(tuneNum(modal, 70, 100, 90)) + '%';
    }

    function scheduleTunePersist() {
      // change（松手）后 300ms 才落盘：连续微调只产生一次 POST，避免拖动过程中
      // 每 1px 一次请求把网关打爆（磁盘写 + 事件广播）。
      if (tuneChangeTimers.persist) window.clearTimeout(tuneChangeTimers.persist);
      tuneChangeTimers.persist = window.setTimeout(() => {
        tuneChangeTimers.persist = null;
        fetchFirstOk('/api/v1/action', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            type: 'SET_THEME',
            payload: {
              blur: state.blur,
              opacity: state.opacity,
              modal_opacity: state.modalOpacity
            }
          })
        }).then(() => {
          showToast('[2Ag] 微调参数已写入配置');
        }).catch(() => {
          showToast('[2Ag] 微调已生效（仅当前会话，配置未落盘）');
        });
      }, 300);
    }

    function bindTuneRange(id) {
      const p = tuneDrawerParts();
      const el = p && p[id];
      if (!el || el.dataset.bound === 'true') return;
      el.dataset.bound = 'true';
      el.addEventListener('input', () => {
        // 毫秒级：只改内存 + DOM，不同步落盘
        try { tunePushToState(); } catch (_) {}
        try { ensureDreamSkin(); } catch (_) {}
        try { ensureNativeStyles(); } catch (_) {}
        try { syncThemeChips(); } catch (_) {}
      });
      el.addEventListener('change', () => {
        try { tunePushToState(); } catch (_) {}
        try { saveSkinConfig(state.preset); } catch (_) {}
        scheduleTunePersist();
      });
    }
    ['blur', 'dark', 'modal'].forEach(bindTuneRange);

    // 外部点击 / ESC 关闭抽屉：与面板关闭同源（hubPanelDismisser 只负责面板本身，
    // 抽屉是面板内的独立浮层，必须自己收束，否则会出现「面板关了抽屉还开着」的幽灵态）。
    const tuneDrawerEl = shadow.getElementById('live-tune-drawer');
    if (tuneDrawerEl) {
      window.addEventListener('pointerdown', (e) => {
        if (tuneDrawerEl.dataset.open !== 'true') return;
        const path = e.composedPath ? e.composedPath() : [];
        if (path.indexOf(tuneDrawerEl) !== -1) return;
        if (path.indexOf(shadow.getElementById('theme-chip-tune')) !== -1) return;
        setTuneDrawer(false);
      }, true);
    }

    // ------------------------------------------------------------------
    // ACCOUNT ROTATION：舱内账号轮转（无需切回 2Ag 主窗口）
    // ------------------------------------------------------------------
    // 端口惯例与 quota/account 拉取完全一致：2ag API 固定监听 28470，28471 为备用。
    const API_PORTS = [28470, 28471];

    let accountListLoaded = false;
    let accountSwitchPending = false;

    function apiUrlFor(path) {
      return API_PORTS.map((p) => `http://127.0.0.1:${p}${path}`);
    }

    // 依次尝试候选端口，返回第一个成功的 Response；全失败抛出最后一个错误。
    async function fetchFirstOk(path, init) {
      let lastErr = null;
      for (const url of apiUrlFor(path)) {
        try {
          const res = await fetch(url, Object.assign({ cache: 'no-store' }, init || {}));
          if (!res.ok) {
            // 把响应体带进错误里：后端的 4xx/5xx 都是带原因的（例如
            // 「该账号不在本地账号文件中: x@y.z」「未能确认系统登录凭据已切换」），
            // 只报「HTTP 500」等于把唯一的诊断信息丢掉，用户无从判断该怎么修。
            // 用 text() 而非 json()：不知道对方是 JSON 还是 http.Error 的纯文本。
            let detail = '';
            try { detail = (await res.text() || '').trim(); } catch (_) {}
            if (detail.length > 300) detail = detail.slice(0, 300) + '…';
            lastErr = new Error(detail ? `HTTP ${res.status}: ${detail}` : `HTTP ${res.status}`);
            continue;
          }
          return res;
        } catch (err) {
          lastErr = err;
        }
      }
      throw lastErr || new Error('网关不可达');
    }

    function setLoading(show) {
      if (!loadingEl) return;
      loadingEl.dataset.show = show ? 'true' : 'false';
    }

    // 遮罩文案必须点名目标账号：用户要确认「正在保存状态并切换至 X 沙箱」，
    // 而不是一句无信息量的「正在重启」。邮箱走 textContent 写入（非 innerHTML），
    // 因此不需要转义也不会被当作标记解析。
    function paintLoadingTarget(email) {
      if (!loadingEl) return;
      const textEl = loadingEl.querySelector('.ghub-loading-text');
      const subEl = loadingEl.querySelector('.ghub-loading-sub');
      const target = email || '目标账号';
      if (textEl) textEl.textContent = `正在保存状态并切换至 ${target} 沙箱...`;
      if (subEl) subEl.textContent = `宿主进程（taskkill /T 全树）将被终止，并以 ${target} 的 profile 目录重新拉起`;
    }

    // 遮罩兜底：宿主若在 T 秒内没有真的重启（页面还活着 = 重启没发生），
    // 必须把遮罩撤掉，否则用户被永久困在一块「正在重启」的黑幕后面。
    let loadingGuardTimer = null;
    function armLoadingGuard(ms) {
      if (loadingGuardTimer) clearTimeout(loadingGuardTimer);
      loadingGuardTimer = setTimeout(() => {
        loadingGuardTimer = null;
        if (accountSwitchPending) {
          accountSwitchPending = false;
          setLoading(false);
          // 超时兜底必须把按钮一并解锁：否则用户面对的是「遮罩没了但按钮全灰」的死界面。
          const btns = shadow.querySelectorAll('.acct-switch');
          for (let i = 0; i < btns.length; i++) btns[i].disabled = false;
          showToast('[2Ag] 切换超时：后端未在预期时间内重启宿主，请检查 2Ag 主进程状态');
        }
      }, ms);
    }

    // 手风琴头部（折叠态那一行）同步：绿点 / 邮箱 / 胶囊标签
    function paintAccountHead(accounts, activeEmail, identityVerified) {
      const headMail = shadow.getElementById('acct-head-mail');
      const headDot = shadow.getElementById('acct-dot');
      const headPill = shadow.getElementById('acct-head-pill');
      if (!headMail || !headDot || !headPill) return;

      const list = Array.isArray(accounts) ? accounts : [];
      // 活跃账号：优先用后端 host/status 的 active_account.email（反映运行中的宿主），
      // 其次退回账号 JSON 里的 is_active 标记，最后才退到主账号。
      let current = null;
      if (activeEmail) current = list.filter((a) => a && a.email === activeEmail)[0] || null;
      if (!current) current = list.filter((a) => a && a.is_active)[0] || null;
      if (!current) current = list.filter((a) => a && a.is_primary)[0] || null;

      if (current) {
        const mail = current.email || '(未知账号)';
        // 默认只显示脱敏形态；完整值留在 dataset.raw 供眼睛按钮还原。
        // 原先是 headMail.title = mail 让悬停可见全文，现在悬停会直接漏出邮箱，
        // 与「默认打码」相冲突，所以 title 交给眼睛按钮承担。
        //
        // 展开态取自 revealedEmails（同一账号在列表行里展开过，头部也要展开），
        // 不写死 false —— 否则宿主重建触发 paintAccountHead 时会把用户的选择抹掉。
        const headShown = isEmailRevealed(mail);
        headMail.dataset.raw = mail;
        headMail.dataset.full = escapeHtml(mail);
        headMail.dataset.shown = headShown ? 'true' : 'false';
        headMail.textContent = headShown ? mail : maskEmail(mail);
        headMail.removeAttribute('title');
        const headEyeHost = shadow.getElementById('acct-head-eye');
        if (headEyeHost) headEyeHost.innerHTML = eyeBtnHtml(headShown, escapeHtml(mail));
        // 身份未确证时不得声称「主账号/活跃」—— 那是一个未被验证的断言。
        // 诚实标注为「待确认」，用户一眼就知道这个邮箱不是从宿主凭据读出来的。
        if (!identityVerified) {
          headPill.textContent = '待确认';
          headDot.dataset.off = 'true';
        } else {
          headPill.textContent = current.is_primary ? '主账号' : '活跃';
          headDot.dataset.off = 'false';
        }
      } else {
        headMail.textContent = list.length ? '未指定活跃账号' : '未检测到本机账号';
        headMail.removeAttribute('title');
        delete headMail.dataset.raw;
        delete headMail.dataset.full;
        headMail.dataset.shown = 'false';
        const headEyeHost2 = shadow.getElementById('acct-head-eye');
        if (headEyeHost2) headEyeHost2.innerHTML = '';
        headPill.textContent = '无主控';
        headDot.dataset.off = 'true';
      }
    }

    // ------------------------------------------------------------------
    // CREDENTIAL VAULT：当前登录凭据快照（P2 单元四）
    // ------------------------------------------------------------------
    // 后端把 Windows 凭据管理器里 gemini:antigravity 的真实 blob（本机 1630 字节）
    // 读出来、从 id_token 的 JWT payload 解出邮箱，落到 ~/.2ag/vault/<email_hash>.bin。
    // 前端只负责触发与如实回报 —— 成功文案里的邮箱来自后端返回值，不是前端猜测。
    function vaultRowHtml() {
      return '<div class="acct-vault" data-busy="false" title="读取 Windows 凭据管理器中的 gemini:antigravity 并归档到 ~/.2ag/vault/">'
        + '<span class="acct-vault-plus">+</span>'
        + '<span class="acct-vault-text">存档当前登录凭据</span>'
        + '</div>';
    }

    async function snapshotCredential(row) {
      if (row) row.dataset.busy = 'true';
      try {
        const res = await fetchFirstOk('/api/v1/accounts/snapshot', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({})
        });
        const data = await res.json().catch(() => ({}));
        const email = data && typeof data.email === 'string' ? data.email : '';
        const bytes = data && typeof data.bytes === 'number' ? data.bytes : 0;
        if (email) {
          showToast(`[2Ag] 已成功归档 ${email} 凭据快照（${bytes} 字节）`);
        } else {
          showToast(`[2Ag] 已归档凭据快照（${bytes} 字节）`);
        }
      } catch (err) {
        // 诚实失败：凭据不存在 / 读不出邮箱 / 网关不可达，各有各的话说，
        // 绝不显示「已归档」这种没发生过的成功。
        const detail = err && err.message ? err.message : 'unknown';
        showToast(`[2Ag] 归档失败：${detail}`);
      } finally {
        if (row) row.dataset.busy = 'false';
      }
    }

    function renderAccountList(accounts, activeEmail, identityVerified) {
      if (!acctListEl) return;
      paintAccountHead(accounts, activeEmail, identityVerified);
      if (!accounts || accounts.length === 0) {
        acctListEl.innerHTML = '<div class="acct-empty">未检测到本机账号</div>' + vaultRowHtml();
        return;
      }
      // 折叠态那一行已经把「当前主控」显示完了（邮箱 + 主账号胶囊）。
      // 展开区若再画一遍同一个账号，既占垂直高度（面板在矮视口里本就要滚动），
      // 又只能给出一个置灰不可点的「已激活」按钮 —— 纯冗余。
      // 因此这里只渲染「可切换的备选」，主控自身从候选中剔除。
      //
      // 判定口径必须与 paintAccountHead 一致（否则会出现「头部显示 A、列表里 A 又出现」
      // 的不一致）：优先用后端 host/status 的 active_account.email，
      // 拿不到再退回账号 JSON 的 is_active 标记。
      const currentEmail = (function () {
        if (activeEmail) return activeEmail;
        const hit = accounts.filter((a) => a && a.is_active)[0];
        return hit && hit.email ? hit.email : '';
      })();

      const candidates = accounts.filter((acc) => {
        if (!acc || !acc.email) return false;
        return !(currentEmail && acc.email === currentEmail);
      });

      if (candidates.length === 0) {
        acctListEl.innerHTML = '<div class="acct-empty">暂无其他备选账号</div>' + vaultRowHtml();
        return;
      }

      acctListEl.innerHTML = candidates.map((acc) => {
        // 邮箱与名称都来自磁盘账号 JSON（用户可自行编辑）⇒ 必须转义后再拼进 innerHTML。
        const emailRaw = acc.email || '(未知账号)';
        const email = escapeHtml(emailRaw);
        const name = escapeHtml(acc.name || '');
        const tag = acc.is_primary
          ? '<span class="acct-tag" style="color:var(--2ag-blue);">主账号</span>'
          : '';
        const subParts = [];
        if (name) subParts.push(name);
        if (acc.role) subParts.push(escapeHtml(acc.role));
        const btn = `<button class="acct-switch" type="button" data-email="${email}">切换</button>`;
        // 第二段：该账号自己的双池 × 双窗口共四个配额微读。
        // 键与面板顶部的四个大圆环完全同源，只是尺寸缩小到 13px；池名跟着
        // 每个数字一起出场，用户不必靠位置去猜某个百分比属于谁。
        // acc 本身就是 /api/v1/accounts 的一项，配额字段都在里面 —— 不存在跨账号取数。
        const quotaLine = `<div class="acct-quota">
              ${accountQuotaPoolHtml(acc.gemini_pool, 'Gemini')}
              ${accountQuotaPoolHtml(acc.claude_pool, 'Claude')}
            </div>`;
        return `<div class="acct-row" data-active="false">
            <div class="acct-line">
              <div class="acct-meta">
                <div class="acct-mail-row2">${emailCellHtml(emailRaw)}</div>
                <div class="acct-sub">${subParts.join(' · ')}</div>
              </div>
              ${tag}
              ${btn}
            </div>
            ${quotaLine}
          </div>`;
      }).join('') + vaultRowHtml();
    }

    async function refreshAccountList() {
      if (!acctListEl) return;
      try {
        const res = await fetchFirstOk('/api/v1/accounts');
        const data = await res.json();
        const accounts = Array.isArray(data) ? data : (data.accounts || []);
        // 活跃账号以 host/status 的 active_account 为准（accounts 里的 is_active
        // 是磁盘扫描结果，可能与运行中的宿主不一致）。
        //
        // 关键修正：active_account 在 /api/v1/host/status 里是 toAccountQuotaDTO
        // 产出的**对象**（{email, gemini_pool, claude_pool, …}），不是字符串。
        // 早期实现写 `activeEmail = stJson.active_account || ''`，于是这里拿到一个
        // 对象，下面的 `acc.email === activeEmail` 永远为假 —— 「当前主控」高亮
        // 从未生效过。这里只接受 .email 字符串，拿不到就留空走 is_active 回退。
        let activeEmail = '';
        let identityVerified = false;
        try {
          const st = await fetchFirstOk('/api/v1/host/status');
          const stJson = await st.json();
          const acc = stJson ? stJson.active_account : null;
          if (acc && typeof acc === 'object' && typeof acc.email === 'string') activeEmail = acc.email;
          else if (typeof acc === 'string') activeEmail = acc;
          // 后端只在真的从系统凭据（Windows 凭据管理器 gemini:antigravity）读出、
          // 且宿主存活时才置 true。为假意味着「宿主当前登录着谁」这件事没有确证，
          // 界面必须照实说，否则又把 active_account.txt 里的旧值伪装成事实。
          identityVerified = !!(stJson && stJson.identity_verified) && !!(stJson && stJson.is_live);
        } catch (_) {}
        renderAccountList(accounts, activeEmail, identityVerified);
        accountListLoaded = true;
      } catch (err) {
        // 网关不可达：如实告知，绝不留一行永远停在「正在读取本机账号…」的假加载态。
        acctListEl.innerHTML = `<div class="acct-empty">网关未连接（${escapeHtml(err && err.message ? err.message : 'unknown')}）</div>`;
        paintAccountHead(null, '');
      }
    }

    async function switchAccount(email, btn) {
      if (accountSwitchPending) return;
      accountSwitchPending = true;
      // 锁定全部备选行的「切换」按钮（不只是被点的那个），
      // 否则用户可以在请求飞行途中从另一行再点一次，制造两次并发重启。
      const allSwitchBtns = shadow.querySelectorAll('.acct-switch');
      for (let i = 0; i < allSwitchBtns.length; i++) allSwitchBtns[i].disabled = true;

      paintLoadingTarget(email);
      setLoading(true);
      showToast(`[2Ag] 正在切换至 ${email} 并重启宿主沙箱...`);
      // 宿主被 taskkill 后本页面会随之销毁 —— 遮罩正是给这段「黑屏空窗」用的。
      // 20s 后若页面仍存活，说明重启没有发生，必须把遮罩撤掉。
      armLoadingGuard(20000);

      try {
        const res = await fetchFirstOk('/api/v1/host/switch-and-restart', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: email })
        });
        const data = await res.json().catch(() => ({}));
        // 走到这里说明宿主还活着，即重启窗口尚未关闭：保持遮罩，只报状态。
        showToast(`[2Ag] ${data && data.message ? data.message : '切换指令已被接受，宿主正在重启...'}`);
      } catch (err) {
        // 请求失败有两种成因，必须区分对待：
        //   (a) 宿主已被杀掉 —— 连接中断（TypeError: Failed to fetch），此时重启其实成功了，遮罩应保持；
        //   (b) 后端拒绝（HTTP 4xx/5xx）—— 重启根本没发生，必须立刻撤遮罩并把原因原样报给用户。
        // 判据是 err.message 是否以 "HTTP " 开头：fetchFirstOk 只在这个形态里塞了状态码与响应体。
        // 此前不区分，把后端的明确拒绝也显示成「切换请求中断」，用户会以为正在重启而干等。
        const detail = err && err.message ? err.message : 'unknown';
        const rejected = /^HTTP \d/.test(detail);
        showToast(rejected
          ? `[2Ag] 切换被拒绝：${detail}`
          : `[2Ag] 切换请求中断：${detail}（若宿主已重启则本页面即将重载）`);
        if (rejected) {
          // 后端拒绝 ⇒ 宿主仍在运行 ⇒ 页面不会重载 ⇒ 必须由这里收尾，否则遮罩永远盖着。
          setLoading(false);
          accountSwitchPending = false;
          if (loadingGuardTimer) { clearTimeout(loadingGuardTimer); loadingGuardTimer = null; }
          for (let i = 0; i < allSwitchBtns.length; i++) allSwitchBtns[i].disabled = false;
        }
      }
    }

    // 手风琴折叠/展开：点击标题整行切换。展开是纯视觉状态，不触发任何网络请求 ——
    // 备选列表在 refreshAccountList 时就已经渲染好了，这里只是显隐切换。
    const acctAccEl = shadow.getElementById('acct-acc');
    if (acctAccEl) {
      acctAccEl.addEventListener('click', (e) => {
        // 眼睛在手风琴头部之内，若不先拦下，点它就会连带把折叠区收起来。
        const headEye = e.target && e.target.closest ? e.target.closest('.acct-eye') : null;
        if (headEye) {
          e.stopPropagation();
          toggleHeadEmailReveal(headEye);
          return;
        }
        const head = e.target && e.target.closest ? e.target.closest('.acct-head') : null;
        if (!head) return;
        const open = acctAccEl.dataset.open === 'true';
        acctAccEl.dataset.open = open ? 'false' : 'true';
        rememberUiOpenState('acct', !open);
        // 展开时若列表尚未成功加载过（例如首次拉取就失败），补一次拉取，
        // 避免用户展开后看到一行过期的「网关未连接」。
        if (!open && !accountListLoaded) {
          try { refreshAccountList(); } catch (_) {}
        }
      });
    }

    if (acctListEl) {
      acctListEl.addEventListener('click', (e) => {
        // 眼睛按钮只切换邮箱可见性，绝不能冒泡到下面的「切换账号」分支。
        const eye = e.target && e.target.closest ? e.target.closest('.acct-eye') : null;
        if (eye) {
          e.stopPropagation();
          toggleEmailReveal(eye);
          return;
        }
        const vault = e.target && e.target.closest ? e.target.closest('.acct-vault') : null;
        if (vault) {
          if (vault.dataset.busy === 'true') return;
          snapshotCredential(vault);
          return;
        }
        const btn = e.target && e.target.closest ? e.target.closest('.acct-switch') : null;
        if (!btn || btn.disabled) return;
        const email = btn.dataset.email;
        if (!email) return;
        switchAccount(email, btn);
      });
    }

    // 窗口尺寸自适应：回调体走窗口级命名槽（重挂载时只更新指向，不再叠加第二份监听）
    hubLayoutRefresher = () => updateGHubLayout(false);
    // 主题卡高亮同步器：同样走窗口级命名槽，供 __2ag_onStateUpdate 转发调用
    hubThemeSyncer = () => syncThemeChipsFromDom();

    // 初始化布局
    updateGHubLayout(false);

    // 恢复「上一实例留下的展开态」（见文件头部 UI_OPEN_KEY 的说明）。
    // 必须排在 updateGHubLayout(false) 之后：面板的四象限弹射要按浮标的**真实**
    // getBoundingClientRect 决定往左还是往右展开，而浮标坐标正是上一步才写进样式的。
    // 顺序也有讲究：先开面板、再开抽屉/手风琴 —— setTuneDrawer(true) 与手风琴展开
    // 都会主动收起对方，反着恢复会让两者互相抵消，最终只剩最后写的那一个。
    // 抽屉与手风琴本身互斥（441px 窄视口下同时展开会撑破控制舱，见 setTuneDrawer 注释），
    // 两者都为真时取抽屉 —— 它是用户刚拖过滑块的那一个。
    try {
      const openState = readUiOpenState();
      if (openState.panel) {
        toggleCockpit(true);
      }
      if (openState.tune) {
        setTuneDrawer(true);
      } else if (openState.acct) {
        // 走一次真实的点击，而不是直接改 data-open：手风琴的展开还带着「首次展开要
        // 拉一次账号列表」的副作用（点击委托里有 accountListLoaded 判定），直接写
        // dataset 会得到一个展开的空壳。
        const head = shadow.querySelector('#acct-acc .acct-head');
        if (head && head.click) head.click();
      }
    } catch (_) {}

    // ------------------------------------------------------------------
    // 真实剩余配额（Live Remaining Quota）
    // 数据源：/api/v1/host/status 的 active_account.gemini_pool / claude_pool，
    // 后端由 queryCacheFor 解析授权缓存（remainingFraction / resetTime）得到，
    // 每个池带 available 标志 —— 只有真的读到 bucket 才为 true。
    //
    // 旧实现（updateQuotaDisplay + pollTokenQuota）已被整体移除：它把
    // gemini_5h_percent 当成「会话 Token 消耗水位」渲染，语义完全错位，
    // 而且模板里写死 88.4% / 红色满条，那是一个从未被测量过的数字。
    // 现在只做一件事：把后端真实读数如实画出来；读不到就如实显示「未载入」。
    // 配额圆环的周长（r = 18，见 markup 里的 <circle r="18">）。CSS 的
    // stroke-dasharray 与这里必须一致，否则弧长会与百分比脱节。
    const QUOTA_RING_C = 2 * Math.PI * 18;

    // 剩余额度 ⇒ 颜色分档（用户规格）：
    //   ≤ 20%        红   额度告急
    //   20% – 50%    黄   偏紧
    //   50% – 80%    蓝   充裕
    //   80% – 100%   绿   健康
    // 注意与旧实现的方向相反：这里是「剩余」语义，越低越危险；旧的水平条
    // 画的是「已消耗」比例，越低越安全，直接用会把红绿画反。
    // 剩余额度的取色。四档标尺与 Manager 侧 getQuotaColor 逐档对齐，
    // 并且两边用的都是同一组**深色界面调制版**语义色，不是纯品牌 RGB：
    // 纯 #EA4335 / #FBBC05 在暗面上会烫眼，且与面板自己的蓝调不在一个色温里。
    // 同一份读数在控制台与浮层上必须是同一种颜色，否则用户会以为在看两个数字。
    function quotaColorForRemaining(pct) {
      if (pct <= 20) return '#f28b82';   // 红：额度告急
      if (pct <= 50) return '#fdd663';   // 黄：偏紧
      if (pct <= 80) return '#8ab4f8';   // 蓝：充裕
      return '#81c995';                  // 绿：健康
    }

    // 绘制单个配额桶（圆环 + 「窗口名 / 倒计时」两行文字）。
    //
    // 口径（P2.2 单元一）：**圆环、百分比、倒计时必须取自同一个池的同一个桶**，
    // 严禁跨池跨桶拼接。历史上这一块同时犯了两种交叉：池 × 窗口被配错
    // （Gemini 只画 5h、Claude 只画周限额），于是「Gemini 周」「Claude 5h」
    // 两个桶在面板上从未露面 —— 而剩余最少的往往正是它们；用户拿 Claude 的周
    // 百分比去比 Manager 里 Gemini 的周百分比，必然对不上。
    // 现在四个桶全部出场，每个桶只读自己那四个字段。
    //
    // 返回值表示「这个桶是否真的画出了数字」，供调用方汇总（不给外部猜）。
    function paintQuotaBucket(el, pool, percentKey, resetKey, knownKey, bucketLabel) {
      if (!el) return false;
      const arcEl = el.querySelector('.quota-ring-arc');
      const valueEl = el.querySelector('.quota-bucket-value');
      const resetEl = el.querySelector('.quota-bucket-reset');
      if (!arcEl || !valueEl || !resetEl) return false;

      // 「整池可用」不等于「这个桶在缓存里」：后端为每个桶单独标记 known，
      // 因为 remainingFraction 为 0（额度真的耗尽）是合法真实读数，
      // 与「缓存里根本没有这个桶」必须区分 —— 前者要画空环，后者要诚实认输。
      //
      // 版本偏斜容错：热重载会把新前端瞬间贴到「尚未重启的旧后端」上，
      // 而旧后端根本不发 known 标志。此时若把「字段缺失」当成「桶不存在」，
      // 就会在 API 明明返回了真实百分比的情况下宣称「未载入配额」——
      // 那是拿一个新造的假读数（"没数据"）去覆盖一个真读数。
      // 判据：只有后端真的会发这个字段（key in pool）时才采信它的布尔值；
      // 字段整个不存在 ⇒ 退回「有百分比即为有数据」的旧口径。
      const available = !!(pool && pool.available);
      const hasKnownFlag = !!pool && knownKey in pool;
      const bucketKnown = hasKnownFlag
        ? !!pool[knownKey]
        : !!pool && pool[percentKey] !== undefined && pool[percentKey] !== null;
      const pct = Math.max(0, Math.min(100, Number(pool ? pool[percentKey] : NaN)));
      const known = available && bucketKnown && isFinite(pct);

      // 读不到就画空环 + 「--」：不 fallback 填充任何数值，也不拿同一个池里
      // 另一个窗口的读数顶上（那正是「5h 的百分比配周限额的倒计时」的成因）。
      if (!known) {
        arcEl.style.strokeDashoffset = String(QUOTA_RING_C);
        arcEl.style.stroke = '#9aa0a6';
        valueEl.textContent = '--';
        valueEl.style.color = '#9aa0a6';
        resetEl.textContent = bucketKnown
          ? buildBucketResetText(pool, resetKey, bucketKnown, bucketLabel)
          : `${bucketLabel} · 未载入`;
        return false;
      }

      const color = quotaColorForRemaining(pct);
      /* 剩余额度映射为可见弧长：dashoffset 越小，弧越接近整圈。
         pct=100 ⇒ offset 0（整圈）；pct=0 ⇒ offset = 周长（一圈不可见）。
         中间值线性插值，于是额度下降时弧从上端开始逐段消失。 */
      arcEl.style.strokeDashoffset = String(QUOTA_RING_C * (1 - pct / 100));
      arcEl.style.stroke = color;

      valueEl.textContent = `${pct}%`;
      valueEl.style.color = color;
      resetEl.textContent = buildBucketResetText(pool, resetKey, bucketKnown, bucketLabel);
      return true;
    }

    // 采样时刻超过这个间隔就认为「这份读数已经不可当作实时使用」。
    // 取 5 小时 = 5h 滑窗自身的窗口长度：一旦缓存比一个完整窗口还旧，
    // 连「5h 滑窗还剩多少」这个问题的答案本身都已经失效，与「可能有点不准」
    // 是两回事 —— 后者是猜测，前者是确定的失效判据。周限额桶不受此限，
    // 但整块读数同源，所以统一标注。
    const QUOTA_STALE_MS = 5 * 60 * 60 * 1000;

    // 把后端的采样时刻（Unix 毫秒）折算成「多久以前」。
    //
    // 相对时间在前端算、不在后端算：浏览器此刻的时钟才是「现在」。后端一旦
    // 生成「10 分钟前」这种字面量，那句话从生成的一刻起就已经过期了，而且
    // 会被缓存/重放成更离谱的错值。后端只给时间戳，折算是显示层的事。
    function formatSampledAge(ms) {
      const diff = Date.now() - Number(ms);
      if (!isFinite(diff)) return '';
      // 负值 = 本机时钟落后于采样时钟（缓存来自别的时钟源）⇒ 不编造「0 分钟前」。
      if (diff < 0) return '时钟不同步';
      const min = Math.floor(diff / 60000);
      if (min < 1) return '刚刚';
      if (min < 60) return `${min} 分钟前`;
      const hr = Math.floor(min / 60);
      if (hr < 24) return `${hr} 小时前`;
      return `${Math.floor(hr / 24)} 天前`;
    }

    // 采样时刻的绝对时间（本地时区），让「多久以前」有一个可核对的锚点。
    function formatSampledAt(ms) {
      const d = new Date(Number(ms));
      if (isNaN(d.getTime())) return '--';
      const p2 = (n) => (n < 10 ? '0' + n : '' + n);
      return `${p2(d.getMonth() + 1)}-${p2(d.getDate())} ${p2(d.getHours())}:${p2(d.getMinutes())}`;
    }

    // 副文本只由「与主条同一个桶」的 resetTime 生成。
    //
    // 后端 formatResetTime 的返回只有四种形态：
    //   「已恢复满额」  剩余 >= 99.9%，此时本来就没有倒计时可等
    //   「1d 23h」     剩余窗口 > 24h
    //   「5h 10m」     剩余窗口 <= 24h
    //   「待刷新」     resetTime 解析失败或已过期
    // 只有时长形态能拼「后刷新」—— 对「已恢复满额」再拼一次会得到
    // 「已恢复满额 后刷新」这种读不通的串（历史真机上就出现了这个 bug）。
    function buildBucketResetText(pool, resetKey, bucketKnown, bucketLabel) {
      if (!bucketKnown) return `${bucketLabel}：--`;
      const raw = pool && typeof pool[resetKey] === 'string' ? pool[resetKey] : '';
      // 桶在缓存里但没有倒计时字段 ⇒ 额度是满的，如实说满额，不编造时间。
      if (!raw) return `${bucketLabel} · 已恢复满额`;
      if (raw === '已恢复满额' || raw === '满额') return `${bucketLabel} · ${raw}`;
      if (/^\d+[dh]/.test(raw)) return `${bucketLabel} · ${raw} 后刷新`;
      return `${bucketLabel} · ${raw}`;
    }

    // 账号列表里每行配额的迷你圆环周长（r = 5.5，见 CSS .acct-mini-arc 的
    // stroke-dasharray = 34.5575，两处必须一致，否则弧长与百分比脱节）。
    const ACCT_RING_C = 2 * Math.PI * 5.5;

    // 账号列表里单个配额微读 = 迷你圆环 + 「5h 99%」式文字。
    //
    // 取数与面板顶部的 paintQuotaBucket 逐字同源（available → known → 数值三级判据），
    // 于是不会出现「顶部大圆环说未载入、列表小圆环却给了一个数字」这种自相矛盾。
    // 读不到就画空环 + 「--」：本函数只认传进来的那个 pool，没有任何途径
    // 让它去读别的账号或别的桶 —— 这正是「每行各自显示自己额度」的硬保证。
    function accountQuotaItemHtml(pool, percentKey, knownKey, label) {
      const available = !!(pool && pool.available);
      const hasKnownFlag = !!pool && knownKey in pool;
      const bucketKnown = hasKnownFlag
        ? !!pool[knownKey]
        : !!pool && pool[percentKey] !== undefined && pool[percentKey] !== null;
      const pct = Math.max(0, Math.min(100, Number(pool ? pool[percentKey] : NaN)));
      const known = available && bucketKnown && isFinite(pct);
      const color = known ? quotaColorForRemaining(pct) : '#9aa0a6';
      const offset = known ? ACCT_RING_C * (1 - pct / 100) : ACCT_RING_C;
      const text = known ? `${pct}%` : '--';
      return `<span class="acct-quota-item">
          <svg class="acct-mini-ring" viewBox="0 0 13 13" aria-hidden="true">
            <circle class="acct-mini-track" cx="6.5" cy="6.5" r="5.5"></circle>
            <circle class="acct-mini-arc" cx="6.5" cy="6.5" r="5.5" style="stroke-dashoffset:${offset.toFixed(4)}; stroke:${color};"></circle>
          </svg>
          <span class="acct-quota-label">${escapeHtml(label)}</span>
          <span class="acct-quota-val" style="color:${color};">${text}</span>
        </span>`;
    }

    // 账号列表里一个池的完整微读：池名 + 该池的两个窗口（5h / 周）。
    //
    // 四个桶全部出场是这一轮的核心修正。历史实现每行只显示两个数
    // （gemini 的 5h + claude 的周），标签只写「5h」「周」不提池名 ——
    // 用户拿列表里的「周 92%」（其实是 Claude 的周限额）去比 Manager 里
    // Gemini 的周限额，必然对不上；而 Gemini 的周限额（可能只剩 23%）
    // 在整个面板上根本不存在。一个看不见的读数会让人以为「没有限制」。
    function accountQuotaPoolHtml(pool, poolTag) {
      return `<span class="acct-quota-pool">
          <span class="acct-quota-pool-tag">${escapeHtml(poolTag)}</span>
          ${accountQuotaItemHtml(pool, 'five_hour_percent', 'five_hour_known', '5h')}
          ${accountQuotaItemHtml(pool, 'weekly_percent', 'weekly_known', '周')}
        </span>`;
    }

    // 把 active_account 的两个池 × 两个窗口共四个桶全部画出来，并渲染出处行。
    //
    // 桶绑定（与后端 queryCacheFor 的组判定逐字对应）：
    //   gemini_pool.5h / gemini_pool.weekly   ← 组「Gemini Models」的两个 bucket
    //   claude_pool.5h / claude_pool.weekly   ← 组「Claude and GPT models」的两个 bucket
    // 四个桶各自只读自己那三个字段，不存在任何跨池跨桶取值 —— 这是「面板读数
    // 与 Manager 读数必然一致」的结构性保证（两端都来自同一份 DTO）。
    function updateQuotaPools(activeAccount) {
      const acc = activeAccount && typeof activeAccount === 'object' ? activeAccount : null;
      const gp = acc ? acc.gemini_pool : null;
      const cp = acc ? acc.claude_pool : null;

      const geminiEl = shadow.getElementById('quota-pool-gemini');
      const claudeEl = shadow.getElementById('quota-pool-claude');

      const g5 = paintQuotaBucket(
        shadow.getElementById('quota-bucket-gemini-5h'), gp,
        'five_hour_percent', 'five_hour_reset', 'five_hour_known', '5h 滑窗');
      const gwk = paintQuotaBucket(
        shadow.getElementById('quota-bucket-gemini-wk'), gp,
        'weekly_percent', 'weekly_reset', 'weekly_known', '周限额');
      const c5 = paintQuotaBucket(
        shadow.getElementById('quota-bucket-claude-5h'), cp,
        'five_hour_percent', 'five_hour_reset', 'five_hour_known', '5h 滑窗');
      const cwk = paintQuotaBucket(
        shadow.getElementById('quota-bucket-claude-wk'), cp,
        'weekly_percent', 'weekly_reset', 'weekly_known', '周限额');

      /* 池级置灰只在「这个池一个桶都没画出来」时发生：整池缺失要一眼看出，
         而单个桶缺失已经由那个桶自己的空环 + 「--」表达，不该连带把同池
         另一个还好的桶也调暗 —— 那会把真读数也一并伪装成没数据。 */
      if (geminiEl) geminiEl.dataset.available = (g5 || gwk) ? 'true' : 'false';
      if (claudeEl) claudeEl.dataset.available = (c5 || cwk) ? 'true' : 'false';

      paintQuotaSource(g5 || gwk ? gp : (c5 || cwk ? cp : null));
    }

    // 出处行：这份读数是谁给的、什么时候采的、是不是已经旧到不能当实时看。
    //
    // 为什么必须有这一行：面板上原本只有「99%」这种赤裸的数字，而配额缓存是
    // 第三方工具写盘的快照，本工程只读、从不主动向官方端点握手。9 小时前的
    // 快照和 3 秒前的快照在界面上长得一模一样 —— 用户没有任何途径知道自己
    // 看的是哪一个。这不是「增加一点信息量」，而是把「这个数字的可信度」
    // 从不可见变成可见。
    function paintQuotaSource(pool) {
      const el = shadow.getElementById('quota-source');
      if (!el) return;
      const updatedAt = pool && isFinite(Number(pool.updated_at)) ? Number(pool.updated_at) : 0;
      const source = pool && typeof pool.source === 'string' ? pool.source.trim() : '';

      // 时间戳为 0 = 后端没拿到采样时刻。此时只能如实说「不详」，
      // 绝不用「刚刚」顶上 —— 那是把「未知」渲染成「最新」。
      if (!updatedAt) {
        el.dataset.stale = 'false';
        el.textContent = source
          ? `配额出处：${source} · 采样时刻不详（该缓存未记录时间）`
          : '配额出处：未读取到授权缓存';
        return;
      }

      const stale = Date.now() - updatedAt > QUOTA_STALE_MS;
      el.dataset.stale = stale ? 'true' : 'false';
      const age = formatSampledAge(updatedAt);
      const at = formatSampledAt(updatedAt);
      // 超过 5h ⇒ 已比一个完整的 5h 滑窗还旧，用警示色 + 明确的「不可当实时」措辞。
      const head = stale
        ? `⚠ 配额数据已过期：${age}（${at} 采样），超过 5h 滑窗长度，不可当作实时读数`
        : `配额出处：${age}（${at} 采样）`;
      el.textContent = source ? `${head} · ${source}` : head;
    }

    async function refreshQuotas() {
      try {
        const res = await fetchFirstOk('/api/v1/host/status');
        const json = await res.json();
        /* active_account 是 toAccountQuotaDTO 产出的对象；早期实现把它当字符串读，
           导致账号高亮与配额展示双双失效。这里只接受对象形态，否则如实置为未载入。 */
        updateQuotaPools(json ? json.active_account : null);
      } catch (_) {
        // 网关不可达：与「未载入配额」同样处理 —— 空环 + 明确文案，不编造数值。
        updateQuotaPools(null);
      }
    }

    // 配额轮询：同样走窗口级命名槽（4s 周期只保留一份，dispose 时随 resize 一并清理）
    hubQuotaRefresher = refreshQuotas;
    ensureHubRuntimeHooks();

    refreshQuotas();

    // 关键挂载断言日志（按规范格式输出）
    console.log('[2AG_UI_MOUNTED]', { version: '2.2', root: '#' + SHADOW_HOST_ID });
  }

  // 11.1 对外应急接口：快捷键被抢占/宿主假死时的编程逃生口
  function disposeHub() {
    hubDisposed = true;
    clearHubRuntimeHandles();
    if (window[TIMER_KEY]) {
      try { window.clearInterval(window[TIMER_KEY]); } catch (_) {}
      window[TIMER_KEY] = null;
    }
    if (window[OBSERVER_KEY]) {
      try { window[OBSERVER_KEY].disconnect(); } catch (_) {}
      window[OBSERVER_KEY] = null;
    }
    // 词典层有自己的观察器：不自毁的话，旧实例会继续对新的 DOM 做词条替换。
    if (window[I18N_OBSERVER_KEY]) {
      try { window[I18N_OBSERVER_KEY].disconnect(); } catch (_) {}
      window[I18N_OBSERVER_KEY] = null;
    }
    if (hostI18nTimer) {
      try { window.clearTimeout(hostI18nTimer); } catch (_) {}
      hostI18nTimer = null;
    }
    hostI18nQueue = new Set();
    hubLayoutRefresher = null;
    hubQuotaRefresher = null;
    hubPanelDismisser = null;
    hubThemeSyncer = null;
    try {
      document.querySelectorAll('#' + SHADOW_HOST_ID).forEach((n) => n.remove());
    } catch (_) {}
    console.log('[2AG_DISPOSED]');
  }

  window.__2ag = {
    version: '2.2',
    forceSend: (text) => executeForceDispatch(text),
    dispose: disposeHub,
    // 兼容性自检：返回当前宿主 DOM 上各寻址层各自落在哪一层。
    // 存在的理由：上游改版时，补丁不会「报错」，只会静默下沉到兜底层仍然工作 ——
    // 直到某天兜底层也不灵了才爆。这个函数让「上游到底变了没有」变成一个可以随时问的问题，
    // 而不必等到功能失效才去翻 DOM。
    describeHostDom: () => describeHostDom(),
    // 上游兼容性自检：逐项真去 DOM/API 上试一次，回答「这个宿主还支撑得起哪些能力」。
    // 与 describeHostDom 的分工：那个只答「寻址落在第几层」（强制发送一条链路），
    // 这个答「八项能力各自的四态」，是 Compatibility Guardian 的数据源。
    probeCapabilities: () => probeCapabilities()
  };
  // 把本实例的自毁入口登记到窗口级握手槽，供「下一次求值」的实例调用（见文件开头的接管段）。
  // disposeHub 是函数声明（已提升），此处登记后即可被任意晚于本实例的实例安全调用。
  window[PREV_DISPOSE_KEY] = disposeHub;

  // 11.2 主窗口 → 宿主的状态推送消费者。
  // cmd/2ag/main.go:248 每次配置变更都执行
  //   if (typeof window.__2ag_onStateUpdate === 'function') window.__2ag_onStateUpdate(<config.Config JSON>);
  // 而全仓库此前没有任何地方定义它 —— 这条推送通道一直是空的（发端有、收端无）。
  // 注意：皮肤数值本身仍能生效，因为同一次变更里 Go 侧还会重新注入整个补丁
  // （injector.Inject → Runtime.evaluate 再跑一遍 IIFE），新实例带着新的 INITIAL_CONFIG
  // 覆盖旧实例。所以这不是一个「用户看得见的坏功能」，而是一条被声明却无人接的通道 ——
  // 一旦重注入失败而推送成功，新值就会静默丢失。这里把它接上，让两条路径口径一致。
  // 参数是 config.Config 的 JSON（字段：wallpaper_path / blur / opacity / modal_opacity /
  // language / gravity_boost 等），不是 HubConfig，命名风格因此是下划线式。
  //
  // 返回值：一个说明本次落实结果的对象。这不是为了好看 —— 快路径（增量推送）与慢路径
  // （整段重注入）的分工必须由**消费端的实际结果**决定，而不是由发送端的假设决定：
  // 宿主上跑的可能是旧版实例，它会对新字段「成功地什么都不做」。若不让补丁如实回报，
  // Go 侧就无从知道该不该回落到重注入，换壁纸会变成「点了没反应也不报错」。
  //   wallpaper: 'applied'   本次带图且已写入背景层
  //              'unchanged' 本次带图，但图与该层当前所写完全相同（无谓重绘已跳过）
  //              'cleared'   本次明确要求回到 Native（空串），背景层与透光级联已拆除
  //              'absent'    本次没带 wallpaper 字段（调用方只推了配置字段）
  //              'ignored'   本次带了 wallpaper，但值不是字符串（非法）
  window.__2ag_onStateUpdate = function (cfg) {
    if (hubDisposed || !cfg || typeof cfg !== 'object') return { ok: false, reason: 'disposed' };
    const num = (v) => (typeof v === 'number' && isFinite(v) && v >= 0 ? v : null);

    const nextBlur = num(cfg.blur);
    const nextOpacity = num(cfg.opacity);
    const nextModal = num(cfg.modal_opacity);
    // 只采信真实存在的数值，绝不把 undefined 写成 0 —— 那会让壁纸在每次推送后
    // 变成「零模糊 + 全透明」，看起来像皮肤被清空了。
    if (nextBlur !== null) state.blur = nextBlur;
    if (nextOpacity !== null) state.opacity = nextOpacity;
    if (nextModal !== null) state.modalOpacity = nextModal;
    if (typeof cfg.language === 'string' && cfg.language) state.language = cfg.language;
    // 注意：这里刻意不把 cfg.wallpaper_path 写进 state.wallpaper。
    // state.wallpaper 的取值链是「background-image 的 url(...)」，它只接受 data:
    // 或 http(s) 形态；把 D:/Pictures/x.jpg 这种本地路径写进去，在 https 的宿主页面上
    // 会被 Chromium 同源策略直接拒载 —— 结果是「写了但显示不出来」，
    // 比不写更糟（壁纸层会从「旧图」变成「无图」）。路径→图片的转换是 Go 侧
    // ResolveWallpaperDataURL 的职责，它随 wallpaper 字段一起送达。

    // 壁纸：只在本次真的带了 wallpaper 字段时才动它。
    // 推送侧刻意做了节流 —— 图片是上兆的 base64，滑块拖动时不必每次都重传。
    //
    // 三种取值各有含义，不能合并处理：
    //   undefined  → 'absent'  本次推送没提壁纸（滑块拖动走这条），保持现状
    //   ""         → 'cleared' 明确要求回到 Native：拆除背景层与透光级联
    //   非空字符串 → 'applied' / 'unchanged'  贴图
    // 历史实现把空串与非法值一起归进 'ignored'，于是「清除壁纸」这条指令
    // 抵达了宿主却被这里丢掉 —— 这正是那条链路最后一跳失败的地方。
    let wallpaperStatus = 'absent';
    if (cfg.wallpaper !== undefined) {
      if (typeof cfg.wallpaper === 'string') {
        const before = pushedWallpaper;
        adoptPushedWallpaper(cfg.wallpaper);
        wallpaperStatus = cfg.wallpaper === '' ? 'cleared' : (before === cfg.wallpaper ? 'unchanged' : 'applied');
      } else {
        wallpaperStatus = 'ignored';
      }
    }

    // GravityBoost 九个开关同样在这条通道上更新（主窗口拨开关 → 宿主行为跟着变）
    if (cfg.gravity_boost && typeof cfg.gravity_boost === 'object') {
      for (const key of Object.keys(boost)) {
        if (typeof cfg.gravity_boost[key] === 'boolean') boost[key] = cfg.gravity_boost[key];
      }
      if (typeof cfg.gravity_boost.centered_width === 'boolean') {
        try { ensureNativeStyles(); } catch (_) {}
      }
      try { enforceHostLocale(); } catch (_) {}
      try { installPastePlaintextFix(); } catch (_) {}
      // 词典层：开关刚被拨动时立刻生效，不必等下一次 2s 巡检。
      try { installHostI18n(); } catch (_) {}
      try { installDevtoolsPassthrough(); } catch (_) {}
    }

    try { ensureDreamSkin(); } catch (_) {}
    // 舱内主题卡高亮必须跟着主窗口的选择走，否则会出现「外观是 A、面板亮着 B」
    if (typeof hubThemeSyncer === 'function') {
      try { hubThemeSyncer(); } catch (_) {}
    }
    // 端内微调抽屉的三个滑块同理：主窗口拖完，宿主里的抽屉若还显示旧值，
    // 就成了同一份配置的两套读数。
    if (typeof syncTuneSliders === 'function') {
      try { syncTuneSliders(); } catch (_) {}
    }
    return { ok: true, wallpaper: wallpaperStatus };
  };

  // 12. 统一初始化守护巡检流水线
  function tick() {
    if (hubDisposed) return;
    // document-start 就绪门禁（第一道）。
    // Page.addScriptToEvaluateOnNewDocument 的注入时机是「文档对象已创建、但 DOM 骨架还是空的」
    // 这一刻：document 存在，而 documentElement 与 body 都还是 null。
    // 历史实现没有这道门禁 —— 它立刻 tick()，紧接着对 `documentElement || body`
    // 求值得到的 null 调用 MutationObserver.observe()，抛出
    //   TypeError: Failed to execute 'observe' on 'MutationObserver': parameter 1 is not of type 'Node'
    // 该异常直接打死整个顶层 IIFE，连文件末尾的 2s 巡检定时器都没来得及注册。
    // 于是补丁虽然被 Chromium 内核忠实地注入到了每一个新文档，却在每个新文档上都当场暴毙 ——
    // 表现就是「页面一跳转，补丁就消失」。
    if (!document.documentElement) return;
    try { ensureNativeStyles(); } catch (_) {}
    try { ensureDreamSkin(); } catch (_) {}
    try { mountShadowUI(); } catch (_) {}
    try { performSubsystemGuards(); } catch (_) {}
    // GravityBoost 真实落点：语言锁定 / 剪贴板净化 / DevTools 穿透。
    // 三者内部各自判断对应开关，且都做了幂等保护（可被 2s 巡检反复调用）。
    try { enforceHostLocale(); } catch (_) {}
    try { installPastePlaintextFix(); } catch (_) {}
    try { installDevtoolsPassthrough(); } catch (_) {}
    // 词典层：与上面三者并列的「一次性安装器」，每 2s 被巡检一次，内部幂等。
    try { installHostI18n(); } catch (_) {}
    // observer 的安装在 DOM 就绪前无法完成，纳入巡检后可在就绪后自动补上（幂等）。
    try { installDomObserver(); } catch (_) {}
  }

  // 12.1 MutationObserver 是「DOM 被清空/重建后快速自愈」的通道
  // （实测宿主清空 body 后约 100ms 内即可重建中枢）。它必须等 documentElement
  // 真的存在才能 observe —— 原因见 tick() 里的门禁说明。
  let domObserverReady = false;
  function installDomObserver(force) {
    if (hubDisposed) return;
    // 同一实例内重复调用不必重建；跨实例（重新注入）时旧观察器的回调仍闭包着旧状态，
    // 必须被新实例顶掉，否则两个实例会同时对 DOM 变化做出反应。
    if (domObserverReady && !force) return;
    const root = document.documentElement || document.body;
    if (!root) return; // DOM 尚未成形，交给下一次巡检重试
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
    window[OBSERVER_KEY].observe(root, { childList: true, subtree: true });
    domObserverReady = true;
  }

  // 12.2 唯一初始化入口。就绪门禁（第二道）：宿主 DOM 骨架尚未成形时绝不动手改造它。
  // 启动黑屏的成因是：宿主 React 运行时还在初始化，补丁就把 --background、body 背景
  // 强制透明并往 body 里塞节点，把尚未挂载完的骨架打崩。这里只在 body 真正存在后动作。
  let hubStarted = false;
  function initHub() {
    if (hubStarted || hubDisposed) return;
    hubStarted = true;
    tick();
    try { installDomObserver(true); } catch (_) {}
    if (window[TIMER_KEY]) {
      try { window.clearInterval(window[TIMER_KEY]); } catch (_) {}
    }
    window[TIMER_KEY] = window.setInterval(tick, 2000);
  }

  // 12.3 双通道等待宿主 DOM 就绪。
  // 只等 DOMContentLoaded 是不够的：document-start 注入的脚本在部分宿主里会迟于该事件
  // 才被求值，那时事件早已错过，单纯等事件就会永久哑火。因此补一条 requestAnimationFrame
  // 轮询作为兜底，并用帧数上限保证它是有界的（绝不无限自旋）。
  // 判定条件同时要求 readyState 已离开 'loading' 且 body 真实存在 —— 只满足其一都不算就绪。
  const MAX_READY_FRAMES = 600; // ≈10s @60fps
  function whenBodyReady() {
    if (hubDisposed) return;
    const isReady = () => document.readyState !== 'loading' && !!document.body;
    if (isReady()) {
      initHub();
      return;
    }
    let frames = 0;
    const step = () => {
      if (hubDisposed) return;
      if (isReady() || frames >= MAX_READY_FRAMES) {
        initHub();
        return;
      }
      frames += 1;
      if (typeof requestAnimationFrame === 'function') {
        requestAnimationFrame(step);
      } else {
        setTimeout(step, 50);
      }
    };
    if (typeof requestAnimationFrame === 'function') {
      requestAnimationFrame(step);
    } else {
      setTimeout(step, 50);
    }
  }

  if (document.readyState === 'loading') {
    // 主通道：DOMContentLoaded。rAF 通道并行兜底，覆盖「事件已错过」的宿主。
    document.addEventListener('DOMContentLoaded', () => { whenBodyReady(); }, { once: true });
    whenBodyReady();
  } else {
    // 补丁在当前文档是解析完成后才被求值的（例如 Runtime.evaluate 即时注入路径）。
    whenBodyReady();
  }

})();
