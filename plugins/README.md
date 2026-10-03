# 2Ag Interaction Layer Extensions

**Prompt · Conversation · Context · Command · Extension**

插件可以处理 Prompt 纯文本、导航真实会话、提供 Capsule 上下文，也可以通过命令和面板呈现自己的入口。UI 插件无需 sidecar 进程、模型或网络服务。

## 十分钟写出第一个插件

在仓库或安装目录的 plugins/my-tool/ 中创建两个文件。

manifest.json：

~~~json
{
  "id": "my-tool",
  "name": "My Tool",
  "version": "1.0.0",
  "ui": { "type": "factory", "entry": "index.js" }
}
~~~

index.js 是一个返回定义的 JavaScript 表达式，无需打包器或 export。下面的完整最小示例注册 command、panel、prompt action 和 context provider：

~~~javascript
({
  id: 'my-tool',
  title: 'My Tool',
  version: '1.0.0',
  setup(api) {
    api.registerCommand({
      id: 'my-tool.compose',
      title: 'My Tool: Open Compose',
      keywords: ['prompt', 'write'],
      run() { api.compose.open(); }
    });

    api.registerPanel({
      id: 'my-tool.messages',
      title: 'Loaded messages',
      render(panel) {
        function paint() {
          panel.replaceChildren();
          for (const message of api.host.conversation.messages()) {
            const button = document.createElement('button');
            button.type = 'button';
            button.textContent = message.role + ': ' + message.title;
            button.onclick = () => api.host.conversation.jump(message.locator);
            panel.append(button);
          }
        }
        paint();
        const stop = api.host.conversation.observe(paint);
        return () => { stop(); panel.replaceChildren(); };
      }
    });

    api.registerPromptAction({
      id: 'my-tool.goal',
      title: 'Wrap selected text as Goal',
      run(prompt) {
        const text = '## Goal\n' + (prompt.selection || prompt.text);
        if (prompt.selection) prompt.insert(text);
        else prompt.replace(text);
      }
    });

    api.registerContextProvider({
      id: 'my-tool.project',
      title: 'Available project context',
      provide() {
        const project = api.host.project.current();
        const parts = [];
        if (project.available) parts.push(project.path || project.text);
        for (const pin of api.getSelectedPins()) {
          parts.push('## ' + pin.title + '\n' + pin.text);
        }
        return parts.join('\n\n');
      }
    });
  }
})
~~~

打开 **EXTENSIONS → Refresh local list**。命令进入 Ctrl+Shift+K 命令面板，Prompt Action 出现在 COMPOSE，Context Provider 由用户在 CAPSULE 中点击添加。这个快捷键保留宿主的 Ctrl+Shift+P。

编辑本地 factory 后点 **Reload**。**Disable** 移除扩展入口并执行 cleanup；重新启用会重新执行 setup。状态保存在 ~/.2ag/workspace/extension-state.json。目录来自当前工作目录和程序目录下的 plugins/，不连接在线仓库。

删除本地插件目录后点 **Refresh local list**，会卸载该插件并清理注册；目录读取失败时保留已加载插件。入口文件缺失造成的加载错误，在修复文件后可点 **Reload** 重新读取。

## API v1.4

window.__2AG__.apiVersion 为 '1.4'，向后兼容 v1.1 / v1.2 / v1.3。现有 registerTab 和 ipc 仍可用。所有注册返回 dispose；factory 的 setup(api) 得到可归属到该插件生命周期的 API。

| 注册接口 | 定义 | 入口 |
| --- | --- | --- |
| registerCommand | {id, title, keywords?, run()} | 命令面板，最近使用优先 |
| registerPanel / registerTab | {id, title, render(panel, api)} | EXTENSIONS 的 Panels |
| registerPromptAction | {id, title, run(prompt)} | COMPOSE |
| registerMessageAction | {id, title, run(message)} | LENS 消息行及已打开的 Pin |
| registerContextProvider | {id, title, provide({host})} | CAPSULE |
| registerQuickAction | {id, title, run({host, selectedPins})} | HOME |
| registerLensFilter | {id, title, match(message, {pinned})} | LENS |

