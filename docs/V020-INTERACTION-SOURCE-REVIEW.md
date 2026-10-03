# v0.2.0 Interaction Layer：源码核对记录

> 2026-10-03 更新：下面保留 Phase 2 当时的源码核对记录。此后已在真实宿主修复并检查按钮委托、消息适配、会话地图、项目上下文及扩展生命周期；当前功能范围与已知限制见 [GitHub Release](https://github.com/arukas0623-ai/Anti-Antigravity/releases/latest)。

本记录是对 Phase 2 需求和源码的人工核对，不是测试报告或验收结论。
本轮没有运行测试、构建、lint、语法检查、宿主或自动验证。
此前用户确认的账号切换和 Manager 显示，不证明 v0.2.0 新交互已可用。

上一轮“没有新增延期项”的表述过满。更准确的状态是：源码中有对应入口和实现，运行效果未确认；本轮已修复下列源码缺陷。

## 本轮发现并修改的缺陷

| 场景 | 原实现的问题 | 源码修改 |
| --- | --- | --- |
| 合并 Capsule，Pin/Notes 正文含 `##` 标题 | 从导出 Markdown 反推 section，会把正文标题当成结构，错归或丢失文本 | 以保存的 sections 和结构化上下文为准拼接；只有旧 Markdown 数据走解析兜底；兜底跳过代码围栏 |
| Markdown 兜底中有中文 section 名 | ASCII-only key 将不同中文标题变为空 key，合并时发生碰撞 | section key 保留 Unicode 字母和数字 |
| 两个扩展注册同一命令/面板，先卸载被覆盖者 | 其 dispose 提前返回，后续卸载覆盖者时会恢复已卸载回调 | 按注册顺序维护仍有效的条目；dispose 从队列移除，重复 dispose 无副作用 |
| 替换/卸载当前面板注册但相同 id 仍存在 | 只比较 id 是否存在，旧面板和 cleanup 可能残留 | 比较注册对象；失效时执行面板 cleanup，清空内容并使待完成的 render 失效 |
| 删除本地插件后刷新目录 | 仅处理新目录条目，旧插件继续留在 registry | 成功取得目录后，对消失的目录插件执行 cleanup 并移除；目录读取失败时保留现有插件 |
| 本地入口文件缺失，修复文件后点 Reload | Reload 以旧 source 是否非空决定是否读磁盘，错误条目无法恢复 | 按目录来源决定重新获取 source 和元数据 |
| 插件传入非数组 commands.keywords | renderCommands 在注册中抛错，错误条目可能残留 | 在写入 registry 前检查 keywords 类型 |
| 收藏草稿时，旧保存响应晚到 | 响应覆盖 activeDraft，把较新的收藏意图改回旧值 | 应用保存元数据时保留当前收藏意图 |
| 异步 conversation observer 抛错 | guarded 回调重新抛出的 Promise rejection 未被观察器消费 | 保留局部错误记录，并消费观察器 Promise 的 rejection |

这些是源码修改；本轮未执行对应场景，不能写成“已通过”。

## Phase 2 需求对应

前端实现集中在 [injected_hub.js](../internal/patcher/injected_hub.js) 的 Host Adapter、`mountInteractionLayer` 和 `installShowcase`。

| 需求 | 可见的源码入口 | 尚未确认的边界 |
| --- | --- | --- |
| Selection-aware Insert | `host.prompt.selection/set`、`il-insert-mode` | 原生富文本选区、受控输入的宿主行为 |
| Prompt Actions | `promptContext`、`renderActions`、六个 builtin action | 真实点击、选区保留与剪贴板 |
| Draft UX | `saveDraft/changeDraft`、favorite/duplicate/delete/title 编辑、`lastUsed` | 保存并发、切号前 flush、重新注入后的恢复 |
| Snippet UX | 分类编辑、搜索/分类过滤、插入、收藏 | 真实操作；分类是轻量文本字段 |
| Conversation Structure | Host Adapter 的 `roleOf/messages` 和 kind 文本规则 | 仅扫描已加载 DOM；真实宿主 role/selector 覆盖未确认 |
| Lens filters/search | `renderFilters/renderOutline`；ALL/USER/ASSISTANT/PINNED 和注册 filter | 跳转依赖宿主当前加载的消息 |
| Better Pin / Multi-select | `editPin`、元数据保存、selectedPins、copy/compare/capsule 入口 | 消息重绘/虚拟化后 locator 的可靠性 |
| Capsule Templates / Source | 五个模板、`currentSections`、`renderTrace`、结构化 Pin 快照 | 模板切换与来源展开的实际体验 |
| Capsule Library / Merge | `renderLibrary`、rename/duplicate/delete/favorite/search、`mergeCapsules` | 合并与保存的实际体验；只做文本拼接 |
| Command Palette | `palette/renderCommands/executeCommand`、键盘监听；Ctrl+Shift+K | 宿主快捷键与焦点行为 |
| Quick Capture | `saveCapture`，Draft/Snippet/Pin Note/Capsule Notes | 四个目的地的真实交互 |
| Extension Runtime | 七类 register、`scopedAPI/startExtension/stopExtension`、局部 render 错误 | 本地 factory 通过 `new Function` 载入，宿主执行环境尚未确认 |
| 三个官方示例 | [prompt-toolkit](../plugins/examples/prompt-toolkit/index.js)、[conversation-map](../plugins/examples/conversation-map/index.js)、[project-context](../plugins/examples/project-context/index.js) | 代码存在；没有 README/GIF 真机演示证据 |
| Extensions 页面 | `renderExtensions/loadExtensions`、enable/disable/reload、注册计数 | 目录扫描来自 cwd/exe 旁 plugins 的直接子目录；不递归、不联网 |
| HOME Recent | `record/renderRecent`，最多六条 | 实际使用记录与重启持久化 |
| Workspace UX | `persistRecovery`、偏好 JSON、`pageChanged/dismiss` | 宿主焦点、Esc、左右停靠和宽度的实际体验 |
| JSON persistence | [workspace_store.go](../internal/supervisor/workspace_store.go)、[workspace.go](../internal/api/workspace.go) | 路径 `~/.2ag/workspace/`；本轮未读写真实用户数据 |
| 开发者文档 | [plugins/README.md](../plugins/README.md)，完整四类注册示例 | “十分钟上手”是教程目标，未通过外部开发者体验确认 |

## 明确保留的限制

- Project Context 只读实际 DOM 提供的项目 path/name 和选中 Pins；没有文件系统项目索引或完整项目元数据。缺失信息省略。
- 三个 bundled 示例来自编译时嵌入；其 Reload 重做 setup，不重新读取 examples 源文件。普通本地 factory 的 Reload 才读取磁盘。
- 旧 script/iframe 继续依赖 INITIAL_CONFIG 和既有 CLI 插件链；Manager 不运行 sidecar。这里没有把 UI enable/disable 变成进程管理。
- 新 factory 使用 scoped API 管理资源。直接操作全局对象、未登记的监听器不受 runtime cleanup 管理；没有权限沙箱。
- 只有 Markdown 的旧 Capsule 无法可靠区分正文顶层标题与 Capsule section；当前结构化数据避免这一歧义。
- 改动仍集中在原有大脚本，未做模块拆分；符合本轮不大重构的范围。

真实体验及修整仍待下一轮；本记录不把这些事项标记成验收通过。
