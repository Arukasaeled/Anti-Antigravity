({
  id: 'prompt-toolkit', title: 'Prompt Toolkit', title_zh: '提示词工具箱', version: '1.0.0',
  setup(api) {
    function structure(context) {
      const source = context.selection || context.text;
      const text = '## Goal\n' + source + '\n\n## Context\n\n## Constraints\n\n## Output\n';
      if (context.selection) context.insert(text); else context.replace(text);
    }
    api.registerPromptAction({ id: 'prompt-toolkit.structure', title: 'Structure selection', title_zh: '结构化选区', run: structure });
    api.registerCommand({ id: 'prompt-toolkit.structure-command', title: 'Prompt Toolkit: Structure selected prompt', title_zh: '提示词工具箱：结构化选中提示词', keywords: ['goal','context','constraints','output','目标','上下文','约束','输出'], run() { api.compose.open(); structure(api.compose.current()); } });
    api.registerQuickAction({ id: 'prompt-toolkit.open', title: 'Prompt Toolkit', title_zh: '提示词工具箱', run() { api.compose.open(); } });
  }
})
