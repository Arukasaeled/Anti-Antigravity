(function createLiveTrace(host) {
  // A read-only view of public execution data. Never retain rawThinking,
  // generator prompts, file contents, credentials, or unfiltered tool args.
  const LIMIT=600, listeners=new Set();
  let events=new Map(), files=[], total=0, convo='', state='disconnected', failure='', updatedAt='', idle=null, historyStart=null;
  let controller=null, iterator=null, generation=0, routeTimer=null, retryTimer=null, active=false;
  const sensitiveKey='(?:access[-_]token|refresh[-_]token|id[-_]token|client[-_]secret|api[-_]key|password|passwd|authorization|token|secret)';
  const redact=value=>String(value??'')
    .replace(/((?:Bearer|Basic)\s+)[\w+.\/-]+=*/gi,'$1<REDACTED>')
    .replace(new RegExp('(["\\\']?'+sensitiveKey+'["\\\']?\\s*[=:]\\s*)(?:"(?:[^"\\\\]|\\\\.)*"|\\\'(?:[^\\\'\\\\]|\\\\.)*\\\'|[^\\s,;}]+)','gi'),'$1<REDACTED>')
    .replace(/(https?:\/\/)[^\s/@]+:[^\s/@]+@/gi,'$1<REDACTED>@').slice(0,10000);
  // Shell syntax is too varied to safely redact one argument in isolation.
  // Omit the entire command when it supplies a credential flag or assignment.
  const command=value=>new RegExp('(?:--?'+sensitiveKey+'(?:\\s|=)|["\\\']?'+sensitiveKey+'["\\\']?\\s*[=:])','i').test(String(value??''))?'[Sensitive command arguments hidden]':redact(value);
  const path=value=>redact(value).replace(/^file:\/\/\//,'').replace(/%20/g,' ');
  const client=()=>window.Wj?.lsClient;
  const conversation=()=>{
    const current=host.conversation.current();
    const route=location.pathname.match(/\/c\/([^/?#]+)/);
    return route?.[1] || current.key?.replace(/^(host|route):/,'') || '';
  };
  const stamp=value=>{
    if(!value)return '';
    if(typeof value==='string')return value;
    const s=Number(value.seconds);return Number.isFinite(s)&&s>0?new Date(s*1000+Number(value.nanos||0)/1e6).toISOString():'';
  };
  const statusName=status=>({0:'unknown',1:'pending',2:'running',3:'done',4:'invalid',5:'cleared',6:'canceled',7:'error',8:'generating',9:'waiting',11:'queued',12:'interrupted'}[status] || 'status '+status);
  function normalize(step,index) {
    const kind=step.step?.case||'unknown', value=step.step?.value||{}, meta=step.metadata||{};
    const e={id:convo+':'+index,index,kind,category:'progress',status:statusName(step.status),createdAt:stamp(meta.createdAt),completedAt:stamp(meta.completedAt),title:kind,text:'',path:'',tools:[],retryCount:Array.isArray(meta.retryInfos)?meta.retryInfos.length:0};
    if(kind==='viewFile') {e.category='read';e.path=path(value.absolutePathUri);e.title=e.path||'Read file';e.text=[value.startLine!==undefined?'Lines '+value.startLine+'–'+value.endLine:'',value.numLines!==undefined?value.numLines+' lines':'',value.isSkillFile?'Skill file':''].filter(Boolean).join(' · ');}
    else if(kind==='codeAction') {e.category='edit';e.title=redact(value.description||meta.toolSummary||'File edit');e.additions=Number(value.diffStats?.additions||0);e.deletions=Number(value.diffStats?.deletions||0);}
    else if(kind==='runCommand') {e.category='command';e.title=command(value.commandLine||'Command');e.text=[value.cwd,value.shellName,value.ranInSandbox===true?'Sandbox':''].filter(Boolean).map(redact).join(' · ');}
    else if(kind==='checkpoint') {e.category='summary';e.title='Checkpoint';e.text=redact(value.sessionSummary||value.codeChangeSummary||'');}
    else if(kind==='errorMessage'||step.error) {e.category='error';e.title=redact(step.error?.shortError||value.message||'Execution error');e.text=redact(step.error?.fullError||'');}
    else if(kind==='userInput') {e.category='user';e.title='User input';}
    else if(kind==='plannerResponse') {
      e.title=redact(meta.toolSummary||value.response?.split('\n')[0]||'Assistant / tool request');
      e.text=redact(value.response||''); // Published response only; thinking fields are deliberately omitted.
      e.duration=Number(value.thinkingDuration?.seconds||0)+Number(value.thinkingDuration?.nanos||0)/1e9;
      e.tools=(value.toolCalls||[]).map(tool=>({name:redact(tool.name),id:tool.id||''}));
      if(e.tools.length)e.category='tool';
    } else if(kind==='systemMessage') {e.title=redact(meta.toolSummary||'System / task update');e.text=redact(value.message||'');}
    else if(kind==='generic') {
      e.title=redact(meta.toolSummary||meta.toolCall?.name||value.toolName||'Tool result');
      e.category='tool';e.outputUri=path(value.result?.fullOutputUri||'');
    }
    // Known argument keys only: no arbitrary tool payloads, file bodies or env.
    let args=value.args;
    if(!args&&meta.toolCall?.argumentsJson)try{args=JSON.parse(meta.toolCall.argumentsJson);}catch(_){}
    if(args&&typeof args==='object') {
      e.path=e.path||path(args.AbsolutePath||args.TargetFile||args.FilePath||args.path||'');
      if(e.category==='tool'&&meta.toolCall?.name==='run_command'){e.category='command';e.title=command(args.CommandLine||e.title);}
    }
    if(step.taskDetails)e.task={id:step.taskDetails.id||'',title:redact(step.taskDetails.title||''),progress:redact(step.taskDetails.progress||''),approval:step.taskDetails.requiresInputApproval===true,logUri:path(step.taskDetails.logUri||'')};
    return e;
  }
  function snapshot() {return {conversation:convo,state,failure,total,retained:events.size,updatedAt,idle,historyStart,events:[...events.values()].sort((a,b)=>a.index-b.index),files:files.map(f=>({...f}))};}
  function notify(){const data=snapshot();for(const fn of listeners)try{fn(data);}catch(_){} }
  function consume(update) {
    const steps=update.mainTrajectoryUpdate?.stepsUpdate;
    if(steps) {
      const rows=steps.steps||[], indices=steps.indices||[];
      total=Number(steps.totalLength??total);
      const offset=Math.max(0,total-rows.length);
      if(historyStart===null)rows.forEach((s,i)=>{const index=indices.length===rows.length?Number(indices[i]):offset+i;events.set(index,normalize(s,index));});
      if(events.size>LIMIT)for(const index of [...events.keys()].sort((a,b)=>a-b).slice(0,events.size-LIMIT))events.delete(index);
    }
    if(typeof update.fullyIdle==='boolean')idle=update.fullyIdle;
    state=idle===true?'idle':idle===false?'working':'connected';updatedAt=new Date().toISOString();failure='';notify();
  }
  function disconnect() {
    generation++;clearTimeout(retryTimer);retryTimer=null;
    controller?.abort();controller=null;
    try{const r=iterator?.return?.();r?.catch?.(()=>{});}catch(_){}iterator=null;
  }
  async function connect(force=false) {
    const id=conversation();if(!force&&id===convo&&controller)return;
    disconnect();const ticket=generation;convo=id;events.clear();files=[];total=0;idle=null;historyStart=null;failure='';
    if(!id){state='no-conversation';notify();return;}
    const c=client();if(!c?.streamAgentStateUpdates){state='unavailable';failure='Native Agent RPC client is unavailable in this host version';notify();return;}
    controller=new AbortController();const signal=controller.signal;state='connecting';notify();
    try {
      const stream=c.streamAgentStateUpdates({conversationId:id,subscriberId:'2ag-trace-'+crypto.randomUUID(),trajectoryVerbosity:2,disableRehydration:true,enableLatencyTelemetry:false},{signal});
      iterator=stream[Symbol.asyncIterator]();
      for await(const frame of {[Symbol.asyncIterator]:()=>iterator}) {
        if(!active||generation!==ticket||signal.aborted)break;
        if(frame.update)consume(frame.update);
      }
      if(active&&generation===ticket&&!signal.aborted){state='disconnected';failure='Native stream ended';notify();retryTimer=setTimeout(()=>connect(true),3000);}
    }catch(error){if(active&&generation===ticket&&!signal.aborted){state='error';failure=redact(error.message);notify();retryTimer=setTimeout(()=>connect(true),5000);}}
  }
  function observe(fn) {
    listeners.add(fn);if(!active){active=true;connect();routeTimer=setInterval(()=>{if(conversation()!==convo)connect();},750);}fn(snapshot());
    return()=>{listeners.delete(fn);if(!listeners.size){active=false;clearInterval(routeTimer);routeTimer=null;disconnect();events.clear();files=[];state='disconnected';}};
  }
  async function loadFiles() {
    const id=convo,c=client();if(!id||!c?.getTrajectoryFileDiffs)throw new Error('Native file changes are unavailable');
    const r=await c.getTrajectoryFileDiffs({conversationId:id},{signal:controller?.signal});
    if(id!==convo||!active)return;
    files=(r.diffs||[]).map(f=>({path:path(f.uri),firstStep:f.firstTouchedStepIndex,lastStep:f.lastTouchedStepIndex,edited:f.hasModelEdited===true,artifact:f.isArtifactFile===true})).slice(0,400);
    notify();
  }
  async function loadEarlier() {
    const id=convo,ticket=generation,c=client();
    if(!id||!c?.getCascadeTrajectorySteps)throw new Error('Native trajectory history is unavailable');
    const first=events.size?Math.min(...events.keys()):total,start=Math.max(0,first-LIMIT),end=Math.min(start+LIMIT,total);
    const windowEvents=new Map();
    for(let offset=start;offset<end;) {
      const r=await c.getCascadeTrajectorySteps({cascadeId:id,stepOffset:offset,trajectoryVerbosity:2},{signal:controller?.signal});
      if(!active||ticket!==generation||id!==convo)return;
      const rows=(r.steps||[]).slice(0,end-offset);if(!rows.length)break;
      rows.forEach((step,i)=>windowEvents.set(offset+i,normalize(step,offset+i)));offset+=rows.length;
    }
    if(active&&ticket===generation&&id===convo){events=windowEvents;historyStart=start;notify();}
  }
  function mount(panel,{i18n,copy,context,toast}) {
    let data=snapshot(),selected=new Set(),filter='all',needle='',visibleLimit=180,renderTimer=null,ended=false;
    const t=(zh,en)=>i18n.language==='zh-CN'?zh:en;
    const node=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n;};
    const style=node('style');style.textContent='.lt-toolbar{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin-bottom:10px}.lt-meta{color:var(--2ag-text-secondary,#9aa0a6);font:11px/1.6 system-ui;overflow-wrap:anywhere}.lt-stats{display:flex;gap:14px;margin:12px 0;font-size:12px}.lt-list{max-height:55vh;overflow:auto}.lt-event{border-left:2px solid var(--2ag-outline-variant,#3c4043);padding:8px 12px;margin:4px 0}.lt-event[data-category=edit]{border-color:#81c995}.lt-event[data-category=error]{border-color:#f28b82}.lt-event[data-status=running]{border-color:#8ab4f8}.lt-event summary{cursor:pointer;overflow-wrap:anywhere;font-size:12px;line-height:1.5}.lt-event pre{white-space:pre-wrap;overflow-wrap:anywhere;font:11px/1.6 monospace;max-height:240px;overflow:auto}.lt-event label{display:flex;gap:6px;align-items:center}.lt-event input{width:auto!important}.lt-files{font:11px/1.7 monospace;overflow-wrap:anywhere}.lt-search{flex:1;min-width:100px}.lt-title{font-size:18px;font-weight:650}.lt-note{font-size:11px;color:var(--2ag-text-secondary,#9aa0a6);line-height:1.7}';
    const title=node('div','lt-title'),status=node('p','lt-meta'),note=node('p','lt-note'),toolbar=node('div','lt-toolbar');
    const search=node('input','lt-search');search.type='search';
    const select=node('select');for(const key of ['all','read','edit','command','tool','error','summary']){const o=node('option','',key);o.value=key;select.append(o);}
    const refresh=node('button'),earlier=node('button'),changes=node('button'),copyButton=node('button'),contextButton=node('button');for(const b of [refresh,earlier,changes,copyButton,contextButton])b.type='button';
    toolbar.append(search,select,earlier,refresh,changes);const stats=node('div','lt-stats'),actions=node('div','lt-toolbar');actions.append(copyButton,contextButton);
    const list=node('div','lt-list'),more=node('button'),fileList=node('details','lt-files'),fileTitle=node('summary'),fileBody=node('div');fileList.append(fileTitle,fileBody);more.type='button';
    panel.append(style,title,status,note,toolbar,stats,actions,list,more,fileList);
    const selectedText=()=>data.events.filter(e=>selected.has(e.id)).map(e=>`### Step ${e.index+1} · ${e.category} · ${e.status}\n${e.title}\n${e.path||''}\n${e.text||''}\n\n> Source: Antigravity native trajectory · ${data.conversation} · ${e.createdAt||'timestamp unavailable'}`).join('\n\n');
    function render() {
      if(ended)return;
      title.textContent='Live Trace';
      const phase={connecting:t('连接中','Connecting'),working:t('执行中','Working'),idle:t('空闲','Idle'),connected:t('已连接','Connected'),'no-conversation':t('请打开会话','Open a conversation'),unavailable:t('宿主不支持','Host unavailable'),error:t('连接失败','Connection error'),disconnected:t('已断开','Disconnected')}[data.state]||data.state;
      status.textContent=phase+' · '+data.retained+'/'+data.total+' '+t('真实步骤','real steps')+(data.updatedAt?' · '+new Date(data.updatedAt).toLocaleTimeString():'')+(data.failure?' · '+data.failure:'');
      note.textContent=t('原生事件流 · 每个窗口最多 600 步，先显示最近 180 条匹配结果。可回看更早窗口；回看时点“回到实时”恢复新步骤。仅显示真实工具、文件操作、错误与公开摘要，不读取隐藏思维。','Native event stream · up to 600 steps per window; displays the latest 180 matching results. Browse earlier windows, then return to live updates. Tools, files, errors and published summaries; hidden reasoning is excluded.');
      search.placeholder=t('搜索步骤、命令、文件','Search steps, commands, files');earlier.textContent=t('更早步骤','Earlier steps');earlier.disabled=data.historyStart===0||!data.events.length;refresh.textContent=data.historyStart===null?t('重连','Reconnect'):t('回到实时','Return to live');changes.textContent=t('文件变更','File changes');copyButton.textContent=t('复制选中','Copy selected');contextButton.textContent=t('送入 Capsule','Add to Capsule');fileTitle.textContent=t('真实文件变更','Observed file changes')+' · '+data.files.length;
      const labels={all:t('全部','All'),read:t('读取','Read'),edit:t('编辑','Edit'),command:t('命令','Command'),tool:t('工具','Tool'),error:t('错误','Error'),summary:t('摘要','Summary')};for(const o of select.options)o.textContent=labels[o.value];
      stats.replaceChildren(...['read','edit','command','error'].map(k=>node('span','',labels[k]+' '+data.events.filter(e=>e.category===k).length)));
      const scroll=list.scrollTop,expanded=new Set([...list.querySelectorAll('details[open]')].map(row=>row.dataset.event));list.replaceChildren();
      const matching=data.events.filter(e=>(filter==='all'||e.category===filter)&&(!needle||[e.title,e.path,e.text,e.tools.map(x=>x.name).join(' ')].join(' ').toLowerCase().includes(needle)));
      more.textContent=t('显示更多','Show more')+' · '+Math.min(visibleLimit,matching.length)+'/'+matching.length;more.hidden=matching.length<=visibleLimit;
      for(const e of matching.slice(-visibleLimit)) {
        const row=node('details','lt-event');row.dataset.category=e.category;row.dataset.status=e.status;row.dataset.event=e.id;row.open=expanded.has(e.id);
        const summary=node('summary','',`${String(e.index+1).padStart(3,'0')} · ${labels[e.category]||e.kind} · ${e.status} · ${e.title}`);
        const meta=node('div','lt-meta',[e.createdAt,e.path,e.retryCount?'retries: '+e.retryCount:'',e.category==='edit'?`+${e.additions} −${e.deletions}`:'',e.duration?'duration: '+e.duration.toFixed(1)+'s':'',e.tools.map(x=>x.name).join(', ')].filter(Boolean).join(' · '));
        const checkbox=node('input');checkbox.type='checkbox';checkbox.checked=selected.has(e.id);checkbox.onchange=()=>{if(checkbox.checked)selected.add(e.id);else selected.delete(e.id);};const label=node('label','',t('选中此步骤','Select this step'));label.prepend(checkbox);
        row.append(summary,meta,label);if(e.text)row.append(node('pre','',e.text));if(e.task)row.append(node('p','lt-meta',[e.task.title,e.task.progress,e.task.approval?t('等待批准','Awaiting approval'):'',e.task.logUri].filter(Boolean).join(' · ')));if(e.outputUri)row.append(node('p','lt-meta',t('输出文件：','Output file: ')+e.outputUri));
        list.append(row);
      }
      if(!list.children.length)list.append(node('p','lt-meta',t('暂无匹配事件。请打开有执行记录的会话。','No matching events. Open a conversation with execution history.')));
      list.scrollTop=scroll;fileBody.replaceChildren(...data.files.map(f=>node('div','',`${f.path} · steps ${Number(f.firstStep)+1}–${Number(f.lastStep)+1}${f.artifact?' · artifact':''}`)));
    }
    const unsubscribe=observe(next=>{if(next.conversation!==data.conversation||next.historyStart!==data.historyStart){selected.clear();visibleLimit=180;}data=next;clearTimeout(renderTimer);renderTimer=setTimeout(render,100);});
    const languageOff=i18n.onChange(render);
    search.oninput=()=>{needle=search.value.trim().toLowerCase();render();};select.onchange=()=>{filter=select.value;render();};refresh.onclick=()=>connect(true);
    more.onclick=()=>{visibleLimit=Math.min(LIMIT,visibleLimit+180);render();};
    earlier.onclick=async()=>{earlier.disabled=true;try{await loadEarlier();render();}catch(e){toast(e.message);}finally{if(!ended)render();}};
    changes.onclick=async()=>{changes.disabled=true;try{await loadFiles();fileList.open=true;render();}catch(e){toast(e.message);}finally{changes.disabled=false;}};
    copyButton.onclick=async()=>{const text=selectedText();if(!text){toast(t('请先选择步骤','Select steps first'));return;}try{await copy(text);toast(t('已复制真实事件','Observed events copied'));}catch(e){toast(e.message);}};
    contextButton.onclick=()=>{const text=selectedText();if(!text){toast(t('请先选择步骤','Select steps first'));return;}context.add({id:'trace-'+Date.now(),title:'Live Trace · '+data.conversation,text});};
    render();return()=>{ended=true;clearTimeout(renderTimer);unsubscribe();languageOff();panel.replaceChildren();};
  }
  return {observe,snapshot,mount,loadFiles,loadEarlier,dispose(){active=false;listeners.clear();clearInterval(routeTimer);disconnect();events.clear();files=[];}};
})
