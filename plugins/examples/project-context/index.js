({
  id: 'project-context', title: 'Project Context', title_zh: '项目上下文', version: '1.0.0',
  setup(api) {
    function context() {
      const project = api.host.project.current(), conversation = api.host.conversation.current(), pins = api.getSelectedPins();
      const lines = [];
      if (project.available) {
        if (project.name) lines.push('Project: ' + project.name);
        if (project.path) lines.push('Path: ' + project.path);
        else if (!project.name && project.text) lines.push('Project context: ' + project.text);
      }
      if (conversation.available && conversation.key) lines.push('Conversation: ' + conversation.key);
      if (pins.length) {
        lines.push('\nSelected conversation pins:');
        for (const pin of pins) lines.push('\n### ' + pin.title + '\n' + pin.text + (pin.note ? '\nNote: ' + pin.note : '') + '\nSource: ' + pin.conversation_key + ' · ' + pin.role + ' · ' + (pin.created_at || ''));
      }
      return lines.join('\n');
    }
    api.registerContextProvider({ id: 'project-context.provider', title: 'Project & selected Pins', title_zh: '项目与选中的固定消息', provide: context });
    api.registerCommand({ id: 'project-context.add', title: 'Project Context: Add available context to Capsule', title_zh: '项目上下文：将可获取的信息加入胶囊', keywords: ['project','pins','handoff','项目','固定消息','交接'], run() { const text = context(); if (!text) { api.toast(api.i18n.t('没有可获取的项目元数据或选中的上下文','No project metadata or selected context is available')); return; } api.context.add({ id: 'project-context.provider', title: 'Project & selected Pins', text }); } });
  }
})
