(() => {
  'use strict';
  // Shared, narrowly selected native descriptors. No planner response bodies.
  const schema = __2AG_ACTIVITY_SCHEMA__;
  const STEP = 'gemini_coder.Step';
  const cases = ['generic','codeAction','grepSearch','runCommand','find','readUrlContent','searchWeb','codeSearch','viewFile','checkpoint'];
  const statusNames = {0:'UNKNOWN',1:'PENDING',2:'RUNNING',3:'DONE',4:'INVALID',5:'CLEARED',6:'CANCELED',7:'ERROR',8:'GENERATING',9:'WAITING',11:'QUEUED',12:'INTERRUPTED'};
  const runStatusNames = {0:'UNKNOWN',1:'IDLE',2:'RUNNING',3:'CANCELING',4:'BUSY'};
  const known = n => typeof n === 'number' && Number.isFinite(n);
  const number = v => v === undefined || v === null || v === '' ? null : Number.isSafeInteger(Number(v)) ? Number(v) : null;
  const safe = value => {
    let s = String(value ?? '').replace(/(bearer\s+)[A-Za-z0-9._~+/-]+/gi,'$1[redacted]')
      .replace(/((?:access_token|refresh_token|id_token|client_secret|authorization|password|cookie)["'\s]*[:=]["'\s]*)[^\s,"';}]+/gi,'$1[redacted]')
      .replace(/\beyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b/g,'[redacted]');
    return s.length > 32768 ? s.slice(0,32768)+'\n[truncated by 2Ag at 32 KiB]' : s;
  };
  const esc = s => safe(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const timestamp = v => {
    if (!v) return null;
    const sec = number(v.seconds), nano = number(v.nanos);
    return sec === null ? null : sec*1000+(nano??0)/1000000;
  };
  const basename = s => safe(s).replace(/\\/g,'/').split('/').filter(Boolean).pop() || '';
  const mapArgs = a => Array.isArray(a) ? Object.fromEntries(a.map(v=>[v.key,v.value])) : a || {};
  const pathOf = p => typeof p === 'string' ? p : p?.absoluteUri || '';
  const redactArgument = (out,message) => {
    if(message==='exa.cortex_pb.CortexStepGeneric.ArgsEntry' && /^(accessToken|refreshToken|idToken|clientSecret|authorization|password|cookie)$/i.test(String(out.key||'').replace(/[_\s-]/g,'')))out.value='[redacted]';
    return out;
  };

  function readWire(bytes) {
    const values=[];let i=0;
    const varint=()=>{let n=0n,shift=0n;for(let k=0;k<10;k++){if(i>=bytes.length)throw Error('Short native field');const b=bytes[i++];n|=BigInt(b&127)<<shift;if(b<128)return n;shift+=7n;}throw Error('Invalid native integer');};
    while(i<bytes.length){const tag=varint(),field=Number(tag>>3n),wire=Number(tag&7n);let value;
      if(!field)throw Error('Invalid native tag');
      if(wire===0)value=varint();
      else if(wire===2){const n=Number(varint());if(!Number.isSafeInteger(n)||n<0||i+n>bytes.length)throw Error('Short native message');value=bytes.subarray(i,i+n);i+=n;}
      else if(wire===1||wire===5){const n=wire===1?8:4;if(i+n>bytes.length)throw Error('Short native fixed field');value=bytes.subarray(i,i+n);i+=n;}
      else throw Error('Unsupported native wire');
      values.push({field,wire,value});if(values.length>20000)throw Error('Native field limit');
    }
    return values;
  }
  function decode(bytes,message,depth=0) {
    if(depth>14)throw Error('Native nesting limit');
    const fields=readWire(bytes),out={};
    if(message==='google.protobuf.Any'){
      const url=fields.find(v=>v.field===1),value=fields.find(v=>v.field===2);
      out.typeUrl=url?safe(new TextDecoder().decode(url.value)):'';
      if(out.typeUrl==='type.googleapis.com/gemini_coder.Step'&&value)out.step=decode(value.value,STEP,depth+1);
      if(out.step?.projectionPartial)out.projectionPartial=true;
      return out;
    }
    const definitions=schema[message];if(!definitions)throw Error('Unknown native message');
    for(const f of fields){const definition=definitions[f.field];if(!definition)continue;
      const [name,kind,child,repeated]=definition;let v;
      if(kind===9&&f.wire===2)v=safe(new TextDecoder().decode(f.value));
      else if(kind===11&&f.wire===2)v=decode(f.value,child,depth+1);
      else if(kind===8&&f.wire===0)v=f.value!==0n;
      else if([3,4,5,13,14].includes(kind)&&f.wire===0){v=Number(kind===5?BigInt.asIntN(32,f.value):f.value);if(!Number.isSafeInteger(v))continue;}
      else continue;
      if(v?.projectionPartial)out.projectionPartial=true;
      if(repeated){out[name]||=[];if(out[name].length<256)out[name].push(v);else out.projectionPartial=true;}
      else out[name]=v;
    }
    return redactArgument(out,message);
  }

  // Runtime and SQLite projections use the identical field allowlist. Runtime
  // protobuf oneofs are flattened without visiting the plannerResponse value.
  function project(object,message,depth=0) {
    if(!object||depth>14)return {};
    if(message==='google.protobuf.Any'){
      const typeUrl=safe(object.typeUrl),out={typeUrl};
      if(typeUrl==='type.googleapis.com/gemini_coder.Step'){
        if(object.step)out.step=project(object.step,STEP,depth+1);
        else if(object.value instanceof Uint8Array)out.step=decode(object.value,STEP,depth+1);
        if(out.step?.projectionPartial)out.projectionPartial=true;
      }
      return out;
    }
    const out={};
    for(const [name,kind,child,repeated] of Object.values(schema[message]||{})){
      let value=object[name];
      if(value===undefined)for(const key of ['step','spec','result']){const oneof=object[key];if(oneof?.case===name){value=oneof.value;break;}}
      if(value===undefined||value===null)continue;
      if(name==='args'&&!Array.isArray(value))value=Object.entries(value).map(([key,value])=>({key,value}));
      const convert=v=>{const projected=kind===9?safe(v):kind===11?project(v,child,depth+1):kind===8?!!v:number(v);if(projected?.projectionPartial)out.projectionPartial=true;return projected;};
      out[name]=repeated?(Array.isArray(value)?value.slice(0,256).map(convert):[]):convert(value);
      if(repeated&&value.length>256)out.projectionPartial=true;
    }
    return redactArgument(out,message);
  }

  function requestUsage(native,sessionId,source) {
    const u=native.metadata?.modelUsage;if(!u)return null;
    const cache=v=>number(v)>0?number(v):null;
    return window.TwoAgContextView.normalizeRequest({session_id:sessionId,response_id:u.responseId||'',
      timestamp:timestamp(native.metadata?.createdAt),provider:'antigravity',model:'',provenance:'Native',source:source+' native modelUsage',
      // thinkingOutputTokens is not verified equivalent to the shared Reader's
      // Reasoning category. Let matched persistent telemetry supply that field.
      input:number(u.inputTokens),unclassified_input:null,output:number(u.outputTokens),cache_read:cache(u.cacheReadTokens),cache_write:cache(u.cacheWriteTokens),reasoning:null});
  }

  /**
   * ActivityEntry: sessionId, trajectoryId, stepIndex, executionId, type, status,
   * title, target, createdAt, startedAt, completedAt, duration (milliseconds),
   * toolCallId, responseId, details, result, error, tokenUsage, source, derived.
   * Unknown numbers are null; identifiers are scoped to their native session.
   * This is a display model, never a raw React/protobuf object.
   */
  function normalize(native,stepIndex,sessionId,source,tokenUsage=null) {
    const m=native.metadata||{},g=native.generic,inner=g?.result?.payload?.step;
    const body=inner||native,kind=cases.find(k=>body[k])|| (native.type===15?'plannerResponse':'');
    const v=body[kind]||{},args=mapArgs(g?.args);let callArgs={};
    try{callArgs=JSON.parse(m.toolCall?.argumentsJson||'{}');}catch{}
    const arg=(...names)=>{for(const name of names){const value=args[name]??callArgs[name];if(value!==undefined&&value!==null)return safe(value);}return '';};
    let type='',target='',title='',details={},result=null;
    const rawStatus=number(native.status),status=statusNames[rawStatus]||'UNKNOWN',active=['RUNNING','GENERATING','PENDING','QUEUED'].includes(status);
    const toolName=m.toolCall?.name||'';
    if(kind==='viewFile'||toolName==='view_file'){
      type='file_read';target=v.absolutePathUri||arg('AbsolutePath','TargetFile');title='Read '+(basename(target)||'file');
      details={path:target,startLine:number(v.startLine)??number(arg('StartLine')),endLine:number(v.endLine)??number(arg('EndLine')),numLines:number(v.numLines),numBytes:number(v.numBytes)};
      result=v.content?{content:v.content}:null;
    }else if(kind==='codeAction'||['replace_file_content','multi_replace_file_content','write_to_file'].includes(toolName)){
      type='file_edit';const spec=v.actionSpec||{},action=['command','createFile','deleteFile','sed'].find(k=>spec[k]),a=spec[action]||{},edit=v.actionResult?.edit;
      target=edit?.absoluteUri||edit?.absolutePathMigrateMeToUri||pathOf(a.file||a.path)||arg('TargetFile','AbsolutePath');
      const verb=action==='deleteFile'?(active?'Deleting':'Deleted'):action==='createFile'&&!a.overwrite?(active?'Creating':'Created'):(active?'Editing':'Edited');
      title=verb+' '+(basename(target)||'file');
      details={path:target,action:action||null,additions:number(v.diffStats?.additions),deletions:number(v.diffStats?.deletions),description:v.description||null};
      result={actionResult:v.actionResult||null,replacementInfos:v.replacementInfos||null,replacementChunks:a.replacementChunks||null};
    }else if(kind==='runCommand'||toolName==='run_command'){
      type='command';target=v.commandLine||arg('CommandLine');title=(active?'Running ':'Ran ')+(target||'command');
      details={commandLine:target,proposedCommandLine:v.proposedCommandLine||null,cwd:v.cwd||arg('Cwd'),exitCode:number(v.exitCode),shellName:v.shellName||null,commandId:v.commandId||null,timedOut:v.timedOut??null};
      result=v.combinedOutput||v.combinedOutputSnapshot||g?.result?.result||null;
    }else if(['grepSearch','find','codeSearch','searchWeb'].includes(kind)||['grep_search','find_by_name','code_search','search_web'].includes(toolName)){
      type='search';target=v.query||v.pattern||arg('Query','SearchQuery','Pattern');title=(active?'Searching ':'Searched ')+(target?'“'+target+'”':'');
      details={query:v.query||target,pattern:v.pattern||null,path:v.searchPathUri||v.searchDirectory||arg('SearchPath','SearchDirectory'),totalResults:number(v.totalResults),estimatedTotalResults:number(v.estimatedTotalResults),searchType:number(v.searchType),timedOut:v.timedOut??null};
      result=v.summary||v.rawOutput||v.truncatedOutput||g?.result?.result||null;
    }else if(kind==='plannerResponse'){
      type='model_response';title='Model response';details={'thinkingOutputTokens · Native':number(m.modelUsage?.thinkingOutputTokens)};
    }else if(kind==='checkpoint'||native.type===23){
      type='checkpoint';title='Checkpoint';details={checkpointIndex:number(v.checkpointIndex),includedStepIndexStart:number(v.includedStepIndexStart),includedStepIndexEnd:number(v.includedStepIndexEnd)};
    }else if(toolName||g){
      type='tool';title=m.toolSummary||inner?.metadata?.toolSummary||arg('toolSummary')||g?.result?.stepRenderInfo?.title||toolName||'Tool';
      target=kind==='readUrlContent'?v.resolvedUrl||v.url||arg('Url'):'';
      details={tool:toolName,action:m.toolAction||arg('toolAction')||null,url:target||null,latencyMs:number(v.latencyMs),task:native.taskDetails||null};
      result=g?.result?.result||null;
    }else if(status==='ERROR'||native.error){type='error';title='Error';}
    else if(status==='WAITING'){type='waiting';title='Waiting';}
    else return null;
    const createdAt=timestamp(m.createdAt),startedAt=timestamp(m.startedAt),completedAt=timestamp(m.completedAt),viewableAt=timestamp(m.viewableAt),finishedGeneratingAt=timestamp(m.finishedGeneratingAt);
    // Native model started/completed timestamps can be equal. Generation spans
    // created -> finished/completed, while tool execution spans started -> completed.
    const durationStart=type==='model_response'?createdAt:startedAt,durationEnd=type==='model_response'?(finishedGeneratingAt??completedAt):completedAt;
    const duration=known(durationStart)&&known(durationEnd)&&durationEnd>=durationStart?durationEnd-durationStart:null;
    const usage=tokenUsage||requestUsage(native,sessionId,source);
    return {sessionId,stepIndex:number(stepIndex),trajectoryId:m.sourceTrajectoryStepInfo?.trajectoryId||sessionId,executionId:m.executionId||'',
      type,status,title:safe(title),target:safe(target),createdAt,startedAt,completedAt,duration,
      toolCallId:m.toolCall?.id||'',responseId:usage?.response_id||m.modelUsage?.responseId||'',
      details:{...details,toolName:toolName||null,rawStatus,viewableAt,finishedGeneratingAt,retries:m.retryInfos?.length?m.retryInfos:null},
      result,error:native.error||inner?.error||null,tokenUsage:usage,source:[source],
      derived:{classification:type,title:safe(title),durationBasis:type==='model_response'?'createdAt → finishedGeneratingAt/completedAt':'startedAt → completedAt'},
      partial:!!native.projectionPartial||!!inner?.projectionPartial};
  }

  function provider() {
    const view=document.querySelector('[data-testid="conversation-view"]');if(!view)return null;
    const key=Object.keys(view).find(k=>k.startsWith('__reactFiber$')),q=key?[view[key]]:[],seen=new Set();
    for(let i=0;i<q.length&&i<2400;i++){const f=q[i];if(!f||seen.has(f))continue;seen.add(f);
      const p=f.memoizedProps?.cascadeContext?.state?.agentStateProvider;
      if(p&&typeof p.getState==='function'&&typeof p.onDidChange==='function')return p;
      q.push(f.return,f.child,f.sibling);
    }
    return null;
  }
  const runtimeSteps = new WeakMap();
  function readRuntime(p=provider()) {
    if(!p)return null;const state=p.getState(),slice=state?.trajectorySlice;if(!slice)return null;
    const sessionId=state.conversationId||slice.conversationId||'',start=number(slice.stepsSlice?.startIndex)??0;
    const entries=[];let failed=0;
    for(const [i,step] of (slice.stepsInSlice||[]).entries()){
      try{const index=step.metadata?.sourceTrajectoryStepInfo?.stepIndex??start+i,cached=runtimeSteps.get(step);
        let entry;if(cached?.sessionId===sessionId&&cached.index===index)entry=cached.entry;
        else{const n=project(step,STEP);entry=normalize(n,index,sessionId,'Runtime');runtimeSteps.set(step,{sessionId,index,entry});}
        if(entry)entries.push(entry);
      }catch{failed++;}
    }
    return {sessionId,totalSteps:number(slice.totalStepsLength),entries,failed,status:runStatusNames[state.status]||'UNKNOWN'};
  }

  function identities(e) {
    const scope=e.sessionId+'|'+e.trajectoryId+'|';const keys=[];
    if(known(e.stepIndex))keys.push(scope+'step:'+e.stepIndex);
    if(e.toolCallId)keys.push(scope+'execution:'+e.executionId+'|tool:'+e.toolCallId);
    if(e.type==='model_response'&&e.responseId)keys.push(scope+'response:'+e.responseId);
    return keys;
  }
  function mergeEntries(history,runtime) {
    const result=[],byKey=new Map();
    for(const e of [...history,...runtime]){
      const keys=identities(e);let i;
      for(const key of keys)if(byKey.has(key)){i=byKey.get(key);break;}
      if(i===undefined){i=result.length;result.push(e);}else{
        const old=result[i],live=e.source.includes('Runtime');
        // Runtime owns current status. Persistence fills missing results/usage;
        // no summing samples, and executionId alone never collapses multiple tools.
        const preferIncoming=live||!old.source.includes('Runtime');
        const preferred=preferIncoming?e:old,fallback=preferIncoming?old:e;
        const usage=preferred.tokenUsage?window.TwoAgContextView.normalizeRequest(preferred.tokenUsage,{recent_requests:[fallback.tokenUsage].filter(Boolean)}):fallback.tokenUsage;
        if(usage && !usage.model && fallback.tokenUsage?.response_id===usage.response_id && fallback.tokenUsage?.session_id===usage.session_id)usage.model=fallback.tokenUsage.model||'';
        const details={...fallback.details};for(const [key,value]of Object.entries(preferred.details||{}))if(value!==null&&value!==undefined&&value!=='')details[key]=value;
        result[i]={...fallback,...preferred,details,result:preferred.result||fallback.result,error:preferred.error||fallback.error,tokenUsage:usage,
          duration:preferred.duration??fallback.duration,createdAt:preferred.createdAt??fallback.createdAt,startedAt:preferred.startedAt??fallback.startedAt,completedAt:preferred.completedAt??fallback.completedAt,
          source:[...new Set([...old.source,...e.source])]};
      }
      for(const key of [...keys,...identities(result[i])])byKey.set(key,i);
    }
    return result.sort((a,b)=>known(a.stepIndex)&&known(b.stepIndex)?a.stepIndex-b.stepIndex:(a.createdAt??a.startedAt??0)-(b.createdAt??b.startedAt??0));
  }

  const css=`
    #il-activity{animation:none}.act-head{display:flex;gap:8px;align-items:center;margin-bottom:8px}.act-head label{flex:1;min-width:0}
    .act-meta{font-size:11px;color:var(--2ag-text-secondary);margin:6px 0 10px;line-height:1.5}.act-list{border-top:1px solid var(--2ag-outline-variant)}
    .act-entry{border-bottom:1px solid var(--2ag-outline-variant);margin:0;border-radius:0;background:transparent}
    .act-entry>summary{display:grid;grid-template-columns:58px 1fr 67px 49px;gap:8px;align-items:start;padding:7px 3px;cursor:pointer;font-size:12px;list-style:none;line-height:1.4}
    .act-entry>summary::-webkit-details-marker{display:none}.act-entry>summary:focus-visible{outline:2px solid var(--2ag-blue);outline-offset:-2px}
    .act-time,.act-duration{font-variant-numeric:tabular-nums;font-size:11px;color:var(--2ag-text-secondary)}.act-title{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
    .act-sub{display:block;font-size:10px;color:var(--2ag-text-secondary);font-variant-numeric:tabular-nums}.act-status{font-size:9px;text-align:right;letter-spacing:.2px}
    .act-entry[data-status=ERROR] .act-status{color:var(--2ag-red,#b3261e)}.act-entry[data-status=RUNNING],.act-entry[data-status=GENERATING]{background:color-mix(in srgb,var(--2ag-blue) 6%,transparent)}
    .act-entry[data-status=WAITING] .act-status{color:var(--2ag-blue)}.act-detail{padding:2px 8px 10px 69px;font-size:11px;overflow-wrap:anywhere}
    .act-detail dl{display:grid;grid-template-columns:105px 1fr;gap:4px;margin:8px 0}.act-detail dt{color:var(--2ag-text-secondary)}.act-detail dd{margin:0;white-space:pre-wrap}
    .act-detail pre{white-space:pre-wrap;overflow:auto;max-height:240px;font:11px/1.5 ui-monospace,Consolas,monospace;margin:6px 0}.act-detail details{margin:7px 0}.act-detail summary{cursor:pointer}
    .act-empty{padding:16px 0;color:var(--2ag-text-secondary);font-size:12px}.act-legend{font-size:10px;color:var(--2ag-text-secondary)}
    .act-filters{display:flex;gap:4px;flex-wrap:wrap;margin:8px 0}.act-filters button{font:inherit;font-size:11px;border:1px solid var(--2ag-outline-variant);background:transparent;color:inherit;padding:3px 7px;border-radius:4px;cursor:pointer}.act-filters button[aria-pressed=true]{color:var(--2ag-blue);border-color:var(--2ag-blue)}
    @media(max-width:540px){.act-entry>summary{grid-template-columns:48px 1fr 57px 40px;gap:4px}.act-detail{padding-left:8px}}`;

  const fmtDuration=n=>!known(n)?'—':n<1000?Math.round(n)+'ms':(n/1000).toFixed(n<10000?1:0)+'s';
  const tokenText=(u,label=(cn,en)=>cn)=>{if(!u)return '';const v=window.TwoAgContextView;return v.amount(u.request_input,u.observed_input)+' '+label('输入处理','processed')+' → '+v.tokens(u.output)+' '+label('输出','output');};
  function detailHTML(e,label=(cn,en)=>cn){const pairs=[['Step',e.stepIndex],['Type · Derived',e.type],['Status · Native',e.status],['Tool · Native',e.details.toolName],['executionId',e.executionId],['toolCallId',e.toolCallId],['responseId',e.responseId],['Source',e.source.join(' + ')],['Duration · Derived',fmtDuration(e.duration)+' · '+e.derived.durationBasis],['createdAt',known(e.createdAt)?new Date(e.createdAt).toISOString():null],['startedAt',known(e.startedAt)?new Date(e.startedAt).toISOString():null],['completedAt',known(e.completedAt)?new Date(e.completedAt).toISOString():null]];
      for(const [k,v]of Object.entries(e.details))if(!['retries','toolName','rawStatus'].includes(k)&&v!==null&&v!==undefined&&v!=='')pairs.push([k,typeof v==='object'?JSON.stringify(v):v]);
      if(e.tokenUsage){const u=e.tokenUsage,v=window.TwoAgContextView;pairs.push(['Token · Native',tokenText(u,label)],['Total',v.amount(u.total,u.observed_total)],['New Input',v.tokens(u.input)],['Unclassified Input',v.tokens(u.unclassified_input)],['Cache Read',v.tokens(u.cache_read)],['Cache Write',v.tokens(u.cache_write)],['Output',v.tokens(u.output)],['Reasoning ⊂ Output',v.tokens(u.reasoning)],['Cache Hit',v.rate(u.cache_hit_rate)],['Model',u.model||'—'],['Token source',u.source||'Unavailable']);}
      // A child element keeps HTML's <pre> parsing from discarding the first
      // output newline. Preserve CRs too; localization never rewrites output.
      const block=(name,value)=>value?'<details><summary>'+esc(name)+'</summary><pre><code data-user-content>'+esc(typeof value==='string'?value:JSON.stringify(value,null,2)).replace(/\r/g,'&#13;')+'</code></pre></details>':'';
      return '<dl>'+pairs.filter(([,v])=>v!==null&&v!==undefined&&v!=='').map(([k,v])=>'<dt'+(k==='thinkingOutputTokens · Native'?' title="'+esc(label('原生字段；尚未确认与共享 Reader 的 Reasoning 分类等价。','Native field; equivalence to the shared Reader Reasoning category is unconfirmed.'))+'"':'')+'>'+esc(k)+'</dt><dd>'+esc(v)+'</dd>').join('')+'</dl>'+block(label('结果 / 输出 · 原生','Result / output · Native'),e.result)+block(label('错误 · 原生','Error · Native'),e.error)+block(label('重试记录 · 原生','Retries · Native'),e.details.retries)+(e.partial?'<p>'+label('部分字段已截断。','Some fields were truncated.')+'</p>':'');}

  // One reader per selected source. The inline presentation and G-Hub's
  // "Follow current" share the same subscription, history and merge operation.
  const readers=new Map();
  function acquireReader(request,selection,listener) {
    let reader=readers.get(selection);
    if(!reader){reader=createReader(request,selection);readers.set(selection,reader);}
    const release=reader.listen(listener);
    return ()=>{release();if(!reader.size()){reader.dispose();readers.delete(selection);}};
  }
  function createReader(request,selection='') {
    let alive=true,visible=true,p=null,subscription=null,timer=null,historyTimer=null,paintTimer=null,runtimeTimer=null,sessionId='',store='',liveStore='',liveSessionId='',sourcePending='',history=[],runtime=[],epoch=0,loading=false,historyComplete=false,historyPartial=false,note='',total=null,failed=0,lastAfter=-1;
    const listeners=new Set(),paintChannel=new MessageChannel(),runtimeChannel=new MessageChannel();
    const readJSON=async path=>(await request(path)).json();
    if(selection){const chosen=JSON.parse(selection);sessionId=chosen.id;store=chosen.store;}
    const snapshot=()=>({sessionId,store,entries:mergeEntries(history,runtime),loading,historyComplete,historyPartial,note,total,failed,runtime:runtime.length});
    function paint(){if(!alive)return;const state=snapshot();for(const listen of listeners)listen(state);}
    paintChannel.port1.onmessage=()=>{paintTimer=null;paint();};
    runtimeChannel.port1.onmessage=()=>{runtimeTimer=null;if(alive)updateRuntime();};
    function queuePaint(){if(paintTimer!==null)return;paintTimer=true;paintChannel.port2.postMessage(null);}
    function detach(){if(typeof subscription==='function')subscription();else subscription?.dispose?.();subscription=null;p=null;}
    function reset(id,nextStore=''){epoch++;sessionId=id;store=nextStore;history=[];runtime=[];historyComplete=false;historyPartial=false;note='';total=null;failed=0;lastAfter=-1;loading=false;paint();}
    async function loadHistory(full=false){if(!alive||!sessionId||loading)return;const stamp=epoch;loading=true;if(full){historyPartial=false;historyComplete=false;note='';}queuePaint();let after=full?-1:Math.max(-1,lastAfter-32);
      // Long commands can finish after leaving the Runtime slice and the usual
      // history overlap. Revisit unsettled rows so their result updates in place.
      if(!full)for(const entry of history)if(activeStatus(entry)&&known(entry.stepIndex))after=Math.min(after,Math.max(-1,entry.stepIndex-1));
      try{let first=true;while(alive&&stamp===epoch){const data=await readJSON('/api/v1/sessions/activity?id='+encodeURIComponent(sessionId)+'&store='+encodeURIComponent(store)+'&after='+after);if(!alive||stamp!==epoch)return;
          if(data.sessionId!==sessionId)throw Error('Activity session mismatch');
          if(data.source?.store){store=data.source.store;if(!selection)liveStore=store;}
          const page=[];for(const step of data.steps||[]){const e=normalize(step.native,step.stepIndex,sessionId,'SQLite · '+store+' · '+(data.source?.conversation_path||''),step.tokenUsage);if(e)page.push(e);}
          history=mergeEntries(full&&first?[]:history,page);first=false;total=data.totalSteps;historyPartial||=!!data.partial;note=data.note||'';historyComplete=!!data.complete;lastAfter=data.nextAfter;queuePaint();if(data.complete)break;if(!known(data.nextAfter)||data.nextAfter<=after)throw Error('Activity history cursor did not advance');after=data.nextAfter;
        }
      }catch(e){if(alive&&stamp===epoch){historyComplete=false;note=safe(e.message);}}
      finally{if(alive&&stamp===epoch){loading=false;queuePaint();}}
    }
    function updateRuntime(){if(!alive||!p)return;const data=readRuntime(p);if(!data){runtime=[];queuePaint();return;}
      const route=location.pathname.match(/\/c\/([^/?]+)/);
      if(location.pathname==='/'||route&&route[1]!==data.sessionId){runtime=[];queuePaint();return;}
      if(data.sessionId!==liveSessionId){liveSessionId=data.sessionId;liveStore='';}
      if(!selection&&data.sessionId!==sessionId){reset(data.sessionId);void loadHistory(true);}
      if(data.sessionId!==sessionId){runtime=[];queuePaint();return;}
      // A restored explicit selection has not visited "Follow current" yet.
      // Resolve the live session's canonical source independently; a backup of
      // the same session must never gain Runtime data from the primary store.
      if(selection&&!liveStore){const id=data.sessionId;if(sourcePending!==id){sourcePending=id;
        void readJSON('/api/v1/sessions/activity?id='+encodeURIComponent(id)+'&after='+Math.max(-1,(data.totalSteps??0)-1)).then(d=>{
          if(!alive||p?.getState()?.conversationId!==id||liveSessionId!==id)return;
          if(d.sessionId!==id||!d.source?.store)throw Error('Activity live source unavailable');
          liveStore=d.source.store;updateRuntime();
        }).catch(()=>{if(alive&&liveSessionId===id){note='Runtime source unavailable; showing SQLite only.';queuePaint();}}).finally(()=>{if(sourcePending===id)sourcePending='';});
      }return;}
      if(selection&&store!==liveStore){runtime=[];queuePaint();return;}
      // Use the provider's current slice, never retain a stale RUNNING/WAITING
      // entry after it has left that slice. SQLite owns the complete history.
      runtime=data.entries;failed=data.failed;total=data.totalSteps??total;queuePaint();
      if(!historyTimer)historyTimer=setTimeout(()=>{historyTimer=null;if(visible)void loadHistory();},1500);
    }
    function attach(){const next=provider();if(next!==p){detach();p=next;if(p){subscription=p.onDidChange(()=>{if(runtimeTimer===null){runtimeTimer=true;runtimeChannel.port2.postMessage(null);}});}else{runtime=[];queuePaint();}}
      if(!selection){const id=p?.getState()?.conversationId||location.pathname.match(/\/c\/([^/?]+)/)?.[1]||'';if(id!==sessionId){reset(id);void loadHistory(true);}}
      updateRuntime();
    }

    return {
      listen(fn){listeners.add(fn);fn(snapshot());if(!timer){attach();timer=setInterval(attach,2000);if(sessionId&&!history.length)void loadHistory(true);}return ()=>listeners.delete(fn);},
      size:()=>listeners.size,
      reload(){void loadHistory(true);attach();},
      dispose(){alive=false;visible=false;epoch++;detach();clearInterval(timer);clearTimeout(historyTimer);paintChannel.port1.close();paintChannel.port2.close();runtimeChannel.port1.close();runtimeChannel.port2.close();listeners.clear();}
    };
  }

  function mount(element,{request,language=()=> 'zh-CN',initialSelection='',selectionChanged=()=>{}}) {
    let alive=true,visible=false,selection='',release=null,state={entries:[]},filter='all';
    const nodes=new Map(),signatures=new Map();
    if(initialSelection){try{const chosen=JSON.parse(initialSelection);if(typeof chosen.id==='string'&&typeof chosen.store==='string')selection=initialSelection;}catch{}}
    const zh=()=>language()!=='en-US',label=(cn,en)=>zh()?cn:en;
    element.innerHTML='<style>'+css+'</style><div class="act-head"><label><span data-act-session-label></span><select data-act-session></select></label><button type="button" data-act-refresh></button></div><div class="act-filters" role="group"></div><div class="act-meta" role="status"></div><div class="act-legend"></div><div class="act-list"></div>';
    const select=element.querySelector('[data-act-session]'),list=element.querySelector('.act-list'),meta=element.querySelector('.act-meta');
    function labels(){element.querySelector('[data-act-session-label]').textContent=label('会话','Session');element.querySelector('[data-act-refresh]').textContent=label('重新读取','Reload');element.querySelector('.act-legend').textContent=label('原生事实：状态、工具、参数、结果 · 2Ag 推导：分类、标题、耗时','Native: status, tool, arguments, result · Derived: classification, title, duration');const current=select.querySelector('option[value=""]');if(current)current.textContent=label('跟随当前会话','Follow current conversation');signatures.clear();paint();}
    const fmtTime=n=>known(n)?new Date(n).toLocaleTimeString(zh()?'zh-CN':'en-US',{hour12:false}):'—';
    function rowHTML(e){const d=e.details,extras=[],title=actionTitle(e,label)+(e.type==='command'&&e.target?' · '+e.target:'');if(e.type==='file_read'&&(known(d.startLine)||known(d.endLine)))extras.push('L'+(d.startLine??'—')+'–'+(d.endLine??'—'));if(e.type==='file_edit'&&(known(d.additions)||known(d.deletions)))extras.push('+'+(d.additions??'—')+' −'+(d.deletions??'—'));if(e.type==='command'&&known(d.exitCode))extras.push('exit '+d.exitCode);if(e.type==='search'&&known(d.totalResults))extras.push(d.totalResults+' '+label('个结果','results'));if(e.type==='model_response')extras.push(tokenText(e.tokenUsage,label));return '<span class="act-time">'+esc(fmtTime(e.createdAt??e.startedAt))+'</span><span class="act-title" title="'+esc(title)+'">'+esc(title)+(extras.filter(Boolean).length?'<span class="act-sub">'+esc(extras.filter(Boolean).join(' · '))+'</span>':'')+'</span><span class="act-status">'+esc(e.status)+'</span><span class="act-duration">'+esc(fmtDuration(e.duration))+'</span>';}
    function paint(){if(!alive)return;const entries=filterEntries(state.entries,filter);const seen=new Set();
      for(const e of entries){const key=identities(e)[0]||e.type+':'+e.createdAt;seen.add(key);let node=nodes.get(key);if(!node){node=document.createElement('details');node.className='act-entry';node.innerHTML='<summary></summary><div class="act-detail"></div>';nodes.set(key,node);node.addEventListener('toggle',()=>{if(node.open)node.querySelector('.act-detail').innerHTML=detailHTML(node.entry,label);});}
        const signature=JSON.stringify(e);node.entry=e;node.dataset.status=e.status;if(signatures.get(key)!==signature){node.querySelector('summary').innerHTML=rowHTML(e);if(node.open){const detail=node.querySelector('.act-detail'),opened=[...detail.querySelectorAll('details[open]')].map(n=>n.querySelector('summary').textContent);detail.innerHTML=detailHTML(e,label);for(const d of detail.querySelectorAll('details'))if(opened.includes(d.querySelector('summary').textContent))d.open=true;}signatures.set(key,signature);}
        const previous=seen.size>1?entries[seen.size-2]:null;const prevKey=previous?(identities(previous)[0]||previous.type+':'+previous.createdAt):null;const expected=prevKey?nodes.get(prevKey)?.nextSibling:list.firstChild;if(node!==expected)list.insertBefore(node,expected||null);
      }
      for(const [key,node]of nodes)if(!seen.has(key)){node.remove();nodes.delete(key);signatures.delete(key);}
      let empty=list.querySelector('.act-empty');if(!entries.length){if(!empty){empty=document.createElement('div');empty.className='act-empty';list.append(empty);}empty.textContent=state.loading?label('正在读取真实步骤…','Reading native steps…'):label('当前来源没有可读取的 Activity。','Activity unavailable for this source.');}else empty?.remove();
      const {sessionId,total,loading,historyComplete,historyPartial,failed,note,runtime}=state;
      meta.textContent=(sessionId?sessionId+' · ':'')+entries.length+' / '+state.entries.length+' '+label('行为','activities')+' / '+(total??'—')+' '+label('原生步骤','native steps')+' · '+(loading?label('历史读取中','Loading history'):historyComplete?label('历史已读取','History loaded'):label('历史不完整','History incomplete'))+(runtime?' · Runtime':'')+(historyPartial||failed?' · '+label('部分不可解析','Partially unavailable'):'')+(note?' · '+note:'');
    }
    async function sessions(){select.innerHTML='<option value="">'+label('跟随当前会话','Follow current conversation')+'</option>';try{const data=await (await request('/api/v1/sessions')).json();if(!alive)return;const items=Array.isArray(data)?data:data.sessions||[];for(const s of items){if(s.provider&&s.provider!=='antigravity')continue;const id=s.id||s.session_id,src=s.storage?.store||s.store||'';if(!id)continue;const o=document.createElement('option');o.value=JSON.stringify({id,store:src});o.textContent=(s.title||id)+' · '+src;select.append(o);}select.value=selection;}catch{} }
    function connect(){release?.();release=acquireReader(request,selection,next=>{state=next;if(visible)paint();});}
    select.addEventListener('change',()=>{selection=select.value;selectionChanged(selection);connect();});
    element.querySelector('[data-act-refresh]').addEventListener('click',()=>{readers.get(selection)?.reload();void sessions();});
    const filterNames=[['all','全部','All'],['errors','错误','Errors'],['edits','修改','Edits'],['commands','命令','Commands'],['reads','读取','Reads'],['model','模型','Model']];
    function paintFilters(){element.querySelector('.act-filters').innerHTML=filterNames.map(([key,cn,en])=>'<button type="button" data-act-filter="'+key+'" aria-pressed="'+(filter===key)+'">'+label(cn,en)+'</button>').join('');}
    element.querySelector('.act-filters').addEventListener('click',event=>{const next=event.target.closest('[data-act-filter]');if(next){filter=next.dataset.actFilter;paintFilters();paint();}});
    const localizedLabels=()=>{labels();paintFilters();};
    localizedLabels();
    return {selection:()=>selection,show(){if(!visible){visible=true;connect();}void sessions();},hide(){visible=false;release?.();release=null;},languageChanged:localizedLabels,
      dispose(){alive=false;release?.();release=null;nodes.clear();signatures.clear();}};
  }

  function filterEntries(entries,filter) {
    if(filter==='all')return entries;
    return entries.filter(e=>filter==='errors'?(e.status==='ERROR'||e.type==='error'):e.type===({edits:'file_edit',commands:'command',reads:'file_read',model:'model_response'})[filter]);
  }

  const inlineCSS=css+`
    :host{display:block;min-width:0;color:inherit;font:inherit;--2ag-text-secondary:var(--vscode-descriptionForeground,#888);--2ag-blue:var(--vscode-textLink-foreground,#4285f4);--2ag-outline-variant:color-mix(in srgb,currentColor 12%,transparent)}
    .react-task{margin:4px 0 8px}.react-task>summary{list-style:none;cursor:pointer;display:flex;align-items:baseline;gap:7px;padding:6px 8px;font-size:13px}
    summary::-webkit-details-marker{display:none}.react-chevron{width:10px;height:10px;flex:none;align-self:center;transition:transform 100ms}
    details[open]>summary>.react-chevron{transform:rotate(90deg)}.react-heading-wrap{flex:1;min-width:0}.react-summary{display:block;font-size:11px;color:var(--2ag-text-secondary);line-height:1.5;padding:2px 0 0}
    .react-duration,.react-count{font-size:11px;font-variant-numeric:tabular-nums;color:var(--2ag-text-secondary)}.react-duration{margin-left:auto}
    .react-body{padding-left:12px;border-left:1px solid var(--2ag-outline-variant);margin-left:12px}.react-controls{display:flex;align-items:center;gap:6px;flex-wrap:wrap;padding:5px 4px 8px;color:var(--2ag-text-secondary);font-size:11px}
    .react-controls select,.react-controls button{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;padding:3px 4px;border-radius:3px}.react-controls select{color-scheme:light dark}
    button:hover,summary:hover{background:color-mix(in srgb,currentColor 4%,transparent)}:is(button,select,summary):focus-visible{outline:1px solid var(--2ag-blue);outline-offset:1px}
    .react-phase{margin:5px 0}.react-phase>summary{display:flex;align-items:baseline;gap:7px;list-style:none;cursor:pointer;padding:5px 8px;font-size:12px}
    .react-phase-heading{flex:1;min-width:0}.react-phase-title{overflow-wrap:anywhere}.react-phase-stats{display:block;font-size:11px;line-height:1.6;color:var(--2ag-text-secondary)}
    .react-phase-body{padding:0 8px 5px 24px}.react-focus,.react-current{font-size:11px;line-height:1.6;color:var(--2ag-text-secondary);overflow-wrap:anywhere;margin:3px 0}
    .react-highlights{margin:4px 0}.react-highlight{font-size:12px;display:block;max-width:100%;max-height:3.2em;overflow:hidden;overflow-wrap:anywhere;color:inherit;background:transparent;border:0;text-align:left;padding:3px 0;cursor:pointer}
    .react-highlight-meta{color:var(--2ag-text-secondary);font-size:11px}.react-highlight[data-error=true]{color:var(--2ag-red,#b3261e)}
    .react-trace>summary{cursor:pointer;list-style:none;display:flex;align-items:center;gap:7px;font-size:11px;color:var(--2ag-text-secondary);padding:5px 0}
    .react-trace-body{border-left:1px solid var(--2ag-outline-variant);margin:3px 0 4px 4px;padding-left:5px}.react-task .act-time{display:inline-block;min-width:52px;margin-right:7px}
    .act-entry{border:0;margin:0}.act-entry>summary{display:flex;align-items:start;gap:8px;padding:5px 8px;grid-template-columns:none;font-size:12px;min-height:28px;box-sizing:border-box}
    .act-entry>summary .react-chevron{width:8px;height:8px;margin-top:4px}.react-action{flex:1;min-width:0}.act-title{white-space:normal;overflow-wrap:anywhere;display:block}
    .act-sub{font-size:11px;line-height:1.5}.react-command{display:block;white-space:pre-wrap;overflow-wrap:anywhere;font:11px/1.5 ui-monospace,Consolas,monospace;max-height:3em;overflow:hidden}
    .act-status{font-size:10px;white-space:nowrap}.act-duration{font-size:11px;min-width:36px;text-align:right;white-space:nowrap}.act-detail{padding:0 8px 8px 23px}
    .act-model{border:0;background:transparent;color:inherit;font:inherit;cursor:pointer;text-align:left;padding:0}.act-model:hover{text-decoration:underline}
    .react-note{font-size:10px;color:var(--2ag-text-secondary);padding:4px 8px}
  `;
  const chevron='<svg class="react-chevron" viewBox="0 0 12 12" aria-hidden="true"><path d="m4 2 4 4-4 4" fill="none" stroke="currentColor" stroke-width="1.3"/></svg>';
  const activeStatus=e=>['PENDING','RUNNING','GENERATING','WAITING','QUEUED'].includes(e.status);
  const entryKey=e=>identities(e)[0];
  function actionPhase(e){
    if(e.type==='file_read')return 'inspect';
    if(e.type==='search')return 'search';
    if(e.type==='file_edit')return 'edit';
    if(e.type==='command')return /^(?:npx\s+)?(?:vitest|jest|pytest|go\s+test|cargo\s+(?:test|check)|(?:npm|pnpm|yarn|bun)\s+(?:(?:run|exec)\s+)?(?:test|check|lint|typecheck|vitest|jest|pytest|tsc)|tsc)(?:\s|$)/i.test(e.target.trim())?'verify':'command';
    return 'tool';
  }
  const quietEntry=e=>['model_response','checkpoint','waiting'].includes(e.type);
  const directory=e=>['file_read','file_edit'].includes(e.type)?e.target.replace(/\\/g,'/').replace(/\/[^/]*$/,''):'';
  // Presentation phases keep every entry, in its original order. Ordinary
  // models/checkpoints belong to the surrounding work, never a parallel log.
  function presentationPhases(entries){
    const phases=[],nextActions=[];let current=null,next=null;
    for(let i=entries.length-1;i>=0;i--){nextActions[i]=next;if(!quietEntry(entries[i]))next=entries[i];}
    for(let i=0;i<entries.length;i++){
      const e=entries[i],error=e.status==='ERROR'||e.type==='error',quiet=quietEntry(e)&&!error;
      const kind=error?'error':quiet?'model':actionPhase(e),dir=directory(e);
      const executionBoundary=current?.executionId&&e.executionId&&current.executionId!==e.executionId;
      const actionBoundary=current&&!quiet&&current.actions.length&&current.kind!==kind;
      // A repeated move into another directory is evidence of a new locality.
      // A single ancillary read is insufficient to split a phase.
      const nextAction=nextActions[i];
      const localityBoundary=current&&kind==='inspect'&&current.kind==='inspect'&&current.actions.length>=3&&dir&&current.directory&&dir!==current.directory&&nextAction?.type==='file_read'&&directory(nextAction)===dir;
      if(!current||executionBoundary||actionBoundary||localityBoundary){
        current={key:entryKey(e),kind,executionId:e.executionId,directory:dir,entries:[],actions:[]};phases.push(current);
      }
      if(!quiet){if(!current.actions.length){current.kind=kind;current.directory=dir;}current.actions.push(e);}
      if(!current.executionId&&e.executionId)current.executionId=e.executionId;
      current.entries.push(e);
    }
    return phases;
  }
  function actionTitle(e,label){
    const doing=activeStatus(e),done=e.status==='DONE',failed=e.status==='ERROR',name=basename(e.target)||label('文件','file');
    if(e.type==='file_read')return label(failed?'读取失败 · ':doing?'正在读取 ':done?'读取了 ':'读取记录 · ',failed?'Read failed · ':doing?'Reading ':done?'Read ':'Read record · ')+name;
    if(e.type==='file_edit'){const deleted=e.details.action==='deleteFile';return label(deleted?(failed?'删除失败 · ':doing?'正在删除 ':done?'删除了 ':'删除记录 · '):(failed?'修改失败 · ':doing?'正在修改 ':done?'修改了 ':'修改记录 · '),deleted?(failed?'Delete failed · ':doing?'Deleting ':done?'Deleted ':'Delete record · '):(failed?'Edit failed · ':doing?'Editing ':done?'Edited ':'Edit record · '))+name;}
    if(e.type==='command')return label(failed?'命令失败':doing?'命令正在运行':done?'运行了命令':'命令记录',failed?'Command failed':doing?'Running command':done?'Ran command':'Command record');
    if(e.type==='search')return label(failed?'搜索失败 · ':doing?'正在搜索 ':done?'搜索了 ':'搜索记录 · ',failed?'Search failed · ':doing?'Searching ':done?'Searched ':'Search record · ')+(e.target||'—');
    if(e.type==='model_response')return doing?label('模型处理中','Model processing'):label('模型响应','Model response');
    if(e.type==='checkpoint')return label('检查点','Checkpoint');
    if(e.type==='waiting')return label('等待中','Waiting');
    if(e.type==='error')return label('错误','Error');
    return label('工具 · ','Tool · ')+(e.details.toolName||e.title);
  }
  function displayPaths(paths){
    return paths.map(path=>{
      const parts=path.replace(/\\/g,'/').split('/').filter(Boolean);let depth=1;
      while(depth<parts.length&&paths.some(other=>other!==path&&other.replace(/\\/g,'/').split('/').filter(Boolean).slice(-depth).join('/')===parts.slice(-depth).join('/')))depth++;
      return parts.slice(-depth).join('/');
    });
  }
  function phaseTitle(phase,current,label){
    const paths=[...new Set(phase.actions.filter(e=>['file_read','file_edit'].includes(e.type)).map(e=>e.target).filter(Boolean))];
    const files=!paths.length?label('文件','files'):paths.length<=3?displayPaths(paths).join(label('、',', ')):label(paths.length+' 个文件',paths.length+' files');
    const hasModel=phase.entries.some(e=>e.type==='model_response');
    const confirmed=phase.actions.length>0&&phase.actions.every(e=>e.status==='DONE');
    const names={
      inspect:[(current?'正在检查 ':confirmed?'已检查 ':'读取记录 · ')+files,(current?'Inspecting ':confirmed?'Inspected ':'Read records · ')+files],
      edit:[(current?'正在修改 ':confirmed?'已修改 ':'修改记录 · ')+files,(current?'Editing ':confirmed?'Edited ':'Edit records · ')+files],
      search:[current?'正在搜索':confirmed?'已搜索':'搜索记录',current?'Searching':confirmed?'Searched':'Search records'],
      verify:[current?'正在运行验证':confirmed?'已运行验证':'验证记录',current?'Running verification':confirmed?'Ran verification':'Verification records'],
      command:[current?'正在运行命令':confirmed?'已运行命令':'命令记录',current?'Running commands':confirmed?'Ran commands':'Command records'],
      error:[current?'正在处理错误记录':'发现错误记录',current?'Processing error records':'Error recorded'],
      tool:[current?'正在执行工具操作':confirmed?'已执行工具操作':'工具记录',current?'Running tools':confirmed?'Ran tools':'Tool records'],
      model:hasModel?[current?'正在进行模型处理':'已进行模型处理',current?'Processing model requests':'Processed model requests']:[(current?'正在执行 ':'已记录 ')+phase.entries.length+' 个操作',(current?'Processing ':'Recorded ')+phase.entries.length+' activities']
    };
    return label(...(names[phase.kind]||names.tool));
  }
  function phaseStats(phase,label){
    const entries=phase.entries,paths=new Set(entries.filter(e=>['file_read','file_edit'].includes(e.type)).map(e=>e.target).filter(Boolean)),items=[];
    if(paths.size)items.push(label(paths.size+' 个文件',paths.size+(paths.size===1?' file':' files')));
    const count=type=>entries.filter(e=>e.type===type).length;
    const nativeTools=entries.filter(e=>e.toolCallId).length;if(nativeTools)items.push(label(nativeTools+' 次原生工具调用',nativeTools+' native tool calls'));
    for(const [type,cn,en]of [['file_read','次读取','reads'],['command','个命令','commands'],['file_edit','次修改','edits'],['search','次搜索','searches'],['model_response','次模型处理','model requests'],['tool','次工具调用','tool calls'],['checkpoint','个检查点','checkpoints'],['waiting','次等待','waits']]){
      const n=count(type);if(n)items.push(label(n+' '+cn,n+' '+en));
    }
    const requests=entries.filter(e=>e.type==='model_response'),usage=requests.map(e=>e.tokenUsage),values=usage.map(u=>u?.request_input??u?.observed_input).filter(known);
    if(values.length){const sum=values.reduce((a,b)=>a+b,0),exact=usage.length===values.length&&usage.every(u=>known(u?.request_input));items.push(window.TwoAgContextView.amount(exact?sum:null,sum)+' '+label('输入处理','processed'));}
    const errors=entries.filter(e=>e.status==='ERROR'||e.type==='error').length;if(errors)items.push(label(errors+' 条错误记录',errors+' error records'));
    return items.join(' · ');
  }
  function phaseDuration(phase,current){
    const starts=phase.entries.map(e=>e.createdAt??e.startedAt).filter(known),ends=phase.entries.map(e=>e.completedAt??e.details.finishedGeneratingAt).filter(known);
    const start=starts.length?Math.min(...starts):null,end=current?Date.now():ends.length?Math.max(...ends):null;
    return known(start)&&known(end)&&end>=start?end-start:null;
  }
  function taskSummary(entries,label){
    const count=type=>entries.filter(e=>e.type===type).length,items=[];
    for(const [type,cn,en]of [['file_read','读取','reads'],['command','运行命令','commands'],['file_edit','修改','edits'],['search','搜索','searches'],['model_response','模型响应','model responses'],['tool','工具','tools']]){const n=count(type);if(n)items.push(label(cn+' '+n+' 次',n+' '+en));}
    return items.join(' · ');
  }
  function elapsedText(ms,label){
    if(!known(ms))return '—';const seconds=Math.floor(ms/1000),minutes=Math.floor(seconds/60);
    return minutes?label(minutes+'分'+seconds%60+'秒',minutes+'m '+seconds%60+'s'):label(seconds+'秒',seconds+'s');
  }

  function currentFiber(node){
    let f=node?.[Object.keys(node).find(k=>k.startsWith('__reactFiber$'))];
    if(!f)return null;let root=f;while(root.return)root=root.return;
    return root.stateNode?.current&&root.stateNode.current!==root ? f.alternate||f : f;
  }
  function nativeProps(node,predicate){
    for(let f=currentFiber(node),i=0;f&&i<26;f=f.return,i++){
      const props=f.memoizedProps;if(props&&predicate(props))return props;
    }
    return null;
  }

  // Presentation only: native turn props provide identity/placement; every
  // displayed action, result and usage still comes from the shared reader.
  function mountInline({request,language=()=> 'zh-CN',openRequest=()=>{},entriesChanged=()=>{},initialPresentation={},persistPresentation=async()=>{}}){
    let alive=true,view=null,state={sessionId:'',entries:[]},queued=false,session='',saveTimer=null,saveState='',userChanged=false,saveRevision=0,scroll=null;
    let preferences={language:'follow'},folds={};
    const tasks=new Map(),rows=new Map(),channel=new MessageChannel();
    const label=(cn,en)=>(preferences.language==='follow'?language():preferences.language)==='en-US'?en:cn;
    const snapshot=()=>({version:3,language:preferences.language,folds});
    function restore(value){if(!value||typeof value!=='object')return;if(['follow','zh-CN','en-US'].includes(value.language))preferences.language=value.language;if(value.version===3&&value.folds&&typeof value.folds==='object'&&!Array.isArray(value.folds))folds=value.folds;}
    restore(initialPresentation);
    const sheet=document.createElement('style');sheet.dataset.twoagOwned='inline-activity';sheet.textContent='[data-twoag-react-original]{display:none!important}';document.head.append(sheet);
    const observer=new MutationObserver(changes=>{if(changes.some(c=>!c.target.closest?.('[data-twoag-owned],[data-testid="agent-input-box"],input,textarea,[contenteditable="true"]')))schedule();});
    const rootObserver=new MutationObserver(changes=>{if(!view?.isConnected||changes.some(c=>[...c.addedNodes].some(n=>n.nodeType===1&&(n.matches('[data-testid="conversation-view"]')||n.querySelector('[data-testid="conversation-view"]')))))schedule();});
    rootObserver.observe(document.body,{childList:true,subtree:true});
    channel.port1.onmessage=()=>{queued=false;if(alive)paint();};
    function schedule(){if(alive&&!queued){queued=true;channel.port2.postMessage(null);}}
    function save(){saveRevision++;clearTimeout(saveTimer);saveState='pending';saveTimer=setTimeout(flushSave,300);}
    async function flushSave(){clearTimeout(saveTimer);saveTimer=null;const value=snapshot(),revision=saveRevision;try{await persistPresentation(value);if(revision===saveRevision)saveState='saved';}catch(error){if(revision===saveRevision)saveState='save_failed';console.warn('[2Ag ReAct] Presentation preference save failed:',error.message);}if(alive)schedule();}
    function changed(){userChanged=true;save();schedule();}
    function hide(r,node){if(!node||node===r.turn||node.contains(r.host))return;r.nextOriginals.add(node);if(!r.originals.has(node))r.originals.set(node,node.getAttribute('data-twoag-react-original'));node.setAttribute('data-twoag-react-original','');}
    function restoreNode(node,value){if(value===null)node.removeAttribute('data-twoag-react-original');else node.setAttribute('data-twoag-react-original',value);}
    function disposeTask(r){r.host.remove();for(const [node,value]of r.originals)restoreNode(node,value);}
    function clear(keepRows=false){for(const r of tasks.values())disposeTask(r);tasks.clear();if(!keepRows)rows.clear();}
    function bindView(){const next=document.querySelector('[data-testid="conversation-view"]');if(view===next)return;observer.disconnect();scroll?.dispose();scroll=null;clear(true);view=next;if(view)observer.observe(view,{childList:true,subtree:true});}
    function bindScroll(viewport){
      if(scroll?.viewport===viewport)return;scroll?.dispose();scroll=null;if(!viewport)return;
      const c={viewport,follow:viewport.scrollHeight-viewport.scrollTop-viewport.clientHeight<48,top:viewport.scrollTop,pin:null,inputUntil:0,direction:0,adjusting:false,frame:null,adjustFrame:null};
      const bottom=()=>Math.max(0,viewport.scrollHeight-viewport.clientHeight);
      function remember(){
        c.top=viewport.scrollTop;if(c.follow){c.pin=null;return;}
        const rect=viewport.getBoundingClientRect(),candidates=[...rows.values(),...[...tasks.values()].flatMap(r=>[...r.groups.values()].map(g=>g.wrapper))];
        const node=candidates.filter(n=>{if(!n.isConnected||n.checkVisibility?.()===false)return false;const r=n.getBoundingClientRect();return r.height>0&&r.bottom>rect.top&&r.top<rect.bottom;}).sort((a,b)=>Math.abs(a.getBoundingClientRect().top-rect.top)-Math.abs(b.getBoundingClientRect().top-rect.top))[0];
        if(node)c.pin={node,offset:node.getBoundingClientRect().top-rect.top};
      }
      function write(top){
        const target=Math.max(0,Math.min(top,bottom()));if(Math.abs(target-viewport.scrollTop)<1)return;
        c.adjusting=true;cancelAnimationFrame(c.adjustFrame);viewport.scrollTop=target;
        c.adjustFrame=requestAnimationFrame(()=>{c.adjusting=false;});
      }
      function repair(){
        if(!alive||!viewport.isConnected)return;
        const pin=c.pin,target=pin?.node.isConnected&&pin.node.checkVisibility?.()!==false&&pin.node.getBoundingClientRect().height>0?viewport.scrollTop+pin.node.getBoundingClientRect().top-viewport.getBoundingClientRect().top-pin.offset:c.top;
        write(target);
      }
      function queueRepair(){if(c.frame!==null)return;c.frame=requestAnimationFrame(()=>{c.frame=null;if(!c.follow)repair();});}
      function input(event){
        if(event.target.closest?.('[data-testid="agent-input-box"],input,textarea,[contenteditable="true"]'))return;
        const jump=event.target.closest?.('[data-testid="scroll-to-bottom"],[aria-label="Scroll to Bottom"]');
        if(event.type==='keydown'&&!jump&&!['ArrowUp','ArrowDown','PageUp','PageDown','Home','End'].includes(event.key))return;
        // A pointer on the viewport itself can be a scrollbar gesture. Clicks
        // inside phase controls do not grant native resize-triggered scrolling.
        if(event.type==='pointerdown'&&event.target!==viewport&&!jump)return;
        if(jump)c.follow=true;
        c.inputUntil=performance.now()+1000;
        c.direction=event.type==='wheel'?Math.sign(event.deltaY):event.type==='keydown'?(['ArrowUp','PageUp','Home'].includes(event.key)?-1:1):0;
        if(event.type==='wheel'&&event.deltaY<0||event.type==='keydown'&&['ArrowUp','PageUp','Home'].includes(event.key)){c.follow=false;remember();}
      }
      function onScroll(){
        if(c.adjusting)return;
        if(performance.now()<c.inputUntil){c.follow=c.direction<0?false:bottom()-viewport.scrollTop<48;remember();}
        else if(!c.follow)queueRepair();else remember();
      }
      for(const name of ['wheel','touchstart','pointerdown','keydown'])view.addEventListener(name,input,{passive:true});
      viewport.addEventListener('scroll',onScroll,{passive:true});remember();
      scroll={viewport,before(){if(c.follow)remember();else repair();},after(){if(c.follow)write(bottom());else{repair();queueRepair();}},dispose(){
        for(const name of ['wheel','touchstart','pointerdown','keydown'])view?.removeEventListener(name,input);
        viewport.removeEventListener('scroll',onScroll);cancelAnimationFrame(c.frame);cancelAnimationFrame(c.adjustFrame);
      }};
    }
    function prefsFor(key){const saved=folds[key];if(!saved||typeof saved!=='object'||Array.isArray(saved))folds[key]={open:null,wasRunning:null,groupMode:'default',groups:{},traces:{}};const p=folds[key];for(const field of ['groups','traces'])if(!p[field]||typeof p[field]!=='object'||Array.isArray(p[field]))p[field]={};return p;}
    function makeTask(key,turn){
      const host=document.createElement('div');host.dataset.twoagOwned='inline-activity';host.dataset.twoagInline='';host.dataset.twoagReactTask=key;
      const shadow=host.attachShadow({mode:'open'});shadow.innerHTML='<style>'+inlineCSS+'</style><details class="react-task"><summary>'+chevron+'<span class="react-heading-wrap"><span class="react-heading"></span><span class="react-summary"></span></span><span class="react-duration"></span></summary><div class="react-body"><div class="react-controls"><label><span data-language-label></span> <select data-react-language><option value="follow"></option><option value="zh-CN">中文</option><option value="en-US">English</option></select></label><button type="button" data-react-expand></button><button type="button" data-react-collapse></button></div><div class="react-groups"></div><div class="react-note"></div></div></details>';
      const r={key,turn,host,shadow,shell:shadow.querySelector('.react-task'),list:shadow.querySelector('.react-groups'),groups:new Map(),originals:new Map(),nextOriginals:new Set(),entries:[],running:null};tasks.set(key,r);
      r.shell.addEventListener('toggle',()=>{const p=prefsFor(key);if(r.shell.open!==p.open){p.open=r.shell.open;changed();}});
      shadow.querySelector('[data-react-language]').addEventListener('change',event=>{preferences.language=event.target.value;changed();});
      shadow.querySelector('[data-react-expand]').addEventListener('click',()=>{const p=prefsFor(key);p.open=true;p.groupMode='open';p.groups={};p.traces={};changed();});
      shadow.querySelector('[data-react-collapse]').addEventListener('click',()=>{const p=prefsFor(key);p.open=false;p.groupMode='closed';p.groups={};p.traces={};changed();});
      return r;
    }
    function renderEntry(e){
      const key=entryKey(e);if(!key)return null;let node=rows.get(key);
      if(!node){node=document.createElement('details');node.className='act-entry';node.dataset.activityKey=key;node.innerHTML='<summary>'+chevron+'<span class="react-action"><span class="act-title"></span><code class="react-command" data-user-content></code><span class="act-sub"></span></span><span class="act-status"></span><span class="act-duration"></span></summary><div class="act-detail"></div>';rows.set(key,node);
        node.addEventListener('toggle',()=>{if(node.open)node.querySelector('.act-detail').innerHTML=detailHTML(node.entry,label);});
        node.addEventListener('click',event=>{if(event.target.closest('.act-model')){event.preventDefault();openRequest(node.entry);}});
      }
      const signature=JSON.stringify(e)+'|'+label('zh','en');node.entry=e;
      if(node.dataset.status!==e.status)node.dataset.status=e.status;
      if(node.signature!==signature){node.signature=signature;const title=node.querySelector('.act-title'),text=actionTitle(e,label);title.title=e.target||e.title;
        if(e.type==='model_response'){if(!title.querySelector('button'))title.innerHTML='<button type="button" class="act-model"></button>';title.firstChild.textContent=text;title.firstChild.title=label('打开此请求的 Token Inspector','Open this request in Token Inspector');}else title.textContent=text;
        const command=node.querySelector('.react-command');command.hidden=e.type!=='command';command.textContent=e.type==='command'?e.target:'';command.title=command.textContent;
        const d=e.details,extras=[];
        if(e.type==='file_read'&&(known(d.startLine)||known(d.endLine)))extras.push('L'+(d.startLine??'—')+'–'+(d.endLine??'—'));
        if(e.type==='file_edit'&&(known(d.additions)||known(d.deletions)))extras.push('+'+(d.additions??'—')+' −'+(d.deletions??'—'));
        if(e.type==='command'&&known(d.exitCode))extras.push('exit '+d.exitCode);
        if(e.type==='search'&&known(d.totalResults))extras.push(d.totalResults+label(' 个结果',' results'));
        if(e.type==='model_response'){if(e.tokenUsage?.model)extras.push(e.tokenUsage.model);if(e.tokenUsage)extras.push(tokenText(e.tokenUsage,label));}
        const sub=node.querySelector('.act-sub');sub.replaceChildren();const time=document.createElement('span');time.className='act-time';time.textContent=known(e.createdAt??e.startedAt)?new Date(e.createdAt??e.startedAt).toLocaleTimeString(label('zh-CN','en-US'),{hour12:false,hour:'2-digit',minute:'2-digit',second:'2-digit'}):'—';sub.append(time,document.createTextNode(extras.join(' · ')));
        const statuses={PENDING:['等待执行','Pending'],RUNNING:['运行中','Running'],GENERATING:['生成中','Generating'],WAITING:['等待中','Waiting'],QUEUED:['排队中','Queued'],ERROR:['错误','Error'],CANCELED:['已取消','Canceled'],INTERRUPTED:['已中断','Interrupted'],INVALID:['无效','Invalid'],CLEARED:['已清理','Cleared'],UNKNOWN:['状态未知','Status unknown']};
        node.querySelector('.act-status').textContent=statuses[e.status]?label(...statuses[e.status]):e.status==='DONE'?'':e.status;
        node.querySelector('.act-duration').textContent=fmtDuration(e.duration);
        if(node.open){const detail=node.querySelector('.act-detail'),opened=[...detail.querySelectorAll('details[open]')].map(n=>n.querySelector('summary').textContent);detail.innerHTML=detailHTML(e,label);for(const n of detail.querySelectorAll('details'))if(opened.includes(n.querySelector('summary').textContent))n.open=true;}
      }
      return node;
    }
    function renderPhases(r){
      const phases=presentationPhases(r.entries),seen=new Set(),prefs=prefsFor(r.key);let previous=null;
      for(const [index,phase]of phases.entries()){seen.add(phase.key);let g=r.groups.get(phase.key);const current=r.running&&index===phases.length-1;
        if(!g){
          const wrapper=document.createElement('details');wrapper.className='react-phase';wrapper.dataset.phaseKey=phase.key;
          wrapper.innerHTML='<summary>'+chevron+'<span class="react-phase-heading"><span class="react-phase-title"></span><span class="react-phase-stats"></span></span><span class="react-duration"></span></summary><div class="react-phase-body"><div class="react-focus"></div><div class="react-highlights"></div><div class="react-current"></div><details class="react-trace"><summary>'+chevron+'<span class="react-trace-label"></span></summary><div class="react-trace-body"></div></details></div>';
          g={wrapper,shell:wrapper,trace:wrapper.querySelector('.react-trace'),body:wrapper.querySelector('.react-trace-body')};r.groups.set(phase.key,g);
          g.shell.addEventListener('toggle',()=>{const p=prefsFor(r.key);if(g.shell.open!==g.expectedOpen){p.groups[phase.key]=g.shell.open;g.expectedOpen=g.shell.open;changed();}});
          g.trace.addEventListener('toggle',()=>{const p=prefsFor(r.key);if(g.trace.open!==g.expectedTrace){p.traces[phase.key]=g.trace.open;g.expectedTrace=g.trace.open;changed();}});
        }
        g.wrapper.dataset.current=String(current);g.wrapper.dataset.kind=phase.kind;g.phase=phase;
        g.wrapper.querySelector('.react-phase-title').textContent=phaseTitle(phase,current,label);
        const stats=g.wrapper.querySelector('.react-phase-stats');stats.textContent=phaseStats(phase,label);stats.title=label('按本阶段已读取的行为与共享 Token Usage 汇总；缺失输入时保留 ≥ 下界。','Derived from observed phase activities and shared Token Usage; missing input preserves a ≥ lower bound.');
        g.wrapper.querySelector('.react-duration').textContent=elapsedText(phaseDuration(phase,current),label);
        g.expectedOpen=prefs.groups[phase.key]??(prefs.groupMode==='open'?true:prefs.groupMode==='closed'?false:current);g.shell.open=g.expectedOpen;
        g.expectedTrace=prefs.traces[phase.key]??(prefs.groupMode==='open'||prefs.groups[phase.key]===true);g.trace.open=g.expectedTrace;
        g.wrapper.querySelector('.react-trace-label').textContent=label('详细过程 · '+phase.entries.length+' 条行为','Detailed process · '+phase.entries.length+' activities');
        const observed=phase.entries.filter(e=>['file_read','file_edit'].includes(e.type)&&e.status==='DONE'),paths=[...new Set(observed.map(e=>e.target).filter(Boolean))];
        const focus=g.wrapper.querySelector('.react-focus');focus.textContent=paths.length?label('已处理文件：','Observed files: ')+displayPaths(paths).slice(0,4).join(label('、',', '))+(paths.length>4?label(' 等 '+paths.length+' 个文件',' and '+(paths.length-4)+' more files'):''):'';focus.hidden=!paths.length;focus.title=paths.join('\n');
        const important=phase.entries.filter(e=>['command','file_edit','search'].includes(e.type)||e.status==='ERROR'||e.type==='error');
        const selected=[...new Set([...important.filter(e=>e.status==='ERROR'||e.type==='error').slice(-2),...important.slice(-3)])];
        const highlights=g.wrapper.querySelector('.react-highlights'),wantedHighlights=new Set(selected.map(entryKey));
        for(const button of [...highlights.children])if(!wantedHighlights.has(button.dataset.entryKey))button.remove();
        let priorHighlight=null;
        for(const e of selected){
          const key=entryKey(e);let button=[...highlights.children].find(n=>n.dataset.entryKey===key);
          if(!button){button=document.createElement('button');button.type='button';button.className='react-highlight';button.dataset.entryKey=key;button.addEventListener('click',()=>{const p=prefsFor(r.key);p.traces[phase.key]=true;g.expectedTrace=true;g.trace.open=true;const row=rows.get(key);if(row)row.open=true;changed();});}
          button.dataset.error=String(e.status==='ERROR'||e.type==='error');button.title=e.target||e.title;
          const extras=e.type==='file_edit'?((known(e.details.additions)?' +'+e.details.additions:'')+(known(e.details.deletions)?' −'+e.details.deletions:'')):e.type==='command'&&known(e.details.exitCode)?' · exit '+e.details.exitCode:'';
          button.textContent=actionTitle(e,label)+(e.type==='command'&&e.target?' · '+e.target.split(/\r?\n/)[0]:'')+extras;
          const expected=priorHighlight?priorHighlight.nextSibling:highlights.firstChild;if(button!==expected)highlights.insertBefore(button,expected||null);priorHighlight=button;
        }
        const activity=current?(phase.entries.findLast(activeStatus)||phase.entries.at(-1)):null,now=g.wrapper.querySelector('.react-current');
        now.textContent=activity?(activeStatus(activity)?label('当前：','Current: '):label('最近完成：','Latest completed: '))+actionTitle(activity,label)+(activity.type==='model_response'&&activity.tokenUsage?' · '+tokenText(activity.tokenUsage,label):''):'';now.hidden=!current;
        const nodes=phase.entries.map(renderEntry).filter(Boolean),wanted=new Set(nodes);let prev=null;
        for(const node of nodes){const expected=prev?prev.nextSibling:g.body.firstChild;if(node!==expected)g.body.insertBefore(node,expected||null);prev=node;}
        for(const node of [...g.body.children])if(!wanted.has(node))node.remove();
        const expected=previous?previous.nextSibling:r.list.firstChild;if(g.wrapper!==expected)r.list.insertBefore(g.wrapper,expected||null);previous=g.wrapper;
      }
      for(const [key,g]of r.groups)if(!seen.has(key)){g.wrapper.remove();r.groups.delete(key);}
    }
    function nativeStepRoot(anchor){
      let host=null;
      for(let f=currentFiber(anchor),i=0;f&&i<26;f=f.return,i++){
        if(f.tag===5)host=f.stateNode;
        if(f.memoizedProps?.step?.metadata&&Object.hasOwn(f.memoizedProps,'debugMode'))return {node:host,step:f.memoizedProps.step};
      }
      return null;
    }
    function maskNative(r){
      r.nextOriginals=new Set();
      // Official renderer evidence: execution body is the Worked-for container;
      // lastTextResponse is rendered as a sibling tail, never inside this body.
      for(const button of r.turn.querySelectorAll('[data-testid="worked-for-collapsible"]'))hide(r,button.parentElement);
      for(const button of r.turn.querySelectorAll('[data-testid="tool-group-collapsible"]'))hide(r,button.parentElement);
      const models=r.entries.filter(e=>e.type==='model_response'),lastModel=models.at(-1)?.stepIndex;
      const keys=new Set(r.entries.map(entryKey));
      for(const anchor of r.turn.querySelectorAll('[data-testid="view-file-step"],[data-testid="run-command-step"],[data-testid="diff-line-count"],[data-testid="planner-response-text"]')){
        const resolved=nativeStepRoot(anchor),source=resolved?.step.metadata?.sourceTrajectoryStepInfo;if(!source)continue;
        const e=r.entries.find(e=>e.stepIndex===source.stepIndex&&e.trajectoryId===source.trajectoryId);if(!e||!keys.has(entryKey(e)))continue;
        if(e.type!=='model_response'||e.stepIndex!==lastModel)hide(r,resolved.node);
      }
      // Only hide the dedicated native Thought trigger/component. No thought
      // text is accessed; Model response displays generation facts, not CoT.
      for(const thought of r.turn.querySelectorAll('[data-testid="thinking-collapsible-trigger"]'))hide(r,thought.parentElement);
      for(const [node,value]of r.originals)if(!r.nextOriginals.has(node)){restoreNode(node,value);r.originals.delete(node);}
    }
    function taskDefinition(turn){
      const props=nativeProps(turn,p=>p.container&&Array.isArray(p.container.steps)&&typeof p.isRunning==='boolean');if(!props||props.cascadeId!==state.sessionId)return null;
      const steps=props.container.steps,first=steps.find(s=>s.type===14||s.step?.case==='userInput')||steps[0],source=first?.metadata?.sourceTrajectoryStepInfo;
      if(!source||source.cascadeId&&source.cascadeId!==state.sessionId)return null;
      const members=new Set(steps.map(s=>{const m=s.metadata?.sourceTrajectoryStepInfo;return m?m.trajectoryId+'|'+m.stepIndex:null;}).filter(Boolean));
      const entries=state.entries.filter(e=>e.sessionId===state.sessionId&&members.has(e.trajectoryId+'|'+e.stepIndex));
      if(!entries.length)return null;
      return {key:state.sessionId+'|'+source.trajectoryId+'|turn:'+source.stepIndex,entries,running:props.isRunning,start:timestamp(first.metadata?.createdAt),hasUserInput:first.type===14||first.step?.case==='userInput'};
    }
    function paint(){
      bindView();if(!view)return;const native=provider()?.getState(),route=location.pathname.match(/\/c\/([^/?]+)/);
      if(!state.sessionId||native?.conversationId!==state.sessionId||location.pathname==='/'||route&&route[1]!==state.sessionId){clear();return;}
      if(session!==state.sessionId){scroll?.dispose();scroll=null;clear();session=state.sessionId;}
      bindScroll(view.querySelector('[data-testid="autoscroll-viewport"]'));scroll?.before();
      const seen=new Set();
      for(const turn of view.querySelectorAll('[data-turn-content]')){
        const definition=taskDefinition(turn);if(!definition)continue;seen.add(definition.key);let r=tasks.get(definition.key);
        if(r&&r.turn!==turn){disposeTask(r);tasks.delete(definition.key);r=null;}
        r||=makeTask(definition.key,turn);r.entries=definition.entries;r.running=definition.running;
        const p=prefsFor(r.key),previousRunning=p.wasRunning;
        if(p.open===null||p.open===undefined)p.open=r.running;
        if(previousRunning===true&&!r.running){p.open=false;p.groupMode='default';p.groups={};p.traces={};save();}
        else if(previousRunning===false&&r.running){p.open=true;save();}
        p.wasRunning=r.running;r.shell.open=!!p.open;r.shell.dataset.running=String(r.running);
        const firstTime=definition.start??r.entries.map(e=>e.createdAt??e.startedAt).find(known),ends=r.entries.map(e=>e.completedAt??e.details.finishedGeneratingAt).filter(known),end=r.running?Date.now():ends.length?Math.max(...ends):null;
        const duration=known(firstTime)&&known(end)&&end>=firstTime?end-firstTime:null;
        const stopped=!r.running&&['CANCELED','INTERRUPTED'].includes(r.entries.at(-1)?.status),errors=r.entries.filter(e=>e.status==='ERROR').length;
        r.shadow.querySelector('.react-heading').textContent=r.running?label('正在执行','Running'):stopped?label('已停止','Stopped'):label('已完成','Completed');
        r.shadow.querySelector('.react-duration').textContent=elapsedText(duration,label);r.shadow.querySelector('.react-duration').title=label('根据原生步骤时间戳计算的任务跨度；未获取的步骤不计入。','Task span from native step timestamps; unavailable steps are not included.');
        r.shadow.querySelector('.react-summary').textContent=taskSummary(r.entries,label)+(errors?' · '+label(errors+' 条错误记录',errors+' error records'):'');
        const controls=r.shadow.querySelector('.react-controls');controls.querySelector('[data-language-label]').textContent=label('语言','Language');
        controls.querySelector('option[value="follow"]').textContent=label('跟随 2Ag','Follow 2Ag');controls.querySelector('[data-react-language]').value=preferences.language;
        controls.querySelector('[data-react-expand]').textContent=label('全部展开','Expand all');controls.querySelector('[data-react-collapse]').textContent=label('全部折叠','Collapse all');
        r.shadow.querySelector('.react-note').textContent=saveState==='save_failed'?label('显示偏好未保存；可再次选择重试。','Display preferences were not saved; change the selection to retry.'):!definition.hasUserInput||!state.historyComplete?label('正在补齐已持久化历史；当前显示已读取的步骤。','Loading persisted history; showing observed steps.') : '';
        renderPhases(r);maskNative(r);
        const response=[...turn.children].find(n=>n.getAttribute('aria-label')==='Agent response');if(r.host.parentElement!==turn)turn.insertBefore(r.host,response||null);
      }
      for(const [key,r]of tasks)if(!seen.has(key)||!r.host.isConnected){disposeTask(r);tasks.delete(key);}
      const validKeys=new Set(state.entries.map(entryKey));for(const key of rows.keys())if(!validKeys.has(key))rows.delete(key);
      scroll?.after();
    }
    const release=acquireReader(request,'',next=>{state=next;entriesChanged(next.entries);schedule();});schedule();
    return {languageChanged(){schedule();},preferences:snapshot,restorePreferences(value){if(!userChanged){restore(value);schedule();}},
      dispose(){alive=false;release();observer.disconnect();rootObserver.disconnect();scroll?.dispose();channel.port1.close();channel.port2.close();clear();sheet.remove();if(saveTimer)void flushSave();}};
  }

  window.TwoAgActivity={mount,mountInline,normalize,mergeEntries,readRuntime,project,decode,filterEntries};
})();
