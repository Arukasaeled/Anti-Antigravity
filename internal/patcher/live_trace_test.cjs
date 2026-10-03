const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const fs=require('node:fs');
const {webcrypto}=require('node:crypto');

function fixture(steps) {
  const events=steps.map((step,index)=>({step,metadata:{createdAt:{seconds:1700000000},...(step.case==='generic'?{toolCall:{name:'run_command'}}:{})},status:3}));
  let aborted=false;
  const scope={console,crypto:webcrypto,setInterval,clearInterval,setTimeout,clearTimeout,AbortController,location:{pathname:'/c/fixture-conversation'},window:{Wj:{lsClient:{
    async *streamAgentStateUpdates(_,options) {
      try { yield {update:{fullyIdle:true,mainTrajectoryUpdate:{stepsUpdate:{steps:events,indices:events.map((_,i)=>i),totalLength:events.length}}}};
        await new Promise(resolve=>options.signal.addEventListener('abort',resolve,{once:true}));
      } finally {aborted=true;}
    }
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
