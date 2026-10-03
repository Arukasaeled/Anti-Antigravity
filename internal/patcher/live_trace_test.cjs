const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const fs=require('node:fs');
const {webcrypto}=require('node:crypto');

function fixture(steps,extra={}) {
  const events=steps.map((step,index)=>({step,metadata:{createdAt:{seconds:1700000000},...(step.case==='generic'?{toolCall:{name:'run_command'}}:{})},status:3}));
  let aborted=false;
  const scope={console,crypto:webcrypto,setInterval,clearInterval,setTimeout,clearTimeout,AbortController,location:{pathname:'/c/fixture-conversation'},window:{Wj:{lsClient:{
    async *streamAgentStateUpdates(_,options) {
      try { yield {update:{fullyIdle:true,mainTrajectoryUpdate:{stepsUpdate:{steps:events,indices:events.map((_,i)=>i),totalLength:events.length}}}};
        await new Promise(resolve=>options.signal.addEventListener('abort',resolve,{once:true}));
      } finally {aborted=true;}
    },...extra
  }}}};
  const create=vm.runInNewContext(fs.readFileSync(__dirname+'/live_trace.js','utf8'),scope);
  return {trace:create({conversation:{current:()=>({key:'host:fixture-conversation'})}}),aborted:()=>aborted};
}

test('public trace omits hidden reasoning and credential-bearing commands',async()=>{
  const secret='fixture-private-value';
  const f=fixture([
    {case:'runCommand',value:{commandLine:'tool --password '+secret}},
    {case:'runCommand',value:{commandLine:'tool --api-key "'+secret+'"'}},
    {case:'generic',value:{args:{CommandLine:'tool --token='+secret}}},
    {case:'plannerResponse',value:{response:'Published response {"access_token":"'+secret+'"} https://user:'+secret+'@example.invalid/',thinking:secret,rawThinking:secret,generatorMetadata:{prompt:secret}}},
    {case:'runCommand',value:{commandLine:'git status --short'}}
  ]);
  const off=f.trace.observe(()=>{});
  await new Promise(setImmediate);
  const data=f.trace.snapshot(),serialized=JSON.stringify(data);
  assert.ok(!serialized.includes(secret));
  assert.ok(!serialized.includes('rawThinking'));
  assert.ok(!serialized.includes('generatorMetadata'));
  assert.ok(serialized.includes('git status --short'));
  assert.ok(serialized.includes('Published response'));
  off();await new Promise(setImmediate);
  assert.ok(f.aborted());
  assert.equal(f.trace.snapshot().retained,0);
});


test('large streams are bounded and release their subscription',async()=>{
  const f=fixture(Array.from({length:12000},()=>({case:'runCommand',value:{commandLine:'git status --short'}})));
  const off=f.trace.observe(()=>{});await new Promise(setImmediate);
  const data=f.trace.snapshot();assert.equal(data.retained,10000);assert.equal(data.total,12000);
  assert.equal(data.events[0].index,2000);assert.ok(f.trace.overview(data).partial);
  assert.ok(f.trace.matches(data.events[0],'kind:command git'));
  off();await new Promise(setImmediate);assert.ok(f.aborted());assert.equal(f.trace.snapshot().retained,0);
});

test('diff preview uses real hunks, invalidates cache, and excludes sensitive files',async(t)=>{
  let after='first\nchanged\nlast\n';
  const f=fixture([{case:'checkpoint',value:{sessionSummary:'Public checkpoint'}}],{
    getTrajectoryFileDiffs:async()=>({diffs:[{uri:'file:///C:/project/app.ts',originalContent:'first\nold\nlast\n',modifiedContent:after},{uri:'file:///C:/project/.env',originalContent:'KEY=a',modifiedContent:'KEY=b'}]})
  });
  t.after(()=>f.trace.dispose());
  const off=f.trace.observe(()=>{});await new Promise(setImmediate);
  await f.trace.loadFiles();const before=await f.trace.loadDiff('C:/project/app.ts');
  assert.ok(before.includes('@@ -'));assert.ok(before.includes('-old'));assert.ok(before.includes('+changed'));
  after='first\nnewer\nlast\n';await f.trace.loadFiles();const newer=await f.trace.loadDiff('C:/project/app.ts');assert.ok(newer.includes('+newer'));assert.ok(!newer.includes('+changed'));
  await assert.rejects(f.trace.loadDiff('C:/project/.env'),/Sensitive/);
  const json=f.trace.exportTrace('json');assert.ok(!json.includes('originalContent'));assert.ok(!json.includes('modifiedContent'));assert.ok(json.includes('Public checkpoint'));
  off();await new Promise(setImmediate);
});