prompt 提供 text、selection、start、end、insert(text)、replace(text)、copy(text)。这些操作处理 COMPOSE 纯文本，不发送消息。api.compose.current() 返回相同上下文；api.compose.open() 打开编辑器。

api.openPanel(id) 打开已注册面板。api.getSelectedPins() 返回 LENS 选中的 Pins。api.context.add({id, title, text}) 将内容加入 CAPSULE，用户仍可编辑、复制或手动插入。

宿主访问集中在 api.host：

- prompt.find/get/set；set(text, {mode: 'replace-selection' | 'after-selection' | 'replace-all'}) 只填写原生输入框。无选区时正常写入全文。
- conversation.messages/current/jump/observe；observe 返回 unsubscribe。消息包含 locator、role、kind、title、text、conversation_key。适配现有 markdown-* 及新版 article/User message/Agent response；角色仍按 DOM 标记识别。
- conversation.history/list/open：通过原生历史入口展示已加载会话，list 返回 `{href,title}`，open 只点击宿主已有会话链接。不会伪造会话、读取未加载的正文或发起模型请求。
- project.current()；只返回宿主实际提供的路径／名称，包括可见项目 breadcrumb。宿主只提供名称时省略 path；全部缺失时 available 为 false。
- trace.snapshot() / trace.observe(callback)：原生执行事件的只读规范化快照。包括 conversation、state、total、retained、historyStart、events、files；events 包含 index、kind、category、status、公开标题／文本、路径、时间、工具名和可用任务状态。observe 返回 unsubscribe，最后一个订阅结束即停止流。公开数据经过字段白名单及常见凭据形式脱敏，不提供隐藏思维、生成器上下文或文件全文。

在 panel 中观察 Live Trace 的最小用法：

~~~javascript
api.registerPanel({
  id: 'my-tool.trace', title: 'Agent activity', title_zh: 'Agent 动态',
  render(panel) {
    const label = document.createElement('p'); panel.append(label);
    const stop = api.host.trace.observe(snapshot => {
      const last = snapshot.events.at(-1);
      label.textContent = snapshot.state + ' · ' + (last?.title || 'No events');
    });
    return () => { stop(); panel.replaceChildren(); };
  }
});
~~~

请在 cleanup 中取消订阅。实时窗口最多保留 10,000 步（时间线使用虚拟列表）；用户回看历史时快照指向当前历史窗口，state 仍来自原生执行流。文件元数据在用户点击 Live Trace 的文件变更入口后提供。宿主版本缺少原生 RPC 时 state 为 unavailable；不以 mock 填充。

call(method, params) / ipc(method, params) 沿用核心 IPC。toast(text) 显示通知。onCleanup(fn) 登记事件或观察器资源的清理，返回值可取消登记。render 也可返回其面板 cleanup。异步 setup、action、provider 和 render 支持 Promise。

单个插件的 setup、回调、render 和 cleanup 错误显示在该插件状态或面板中。注册随 Disable、Reload、Hub 卸载清理。这是生命周期隔离，插件仍是用户安装的本地 JavaScript，不是权限沙箱。

### 中英文界面

G-Hub 顶部提供 **中文 / English**，沿用 `SET_LANGUAGE` 保存到 2Ag 配置。切换更新界面标签，不翻译草稿、Snippet、消息、Pin note 或 Capsule 正文；已生成的提示词段落和导出 Markdown 标题保留原文。

注册项可增加 `title_zh`，`title` 保留英文。例如：

~~~javascript
api.registerCommand({
  id: 'my-tool.open', title: 'My Tool: Open', title_zh: '我的工具：打开',
  keywords: ['open', '打开'], run() { api.openPanel('my-tool.panel'); }
});
~~~

