(() => {
  'use strict';
  const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const active = id => document.querySelector('.sidebar-tab.active')?.dataset.target === id;
  const get = async path => { const response = await twoAgFetch(path); if (!response.ok) throw new Error(await response.text()); return response.json(); };
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
    window.TwoAgContextView.render(document.getElementById('context-content'), target?.context, target?.timeline || [], false, '', TwoAgI18n.locale);
    const state = document.getElementById('context-connection');
    if (target) {
      const age = target.context ? Math.max(0, Math.round((Date.now() - target.context.sampled_at) / 1000)) : null;
      state.textContent = (target.connected ? 'CDP Connected' : t("Read-only snapshot")) + (age === null ? '' : ' · ' + age + t(" seconds ago"));
    } else state.textContent = t("No conversation target found");
  }
  window.loadContextInspector = () => once('context', async () => {
    try {
      const data = await get('/api/v1/context');
      contextTargets = data.targets || [];
      const select = document.getElementById('context-target'), previous = select.value;
      select.innerHTML = contextTargets.length ? contextTargets.map(t => '<option value="' + esc(t.id) + '">' + esc(t.title || t.kind + ' · ' + t.id.slice(0, 8)) + '</option>').join('') : t("<option value=\"\">No targets available</option>");
      if (contextTargets.some(t => t.id === previous)) select.value = previous;
      else if (contextTargets.some(t => t.context?.session_id)) select.value = contextTargets.find(t => t.context?.session_id).id;
      paintSelectedContext();
    } catch (error) { document.getElementById('context-connection').textContent = t("Collection failed"); document.getElementById('context-content').textContent = error.message; }
  });
  document.getElementById('context-target')?.addEventListener('change', paintSelectedContext);

  window.loadDoctor = () => once('doctor', async () => {
    try {
      const data = await get('/api/v1/doctor');
      const colors = { ok: 'var(--2ag-green,#188038)', error: 'var(--2ag-red,#d93025)', unknown: 'var(--text-tertiary,#80868b)' };
      document.getElementById('doctor-checks').innerHTML = data.checks.map(check => '<div class="cluster-row"><span class="cluster-label">' + esc(t(check.label)) + '</span><span class="cluster-val" style="text-align:right;color:' + colors[check.state] + '">' + esc(localizeNative(check.detail)) + '</span></div>').join('');
      const labels = { connections: 'Connections', reconnects: 'Reconnects', injections: 'Injection count', injection_failures: 'Injection failures', dom_events: 'DOM events', activity_events: 'Activity events', context_updates: 'Context updates' };
      document.getElementById('runtime-counters').innerHTML = Object.entries(data.counters).map(([key, value]) => '<div class="cluster-row"><span class="cluster-label">' + esc(t(labels[key] || key)) + '</span><span class="cluster-val">' + value + '</span></div>').join('');
      const runtime = await get('/api/v1/runtime');
      document.getElementById('runtime-targets').innerHTML = '<table class="observation-table"><thead><tr><th>'+t('Target')+'</th><th>'+t('Connection')+'</th><th>'+t('Injected features')+'</th></tr></thead><tbody>' + runtime.targets.map(target => '<tr><td>' + esc(target.kind + ' · ' + target.id.slice(0, 8)) + '</td><td>' + t(target.connected ? 'Connected' : 'Disconnected / Snapshot') + '</td><td>' + Object.entries(target.injected_features || {}).filter(([name, installed]) => installed).map(([name]) => esc(name)).join(' · ') + '</td></tr>').join('') + '</tbody></table>';
    } catch (error) { document.getElementById('doctor-checks').textContent = error.message; }
  });

  function quotaWindow(observation) {
    if (observation.drop_pp === null) return t("Insufficient history");
    return '−' + observation.drop_pp + t(" pp · covered ") + Math.round(observation.covered_ms / 60000) + 'm';
  }
  function countdown(reset) {
    const at = Date.parse(reset);
    if (!Number.isFinite(at)) return t("Reset time unknown");
    const minutes = Math.ceil((at - Date.now()) / 60000);
    if (minutes <= 0) return t("Waiting for a new quota sample");
    return Math.floor(minutes / 60) + 'h ' + minutes % 60 + 'm';
  }
  window.loadQuotaIntelligence = () => once('quota', async () => {
    try {
      const data = await get('/api/v1/quota/history');
      const accounts = new Map(data.accounts.map(a => [a.id, a]));
      const container = document.getElementById('quota-intelligence');
      if (!data.trends.length) { container.textContent = t("No reliable history yet. Trends record observed quota samples; cached data retains its original timestamp."); return; }
      container.innerHTML = t("<table class=\"observation-table\"><thead><tr><th>Account / quota pool</th><th>Remaining / reset</th><th>Observed in 1h</th><th>Observed in 24h</th><th>Observed rate</th></tr></thead><tbody>") + data.trends.map(trend => {
        const account = accounts.get(trend.account_id), latest = trend.history.at(-1);
        const email = account?.email || '';
        const masked = typeof maskEmail === 'function' ? maskEmail(email) : email.replace(/^(.).+(@)/, '$1***$2');
        return '<tr><td>' + esc(masked) + (account?.is_active ? t(" · Selected account") : '') + '<br><span class="ctx-muted">' + esc(trend.pool + ' · ' + trend.window) + '</span></td><td>' + latest.remaining + '%<br>' + esc(countdown(latest.reset_at)) + '</td><td>' + esc(quotaWindow(trend.one_hour)) + '</td><td>' + esc(quotaWindow(trend.day)) + '</td><td>' + (trend.one_hour.rate_pph === null ? t("Insufficient history") : trend.one_hour.rate_pph.toFixed(1) + ' pp/h') + '<br><span class="ctx-muted">' + esc(new Date(latest.at).toLocaleString()) + '</span></td></tr>';
      }).join('') + t("</tbody></table><p class=\"compat-detail\">Only observed decreases are counted, separated by reset cycles. pp means percentage points; remaining usage time is not predicted.</p><details><summary>Recent samples</summary>") + data.trends.map(t => '<p>' + esc(t.pool + ' · ' + t.window + ' · ' + t.account_id.slice(0, 8)) + '</p><div class="compat-detail">' + t.history.slice(-12).map(p => esc(new Date(p.at).toLocaleString()) + ' ' + p.remaining + '%').join(' · ') + '</div>').join('') + '</details>';
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
    if (!skills.length) { list.textContent = t("No installed Skills found. Workspace Skills require a discoverable project directory."); return; }
    for (const skill of skills) {
      const article = document.createElement('article'); article.className = 'session-card';
      const body = document.createElement('div'); body.className = 'session-main';
      body.innerHTML = '<div class="session-title">' + esc(skill.name) + ' <span class="tag-muted">' + esc(skill.scope) + '</span></div><p class="compat-detail">' + esc(skill.description || skill.metadata_warning || t("No description")) + '</p><div class="session-meta">' + [skill.resources && 'resources/', skill.examples && 'examples/', skill.scripts && 'scripts/'].filter(Boolean).map(esc).join(' · ') + '</div><div class="compat-detail">' + esc(skill.path) + '</div>';
      const button = document.createElement('button'); button.className = 'btn btn-tonal'; button.textContent = t("View SKILL.md");
      button.addEventListener('click', async () => {
        try { const data = await get('/api/v1/skills/read?id=' + encodeURIComponent(skill.id)); showTextPreview(skill.name, data.content); }
        catch (error) { showTextPreview(t("Read failed"), error.message); }
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
      const data = await get('/api/v1/sessions/preview?id=' + encodeURIComponent(id) + '&store=' + encodeURIComponent((currentSessions.find(s=>s.id===id)||{}).store || ''));
      showTextPreview(data.provider + ' · ' + data.id, data.messages.map(m => '## ' + m.role + (m.at ? ' · ' + m.at : '') + '\n\n' + m.content).join('\n\n') + (data.truncated ? t("\n\n[Preview truncated; complete messages were not loaded]") : ''));
    } catch (error) { showTextPreview(t("Messages unavailable"), error.message); }
  };
  // Read usage for visible Antigravity rows only, with a small concurrency bound.
  let usageObserver = null, usageQueue = [], usageWorkers = 0;
  const usageCache = new Map();
  function drainUsageQueue() {
    while (usageWorkers < 3 && usageQueue.length) {
      const element = usageQueue.shift(), id = element.dataset.sessionUsage;
      if (!element.isConnected) continue;
      const store = (currentSessions.find(s=>s.id===id)||{}).store || '';
      const key=id+'|'+store;
      const cached = usageCache.get(key);
      if (cached && Date.now() - cached.at < 5000) { window.TwoAgContextView.renderSession(element, cached.value,TwoAgI18n.locale); continue; }
      usageWorkers++;
      get('/api/v1/sessions/usage?id=' + encodeURIComponent(id) + '&store=' + encodeURIComponent((currentSessions.find(s=>s.id===id)||{}).store || '')).then(value => {
        if (usageCache.size > 128) usageCache.clear();
        usageCache.set(key, {at:Date.now(),value});
        if (element.isConnected) window.TwoAgContextView.renderSession(element,value,TwoAgI18n.locale);
      }).catch(() => { if (element.isConnected) element.textContent = 'Token · — · Unavailable'; })
        .finally(() => { usageWorkers--; drainUsageQueue(); });
    }
  }
  window.loadVisibleSessionUsage = list => {
    usageObserver?.disconnect(); usageQueue = [];
    usageObserver = new IntersectionObserver(entries => {
      for (const entry of entries) if (entry.isIntersecting) { usageObserver.unobserve(entry.target); usageQueue.push(entry.target); }
      drainUsageQueue();
    }, {rootMargin:'100px'});
    list.querySelectorAll('[data-session-usage]').forEach(el => usageObserver.observe(el));
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
  window.addEventListener('2ag:observability-locale',()=>{
    if(active('context'))paintSelectedContext();
    if(active('diagnostics'))window.loadDoctor();
    if(active('skills'))window.loadSkillsHub();
    if(active('dashboard'))window.loadQuotaIntelligence();
    for(const element of document.querySelectorAll('[data-session-usage]')){
      const source=(currentSessions.find(s=>s.id===element.dataset.sessionUsage)||{}).store||'';
      const cached=usageCache.get(element.dataset.sessionUsage+'|'+source);
      if(cached)window.TwoAgContextView.renderSession(element,cached.value,TwoAgI18n.locale);
    }
  });
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
