# v0.2.0 — Interaction Layer

当前为源码与本地安装包更新，尚未创建 GitHub Release。

## G-Hub

- 中英文切换；账号选择器与 Gemini / Claude+GPT 的 5h、周配额四个圆环常驻顶部。
- 编写：草稿、片段、纯文本 Prompt Actions、原生选区插入、一键结构化；管理工具按需展开。
- 会话：原生历史会话选择、已加载消息搜索与筛选；直接固定、加入胶囊或执行 Message Action。
- 胶囊：模板、来源、库管理、收藏与简单合并；所有内容可编辑，不调用模型。
- 命令面板、Quick Capture、最近使用、本地扩展启用/禁用/重载。
- Interaction API v1.2 的七类注册均返回 dispose；局部错误与 cleanup 独立处理。

官方示例为 Prompt Toolkit、Conversation Map、Project Context。开发说明见 [plugins/README.md](plugins/README.md)。

本地扩展目录或 manifest 读取失败时，刷新会保留已加载扩展；目录确实被删除时才卸载。

## 实际修复

委托按钮处理此前进入 Promise microtask 后才读取 event.target。Shadow DOM 事件结束后目标变成外层宿主，导致导航及多个按钮失效。现在在事件派发期间执行入口，仍捕获同步错误和异步 rejection。

新版宿主以 article、User message、Agent response 和 data-cascade-id 标记消息。适配器此前只识别 markdown-*，真实会话因此返回零条消息；本次兼容两种结构，保留真实会话 ID，并读取项目 breadcrumb 中确实存在的名称。

## 检查范围

2026-10-03 在运行中的增强宿主内通过真实 DOM 和按钮检查：

- 页面导航、顶部四个圆环、已加载消息、用户筛选与全文搜索。
- 原生历史会话列表、会话打开；会话地图绘制与跳回原消息。
- Prompt Toolkit 的结构化操作；Project Context 添加真实项目名称与会话 ID，缺失路径省略。
- 七类接口注册及 dispose、局部 render failure、cleanup；Conversation Map 禁用/启用/重载。
- 从安装目录加载本地 factory 并打开真实面板；移出目录后刷新会卸载。

检查保留 2Ag 与宿主进程，未运行 Go 测试、lint/vet 或全量自动验证。临时编辑内容已恢复，临时插件已移出加载目录。

账号配额、保险库和登录恢复的此前未提交源码一并保留。源码审查另修正 HTTPS 上游 CONNECT 缺少 TLS 握手、错误格式的代理地址可能把凭据写入日志，以及账号移除只在两处同时失败时才报错；这些路径本轮未重新操作真实账号或网络链路。

## 保留的边界

- 消息正文仅来自宿主当前加载的 DOM，不承诺读取整个历史会话正文。
- 宿主未提供项目路径时省略，不以配额项目 ID 代替文件系统路径。
- bundled 示例重载重做嵌入定义；修改其源码后需要重新构建。
- 所有历史宿主版本、持久化并发与原生富文本选区尚未完整回归。
- 无 AI 总结、自动路由、云同步、Marketplace、Agent orchestration 或权限沙箱。
