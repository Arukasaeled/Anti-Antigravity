(() => {
  'use strict';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const active = id => document.querySelector('.sidebar-tab.active')?.dataset.target === id;
  const get = async path => { const response = await fetch(path); if (!response.ok) throw new Error(await response.text()); return response.json(); };
  const pending = new Set();
  async function once(name, fn) {
    if (pending.has(name)) return;
    pending.add(name);
    try { await fn(); } finally { pending.delete(name); }
  }

  let contextTargets = [];
  function paintSelectedContext() {
    const id = document.getElementById('context-target').value;
    const target = contextTargets.find(t => t.id === id);
    window.TwoAgContextView.render(document.getElementById('context-content'), target?.context, target?.timeline || []);
    const state = document.getElementById('context-connection');
    if (target) {
      const age = target.context ? Math.max(0, Math.round((Date.now() - target.context.sampled_at) / 1000)) : null;
      state.textContent = (target.connected ? 'CDP Connected' : '只读快照') + (age === null ? '' : ' · ' + age + ' 秒前');
    } else state.textContent = '未发现会话 target';
  }
  window.loadContextInspector = () => once('context', async () => {
    try {
      const data = await get('/api/v1/context');
      contextTargets = data.targets || [];
      const select = document.getElementById('context-target'), previous = select.value;
      select.innerHTML = contextTargets.length ? contextTargets.map(t => '<option value="' + esc(t.id) + '">' + esc(t.title || t.kind + ' · ' + t.id.slice(0, 8)) + '</option>').join('') : '<option value="">没有可用 Target</option>';
      if (contextTargets.some(t => t.id === previous)) select.value = previous;
      else if (contextTargets.some(t => t.context?.session_id)) select.value = contextTargets.find(t => t.context?.session_id).id;
      paintSelectedContext();
    } catch (error) { document.getElementById('context-connection').textContent = '采集失败'; document.getElementById('context-content').textContent = error.message; }
  });
  document.getElementById('context-target')?.addEventListener('change', paintSelectedContext);

  window.loadDoctor = () => once('doctor', async () => {
    try {
      const data = await get('/api/v1/doctor');
      const colors = { ok: 'var(--2ag-green,#188038)', error: 'var(--2ag-red,#d93025)', unknown: 'var(--text-tertiary,#80868b)' };
      document.getElementById('doctor-checks').innerHTML = data.checks.map(check => '<div class="cluster-row"><span class="cluster-label">' + esc(check.label) + '</span><span class="cluster-val" style="text-align:right;color:' + colors[check.state] + '">' + esc(check.detail) + '</span></div>').join('');
      const labels = { connections: 'Connections', reconnects: 'Reconnects', injections: 'Injection count', injection_failures: 'Injection failures', dom_events: 'DOM events', activity_events: 'Activity events', context_updates: 'Context updates' };
      document.getElementById('runtime-counters').innerHTML = Object.entries(data.counters).map(([key, value]) => '<div class="cluster-row"><span class="cluster-label">' + esc(labels[key] || key) + '</span><span class="cluster-val">' + value + '</span></div>').join('');
      const runtime = await get('/api/v1/runtime');
      document.getElementById('runtime-targets').innerHTML = '<table class="observation-table"><thead><tr><th>Target</th><th>Connection</th><th>Injected features</th></tr></thead><tbody>' + runtime.targets.map(target => '<tr><td>' + esc(target.kind + ' · ' + target.id.slice(0, 8)) + '</td><td>' + (target.connected ? 'Connected' : 'Disconnected / Snapshot') + '</td><td>' + Object.entries(target.injected_features || {}).filter(([name, installed]) => installed).map(([name]) => esc(name)).join(' · ') + '</td></tr>').join('') + '</tbody></table>';
    } catch (error) { document.getElementById('doctor-checks').textContent = error.message; }
  });

  function quotaWindow(observation) {
    if (observation.drop_pp === null) return '历史不足';
    return '−' + observation.drop_pp + ' pp · 覆盖 ' + Math.round(observation.covered_ms / 60000) + 'm';
  }
  function countdown(reset) {
    const at = Date.parse(reset);
    if (!Number.isFinite(at)) return '重置时间未知';
    const minutes = Math.ceil((at - Date.now()) / 60000);
    if (minutes <= 0) return '等待新额度读数';
    return Math.floor(minutes / 60) + 'h ' + minutes % 60 + 'm';
  }
  window.loadQuotaIntelligence = () => once('quota', async () => {
    try {
      const data = await get('/api/v1/quota/history');
      const accounts = new Map(data.accounts.map(a => [a.id, a]));
      const container = document.getElementById('quota-intelligence');
      if (!data.trends.length) { container.textContent = '尚无可靠历史。额度变化将从真实配额采样开始记录；缓存使用原始采样时间。'; return; }
      container.innerHTML = '<table class="observation-table"><thead><tr><th>账号 / 额度池</th><th>剩余 / 重置</th><th>最近 1h 已观察</th><th>最近 24h 已观察</th><th>观察速率</th></tr></thead><tbody>' + data.trends.map(trend => {
        const account = accounts.get(trend.account_id), latest = trend.history.at(-1);
        const email = account?.email || '';
        const masked = typeof maskEmail === 'function' ? maskEmail(email) : email.replace(/^(.).+(@)/, '$1***$2');
        return '<tr><td>' + esc(masked) + (account?.is_active ? ' · 本地主控' : '') + '<br><span class="ctx-muted">' + esc(trend.pool + ' · ' + trend.window) + '</span></td><td>' + latest.remaining + '%<br>' + esc(countdown(latest.reset_at)) + '</td><td>' + esc(quotaWindow(trend.one_hour)) + '</td><td>' + esc(quotaWindow(trend.day)) + '</td><td>' + (trend.one_hour.rate_pph === null ? '历史不足' : trend.one_hour.rate_pph.toFixed(1) + ' pp/h') + '<br><span class="ctx-muted">' + esc(new Date(latest.at).toLocaleString()) + '</span></td></tr>';
      }).join('') + '</tbody></table><p class="compat-detail">只统计已观察到的下降；重置周期变化分段处理。pp 为百分点，不预测剩余使用时长。</p><details><summary>最近历史点</summary>' + data.trends.map(t => '<p>' + esc(t.pool + ' · ' + t.window + ' · ' + t.account_id.slice(0, 8)) + '</p><div class="compat-detail">' + t.history.slice(-12).map(p => esc(new Date(p.at).toLocaleString()) + ' ' + p.remaining + '%').join(' · ') + '</div>').join('') + '</details>';
    } catch (error) { document.getElementById('quota-intelligence').textContent = error.message; }
  });

  let skillItems = [];
  window.loadSkillsHub = () => once('skills', async () => {
    try { const data = await get('/api/v1/skills'); skillItems = data.skills || []; paintSkills(); }
    catch (error) { document.getElementById('skills-list').textContent = error.message; }
  });
  function paintSkills() {
    const scope = document.getElementById('skills-scope').value;
    const query = document.getElementById('skills-search').value.toLowerCase();
    const skills = skillItems.filter(s => (scope === 'all' || s.scope === scope) && (s.name + ' ' + s.description + ' ' + s.path).toLowerCase().includes(query));
    const list = document.getElementById('skills-list');
    list.innerHTML = '';
    if (!skills.length) { list.textContent = '未发现已安装 Skills。工作区 Skills 需要可发现的项目目录。'; return; }
    for (const skill of skills) {
      const article = document.createElement('article'); article.className = 'session-card';
      const body = document.createElement('div'); body.className = 'session-main';
      body.innerHTML = '<div class="session-title">' + esc(skill.name) + ' <span class="tag-muted">' + esc(skill.scope) + '</span></div><p class="compat-detail">' + esc(skill.description || skill.metadata_warning || '没有描述') + '</p><div class="session-meta">' + [skill.resources && 'resources/', skill.examples && 'examples/', skill.scripts && 'scripts/'].filter(Boolean).map(esc).join(' · ') + '</div><div class="compat-detail">' + esc(skill.path) + '</div>';
      const button = document.createElement('button'); button.className = 'btn btn-tonal'; button.textContent = '查看 SKILL.md';
      button.addEventListener('click', async () => {
        try { const data = await get('/api/v1/skills/read?id=' + encodeURIComponent(skill.id)); showTextPreview(skill.name, data.content); }
        catch (error) { showTextPreview('读取失败', error.message); }
      });
      article.append(body, button); list.append(article);
    }
  }
  document.getElementById('skills-scope')?.addEventListener('change', paintSkills);
  document.getElementById('skills-search')?.addEventListener('input', paintSkills);

  function showTextPreview(title, content) {
    const dialog = document.getElementById('observation-preview');
    document.getElementById('observation-preview-title').textContent = title;
    document.getElementById('observation-preview-content').textContent = content;
    if (!dialog.open) dialog.showModal();
  }
  window.previewAISession = async id => {
    try {
      const data = await get('/api/v1/sessions/preview?id=' + encodeURIComponent(id));
      showTextPreview(data.provider + ' · ' + data.id, data.messages.map(m => '## ' + m.role + (m.at ? ' · ' + m.at : '') + '\n\n' + m.content).join('\n\n') + (data.truncated ? '\n\n[预览已截断；未加载完整消息]' : ''));
    } catch (error) { showTextPreview('消息不可用', error.message); }
  };
  document.querySelectorAll('.sidebar-tab[data-target="context"], .sidebar-tab[data-target="skills"]').forEach(tab => {
    tab.setAttribute('role', 'button'); tab.tabIndex = 0;
    tab.addEventListener('keydown', event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); tab.click(); } });
  });
  document.querySelectorAll('.sidebar-tab').forEach(tab => tab.addEventListener('click', () => {
    if (tab.dataset.target === 'context') window.loadContextInspector();
    if (tab.dataset.target === 'diagnostics') window.loadDoctor();
    if (tab.dataset.target === 'skills') window.loadSkillsHub();
    if (tab.dataset.target === 'dashboard') window.loadQuotaIntelligence();
  }));
  document.getElementById('session-provider')?.addEventListener('change', () => { if (typeof renderFilteredSessions === 'function') renderFilteredSessions(); });
  setInterval(() => {
    if (document.hidden) return;
    if (active('context')) window.loadContextInspector();
  }, 4000);
  setInterval(() => {
    if (document.hidden) return;
    if (active('diagnostics')) window.loadDoctor();
    if (active('dashboard')) window.loadQuotaIntelligence();
  }, 15000);
  window.loadQuotaIntelligence();
})();
