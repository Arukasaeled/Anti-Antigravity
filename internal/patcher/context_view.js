(() => {
  'use strict';
  const labels = {
  "Native Antigravity usage value; its category is unconfirmed in this version.": "来自 Antigravity 原生 usage 字段，数值真实可读取，但当前版本中类别归属尚未确认。",
  "This source does not provide reliable cache token categories.": "当前数据源未提供可靠 Cache Token 分类。",
  "Total = New Input + Unclassified Input + Cache Read + Cache Write + Output. Reasoning is included in Output and is not added again. ≥ marks an observed lower bound.": "Total = New Input + Unclassified Input + Cache Read + Cache Write + Output。Reasoning 已包含于 Output，不重复计入。≥ 为已读取下界。",
  "Processed input = New Input + Unclassified Input + Cache Read + Cache Write. Missing fields preserve an observed ≥ lower bound. This is not the current Context Window usage.": "输入处理 = New Input + Unclassified Input + Cache Read + Cache Write；缺失字段时 ≥ 仅表示已读取下界。它不等同于当前 Context Window 占用。",
  "<button type=\"button\" class=\"ctx-info\" aria-label=\"Usage details\" title=\"": "<button type=\"button\" class=\"ctx-info\" aria-label=\"统计说明\" title=\"",
  "This field is unavailable. Unknown does not mean zero.": "当前数据源未提供此字段，未知不等于 0。",
  " processed → ": " 输入处理 → ",
  " output": " 输出",
  "Processed input": "输入处理",
  "Reasoning · Included in Output": "Reasoning · 包含在 Output 中",
  "<div class=\"ctx-muted\">Lower dedup confidence · Observed ": "<div class=\"ctx-muted\">去重可信度较低 · 已读取 ",
  " usage records</div>": " 条 usage</div>",
  "Latest request · ": "最近请求 · ",
  "Context limit ": "Context 上限 ",
  "Context limit unavailable": "Context 上限暂未获取",
  "Session lifetime · ": "本会话累计 · ",
  "(included in Output)": "（已包含于 Output）",
  "Incomplete data; ≥ marks the observed lower bound.": "数据不完整；≥ 为已读取下界。",
  "Token data loaded": "Token 已载入",
  "Token data unavailable": "Token 不可用",
  "Model —": "模型 —",
  "Latest request": "最近请求",
  "Source": "来源",
  "Selected request · Step ": "所选请求 · Step ",
  "<div class=\"ctx-glance\"><div>Current Context ": "<div class=\"ctx-glance\"><div>当前 Context ",
  "Native Antigravity estimatedTokensUsed; an estimate.": "Antigravity 原生 estimatedTokensUsed；属于估算。",
  "<div class=\"ctx-muted\">Limit unavailable</div>": "<div class=\"ctx-muted\">上限暂未获取</div>",
  "<div class=\"ctx-glance\"><div>Session lifetime ": "<div class=\"ctx-glance\"><div>本会话 ",
  "<button type=\"button\" class=\"ctx-link\" data-context-details>View details</button></div>": "<button type=\"button\" class=\"ctx-link\" data-context-details>查看详情</button></div>",
  " items · ": " 项 · ",
  "Usage": "占用",
  "Limit · Native": "上限 · Native",
  "Current prompt composition": "当前 Prompt 构成",
  "<div class=\"ctx-muted\">Unavailable · Current prompt not exposed</div>": "<div class=\"ctx-muted\">Unavailable · 当前 Prompt 未暴露</div>",
  "Loaded conversation history": "已加载会话历史",
  "<div class=\"ctx-muted\">Not equivalent to the current Prompt / Context Window.</div>": "<div class=\"ctx-muted\">不等同于当前 Prompt / Context Window。</div>",
  "Latest request changes": "最近请求变化",
  "<table class=\"ctx-table\"><thead><tr><th>Time</th><th>Processed input</th></tr></thead><tbody>": "<table class=\"ctx-table\"><thead><tr><th>时间</th><th>请求输入</th></tr></thead><tbody>",
  "<div class=\"ctx-muted\">File references do not prove a file remains in the current prompt.</div>": "<div class=\"ctx-muted\">文件引用不证明文件仍在当前 Prompt。</div>"
};
  let uiLanguage='zh-CN';
  const translate=english=>uiLanguage==='en-US'?english:(labels[english]??english);
  // Reader-owned provenance notes only; raw errors, paths and tool content
  // remain unchanged. Keep this catalog with the shared Context presentation.
  const nativeNotes = {
    '原生会话库不可读或未找到；未使用 Runtime 切片冒充会话累计。':'The native conversation store is unreadable or missing; Runtime slices are not used as session totals.',
    '≥ 表示已读取的原生 Token 下界；字段 #1 单列为 Unclassified Input，类别归属尚未确认。Cache Write 未提供；Reasoning 已包含在 Output 中。':'≥ marks the observed native token lower bound. Field #1 is shown as Unclassified Input because its category is unconfirmed. Cache Write is unavailable; Reasoning is included in Output.',
    ' 无 responseId 的 usage 未计入累计下界。':' Usage records without responseId are excluded from the observed total.'
  };
  const nativeNote = value => {
    const text=String(value??'');
    if(uiLanguage!=='en-US')return text;
    if(nativeNotes[text])return nativeNotes[text];
    const suffix=' 无 responseId 的 usage 未计入累计下界。';
    const base=text.endsWith(suffix)?text.slice(0,-suffix.length):text;
    return nativeNotes[base]?nativeNotes[base]+(base===text?'':nativeNotes[suffix]):text;
  };

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
  const unclassifiedExplanation = "Native Antigravity usage value; its category is unconfirmed in this version.";
  const cacheExplanation = "This source does not provide reliable cache token categories.";
  const totalExplanation = "Total = New Input + Unclassified Input + Cache Read + Cache Write + Output. Reasoning is included in Output and is not added again. ≥ marks an observed lower bound.";
  const explanation = "Processed input = New Input + Unclassified Input + Cache Read + Cache Write. Missing fields preserve an observed ≥ lower bound. This is not the current Context Window usage.";
  const info = text => translate("<button type=\"button\" class=\"ctx-info\" aria-label=\"Usage details\" title=\"")+esc(text)+'">ⓘ</button>';
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
  const unclassifiedInfo=(u,session=false)=>known(u?.unclassified_input)||(session&&known(u?.observed_breakdown?.unclassified_input))?translate(unclassifiedExplanation):translate("This field is unavailable. Unknown does not mean zero.");
  const requestText=r=>amount(r?.request_input,r?.observed_input)+translate(" processed → ")+tokens(r?.output)+translate(" output");
  function breakdownHover(u,session=false) {
    return [['New Input','input'],['Unclassified Input','unclassified_input'],['Cache Read','cache_read'],['Cache Write','cache_write'],['Output','output']].map(([name,key])=>name+'   '+fieldAmount(u,key,session));
  }
  function usageLines(u, session=false) {
    return [
      session?line('Total',amount(u?.total,u?.observed_total),translate(totalExplanation)):line(translate("Processed input"),amount(u?.request_input,u?.observed_input),translate(explanation)),
      line('New Input',fieldAmount(u,'input',session)),
      line('Unclassified Input',fieldAmount(u,'unclassified_input',session),unclassifiedInfo(u,session)),
      line('Cache Read',fieldAmount(u,'cache_read',session),!known(u?.cache_read)?translate(cacheExplanation):''),
      line('Cache Write',fieldAmount(u,'cache_write',session),!known(u?.cache_write)?translate(cacheExplanation):''),
      line('Output',fieldAmount(u,'output',session)),
      line(translate("Reasoning · Included in Output"),fieldAmount(u,'reasoning',session)),
      line('Cache Hit',rate(u?.cache_hit_rate),!known(u?.cache_hit_rate)?translate(cacheExplanation):''),
      session?line('Requests',u?.request_count ?? '—'):'',
      u?.dedup_confidence==='lower'?translate("<div class=\"ctx-muted\">Lower dedup confidence · Observed ")+esc(u.observed_requests)+translate(" usage records</div>"):''
    ].join('');
  }
  function footer(s,language='zh-CN') {
    uiLanguage=language;
    const r=normalizeRequest(s?.request,s?.session_usage),u=s?.session_usage,hasContext=known(s?.context_tokens);
    const contextValue=amount(r?.request_input,r?.observed_input);
    const contextHover=[translate("Latest request · ")+source(r?.provenance),requestText(r),...breakdownHover(r),'Cache   '+rate(r?.cache_hit_rate),...(!known(r?.cache_hit_rate)?[translate(cacheExplanation)]:[]),unclassifiedInfo(r),'',translate(explanation),...(hasContext?['Context '+(s.context_provenance==='Estimated'?'≈ ':'')+tokens(s.context_tokens)+' · '+source(s.context_provenance)]:[]),known(s?.limit_tokens) ? translate("Context limit ")+tokens(s.limit_tokens)+' · Native' : translate("Context limit unavailable")].join('\n');
    const sessionHover=[translate("Session lifetime · ")+source(u?.provenance),'Total   '+amount(u?.total,u?.observed_total),...breakdownHover(u,true),'Reasoning   '+fieldAmount(u,'reasoning',true)+translate("(included in Output)"),'Cache   '+rate(u?.cache_hit_rate),...(!known(u?.cache_hit_rate)?[translate(cacheExplanation)]:[]),'Requests   '+(u?.request_count ?? '—'),unclassifiedInfo(u,true),...(u?.partial ? [nativeNote(u.note) || translate("Incomplete data; ≥ marks the observed lower bound.")] : [])].join('\n');
    return {contextLabel:'Request',contextValue,sessionValue:amount(u?.total,u?.observed_total),contextHover,sessionHover};
  }
  function renderSession(element,u,language='zh-CN') {
    uiLanguage=language;
    if(!element)return;
    const open=element.querySelector('details')?.open;
    element.innerHTML='<style>'+css+'</style><div class="ctx"><div class="ctx-value">'+esc(amount(u?.total,u?.observed_total))+' tokens '+badge(u?.provenance)+'</div><div class="ctx-muted">'+(known(u?.total)||known(u?.observed_total)?translate("Token data loaded"):translate("Token data unavailable"))+' · '+esc((u?.models||[]).join(' · ')||translate("Model —"))+' · Cache '+rate(u?.cache_hit_rate)+' · '+esc(u?.request_count ?? '—')+' requests · '+time(u?.started_at)+' → '+time(u?.updated_at)+'</div><details><summary>Token Breakdown</summary>'+usageLines(u,true)+(u?.latest_request ? line(translate("Latest request"),requestText(normalizeRequest(u.latest_request)))+line('Model / Time',(u.latest_request.model||'—')+' · '+time(u.latest_request.timestamp)) : '')+'<div class="ctx-muted">'+esc(nativeNote(u?.note))+'</div>'+line(translate("Source"),(u?.sources||[]).join(' · ')||'Unavailable')+'</details></div>';
    if(open)element.querySelector('details').open=true;
  }
  function render(element,snapshot,timeline=[],compact=false,focus='',language='zh-CN') {
    uiLanguage=language;
    if(!element)return;
    const opened=new Set([...element.querySelectorAll('details[open]')].map(n=>n.dataset.section));
    const s=snapshot||{},u=s.session_usage,r=normalizeRequest(s.request,u);
    const context=known(s.context_tokens) ? (s.context_provenance==='Estimated' ? '≈ ' : '')+tokens(s.context_tokens) : '—';
    const percent=known(s.context_tokens)&&s.limit_tokens>0 ? Math.round(s.context_tokens/s.limit_tokens*100) : null;
    const current=context+(percent===null ? '' : ' / '+tokens(s.limit_tokens)+' · '+percent+'%');
    const glance='<div class="ctx-glance"><div>'+ (s.request_selection?(language==='en-US'?'Selected request · Step ':translate("Selected request · Step "))+esc(s.request_selection.stepIndex):(language==='en-US'?'Latest request':translate("Latest request"))) +' '+badge(r?.provenance)+info(translate(explanation))+'</div><div class="ctx-value">'+esc(requestText(r))+'</div><div class="ctx-muted">Cache '+rate(r?.cache_hit_rate)+(!known(r?.cache_hit_rate)?info(translate(cacheExplanation)):'')+'</div></div>'+translate("<div class=\"ctx-glance\"><div>Current Context ")+badge(s.context_provenance)+(s.context_provenance==='Estimated'?info(translate("Native Antigravity estimatedTokensUsed; an estimate.")):'')+'</div><div class="ctx-value">'+esc(current)+'</div>'+(known(s.limit_tokens)?'':translate("<div class=\"ctx-muted\">Limit unavailable</div>"))+'</div>'+translate("<div class=\"ctx-glance\"><div>Session lifetime ")+badge(u?.provenance)+info(translate(totalExplanation))+'</div><div class="ctx-value">'+esc(amount(u?.total,u?.observed_total))+' tokens</div><div class="ctx-muted">'+esc(u?.request_count ?? '—')+' requests</div></div>';
    if(compact){element.innerHTML='<style>'+css+'</style><div class="ctx">'+glance+translate("<button type=\"button\" class=\"ctx-link\" data-context-details>View details</button></div>");element.querySelector('[data-context-details]').addEventListener('click',()=>element.dispatchEvent(new CustomEvent('twoag-context-details',{bubbles:true,composed:true})));return;}
    const groups=s.composition_scope==='active_prompt'?s.composition||[]:[];
    const history=s.loaded_history?.length?s.loaded_history:s.composition_scope==='loaded_history'?s.composition||[]:[];
    const composition=list=>list.filter(g=>g.kind!=='files').map(g=>line(names[g.kind]||g.kind,g.items+translate(" items · ")+(g.estimated&&known(g.tokens)?'≈ ':'')+tokens(g.tokens))).join('');
    const section=(id,title,body,open=false)=>'<details data-section="'+id+'"'+(open||opened.has(id)||focus===id?' open':'')+'><summary>'+title+'</summary>'+body+'</details>';
    element.innerHTML='<style>'+css+'</style><div class="ctx">'+glance+section('request','Request Telemetry',usageLines(r)+(s.request_selection?line('Step',s.request_selection.stepIndex)+line('responseId',r?.response_id||'—'):'')+line('Model',r?.model||'—')+line('Time',time(r?.timestamp))+line(translate("Source"),r?.source||'Unavailable'),true)+section('session','Session Usage',usageLines(u,true)+line(translate("Source"),(u?.sources||[]).join(' · ')||'Unavailable')+(u?.note?'<div class="ctx-muted">'+esc(nativeNote(u.note))+'</div>':''),true)+section('context','Current Context',line(translate("Usage"),current)+line(translate("Source"),source(s.context_provenance))+line(translate("Limit · Native"),tokens(s.limit_tokens))+(percent===null?'':'<div class="ctx-track"><div class="ctx-fill" style="width:'+Math.min(100,percent)+'%"></div></div>'))+section('prompt',translate("Current prompt composition"),groups.length?composition(groups):translate("<div class=\"ctx-muted\">Unavailable · Current prompt not exposed</div>"))+section('history',translate("Loaded conversation history"),translate("<div class=\"ctx-muted\">Not equivalent to the current Prompt / Context Window.</div>")+composition(history))+(timeline.length<2?'':section('timeline',translate("Latest request changes"),translate("<table class=\"ctx-table\"><thead><tr><th>Time</th><th>Processed input</th></tr></thead><tbody>")+timeline.slice(-12).map(p=>'<tr><td>'+time(p.at)+'</td><td>'+tokens(p.tokens)+'</td></tr>').join('')+'</tbody></table>'))+section('files','Files / Details',(s.items||[]).slice(0,100).map(i=>line(i.name,(i.estimated&&known(i.tokens)?'≈ ':'')+tokens(i.tokens))).join('')+translate("<div class=\"ctx-muted\">File references do not prove a file remains in the current prompt.</div>"))+'</div>';
  }
  window.TwoAgContextView={render,renderSession,footer,tokens,amount,rate,normalizeRequest};
})();
