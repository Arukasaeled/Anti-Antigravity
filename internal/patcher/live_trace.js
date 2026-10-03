(function createLiveTrace(host) {
  // A read-only view of public execution data. Never retain rawThinking,
  // generator prompts, file contents, credentials, or unfiltered tool args.
  const LIMIT=10000, PAGE=600, listeners=new Set();
  let sessions=[], diffCache=new Map();
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
      // Duration comes from public step timestamps, never private reasoning metadata.
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
      const tool=meta.toolCall?.name||value.toolName||'';
      if(['view_file','read_file','view_file_outline'].includes(tool))e.category='read';
      if(['write_to_file','replace_file_content','multi_replace_file_content'].includes(tool))e.category='edit';
      if(['grep_search','find_by_name','list_dir','search_web'].includes(tool))e.category='search';
    }
    if(step.taskDetails)e.task={id:step.taskDetails.id||'',title:redact(step.taskDetails.title||''),progress:redact(step.taskDetails.progress||''),approval:step.taskDetails.requiresInputApproval===true,logUri:path(step.taskDetails.logUri||'')};
    if(step.error&&e.category!=='error')e.text=redact(e.text+'\n'+(step.error.shortError||'')+'\n'+(step.error.fullError||''));
    e.eventKind=({read:'file-read',edit:'file-write',command:'command',error:'error',summary:'checkpoint',user:'user',tool:'tool',search:'search'})[e.category] || (kind==='plannerResponse'?'assistant':'system');
    e.tool=e.tools[0]?.name || redact(meta.toolCall?.name||'');
    e.timestamp=e.createdAt;e.summary=e.text;e.file=e.path;
    const duration=(Date.parse(e.completedAt)-Date.parse(e.createdAt))/1000;
    if(Number.isFinite(duration)&&duration>=0)e.duration=duration;
    e.phase=['read','search'].includes(e.category)?'Research':e.category==='edit'?'Implementation':e.category==='command'?( /(?:\b(?:test|pytest|lint|vet|check)\b)/i.test(e.title)?'Validation':/\b(?:build|pack|compile)\b/i.test(e.title)?'Build':'Command'):e.category==='error'?'Error':e.category==='summary'?'Checkpoint':e.category==='tool'?'Tool':'Conversation';
    e.phaseSource='event-kind'; // A classification of observed work, not an inferred plan.
    return e;
  }
  function snapshot() {return {conversation:convo,state,failure,total,retained:events.size,updatedAt,idle,historyStart,events:[...events.values()].sort((a,b)=>a.index-b.index),files:files.map(f=>({...f})),sessions:sessions.map(s=>({...s})),cacheLimit:LIMIT};}
  function notify(){const data=snapshot();for(const fn of listeners)try{fn(data);}catch(_){} }
  function consume(update) {
    const steps=update.mainTrajectoryUpdate?.stepsUpdate;
    if(steps) {
      const rows=steps.steps||[], indices=steps.indices||[];
      total=Number(steps.totalLength??total);
      const offset=Math.max(0,total-rows.length);
      if(historyStart===null)rows.slice(-LIMIT).forEach((s,j)=>{const i=j+Math.max(0,rows.length-LIMIT);const index=indices.length===rows.length?Number(indices[i]):offset+i;events.set(index,normalize(s,index));});
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
    disconnect();const ticket=generation;convo=id;events.clear();files=[];diffCache.clear();total=0;idle=null;historyStart=null;failure='';
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
    return()=>{listeners.delete(fn);if(!listeners.size){active=false;clearInterval(routeTimer);routeTimer=null;disconnect();events.clear();files=[];sessions=[];diffCache.clear();state='disconnected';}};
  }
  async function loadFiles() {
    const id=convo,c=client();if(!id||!c?.getTrajectoryFileDiffs)throw new Error('Native file changes are unavailable');
    const r=await c.getTrajectoryFileDiffs({conversationId:id},{signal:controller?.signal});
    if(id!==convo||!active)return;
    diffCache.clear();
    files=(r.diffs||[]).slice(0,400).map(f=>({path:path(f.uri),firstStep:f.firstTouchedStepIndex,lastStep:f.lastTouchedStepIndex,edited:f.hasModelEdited===true,artifact:f.isArtifactFile===true,noChange:f.isNoop===true,diffAvailable:typeof f.originalContent==='string'&&typeof f.modifiedContent==='string'&&(f.originalContent!==''||f.modifiedContent!==''),...diffStats(f)}));
    notify();
  }
  async function loadEarlier() {
    const id=convo,ticket=generation,c=client();
    if(!id||!c?.getCascadeTrajectorySteps)throw new Error('Native trajectory history is unavailable');
    const first=events.size?Math.min(...events.keys()):total,start=Math.max(0,first-PAGE),end=Math.min(start+PAGE,total);
    const windowEvents=new Map();
    for(let offset=start;offset<end;) {
      const r=await c.getCascadeTrajectorySteps({cascadeId:id,stepOffset:offset,trajectoryVerbosity:2},{signal:controller?.signal});
      if(!active||ticket!==generation||id!==convo)return;
      const rows=(r.steps||[]).slice(0,end-offset);if(!rows.length)break;
      rows.forEach((step,i)=>windowEvents.set(offset+i,normalize(step,offset+i)));offset+=rows.length;
    }
    if(active&&ticket===generation&&id===convo){events=windowEvents;historyStart=start;notify();}
  }
  // Diff source is transient. Only bounded, redacted hunks enter the cache.
  const privateFile=value=>/(?:^|[\\/])(?:\.env(?:\.[^\\/]*)?|credentials?[^\\/]*|[^\\/]*\.(?:pem|key|p12|pfx)|vault)(?:[\\/]|$)/i.test(value);
  function diffParts(f) {
    const before=f.originalContent,after=f.modifiedContent;
    if(typeof before!=='string'||typeof after!=='string'||privateFile(path(f.uri))||before.length+after.length>250000)return null;
    if(before===after)return f.isNoop===true?[]:null;
    // An empty before snapshot with a content reference is missing data, not a new file.
    if(before===''&&f.originalContentHash)return null;
    const a=before?before.replace(/\r\n/g,'\n').split('\n'):[],b=after?after.replace(/\r\n/g,'\n').split('\n'):[];
    if(a.at(-1)==='')a.pop();if(b.at(-1)==='')b.pop();
    let prefix=0,suffix=0;while(prefix<a.length&&prefix<b.length&&a[prefix]===b[prefix])prefix++;
    while(suffix<a.length-prefix&&suffix<b.length-prefix&&a[a.length-1-suffix]===b[b.length-1-suffix])suffix++;
    const x=a.slice(prefix,a.length-suffix),y=b.slice(prefix,b.length-suffix);
    if(x.length*y.length>1000000||x.length+y.length>6000)return null;
    const width=y.length+1,dp=new Uint16Array((x.length+1)*width);
    for(let i=x.length-1;i>=0;i--)for(let j=y.length-1;j>=0;j--)dp[i*width+j]=x[i]===y[j]?1+dp[(i+1)*width+j+1]:Math.max(dp[(i+1)*width+j],dp[i*width+j+1]);
    const rows=a.slice(0,prefix).map(text=>({sign:' ',text}));let i=0,j=0;
    while(i<x.length||j<y.length){if(i<x.length&&j<y.length&&x[i]===y[j]){rows.push({sign:' ',text:x[i++]});j++;}else if(j<y.length&&(i===x.length||dp[i*width+j+1]>=dp[(i+1)*width+j]))rows.push({sign:'+',text:y[j++]});else rows.push({sign:'-',text:x[i++]});}
    rows.push(...a.slice(a.length-suffix).map(text=>({sign:' ',text})));return rows;
  }
  function diffStats(f){
    const rows=diffParts(f);return rows?{additions:rows.filter(r=>r.sign==='+').length,deletions:rows.filter(r=>r.sign==='-').length}:{};
  }
  async function loadDiff(file) {
    if(diffCache.has(file))return diffCache.get(file);
    if(privateFile(file))throw new Error('Sensitive file content is excluded');
    const id=convo,ticket=generation,c=client();
    const r=await c.getTrajectoryFileDiffs({conversationId:id},{signal:controller?.signal});
    if(id!==convo||ticket!==generation||!active)return '';
    const f=(r.diffs||[]).find(f=>path(f.uri)===file);if(!f)throw new Error('No native diff available');
    const rows=diffParts(f);if(!rows)throw new Error('Native diff is unavailable or exceeds the safe preview size');
    let old=1,next=1;for(const row of rows){row.old=old;row.next=next;if(row.sign!=='+')old++;if(row.sign!=='-')next++;}
    const ranges=[];for(let i=0;i<rows.length;i++)if(rows[i].sign!==' '){const start=Math.max(0,i-3),end=Math.min(rows.length,i+4),last=ranges.at(-1);if(last&&start<=last.end)last.end=end;else ranges.push({start,end});}
    const hunks=ranges.slice(0,80).map(({start,end})=>{const chunk=rows.slice(start,end),oldCount=chunk.filter(r=>r.sign!=='+').length,newCount=chunk.filter(r=>r.sign!=='-').length;return `@@ -${chunk[0].old},${oldCount} +${chunk[0].next},${newCount} @@\n`+chunk.map(r=>r.sign+redact(r.text)).join('\n');});
    const rawDiff=`--- ${file}\n+++ ${file}\n`+hunks.join('\n');
    const diff=redact(rawDiff)+(ranges.length>80||rawDiff.length>10000?'\n[Preview truncated]':'');
    diffCache.set(file,diff);if(diffCache.size>6)diffCache.delete(diffCache.keys().next().value);return diff;
  }
  async function loadSessions(){
    const c=client();if(!c?.getAllCascadeTrajectories)return [];
    const ticket=generation,r=await c.getAllCascadeTrajectories({},{signal:controller?.signal});
    if(!active||ticket!==generation)return [];
    sessions=Object.entries(r.trajectorySummaries||{}).map(([id,s])=>({id,title:redact(s.summary||id).slice(0,120),steps:Number(s.stepCount||0),createdAt:stamp(s.createdTime),updatedAt:stamp(s.lastModifiedTime)})).sort((a,b)=>String(b.updatedAt).localeCompare(String(a.updatedAt))).slice(0,12);notify();return sessions;
  }
  function overview(data=snapshot()){
    const dates=data.events.flatMap(e=>[Date.parse(e.createdAt),Date.parse(e.completedAt)]).filter(Number.isFinite);
    const stages=[];
    for(const e of data.events){let stage=stages.at(-1);if(!stage||stage.name!==e.phase){stage={name:e.phase,first:e.id,events:0,start:null,end:null};stages.push(stage);}stage.events++;for(const value of [e.createdAt,e.completedAt]){const ms=Date.parse(value);if(Number.isFinite(ms)){stage.start=stage.start===null?ms:Math.min(stage.start,ms);stage.end=stage.end===null?ms:Math.max(stage.end,ms);}}}
    return {duration:dates.length?(Math.max(...dates)-Math.min(...dates))/1000:null,commands:data.events.filter(e=>e.category==='command').length,errors:data.events.filter(e=>e.category==='error'||e.status==='error'||e.status==='invalid'||e.status==='interrupted').length,files:data.files.filter(f=>!f.noChange).length,stages:stages.map(s=>({...s,duration:s.start!==null?(s.end-s.start)/1000:null})),partial:data.retained<data.total};
  }
  function matches(e,query){
    const text=[e.title,e.text,e.path,e.tool,e.eventKind,e.kind,e.phase,e.status].join(' ').toLowerCase();
    return String(query).toLowerCase().split(/\s+/).filter(Boolean).every(token=>token.startsWith('file:')?e.path.toLowerCase().includes(token.slice(5)):token.startsWith('kind:')?[e.kind.toLowerCase(),e.eventKind,e.category].includes(token.slice(5)):text.includes(token));
  }
  function exportTrace(format='markdown',data=snapshot()){
    const info=overview(data),focus=data.events.filter(e=>e.category==='error'||e.category==='summary'||e.status==='error').slice(-60);
    const safe={session:data.conversation,duration_seconds:info.duration,total_steps:data.total,loaded_steps:data.retained,partial:info.partial,commands:info.commands,errors:info.errors,files:data.files.map(f=>({...f})),stages:info.stages,events:focus.map(e=>({id:e.id,timestamp:e.timestamp,kind:e.eventKind,status:e.status,title:redact(e.title),summary:redact(e.text).slice(0,2000),file:e.file,duration:e.duration}))};
    if(format==='json')return JSON.stringify(safe,null,2);
    return `# Live Trace\n\nSession: ${safe.session}\nDuration (loaded range): ${safe.duration_seconds??'Unavailable'}s\nSteps: ${safe.loaded_steps}/${safe.total_steps}\nCommands: ${safe.commands}\nErrors: ${safe.errors}\nFiles: ${safe.files.length}\n\n## Files\n`+safe.files.map(f=>`- ${f.path}${f.additions!==undefined?' +'+f.additions+' -'+f.deletions:' (line counts unavailable)'}`).join('\n')+'\n\n## Checkpoints / Errors\n'+safe.events.map(e=>`### ${e.kind} · ${e.timestamp||'Timestamp unavailable'}\n${e.title}\n${e.summary}\n${e.file||''}`).join('\n\n');
  }
  function mount(panel,{i18n,copy,context,toast,openFile}) {
    let fileSignature='',data=snapshot(),selected=new Set(),filter='all',needle='',phaseFilter='',follow=true,ended=false,renderTimer=null,chosen=null,matching=[];
    const ROW=60,OVERSCAN=6;
    const t=(zh,en)=>i18n.language==='zh-CN'?zh:en;
    const node=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n;};
    const button=(zh,en,fn)=>{const b=node('button','',t(zh,en));b.type='button';b.onclick=fn;b.dataset.zh=zh;b.dataset.en=en;return b;};
    const duration=s=>s===null||s===undefined?'—':s>=60?Math.floor(s/60)+'m '+Math.round(s%60)+'s':s.toFixed(1)+'s';
    const label={all:['全部','All'],read:['文件','Files'],edit:['编辑','Edits'],command:['命令','Commands'],tool:['工具','Tools'],error:['错误 / 中断','Errors / interruptions'],summary:['检查点','Checkpoints'],user:['用户','User'],progress:['公开输出','Public output'],search:['搜索','Search']};
    const phaseLabel={Research:['读取 / 调查','Research'],Implementation:['修改','Implementation'],Validation:['验证','Validation'],Build:['构建','Build'],Command:['命令','Command'],Error:['错误','Error'],Checkpoint:['检查点','Checkpoint'],Tool:['工具','Tool'],Conversation:['会话','Conversation']};
    const states={running:['执行中','Running'],done:['完成','Done'],pending:['待执行','Pending'],generating:['生成中','Generating'],waiting:['等待','Waiting'],queued:['排队','Queued'],error:['失败','Error'],invalid:['无效','Invalid'],interrupted:['中断','Interrupted'],canceled:['取消','Canceled'],unknown:['未知','Unknown'],cleared:['已清除','Cleared']};
    const style=node('style');style.textContent='.lt-toolbar{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin:8px 0}.lt-meta,.lt-note{color:var(--2ag-text-secondary,#9aa0a6);font:11px/1.6 system-ui;overflow-wrap:anywhere}.lt-stats{font:12px/1.7 system-ui;padding:8px 0}.lt-list{height:45vh;min-height:180px;max-height:500px;overflow:auto;position:relative;overflow-anchor:none}.lt-window{position:relative}.lt-event{box-sizing:border-box;position:absolute;left:0;right:0;height:60px;display:flex;gap:8px;align-items:center;border-left:2px solid #68707855;padding:6px 8px}.lt-event[data-category=error]{border-color:#f28b82}.lt-event[data-status=running],.lt-event[data-status=generating]{border-color:#8ab4f8;background:#8ab4f811}.lt-event button{background:transparent!important;text-align:left;border:0!important;min-width:0;flex:1;padding:0!important}.lt-event .lt-line{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:12px/1.5 system-ui}.lt-event input{width:auto!important;flex:none}.lt-detail pre,.lt-files pre{font:11px/1.7 monospace;white-space:pre-wrap;max-height:300px;overflow:auto;overflow-wrap:anywhere}.lt-current{padding:10px 12px;background:#8ab4f811;border-left:2px solid #8ab4f8;font:12px/1.7 system-ui;overflow-wrap:anywhere}.lt-search{flex:1;min-width:100px}.lt-title{font-size:18px;font-weight:650}.lt-pivots{display:flex;gap:4px;flex-wrap:wrap;max-height:110px;overflow:auto}.lt-pivots button{font-size:11px!important}.lt-files>div{max-height:350px;overflow:auto}';
    const title=node('div','lt-title','Live Trace'),status=node('div','lt-meta'),stats=node('div','lt-stats'),current=node('div','lt-current'),note=node('p','lt-note');
    const toolbar=node('div','lt-toolbar'),search=node('input','lt-search'),select=node('select'),sessionSelect=node('select');search.type='search';
    for(const key of ['all','error','command','read','edit','tool','search','summary']){const o=node('option');o.value=key;select.append(o);}
    const refresh=button('回到实时','Return to live',()=>{follow=true;phaseFilter='';if(data.historyStart!==null)connect(true);else render();}),earlier=button('更早步骤','Earlier steps',async()=>{try{follow=false;await loadEarlier();}catch(e){toast(e.message);}}),changes=button('文件变更','File changes',async()=>{changes.disabled=true;try{await loadFiles();fileList.open=true;}catch(e){toast(e.message);}finally{changes.disabled=false;}});
    toolbar.append(search,select,earlier,refresh,changes);
    const sessionsBar=node('div','lt-toolbar');sessionsBar.append(sessionSelect,button('最近会话','Recent sessions',async()=>{try{await loadSessions();}catch(e){toast(e.message);}}));
    const stageList=node('details'),stageTitle=node('summary'),stages=node('div','lt-pivots');stageList.append(stageTitle,stages);
    const errorList=node('details'),errorTitle=node('summary'),errors=node('div','lt-pivots');errorList.append(errorTitle,errors);
    const actions=node('div','lt-toolbar'),list=node('div','lt-list'),windowRows=node('div','lt-window'),detail=node('div','lt-detail'),tail=button('↓ 回到实时','↓ Return to live',()=>{follow=true;phaseFilter='';filter='all';select.value='all';if(data.historyStart!==null)connect(true);else render();});list.append(windowRows);
    const fileList=node('details','lt-files'),fileTitle=node('summary'),fileBody=node('div');fileList.append(fileTitle,fileBody);
    const selectedText=()=>data.events.filter(e=>selected.has(e.id)).slice(-12).map(e=>`### ${e.title}\n${e.path||''}\n${e.text.slice(0,2000)}\n\n> Source: ${data.conversation} · step ${e.index+1} · ${e.eventKind} · ${e.createdAt||'timestamp unavailable'}`).join('\n\n');
    actions.append(button('复制选中','Copy selected',async()=>{const text=selectedText();if(!text)return toast(t('请先选择事件','Select events first'));await copy(text);toast(t('已复制','Copied'));}),button('送入 Capsule','Send to Capsule',()=>{const text=selectedText();if(!text)return toast(t('请先选择事件','Select events first'));context.add({id:'trace-'+Date.now(),title:'Live Trace · '+data.conversation,text});}));
    for(const format of ['markdown','json'])actions.append(button('导出 '+(format==='markdown'?'MD':'JSON'),'Export '+(format==='markdown'?'MD':'JSON'),()=>{const blob=new Blob([exportTrace(format,data)],{type:format==='json'?'application/json':'text/markdown'}),url=URL.createObjectURL(blob),a=node('a');a.href=url;a.download='2ag-trace-'+data.conversation+(format==='json'?'.json':'.md');a.click();URL.revokeObjectURL(url);}));
    panel.append(style,title,status,stats,current,note,sessionsBar,toolbar,stageList,errorList,actions,list,tail,detail,fileList);
    function jump(id){phaseFilter='';filter='all';select.value='all';needle='';search.value='';follow=false;chosen=id;render();const index=matching.findIndex(e=>e.id===id);if(index>=0){list.scrollTop=index*ROW;renderRows();}}
    function showDetail(e){
      detail.replaceChildren();if(!e)return;
      detail.append(node('strong','',e.title),node('p','lt-meta',[e.createdAt,e.completedAt,e.path,e.duration!==undefined?duration(e.duration):'',e.tool].filter(Boolean).join(' · ')));
      if(e.text)detail.append(node('pre','',e.text));if(e.task)detail.append(node('p','lt-meta',[e.task.title,e.task.progress,e.task.approval?t('等待批准','Awaiting approval'):''].filter(Boolean).join(' · ')));
      if(e.path)detail.append(button('打开文件','Open file',()=>openFile?.(e.path).catch(e=>toast(e.message))));
    }
    function renderRows(){
      if(ended)return;
      const start=Math.max(0,Math.floor(list.scrollTop/ROW)-OVERSCAN),end=Math.min(matching.length,start+Math.ceil((list.clientHeight||360)/ROW)+OVERSCAN*2);
      windowRows.style.height=(matching.length*ROW)+'px';windowRows.replaceChildren();
      for(let i=start;i<end;i++){
        const e=matching[i],row=node('div','lt-event');row.dataset.event=e.id;row.dataset.category=e.category;row.dataset.status=e.status;row.style.top=(i*ROW)+'px';
        const check=node('input');check.type='checkbox';check.checked=selected.has(e.id);check.setAttribute('aria-label',t('选择事件','Select event'));check.onchange=()=>{check.checked?selected.add(e.id):selected.delete(e.id);};
        const b=node('button'),line=node('span','lt-line',`${e.index+1} · ${t(...(states[e.status]||[e.status,e.status]))} · ${e.title}`),meta=node('span','lt-line lt-meta',[t(...(label[e.category]||[e.eventKind,e.eventKind])),e.path,e.duration!==undefined?duration(e.duration):''].filter(Boolean).join(' · '));b.type='button';b.append(line,meta);b.onclick=()=>{chosen=e.id;showDetail(e);};row.append(check,b);windowRows.append(row);
      }
      if(!matching.length)windowRows.append(node('p','lt-meta',t('暂无匹配事件','No matching events')));
    }
    function render(){
      if(ended)return;
      const info=overview(data),phase={connecting:t('连接中','Connecting'),working:t('执行中','Working'),idle:t('空闲','Idle'),connected:t('已连接','Connected'),'no-conversation':t('请打开会话','Open a conversation'),unavailable:t('宿主不支持','Host unavailable'),error:t('连接失败','Connection error'),disconnected:t('已断开','Disconnected')}[data.state]||data.state;
      status.textContent=phase+(data.failure?' · '+data.failure:'');
      stats.textContent=`${t('时长','Duration')} ${duration(info.duration)} · ${t('步骤','Steps')} ${data.retained}/${data.total} · ${t('命令','Commands')} ${info.commands} · ${t('错误','Errors')} ${info.errors} · ${t('文件','Files')} ${info.files}${info.partial?' · '+t('统计仅覆盖已加载范围','Statistics cover loaded range'):''}`;
      const activeEvent=data.idle===false?[...data.events].reverse().find(e=>['running','generating','waiting','pending'].includes(e.status)):null;
      current.textContent=activeEvent?t('当前：','CURRENT: ')+activeEvent.title+(activeEvent.path&&activeEvent.path!==activeEvent.title?' · '+activeEvent.path:''):data.idle===true?t('当前空闲','Currently idle'):t('当前动作未取得','Current action unavailable');
      note.textContent=t('原生公开事件 · 阶段按事件类型归组 · 最多保留 10,000 步 · 不读取隐藏思维','Native public events · stages grouped by event type · up to 10,000 retained steps · hidden reasoning excluded');
      search.placeholder=t('关键词 / file:路径 / kind:error','Keyword / file:path / kind:error');earlier.disabled=data.historyStart===0||!data.events.length;
      for(const o of select.options)o.textContent=t(...label[o.value]);for(const b of panel.querySelectorAll('button[data-zh]'))b.textContent=t(b.dataset.zh,b.dataset.en);
      sessionSelect.replaceChildren();const currentOption=node('option','',t('当前会话','Current session')+' · '+data.conversation.slice(0,8));currentOption.value='';sessionSelect.append(currentOption);for(const s of data.sessions.filter(s=>s.id!==data.conversation)){const o=node('option','',s.title+' · '+s.steps);o.value=s.id;sessionSelect.append(o);}
      stageTitle.textContent=t('阶段（按事件类型）','Stages (by event type)')+' · '+info.stages.length;
      stages.replaceChildren(...info.stages.slice(-100).map(s=>button(t(...phaseLabel[s.name])+' · '+s.events+' · '+duration(s.duration),t(...phaseLabel[s.name])+' · '+s.events+' · '+duration(s.duration),()=>jump(s.first))));
      errorTitle.textContent=t('错误 / 中断','Errors / interruptions')+' · '+info.errors;
      errors.replaceChildren(...data.events.filter(e=>e.category==='error'||['error','invalid','interrupted'].includes(e.status)).slice(-100).map(e=>button((e.index+1)+' · '+e.title,(e.index+1)+' · '+e.title,()=>jump(e.id))));
      matching=data.events.filter(e=>(filter==='all'||(filter==='read'?['read','edit'].includes(e.category):filter==='error'?e.category==='error'||['error','invalid','interrupted'].includes(e.status):e.category===filter))&&matches(e,needle)&&(!phaseFilter||e.phase===phaseFilter));
      if(follow&&data.historyStart===null){windowRows.style.height=(matching.length*ROW)+'px';list.scrollTop=Math.max(0,matching.length*ROW-list.clientHeight);}
      renderRows();tail.hidden=follow&&data.historyStart===null;showDetail(data.events.find(e=>e.id===chosen));
      const known=data.files.filter(f=>f.additions!==undefined),add=known.reduce((n,f)=>n+f.additions,0),del=known.reduce((n,f)=>n+f.deletions,0);
      fileTitle.textContent=t('文件变更','Changed files')+' · '+info.files+' · +'+add+' −'+del+' · '+t('已取得行数','Counts available')+' '+known.length+'/'+data.files.length;
      const signature=i18n.language+JSON.stringify(data.files);
      if(signature!==fileSignature){fileSignature=signature;fileBody.replaceChildren(...data.files.map(f=>{const row=node('details');row.append(node('summary','',f.path+(f.additions!==undefined?' · +'+f.additions+' −'+f.deletions:' · '+t('行数未取得','Counts unavailable'))));let loaded=false;row.ontoggle=async()=>{if(!row.open||loaded)return;loaded=true;const body=node('div');row.append(body);body.textContent=t('读取原生 diff…','Loading native diff…');try{const diff=await loadDiff(f.path);if(ended)return;body.replaceChildren(node('pre','',diff||t('没有变更片段','No changed hunks')),button('复制 Diff','Copy Diff',()=>copy(diff)),button('打开文件','Open file',()=>openFile?.(f.path).catch(e=>toast(e.message))));}catch(e){body.textContent=t('无法显示 Diff：','Diff unavailable: ')+e.message;}};return row;}));}
    }
    const unsubscribe=observe(next=>{if(next.conversation!==data.conversation){selected.clear();chosen=null;follow=true;}if(next.historyStart!==data.historyStart&&next.historyStart!==null){list.scrollTop=0;follow=false;}data=next;clearTimeout(renderTimer);renderTimer=setTimeout(render,100);});
    const languageOff=i18n.onChange(render);
    search.oninput=()=>{needle=search.value;follow=false;list.scrollTop=0;render();};select.onchange=()=>{filter=select.value;follow=false;list.scrollTop=0;render();};
    list.onscroll=()=>{follow=data.historyStart===null&&list.scrollHeight-list.scrollTop-list.clientHeight<35;tail.hidden=follow;renderRows();};
    sessionSelect.onchange=()=>{const id=sessionSelect.value;if(!id)return;if(!host.conversation.open('/c/'+id)){host.conversation.history();setTimeout(()=>{if(!ended&&!host.conversation.open('/c/'+id))toast(t('请从原生历史中打开此会话','Open this session from native history'));},350);}};
    loadSessions().catch(()=>{});render();
    return()=>{ended=true;clearTimeout(renderTimer);list.onscroll=null;unsubscribe();languageOff();selected.clear();data=null;matching=[];panel.replaceChildren();};
  }
  return {observe,snapshot,mount,loadFiles,loadDiff,loadEarlier,loadSessions,overview,matches,exportTrace,dispose(){active=false;listeners.clear();clearInterval(routeTimer);disconnect();events.clear();files=[];sessions=[];diffCache.clear();}};
})
