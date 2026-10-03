(function createAccountHealth({request,language}) {
  const t=(zh,en)=>language()==='zh-CN'?zh:en;
  const stages={Idle:['空闲','Idle'],BackingUp:['备份登录','Backing up'],RefreshingTargetCredential:['刷新目标登录','Refreshing target sign-in'],ValidatingTargetIdentity:['核验 Google 身份','Verifying Google identity'],StoppingHost:['停止宿主','Stopping host'],SavingCurrentCredential:['保存最新登录','Saving current sign-in'],WritingTargetCredential:['恢复目标登录','Restoring target sign-in'],StartingHost:['启动宿主','Starting host'],VerifyingNativeAuth:['核验原生登录','Verifying native sign-in'],Committed:['已核验','Verified'],RollingBack:['恢复原账号','Restoring previous account'],RollbackVerifying:['核验恢复结果','Verifying recovery'],Failed:['切换失败','Switch failed']};
  function stage(value){const pair=stages[value];return pair?t(...pair):value||t('空闲','Idle');}
  function mount(parent,email=''){
    let disposed=false,timer=null,inflight=false;
    const el=(tag,text)=>{const n=document.createElement(tag);if(text)n.textContent=text;return n;};
    const root=el('details'),head=el('summary',t('账号健康','Account Health')),body=el('div'),refresh=el('button',t('刷新状态','Refresh status'));
    const picker=el('select');picker.setAttribute('aria-label',t('查看账号健康','Account to inspect'));picker.onchange=()=>{email=picker.value;update();};
    root.className='ah-card';root.style.cssText='margin:8px 0;padding:10px;border:1px solid #68707855;border-radius:10px;font:12px/1.7 system-ui;overflow-wrap:anywhere';body.style.marginTop='8px';refresh.type='button';root.append(head,body);parent.append(root);
    async function update(){
      if(disposed||inflight)return;inflight=true;refresh.disabled=true;
      try{
        const h=await request(email);if(disposed)return;
        head.textContent=t('账号健康','Account Health');
        picker.replaceChildren(...(h.accounts||[h.account]).map(value=>{const option=el('option',value);option.value=value;option.selected=value===h.account;return option;}));
        const native=h.native||{},unknown=t('未取得','Unavailable');
        const failure=({ineligible:t('资格受限','Ineligible'),'identity-mismatch':t('账号不一致','Account mismatch')})[native.failure]||native.failure;
        const locationRestricted=native.failure==='ineligible'&&/location/i.test(native.message||'');
        const rows=[
          [t('账号','Account'),h.account||unknown],
          [h.native_live?t('原生登录','Native sign-in'):t('上次原生登录','Last native sign-in'),native.available?(native.valid?t('有效','Valid'):(failure||t('未登录','Signed out'))):unknown],
          [t('原因','Reason'),locationRestricted?t('原生服务本次报告地区限制','The native service reported a location restriction'):native.message],
          [t('最后核验','Last verified'),h.last_verified],
          [t('最后观察到刷新','Last observed refresh'),h.last_refresh||unknown],
          [t('保险库','Vault'),({archived:t('已归档','Archived'),incomplete:t('不完整','Incomplete'),missing:t('未归档','Missing')})[h.vault]],
          [t('待核验续期','Pending renewal'),h.refresh_pending?t('已加密保存，重试后核验','Encrypted; verified on retry'):''],
          [t('刷新凭据','Refresh credential'),h.has_refresh_credential?t('已保存','Saved'):t('未取得','Unavailable')],
          [t('宿主','Host'),h.host?.process_found?'PID '+h.host.pid:t('未运行','Stopped')],
          [t('当前宿主 Profile','Current host profile'),h.profile],
          [t('当前事务','Current transaction'),h.transaction?.target?stage(h.transaction.stage)+' · '+h.transaction.target:''],
          [t('上次切换','Last switch'),h.last_switch?.email?[h.last_switch.email,language()==='zh-CN'?h.last_switch.user_message:h.last_switch.user_message_en].filter(Boolean).join(' · '):'']
        ];
        body.replaceChildren(picker,...rows.filter(r=>r[1]).map(([label,value])=>{const row=el('div');row.append(el('strong',label+': '),document.createTextNode(String(value)));return row;}));
        const technical=[native.failure?'Native GetAuthStatus: '+native.failure:'',locationRestricted?native.message:'',h.last_switch?.technical_details].filter(Boolean).join('\n\n');
        if(technical){const detail=el('details');detail.append(el('summary',t('技术详情','Technical details')),el('pre',technical));detail.lastChild.style.cssText='white-space:pre-wrap;font:11px/1.6 monospace';body.append(detail);}
        body.append(refresh);
      }catch(error){if(!disposed)body.replaceChildren(el('p',t('无法读取状态，请重试。','Status unavailable. Try again.')),refresh);}
      finally{inflight=false;refresh.disabled=false;}
    }
    root.ontoggle=()=>{clearInterval(timer);timer=null;if(root.open){update();timer=setInterval(update,3000);}};
    refresh.onclick=update;
    return {update,dispose(){disposed=true;clearInterval(timer);root.remove();}};
  }
  return {mount,stage};
})
