(function readAntigravityContext() {
  'use strict';
  // Pure read-only probe. Return counts and labels, never prompts, tool bodies or auth state.
  const number = value => {
    if (value === undefined || value === null || value === '') return null;
    const n = Number(value);
    return Number.isSafeInteger(n) && n >= 0 ? n : null;
  };
  const estimate = value => typeof value === 'string' ? Math.ceil(value.length / 3.5) : 0;
  const label = value => String(value || '').replace(/[\r\n\t]/g, ' ').slice(0, 180);
  const view = document.querySelector('[data-testid="conversation-view"]');
  const sessionId = view?.getAttribute('data-cascade-id') || location.pathname.match(/\/c\/([^/?]+)/)?.[1] || '';
  const result = {
    schema_version: 2, request: null, context_tokens: null, context_provenance: 'Unavailable', loaded_history: [],
    session_id: sessionId, view_available: !!view, model: '', mode: 'composition', source: 'DOM',
    sampled_at: Date.now(), used_tokens: null, limit_tokens: null,
    input_tokens: null, cache_read_tokens: null, cache_write_tokens: null, output_tokens: null,
    usage_step: null, usage_at: null, composition_scope: 'visible_dom',
    composition: [], items: [], steps: 0, turns: 0, tool_outputs: 0,
    workspace_dirs: [], activity: 'unknown', note: '', partial: false
  };
  if (!view) {
    result.note = '当前页面没有会话视图。';
    return result;
  }

  // Resolve semantic props, not minified component names. React internals are optional.
  let slice = null;
  let selectedModel = null;
  try {
    const key = Object.keys(view).find(k => k.startsWith('__reactFiber$'));
    const queue = key ? [view[key]] : [];
    const seen = new Set();
    for (let i = 0; i < queue.length && i < 1600; i++) {
      const fiber = queue[i];
      if (!fiber || seen.has(fiber)) continue;
      seen.add(fiber);
      const props = fiber.memoizedProps || {};
      const state = props.cascadeContext?.state;
      if (state?.agentStateProvider && typeof state.agentStateProvider.getState === 'function') {
        const current = state.agentStateProvider.getState();
        slice = current?.trajectorySlice || current?.agentState?.trajectorySlice || slice;
        selectedModel = state.selectedModel;
      }
      slice = slice || props.trajectorySlice;
      if (slice && Array.isArray(slice.stepsInSlice)) break;
      for (let child = fiber.child; child; child = child.sibling) queue.push(child);
    }
  } catch (_) { /* An upstream change must degrade to DOM composition. */ }

  const groups = new Map();
  const add = (kind, name, tokens, estimated, count = 1) => {
    const group = groups.get(kind) || { kind, items: 0, tokens: 0, estimated: false, unknown: false };
    group.items += count;
    if (tokens === null) group.unknown = true;
    else group.tokens += tokens;
    group.estimated ||= estimated;
    groups.set(kind, group);
    result.items.push({ kind, name: label(name), tokens, estimated });
  };
  const files = new Map();
  const rememberFile = (path, tokens = null) => {
    if (typeof path !== 'string' || !path || path.length > 2048) return;
    if (!/^(file:\/\/|[A-Za-z]:[\\/]|\/)/.test(path)) return;
    const normalized = path.replace(/\\/g, '/');
    const name = normalized.split('/').filter(Boolean).pop();
    if (!name || !name.includes('.')) return;
    const previous = files.get(normalized);
    files.set(normalized, { name, tokens: tokens ?? previous?.tokens ?? null });
  };
  const timestamp = value => {
    const seconds = number(value?.seconds);
    return seconds === null ? null : seconds * 1000 + Math.floor((number(value.nanos) || 0) / 1000000);
  };
  // Only named rendering fields; do not recursively count duplicate raw/formatted representations.
  const textFromStep = step => {
    const value = step?.step?.value || {};
    switch (step?.step?.case) {
      case 'userInput': return value.userResponse || (value.items || []).map(i => i.chunk?.case === 'text' ? i.chunk.value : '').join('\n');
      case 'plannerResponse': return (value.response || '') + '\n' + (value.thinking || value.rawThinking || '');
      case 'systemMessage': return value.message || '';
      case 'checkpoint': return value.sessionSummary || value.userIntent || '';
      default: {
        const output = value.result?.result ?? value.result ?? value.output ?? value.response ?? value.content ?? value.stdout ?? value.commandOutput;
        if (typeof output === 'string') return output;
        if (output && typeof output === 'object') {
          try { return JSON.stringify(output); } catch (_) {}
        }
        return '';
      }
    }
  };

  if (slice && Array.isArray(slice.stepsInSlice)) {
    result.source = 'Antigravity React · trajectorySlice';
    result.composition_scope = 'loaded_history';
    result.steps = slice.stepsInSlice.length;
    result.partial = slice.stepsSlice?.startIndex > 0 || slice.totalStepsLength > result.steps;
    const generators = slice.generatorMetadatasInSlice || [];
    const chat = [...generators].reverse().find(g => g.metadata?.case === 'chatModel')?.metadata.value;
    const windowMetadata = chat?.chatStartMetadata?.contextWindowMetadata;
    const planner = generators.at(-1)?.plannerConfig;
    result.model = label(chat?.responseModel || planner?.modelName || selectedModel?.label || selectedModel?.modelName);
    result.limit_tokens = number(windowMetadata?.maxContextTokens);
    if (result.limit_tokens === 0) result.limit_tokens = null;
    const estimatedContext = number(windowMetadata?.estimatedTokensUsed);
    if (estimatedContext > 0) { result.context_tokens = estimatedContext; result.context_provenance = 'Estimated'; }
    const steps = slice.stepsInSlice;
    const lastUsageStep = [...steps].reverse().find(s => s.metadata?.modelUsage);
    const usage = lastUsageStep?.metadata?.modelUsage || chat?.usage;
    if (usage) {
      result.input_tokens = number(usage.inputTokens);
      // Runtime protobuf defaults of zero do not establish cache telemetry.
      const cacheCounter = value => number(value) > 0 ? number(value) : null;
      result.cache_read_tokens = cacheCounter(usage.cacheReadTokens);
      result.cache_write_tokens = cacheCounter(usage.cacheWriteTokens);
      result.output_tokens = number(usage.outputTokens);
      result.usage_step = number(lastUsageStep?.metadata?.sourceTrajectoryStepInfo?.stepIndex);
      result.usage_at = timestamp(lastUsageStep?.metadata?.completedAt || lastUsageStep?.metadata?.createdAt);
      const sum = values => values.every(n => n !== null) ? values.reduce((a, b) => a + b, 0) : null;
      const observed = values => values.some(n => n !== null) ? values.reduce((a, b) => a + (b ?? 0), 0) : null;
      // This Runtime view has no confirmed name for native DB usage field #1.
      // A responseId match may fill it from persistence; never guess a field.
      const unclassifiedInput = null;
      const inputValues = [result.input_tokens, unclassifiedInput, result.cache_read_tokens, result.cache_write_tokens];
      const requestInput = sum(inputValues);
      const totalValues = [...inputValues, result.output_tokens];
      result.request = {
        session_id: sessionId, response_id: label(usage.responseId), timestamp: result.usage_at,
        model: result.model, provider: 'antigravity', provenance: 'Native', source: lastUsageStep ? 'React native modelUsage' : 'React native chatModel.usage',
        input: result.input_tokens, output: result.output_tokens, cache_read: result.cache_read_tokens,
        unclassified_input: unclassifiedInput,
        cache_write: result.cache_write_tokens, reasoning: number(usage.reasoningTokens),
        request_input: requestInput, observed_input: observed(inputValues), total: sum(totalValues),
        observed_total: observed(totalValues), partial: sum(totalValues) === null,
        cache_hit_rate: requestInput > 0 && (result.cache_read_tokens > 0 || result.cache_write_tokens > 0)
          ? result.cache_read_tokens / requestInput : null
      };
      // Legacy sampling fields now describe requests only, never Context Window.
      result.used_tokens = requestInput;
      result.mode = requestInput === null ? 'composition' : 'native';
    }
    for (const workspace of slice.metadata?.workspaces || []) {
      if (workspace.workspaceFolderAbsoluteUri) result.workspace_dirs.push(workspace.workspaceFolderAbsoluteUri);
    }
    for (const [index, step] of steps.entries()) {
      const type = step.step?.case || 'unknown';
      const metadata = step.metadata || {};
      const kind = type === 'userInput' || type === 'plannerResponse' ? 'conversation'
        : type === 'systemMessage' || type === 'checkpoint' ? 'system' : 'tools';
      if (type === 'userInput') result.turns++;
      if (kind === 'tools') result.tool_outputs++;
      const text = textFromStep(step);
      const nativeTokens = number(metadata.toolCallOutputTokens);
      const tokens = kind === 'tools' && nativeTokens > 0 ? nativeTokens : text ? estimate(text) : null;
      const stepIndex = metadata.sourceTrajectoryStepInfo?.stepIndex ?? (slice.stepsSlice?.startIndex || 0) + index;
      add(kind, `#${stepIndex} ${metadata.toolCall?.name || type}`, tokens, tokens !== null && !(kind === 'tools' && nativeTokens > 0));
      const value = step.step?.value || {};
      rememberFile(value.fileUri || value.targetFile || value.absolutePath || value.path);
      try {
        const args = value.args || JSON.parse(metadata.toolCall?.argumentsJson || '{}');
        for (const key of ['TargetFile', 'AbsolutePath', 'path', 'file_path', 'FilePath', 'fileUri']) rememberFile(args[key]);
      } catch (_) {}
      for (const attachment of step.attachments || []) rememberFile(attachment.uri || attachment.fileUri);
    }
    // Message prompts describe the current assembled prompt. Do not mix them with all historical steps.
    result.loaded_history = [...groups.values()].map(({ unknown, ...g }) => ({ ...g, partial: unknown, tokens: unknown && g.tokens === 0 ? null : g.tokens }));
    if (chat?.messagePrompts?.length || chat?.promptSections?.length || chat?.systemPrompt) {
      groups.clear(); result.items = []; result.composition_scope = 'active_prompt';
      const stepByIndex = new Map(steps.map((s, i) => [s.metadata?.sourceTrajectoryStepInfo?.stepIndex ?? i, s]));
      for (const message of chat.messagePrompts || []) {
        const step = stepByIndex.get(message.stepIdx);
        const type = step?.step?.case;
        const kind = type === 'userInput' || type === 'plannerResponse' ? 'conversation'
          : type === 'systemMessage' || type === 'checkpoint' ? 'system' : step ? 'tools' : 'other';
        const n = number(message.numTokens);
        const tokens = n > 0 ? n : estimate(message.prompt || '') + estimate(message.thinking || '');
        add(kind, `#${message.stepIdx ?? '?'} ${type || 'Prompt'}`, tokens, !(n > 0));
      }
      const sections = chat.promptSections?.length ? chat.promptSections : chat.systemPrompt ? [{ title: 'System prompt', content: chat.systemPrompt }] : [];
      for (const section of sections) {
        const kind = /workspace|file|artifact/i.test(section.title || '') ? 'workspace' : 'system';
        add(kind, section.title || 'System / Rules', estimate(section.content || ''), true);
      }
      for (const tool of chat.tools || []) {
        // Count schema text only; no execution, no serialization methods on host objects.
        const text = JSON.stringify({ name: tool.name, description: tool.description, parameters: tool.parameters });
        add('tools', `Tool definition · ${tool.name || 'tool'}`, estimate(text), true);
      }
    }
    const latest = steps.at(-1);
    const latestType = latest?.step?.case || '';
    const loading = view.querySelector('[data-testid="agent-loading"]');
    const running = !!loading?.getClientRects().length;
    result.activity = running ? /edit|write|replace/i.test(latest?.metadata?.toolCall?.name || latestType) ? 'edit'
      : latestType === 'plannerResponse' ? 'analyze' : 'working' : 'idle';
  } else {
    const anchors = [
      ['conversation', '[data-testid="user-input-step"], [data-testid="planner-response-text"]'],
      ['tools', '[data-testid="run-command-step"], [data-testid="tool-group-collapsible"]']
    ];
    for (const [kind, selector] of anchors) {
      for (const node of view.querySelectorAll(selector)) {
        if (node.parentElement?.closest(selector)) continue;
        add(kind, node.dataset.testid, null, false);
        result.steps++;
        if (node.dataset.testid === 'user-input-step') result.turns++;
        if (kind === 'tools') result.tool_outputs++;
      }
    }
    result.note = '仅读取当前可见组成；框架状态不可访问，不推断 token 总量。';
    result.activity = view.querySelector('[data-testid="agent-loading"]') ? 'working' : 'idle';
  }
  for (const item of files.values()) add('files', item.name, item.tokens, false);
  if (result.composition_scope !== 'active_prompt') {
    for (const uri of result.workspace_dirs) add('workspace', uri.replace(/\\/g, '/').split('/').filter(Boolean).pop() || 'Workspace', null, false);
  }
  result.composition = [...groups.values()].map(({ unknown, ...g }) => ({ ...g, partial: unknown, tokens: unknown && g.tokens === 0 ? null : g.tokens }));
  result.items.sort((a, b) => (b.tokens ?? -1) - (a.tokens ?? -1));
  if (result.items.length > 400) { result.items = result.items.slice(0, 400); result.partial = true; }
  if (!result.limit_tokens) result.note += ' 当前模型未暴露上下文上限。';
  return result;
})