面板可使用 `api.i18n.language`、`api.i18n.t('中文', 'English')` 和 `api.i18n.onChange(callback)`。后者返回取消订阅函数，并随 scoped factory 卸载清理：

~~~javascript
api.registerPanel({
  id: 'my-tool.panel', title: 'My Panel', title_zh: '我的面板',
  render(panel) {
    const label = document.createElement('p'); panel.append(label);
    const paint = () => { label.textContent = api.i18n.t('准备就绪', 'Ready'); };
    paint();
    const stop = api.i18n.onChange(paint);
    return () => { stop(); panel.replaceChildren(); };
  }
});
~~~

未提供中文标题的第三方插件继续显示其原始标题；插件生成的内容由插件自身决定语言。

## 官方示例

- [prompt-toolkit](examples/prompt-toolkit/index.js)：把选区或全文包装成 Goal / Context / Constraints / Output；演示 Prompt Action、Command、Quick Action。
- [conversation-map](examples/conversation-map/index.js)：HTML/CSS 节点时间线，点击跳回真实消息；演示 Panel、Message Action、Lens Filter、观察器 cleanup。
- [project-context](examples/project-context/index.js)：组合可获取的项目名称／路径、当前对话标识和选中 Pins；演示 Context Provider、Command。缺失信息省略。

三个示例随 2Ag 捆绑，首次默认启用，可单独禁用或重新初始化。无需额外依赖，均不调用模型。

捆绑示例的 **Reload** 重新执行 setup，使用当前二进制嵌入的定义；编辑 examples 源文件后，需要后续重新构建才能更新该定义。普通 plugins/my-tool/ 本地 factory 的 Reload 则重新读取磁盘文件。

## 既有 script / iframe 与 sidecar

现有未声明 type 或 type 为 script 的 manifest UI 仍可通过 window.__2AG__.registerTab(...) 注册；iframe 继续作为嵌入面板。新插件建议采用 factory，便于完整管理所有注册和 cleanup。

需要本地进程的旧插件仍可在 2ag.json 中登记：

~~~json
{
  "plugins": [{
    "name": "eyes-control",
    "executable": "plugins/eyes-control.exe",
    "args": [],
    "enabled": false
  }]
}
~~~

sidecar 只在 2ag run 链路由 Windows Job Object 管理；Manager 默认路径不创建该 Job Object。EXTENSIONS 的 enable / disable 管理 UI 生命周期，不替代既有进程管理。现有 IPC 包括 core.dialog.openFile、core.config.get/set、core.plugins.list/toggle。


### Trace v1.4

现有 `api.host.trace.observe(callback)` / `snapshot()` 保持兼容。`kind` 保留原生类型；新增 `eventKind` 提供 `file-read`、`file-write`、`command`、`error`、`checkpoint`、`assistant`、`system` 等统一分类。`timestamp`、`summary`、`file` 是旧字段的别名；`duration` 仅由公开的 created/completed 时间戳计算，缺失就省略。`phaseSource: 'event-kind'` 表示阶段只是对真实事件类型的归组，不是模型计划。

快照带 `cacheLimit` 和最近最多 12 条 `sessions` 元数据。长会话窗口外的统计不能当作全会话总数。关闭最后一个 observer 会取消原生流并释放事件、diff 缓存与会话列表。

TRACE 界面支持 file:/kind: 搜索、错误定位、按需脱敏 diff hunks、MD/JSON 摘要导出，以及最多 12 条选中事件送 Capsule。全文件内容和原始 RPC 对象不进入扩展 API；缺失 before snapshot 时不假装它是新建文件。Diff 的前后内容只在按需计算期间使用，片段缓存最多 6 个，不读取敏感文件预览。

`filesLoaded` 区分“尚未取得文件元数据”与“已确认零文件”。打开 TRACE 会读取一次原生文件摘要，可用“文件变更”刷新。Windows 的“打开文件”使用本机记事本：Antigravity 2.19.1 不处理普通文件 CLI 参数，因此不借此启动额外宿主。
