({
  id: 'conversation-map', title: 'Conversation Map', title_zh: '会话地图', version: '1.0.0',
  setup(api) {
    let requestedLocator = null;
    api.registerPanel({
      id: 'conversation-map.timeline', title: 'Conversation Map', title_zh: '会话地图',
      render(panel) {
        const style = document.createElement('style');
        style.textContent = '.cm-header{display:flex;justify-content:space-between;gap:12px;align-items:center}.cm-track{position:relative;padding:16px 0 16px 25px;margin:12px 0}.cm-track:before{content:"";position:absolute;top:12px;bottom:12px;left:8px;width:1px;background:linear-gradient(#8ab4f8,#81c995,#fdd663)}.cm-node{position:relative;margin:0 0 17px}.cm-node:before{content:"";position:absolute;left:-22px;top:16px;width:9px;height:9px;border-radius:50%;background:#8ab4f8;box-shadow:0 0 0 4px #202124}.cm-node[data-kind=progress]:before{background:#fdd663}.cm-node[data-kind=error]:before{background:#f28b82}.cm-node button{width:100%;text-align:left;padding:12px 14px;border:1px solid #3c4043;border-radius:8px;background:#1b1c1f;color:#e8eaed;cursor:pointer;font:13px/1.5 system-ui}.cm-node small{display:block;color:#9aa0a6;font-size:10px;letter-spacing:.5px;text-transform:uppercase;margin-bottom:5px}.cm-node[data-selected=true] button{border-color:#8ab4f8;background:#202c3a}.cm-empty{color:#9aa0a6;font:13px/1.7 system-ui}';
        const header = document.createElement('div'); header.className = 'cm-header';
        const title = document.createElement('strong');
        const refresh = document.createElement('button'); refresh.type = 'button';
        header.append(title, refresh);
        const track = document.createElement('div'); track.className = 'cm-track';
        panel.append(style, header, track);
        function paint() {
          title.textContent=api.i18n.t('已加载会话 · 节点时间线','Loaded conversation · node timeline');
          refresh.textContent=api.i18n.t('刷新','Refresh');
          track.replaceChildren(); const messages = api.host.conversation.messages();
          if (!messages.length) { const empty = document.createElement('p'); empty.className = 'cm-empty'; empty.textContent = api.i18n.t('请打开会话，地图仅使用已加载的消息。','Open a conversation. This map uses loaded messages only.'); track.append(empty); return; }
          for (const [index,message] of messages.entries()) {
            const node = document.createElement('article'); node.className = 'cm-node'; node.dataset.kind = message.kind || message.role; node.dataset.selected = String(message.locator === requestedLocator);
            const button = document.createElement('button'); button.type = 'button';
            const role = document.createElement('small'); role.textContent = String(index + 1).padStart(2,'0') + ' · ' + api.i18n.t(message.kind || message.role);
            const text = document.createElement('span'); text.textContent = message.title;
            button.append(role,text); button.onclick = () => { requestedLocator = message.locator; if (!api.host.conversation.jump(message.locator)) api.toast(api.i18n.t('消息已不在当前加载内容中','Message is no longer loaded')); paint(); };
            node.append(button); track.append(node);
          }
        }
        refresh.onclick = paint; paint();
        const unsubscribe = api.host.conversation.observe(paint);
        const unsubscribeLanguage=api.i18n.onChange(paint);
        return () => { unsubscribe(); unsubscribeLanguage(); panel.replaceChildren(); };
      }
    });
    api.registerMessageAction({ id: 'conversation-map.locate', title: 'View in Conversation Map', title_zh: '在会话地图中查看', run(pin) { requestedLocator = pin.locator; api.openPanel('conversation-map.timeline'); } });
    api.registerLensFilter({ id: 'conversation-map.progress', title: 'AGENT PROGRESS', title_zh: 'AGENT 进度', match(message) { return message.kind === 'progress'; } });
  }
})
