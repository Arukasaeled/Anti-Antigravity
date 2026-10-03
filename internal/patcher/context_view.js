(() => {
  'use strict';
  const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', '"':'&quot;', "'":'&#39;' }[c]));
  const known = n => typeof n === 'number' && Number.isFinite(n) && n >= 0;
  const tokens = n => !known(n) ? '—' : n >= 1e6 ? (n / 1e6).toFixed(2).replace(/0$/, '') + 'M' : n >= 1e3 ? (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K' : String(n);
  const amount = (exact, observed) => known(exact) ? tokens(exact) : known(observed) ? '≥ ' + tokens(observed) : '—';
  const rate = n => known(n) ? Math.round(n * 100) + '%' : '—';
  const time = n => known(n) ? new Date(n).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) : '—';
  const source = v => ['Native','Estimated'].includes(v) ? v : 'Unavailable';
  const names = {conversation:'Conversation',workspace:'Workspace',files:'Referenced Files',tools:'Tools',system:'System / Rules',other:'Other'};
  const css = `.ctx{color:var(--text-primary,var(--2ag-text-primary,inherit));font-size:12px;line-height:1.6}.ctx-muted{color:var(--text-secondary,var(--2ag-text-secondary,inherit));opacity:.8;overflow-wrap:anywhere}.ctx-line{display:flex;justify-content:space-between;gap:16px;padding:4px 0}.ctx-line span:last-child{font-variant-numeric:tabular-nums;min-width:0;text-align:right;overflow-wrap:anywhere}.ctx details{margin-top:12px}.ctx summary,.ctx button{cursor:pointer}.ctx-source{font-size:10px;opacity:.65;margin-left:6px}.ctx-glance{padding:7px 0}.ctx-value{font-size:15px;font-variant-numeric:tabular-nums}.ctx-info{border:0;background:transparent;color:inherit;padding:0 4px;font:inherit}.ctx-link{border:0;background:transparent;color:var(--2ag-blue,#4285f4);padding:6px 0;font:inherit}.ctx-track{height:2px;background:var(--2ag-outline,#dadce0);margin:6px 0}.ctx-fill{height:100%;background:#4285f4}.ctx-table{width:100%;font-size:11px}.ctx-table td{padding:5px}`;
  const line = (name,value,tooltip='') => '<div class="ctx-line"><span'+(tooltip?' title="'+esc(tooltip)+'"':'')+'>'+esc(name)+(tooltip?' ⓘ':'')+'</span><span>'+esc(value)+'</span></div>';
  const badge = p => '<span class="ctx-source">'+source(p)+'</span>';
  const unclassifiedExplanation = '来自 Antigravity 原生 usage 字段，数值真实可读取，但当前版本中类别归属尚未确认。';
  const cacheExplanation = '当前数据源未提供可靠 Cache Token 分类。';
  const totalExplanation = 'Total = New Input + Unclassified Input + Cache Read + Cache Write + Output。Reasoning 已包含于 Output，不重复计入。≥ 为已读取下界。';
  const explanation = '输入处理 = New Input + Unclassified Input + Cache Read + Cache Write；缺失字段时 ≥ 仅表示已读取下界。它不等同于当前 Context Window 占用。';
  const info = text => '<button type="button" class="ctx-info" aria-label="统计说明" title="'+esc(text)+'">ⓘ</button>';
  const usageKeys = ['input','unclassified_input','cache_read','cache_write','output','reasoning'];
  function normalizeRequest(runtime,session) {
    const candidate = runtime || session?.latest_request;
    if(!candidate)return null;
    const u={...candidate};
    const stored=[session?.latest_request,...(session?.recent_requests||[])].find(g=>g && u.response_id && g.response_id===u.response_id && g.session_id===u.session_id);
    if(stored && runtime){
      let filled=false;
      for(const key of usageKeys)if(!known(u[key]) && known(stored[key])){u[key]=stored[key];filled=true;}
      if(filled)u.source=(u.source||'React native usage')+' · matched responseId: '+stored.source;
    }
    const exact=values=>values.every(known)?values.reduce((a,b)=>a+b,0):null;
    const observed=values=>values.some(known)?values.reduce((a,b)=>a+(known(b)?b:0),0):null;
    const inputs=[u.input,u.unclassified_input,u.cache_read,u.cache_write];
    u.request_input=exact(inputs);u.observed_input=observed(inputs);
    u.total=exact([...inputs,u.output]);u.observed_total=observed([...inputs,u.output]);u.partial=u.total===null;
    u.cache_hit_rate=u.request_input>0 && u.unclassified_input===0 && (u.cache_read>0 || u.cache_write>0)?u.cache_read/u.request_input:null;
    return u;
  }
  const fieldAmount=(u,key,session)=>session?amount(u?.[key],u?.observed_breakdown?.[key]):tokens(u?.[key]);
  const unclassifiedInfo=(u,session=false)=>known(u?.unclassified_input)||(session&&known(u?.observed_breakdown?.unclassified_input))?unclassifiedExplanation:'当前数据源未提供此字段，未知不等于 0。';
  const requestText=r=>amount(r?.request_input,r?.observed_input)+' 输入处理 → '+tokens(r?.output)+' 输出';
  function breakdownHover(u,session=false) {
    return [['New Input','input'],['Unclassified Input','unclassified_input'],['Cache Read','cache_read'],['Cache Write','cache_write'],['Output','output']].map(([name,key])=>name+'   '+fieldAmount(u,key,session));
  }
  function usageLines(u, session=false) {
    return [
      session?line('Total',amount(u?.total,u?.observed_total),totalExplanation):line('输入处理',amount(u?.request_input,u?.observed_input),explanation),
      line('New Input',fieldAmount(u,'input',session)),
      line('Unclassified Input',fieldAmount(u,'unclassified_input',session),unclassifiedInfo(u,session)),
      line('Cache Read',fieldAmount(u,'cache_read',session),!known(u?.cache_read)?cacheExplanation:''),
      line('Cache Write',fieldAmount(u,'cache_write',session),!known(u?.cache_write)?cacheExplanation:''),
      line('Output',fieldAmount(u,'output',session)),
      line('Reasoning · 包含在 Output 中',fieldAmount(u,'reasoning',session)),
      line('Cache Hit',rate(u?.cache_hit_rate),!known(u?.cache_hit_rate)?cacheExplanation:''),
      session?line('Requests',u?.request_count ?? '—'):'',
      u?.dedup_confidence==='lower'?'<div class="ctx-muted">去重可信度较低 · 已读取 '+esc(u.observed_requests)+' 条 usage</div>':''
    ].join('');
  }
  function footer(s) {
    const r=normalizeRequest(s?.request,s?.session_usage),u=s?.session_usage,hasContext=known(s?.context_tokens);
    const contextValue=amount(r?.request_input,r?.observed_input);
    const contextHover=['最近请求 · '+source(r?.provenance),requestText(r),...breakdownHover(r),'Cache   '+rate(r?.cache_hit_rate),...(!known(r?.cache_hit_rate)?[cacheExplanation]:[]),unclassifiedInfo(r),'',explanation,...(hasContext?['Context '+(s.context_provenance==='Estimated'?'≈ ':'')+tokens(s.context_tokens)+' · '+source(s.context_provenance)]:[]),known(s?.limit_tokens) ? 'Context 上限 '+tokens(s.limit_tokens)+' · Native' : 'Context 上限暂未获取'].join('\n');
    const sessionHover=['本会话累计 · '+source(u?.provenance),'Total   '+amount(u?.total,u?.observed_total),...breakdownHover(u,true),'Reasoning   '+fieldAmount(u,'reasoning',true)+'（已包含于 Output）','Cache   '+rate(u?.cache_hit_rate),...(!known(u?.cache_hit_rate)?[cacheExplanation]:[]),'Requests   '+(u?.request_count ?? '—'),unclassifiedInfo(u,true),...(u?.partial ? [u.note || '数据不完整；≥ 为已读取下界。'] : [])].join('\n');
    return {contextLabel:'Request',contextValue,sessionValue:amount(u?.total,u?.observed_total),contextHover,sessionHover};
  }
  function renderSession(element,u) {
    if(!element)return;
    const open=element.querySelector('details')?.open;
    element.innerHTML='<style>'+css+'</style><div class="ctx"><div class="ctx-value">'+esc(amount(u?.total,u?.observed_total))+' tokens '+badge(u?.provenance)+'</div><div class="ctx-muted">'+(known(u?.total)||known(u?.observed_total)?'Token 已载入':'Token 不可用')+' · '+esc((u?.models||[]).join(' · ')||'模型 —')+' · Cache '+rate(u?.cache_hit_rate)+' · '+esc(u?.request_count ?? '—')+' requests · '+time(u?.started_at)+' → '+time(u?.updated_at)+'</div><details><summary>Token Breakdown</summary>'+usageLines(u,true)+(u?.latest_request ? line('最近请求',requestText(normalizeRequest(u.latest_request)))+line('Model / Time',(u.latest_request.model||'—')+' · '+time(u.latest_request.timestamp)) : '')+'<div class="ctx-muted">'+esc(u?.note||'')+'</div>'+line('来源',(u?.sources||[]).join(' · ')||'Unavailable')+'</details></div>';
    if(open)element.querySelector('details').open=true;
  }
  function render(element,snapshot,timeline=[],compact=false,focus='') {
    if(!element)return;
    const opened=new Set([...element.querySelectorAll('details[open]')].map(n=>n.dataset.section));
    const s=snapshot||{},u=s.session_usage,r=normalizeRequest(s.request,u);
    const context=known(s.context_tokens) ? (s.context_provenance==='Estimated' ? '≈ ' : '')+tokens(s.context_tokens) : '—';
    const percent=known(s.context_tokens)&&s.limit_tokens>0 ? Math.round(s.context_tokens/s.limit_tokens*100) : null;
    const current=context+(percent===null ? '' : ' / '+tokens(s.limit_tokens)+' · '+percent+'%');
    const glance='<div class="ctx-glance"><div>最近请求 '+badge(r?.provenance)+info(explanation)+'</div><div class="ctx-value">'+esc(requestText(r))+'</div><div class="ctx-muted">Cache '+rate(r?.cache_hit_rate)+(!known(r?.cache_hit_rate)?info(cacheExplanation):'')+'</div></div>'+'<div class="ctx-glance"><div>当前 Context '+badge(s.context_provenance)+(s.context_provenance==='Estimated'?info('Antigravity 原生 estimatedTokensUsed；属于估算。'):'')+'</div><div class="ctx-value">'+esc(current)+'</div>'+(known(s.limit_tokens)?'':'<div class="ctx-muted">上限暂未获取</div>')+'</div>'+'<div class="ctx-glance"><div>本会话 '+badge(u?.provenance)+info(totalExplanation)+'</div><div class="ctx-value">'+esc(amount(u?.total,u?.observed_total))+' tokens</div><div class="ctx-muted">'+esc(u?.request_count ?? '—')+' requests</div></div>';
    if(compact){element.innerHTML='<style>'+css+'</style><div class="ctx">'+glance+'<button type="button" class="ctx-link" data-context-details>查看详情</button></div>';element.querySelector('[data-context-details]').addEventListener('click',()=>element.dispatchEvent(new CustomEvent('twoag-context-details',{bubbles:true,composed:true})));return;}
    const groups=s.composition_scope==='active_prompt'?s.composition||[]:[];
    const history=s.loaded_history?.length?s.loaded_history:s.composition_scope==='loaded_history'?s.composition||[]:[];
    const composition=list=>list.filter(g=>g.kind!=='files').map(g=>line(names[g.kind]||g.kind,g.items+' 项 · '+(g.estimated&&known(g.tokens)?'≈ ':'')+tokens(g.tokens))).join('');
    const section=(id,title,body,open=false)=>'<details data-section="'+id+'"'+(open||opened.has(id)||focus===id?' open':'')+'><summary>'+title+'</summary>'+body+'</details>';
    element.innerHTML='<style>'+css+'</style><div class="ctx">'+glance+section('request','Request Telemetry',usageLines(r)+line('Model',r?.model||'—')+line('Time',time(r?.timestamp))+line('来源',r?.source||'Unavailable'),true)+section('session','Session Usage',usageLines(u,true)+line('来源',(u?.sources||[]).join(' · ')||'Unavailable')+(u?.note?'<div class="ctx-muted">'+esc(u.note)+'</div>':''),true)+section('context','Current Context',line('占用',current)+line('来源',source(s.context_provenance))+line('上限 · Native',tokens(s.limit_tokens))+(percent===null?'':'<div class="ctx-track"><div class="ctx-fill" style="width:'+Math.min(100,percent)+'%"></div></div>'))+section('prompt','当前 Prompt 构成',groups.length?composition(groups):'<div class="ctx-muted">Unavailable · 当前 Prompt 未暴露</div>')+section('history','已加载会话历史','<div class="ctx-muted">不等同于当前 Prompt / Context Window。</div>'+composition(history))+(timeline.length<2?'':section('timeline','最近请求变化','<table class="ctx-table"><thead><tr><th>时间</th><th>请求输入</th></tr></thead><tbody>'+timeline.slice(-12).map(p=>'<tr><td>'+time(p.at)+'</td><td>'+tokens(p.tokens)+'</td></tr>').join('')+'</tbody></table>'))+section('files','Files / Details',(s.items||[]).slice(0,100).map(i=>line(i.name,(i.estimated&&known(i.tokens)?'≈ ':'')+tokens(i.tokens))).join('')+'<div class="ctx-muted">文件引用不证明文件仍在当前 Prompt。</div>')+'</div>';
  }
  window.TwoAgContextView={render,renderSession,footer,tokens,amount,rate,normalizeRequest};
})();
