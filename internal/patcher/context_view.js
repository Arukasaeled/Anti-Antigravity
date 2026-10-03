(() => {
  'use strict';
  const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const tokens = n => n === null || n === undefined ? '—' : Math.abs(n) >= 1000000 ? (n / 1000000).toFixed(1) + 'M' : Math.abs(n) >= 1000 ? (n / 1000).toFixed(1) + 'K' : String(n);
  const names = { conversation: 'Conversation', workspace: 'Workspace / Artifacts', files: 'Referenced Files', tools: 'Tool Results', system: 'System / Rules', other: 'Other' };
  const css = `.ctx{color:var(--text-primary,var(--2ag-text-primary,#202124));font-size:13px;line-height:1.6}.ctx-head{display:flex;justify-content:space-between;align-items:baseline;gap:16px;flex-wrap:wrap}.ctx-number{font-size:38px;line-height:1.3;font-weight:500;font-variant-numeric:tabular-nums}.ctx-muted{color:var(--text-secondary,var(--2ag-text-secondary,#5f6368))}.ctx-track{height:8px;background:var(--2ag-outline,#dadce0);margin:16px 0 8px}.ctx-fill{height:100%;background:#4285f4}.ctx-fill[data-high=true]{background:#ea4335}.ctx-line{display:flex;justify-content:space-between;gap:18px;padding:9px 0;border-bottom:1px solid var(--2ag-outline,#dadce0)}.ctx-line span:first-child{min-width:0;overflow-wrap:anywhere}.ctx-line span:last-child{flex-shrink:0;font-variant-numeric:tabular-nums}.ctx h3{font-size:14px;font-weight:500;margin:24px 0 6px}.ctx details{margin-top:16px}.ctx summary{cursor:pointer}.ctx-note{margin:12px 0;line-height:1.7}.ctx-badge{font-size:12px;padding:3px 7px;background:var(--2ag-surface-2,#f1f3f4)}.ctx-native{color:var(--text-secondary,var(--2ag-text-secondary,#5f6368))}.ctx-table{width:100%;border-collapse:collapse;font-size:12px}.ctx-table th,.ctx-table td{text-align:left;padding:8px 4px;border-bottom:1px solid var(--2ag-outline,#dadce0)}.ctx-table th{font-weight:500}.ctx-activity{color:#188038}.ctx-empty{padding:24px 0;color:var(--text-secondary,var(--2ag-text-secondary,#5f6368))}`;
  function render(element, snapshot, timeline = [], compact = false) {
    if (!element) return;
    const opened = Array.from(element.querySelectorAll('details')).map(detail => detail.open);
    if (!snapshot) { element.innerHTML = '<div class="ctx-empty">尚未采集上下文。打开 Antigravity 会话后刷新。</div>'; return; }
    const s = snapshot, estimated = s.mode === 'estimated';
    const prefix = estimated ? '≈ ' : '';
    const percent = s.used_tokens !== null && s.limit_tokens > 0 ? Math.round(s.used_tokens / s.limit_tokens * 100) : null;
    const remaining = percent === null ? null : Math.max(0, s.limit_tokens - s.used_tokens);
    const scope = s.composition_scope === 'active_prompt' ? '当前 Prompt' : s.composition_scope === 'loaded_history' ? '已加载历史 · 不等同于当前上下文' : '可见页面';
    const basis = s.mode === 'native' ? 'Native · 最近请求' : estimated ? '≈ Estimated' : 'Composition Only';
    const timestamp = s.usage_at || s.sampled_at;
    const groups = (s.composition || []).filter(g => g.kind !== 'files');
    const line = (name, value) => '<div class="ctx-line"><span>' + escape(name) + '</span><span>' + escape(value) + '</span></div>';
    element.innerHTML = '<style>' + css + '</style><div class="ctx">' +
      '<div class="ctx-head"><span class="ctx-number">' + (s.used_tokens === null ? '—' : prefix + tokens(s.used_tokens)) +
      '<span class="ctx-muted" style="font-size:18px"> / ' + (s.limit_tokens ? tokens(s.limit_tokens) : '上限未知') + '</span></span><span class="ctx-badge">' + basis + '</span></div>' +
      (percent === null ? '' : '<div class="ctx-track" role="progressbar" aria-label="Context usage" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.min(100, percent) + '"><div class="ctx-fill" data-high="' + (percent >= 85) + '" style="width:' + Math.min(100, percent) + '%"></div></div>' + line('Usage / Remaining', prefix + percent + '% · ' + prefix + tokens(remaining))) +
      '<div class="ctx-muted">' + escape(s.model || '模型未知') + ' · <span class="ctx-activity">' + escape(s.activity || 'unknown') + '</span> · ' + escape(new Date(timestamp).toLocaleTimeString()) + '</div>' +
      '<p class="ctx-note ctx-muted">' + escape(s.note) + (s.partial ? ' 当前明细不完整。' : '') + '</p>' +
      '<details' + (compact ? '' : ' open') + '><summary>原生 Token 读数</summary>' +
      line('Input', tokens(s.input_tokens)) + line('Cache read / write', tokens(s.cache_read_tokens) + ' / ' + tokens(s.cache_write_tokens)) +
      line('Output · 不计入当前 input', tokens(s.output_tokens)) + '</details>' +
      '<h3>Composition <span class="ctx-muted">· ' + scope + '</span></h3>' +
      (groups.length ? groups.map(g => line(names[g.kind] || g.kind, g.items + ' 项 · ' + (g.estimated && g.tokens !== null ? '≈ ' : '') + tokens(g.tokens) + (g.partial ? ' · 部分可计数' : ''))).join('') : '<div class="ctx-muted">尚无可读取的组成明细</div>') +
      '<details><summary>按占用排序的明细 / 文件引用</summary>' + (s.items || []).slice(0, compact ? 25 : 100).map(item => line(item.name, (item.estimated && item.tokens !== null ? '≈ ' : '') + tokens(item.tokens))).join('') +
      '<div class="ctx-muted">— 表示占用未知；文件引用不证明文件仍在当前 Prompt。</div></details>' +
      '<h3>Context Timeline</h3>' + (timeline.length ? '<table class="ctx-table"><thead><tr><th>时间</th><th>Context</th><th>变化 / 原因</th></tr></thead><tbody>' + timeline.slice(-12).reverse().map(point => '<tr><td>' + escape(new Date(point.at).toLocaleTimeString()) + '</td><td>' + (point.mode === 'estimated' ? '≈ ' : '') + tokens(point.tokens) + '</td><td>' + (point.delta === null || point.delta === undefined ? '—' : (point.delta >= 0 ? '+' : '') + tokens(point.delta)) + ' · ' + escape(point.cause || '首次采集') + '</td></tr>').join('') + '</tbody></table>' : '<div class="ctx-muted">等待后续请求。首次采集不会生成虚构历史。</div>') + '</div>';
    Array.from(element.querySelectorAll('details')).forEach((detail,index) => { if (opened[index] !== undefined) detail.open = opened[index]; });
  }
  window.TwoAgContextView = { render, tokens };
})();
