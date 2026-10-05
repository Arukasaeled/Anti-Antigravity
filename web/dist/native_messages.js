// Explicit translations for Manager-owned backend status and error messages.
// This is used at API/render boundaries, never on the DOM or user content.
(() => {
  const pairs = [
    ['宿主已启动；原生登录账号与 Manager 选择一致，身份已验证','Host started; native sign-in matches the Manager selection and identity is verified'],
    ['宿主内部身份已确认，账号切换完成','Native host identity verified; account switch completed'],
    ['目标凭据已应用，宿主已启动；宿主内部登录身份尚未确认，未提交 active account','Target credentials applied and host started; native identity is unverified and active account was not committed'],
    ['宿主身份尚未验证','Host identity has not been verified'],
    ['正在读取受管宿主登录身份','Reading the managed host sign-in identity'],
    ['宿主身份需要重新验证','Host identity needs to be rechecked'],
    ['宿主不是 Manager 已授权验证的运行实例','Host is not an instance authorized for verification by Manager'],
    ['Manager 已选择其他账号，当前宿主身份未验证','Manager selected a different account; current host identity is unverified'],
    ['系统凭据状态暂不可读，宿主身份未验证','Shared credential metadata is unavailable; host identity is unverified'],
    ['系统凭据已被外部切号改变，宿主身份未验证','Shared credential ownership changed externally; host identity is unverified'],
    ['宿主原生登录身份暂不可读，尚未验证','Native host sign-in identity is unavailable; identity remains unverified'],
    ['宿主登录账号与 Manager 选择不一致，身份未验证','Native host account differs from the Manager selection; identity is unverified'],
    ['受管宿主登录账号与 Manager 选择一致，身份已验证','Managed host sign-in matches the Manager selection; identity verified'],
    ['尚未开始','Not started'],['正在备份当前登录凭据…','Backing up current credentials…'],
    ['当前没有登录凭据，无需备份','No current credentials to back up'],
    ['正在停止运行中的 Antigravity…','Stopping the running Antigravity…'],
    ['官方 Antigravity 已启动：请在它的窗口里点击「Continue with Google」','Official Antigravity started; select Continue with Google in its window'],
    ['等待 Google 登录：请在官方 Antigravity 窗口点击「Continue with Google」','Waiting for Google sign-in; select Continue with Google in official Antigravity'],
    ['已检测到中间态凭据（尚未完成登录），继续等待…','Intermediate credentials detected; sign-in is incomplete, still waiting…'],
    ['官方窗口已离开登录页，正在确认凭据…','Official window left the login page; checking credentials…'],
    ['正在等待官方 Antigravity 打开登录页…','Waiting for official Antigravity to open the login page…'],
    ['正在恢复原账号…','Restoring the original account…'],
    ['运行中的宿主与所选形态一致。','The running host matches the configured mode.'],
    ['官方形态','Official mode'],['2Ag 增强形态','Enhanced mode'],
    ['未检测到 Antigravity：','Antigravity not found: '],
    ['本机既没有官方 Antigravity 安装，也没有 2Ag 冻结宿主副本。','Neither an official installation nor a frozen host copy is available. '],
    ['请先安装官方 Antigravity','Install official Antigravity first'],
    ['2Ag 不再随安装包分发官方运行时','2Ag does not redistribute the official runtime'],
    ['当前没有正在运行的 Antigravity 实例','No Antigravity instance is running'],
    ['冻结宿主已就绪。','The frozen host is ready.'],
    ['缓存配额（非实时）','Cached quota (not live)'],['未载入配额 (离线)','Quota unavailable (offline)'],
    ['系统凭据归属','Credential owner'],['宿主内部身份未确认','Host identity unverified'],
    ['目标账号不存在','Account is not registered'],['目标账号凭据不可用或归属不一致','Stored credential unavailable or owner mismatch'],
    ['缺少 email','Email is required'],['无效 email','Invalid email'],
    ['账号未完全移除，请重试','Account was not fully removed; retry the operation'],
    ['已移除账号 ','Account removed: '],['已选择账号','Account selected'],
    ['需要用户明确确认关闭外部实例','Explicit authorization is required to close external instances'],
    ['外部 Antigravity','External Antigravity'],['未授权关闭外部实例','Closing external instances was not authorized'],
    ['账号凭据','Account credential'],['凭据不存在','Credential not found'],['读取凭据失败','Could not read credential'],
    ['解密失败','Decryption failed'],['解析失败','Parsing failed'],['归属不一致','Owner mismatch'],
    ['无法读取凭据归属','Credential owner unavailable'],['目标账号','Requested account'],
    ['启动失败','Launch failed'],['宿主未运行','Host not running'],['宿主已停止','Host stopped'],
    ['宿主操作完成','Host operation completed'],['宿主操作失败','Host operation failed'],
    ['切换失败','Switch failed'],['重启失败','Restart failed'],['停止失败','Stop failed'],
    ['读取请求失败: ','Could not read request: '],['解析请求失败: ','Could not parse request: '],
    ['无法定位用户主目录','Could not locate the user home directory'],
    ['共享数据树不存在','Shared storage not found'],
    ['官方安装可定位','Official installation found'],['官方文件未被补丁化','Official files are not patched'],
    ['两套 profile 相互独立','Official and Enhanced profiles are separate'],
    ['凭据管理器为双方共享','Windows Credential Manager is shared between modes'],
    ['%USERPROFILE%\\.gemini\\antigravity 双方共读','Both modes read %USERPROFILE%\\.gemini\\antigravity'],
    ['当前运行形态：','Current runtime mode: '],
    ['宿主上的补丁实例未回报这一项（可能是旧版补丁）','The injected instance did not report this capability; it may be an older build'],
    ['官方形态不挂 CDP、不注入，因此无法（也不应该）探测宿主侧能力','Official mode does not inject runtime UI; injection capabilities are disabled'],
    ['探测通道失败：','Probe channel failed: '],
    ['无法连接宿主 CDP 通道，本次兼容性探测失败：','Host CDP unavailable; compatibility probe failed: '],
    ['宿主补丁的回执无法解析：','Could not parse the runtime probe response: '],
    ['探测通道失败，没有可用证据。','The probe channel failed; no evidence is available.'],
    ['宿主未回报可用读数。','The host returned no usable reading.'],
    ['CDP 上未找到工作台视窗','No workbench target found over CDP'],
    ['工作台视窗上没有补丁的常驻会话（补丁可能还没挂上）','No persistent injection connection for the workbench; injection may not be loaded'],
    ['宿主上没有 2Ag 补丁实例（window.__2ag.probeCapabilities 不存在）','No injected 2Ag instance (window.__2ag.probeCapabilities is absent)'],
    ['补丁实例存在但没有回报能力读数（可能是旧版补丁）','The injected instance did not return capability readings; it may be an older build'],
    ['无法连接 CDP 端口 ','Could not connect to CDP port '],['读取 CDP 目标列表失败: ','Could not read CDP targets: '],
    ['解析 CDP 目标列表失败: ','Could not parse CDP targets: '],
    ['lang=zh-CN，观察器在场','lang=zh-CN; translation observer present'],
    ['词典观察器未安装','Translation observer is not installed'],
    ['观察器在场但 lang=','Translation observer present; lang='],
    ['未检测到安装','Installation not found'],['未运行','Not running'],['未连接','Disconnected'],['未探测','Not probed'],
    ['未知','Unknown'],['宿主','Host'],['冻结宿主 ','Frozen host '],
    ['官方宿主未在运行','Official host not running'],
    ['当前 Activity 仅支持原生 conversation SQLite','Activity requires native conversation SQLite'],
    ['无效 Activity 游标','Invalid Activity cursor'],['找不到会话','Session not found'],
    ['无法确定 2Ag 根目录，不能在本地建立冻结宿主','2Ag root unavailable; cannot create a frozen host'],
    ['从官方安装建立冻结宿主失败','Could not create a frozen host from the official installation'],
    ['冻结宿主落位失败','Could not move the completed frozen host into place'],
    ['官方安装来源可能不完整','The official installation may be incomplete'],
    ['无法读取进程快照，宿主状态未知','Process snapshot unavailable; host state is unknown']
  ].sort((a,b)=>b[0].length-a[0].length);
  window.localizeNative = value => {
    const original=String(value ?? '');
    if(TwoAgI18n.locale!=='en-US')return t(original);
    const exact=TwoAgI18n.nativeText(original);if(exact!==original)return exact;
    // The caller marks this as Manager-owned copy. Raw tool/error payloads and
    // conversation bodies are displayed separately without this conversion.
    for(const [chinese,english] of pairs)if(original===chinese)return english;
    const stalePort=original.match(/^官方 profile 记着端口 (\d+)，但该端口没有监听 —— 官方宿主当前没在运行。$/);
    if(stalePort)return 'Official profile records port '+stalePort[1]+', but nothing is listening; the official host is not running.';
    for(const [chinese,english] of pairs)if(/[\s:：]$/.test(chinese)&&original.startsWith(chinese))return english+window.localizeNative(original.slice(chinese.length));
    return original;
  };
  window.nativeProbeDetail = value => {
    const text=String(value??''),localized=localizeNative(text),escape=value=>String(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    return TwoAgI18n.locale==='en-US'&&/[\u3400-\u9fff]/.test(localized)?'<details><summary>'+t('Native probe details')+'</summary><pre>'+escape(text)+'</pre></details>':escape(localized);
  };
  TwoAgI18n.register({
    'Native probe details':'原生探针详情','Native error details':'原生错误详情',
    'Antigravity Process':'Antigravity 进程','CDP Targets':'CDP 目标','Runtime Injection':'运行时注入',
    'Credential Slot':'凭据槽','Official Session':'官方会话','2Ag Vault':'2Ag 保险库',
    'Quota API':'配额 API','Quota Cache':'配额缓存','DOM Compatibility':'DOM 兼容性','Storage Path':'存储路径',
    'Connected':'已连接','Disconnected / Snapshot':'未连接 / 快照','Connections':'连接数','Reconnects':'重连次数',
    'Injection count':'注入次数','Injection failures':'注入失败','DOM events':'DOM 事件','Activity events':'Activity 事件',
    'Context updates':'上下文更新','Target':'目标','Connection':'连接','Injected features':'已注入能力',
    'Configured mode applies on next launch. Running instances are left unchanged.':'配置在下次启动生效，已有实例保持运行。',
    'Official files are not patched. Profiles are separate; credentials and conversation storage can be shared.':'官方文件不被补丁化；profile 独立，凭据与会话存储可能共享。',
    'Version comparison is unavailable.':'版本无法比较。',
    'The frozen host matches the official installation.':'冻结宿主与官方安装版本一致。',
    'The frozen host uses a different version; syncing is a manual decision.':'冻结宿主与官方安装版本不同，同步由用户决定。',
    'Manual sync replaces the local host copy. Automatic sync is unavailable.':'手动同步会替换本地宿主副本，不提供自动同步。',
    'Capability readings':'能力读数','No capability evidence available.':'尚无能力证据。'
    ,'Host running; runtime mode unverified':'宿主运行中，运行形态尚未验证'
    ,'Running':'运行中','No process observed by the existing host probe':'现有宿主探针未发现进程'
    ,'Official mode: injection disabled':'Official 模式：不注入'
    ,'Readable identity metadata; credential contents omitted':'身份元数据可读，凭据内容已省略'
    ,'Identity metadata unavailable':'身份元数据不可用'
    ,'Local identity observed; remote authentication not probed':'已读取本地身份，未探测远端认证'
    ,'No confirmed local session':'本地会话尚未确认'
    ,'Index unavailable or not initialized':'索引不可用或尚未初始化','Index could not be read':'索引无法读取'
    ,'Doctor does not probe the remote API; account quota flow handles live reads and cache fallback':'Doctor 不探测远端 API；账号配额流程处理实时读取与缓存回退'
    ,'Authorized cache unavailable':'授权缓存不可用'
    ,'Readable Antigravity storage; no write probe':'Antigravity 存储可读，未执行写入探测'
    ,'Storage unavailable':'存储不可用','Home directory unavailable':'用户目录不可用'
    ,'Installed binary discovered; process health not inferred':'已发现安装文件，不推断进程健康状态'
    ,'Installed binary not discovered':'未发现安装文件'
  });
})();
