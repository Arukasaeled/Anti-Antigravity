# v0.2.0 — Interaction Layer

当前为源码与本地安装包更新，尚未创建 GitHub Release。新增账号生命周期修复、Live Trace 与默认关闭的低配额接力；见 [账号与 Live Trace](docs/ACCOUNT-LIFECYCLE-LIVETRACE.md) 和 [接力说明](docs/ACCOUNT-RELAY.md)。旧运行中的 Manager 未在开发过程中替换。

## 账号与 Live Trace 增量

- G-Hub 变更请求不跨端口重复发送，不用连接错误覆盖原来的 HTTP 拒绝。Manager / G-Hub 使用同一异步切号任务与可回看的完成回执。
- 过期的完整归档交给原生宿主刷新；切号及回滚都核验原生状态、实际邮箱与 access 有效期。同账号操作仍核验，成功不重启。
- 新登录 Broker 归档前核验原生身份；已有登录的原生刷新周期归档。失败／取消从未登录状态恢复未登录；恢复记录涵盖进程中断。
- 启动／接管不从旧主控或不完整中间态自动恢复账号；独立宿主操作与账号事务共享锁，避免重启或停止打断登录。
- 原生地区／资格拒绝如实显示并回滚；没有绕过服务端限制。
- URI 回调绑定实际可执行文件与账号 profile，复用同一个增强宿主；回调绑定失败清理新宿主，不留半初始化进程。
- TRACE 订阅原生事件，支持读／写／命令／工具／错误／公开摘要、搜索、文件变更元数据、旧步骤窗口、显示更多、复制与 Capsule 来源。关闭页面取消订阅并清理缓存。
- Live Trace 不读取隐藏思维；只导出白名单，常见凭据遮罩，敏感命令保守隐藏。
- 接力领取具有独立 claim_id，其他窗口不能清空发送者的领取；不确定发送禁止自动重发。

实际 2.19.1 会话共 1623 步、153 条文件变更；分类、旧步骤、Capsule 与关闭订阅检查通过。同账号保持宿主 PID；系统 URI 未留下原版实例。相关 Go 包与脱敏／恢复／领取回归通过。需要人工交互的新 Google 登录、真实低配额端到端接力及其他宿主版本未在本轮重验。

## G-Hub

- 中英文切换；账号选择器与 Gemini / Claude+GPT 的 5h、周配额四个圆环常驻顶部。
- 编写：草稿、片段、纯文本 Prompt Actions、原生选区插入、一键结构化；管理工具按需展开。
- 会话：原生历史会话选择、已加载消息搜索与筛选；直接固定、加入胶囊或执行 Message Action。
- 胶囊：模板、来源、库管理、收藏与简单合并；所有内容可编辑，不调用模型。
- 命令面板、Quick Capture、最近使用、本地扩展启用/禁用/重载。
- Interaction API v1.3 的七类注册均返回 dispose；新增只读 host.trace，局部错误与 cleanup 独立处理。

官方示例为 Prompt Toolkit、Conversation Map、Project Context。开发说明见 [plugins/README.md](plugins/README.md)。

本地扩展目录或 manifest 读取失败时，刷新会保留已加载扩展；目录确实被删除时才卸载。

## 实际修复

委托按钮处理此前进入 Promise microtask 后才读取 event.target。Shadow DOM 事件结束后目标变成外层宿主，导致导航及多个按钮失效。现在在事件派发期间执行入口，仍捕获同步错误和异步 rejection。

新版宿主以 article、User message、Agent response 和 data-cascade-id 标记消息。适配器此前只识别 markdown-*，真实会话因此返回零条消息；本次兼容两种结构，保留真实会话 ID，并读取项目 breadcrumb 中确实存在的名称。

## 此前 Interaction Layer 检查范围

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
- 无 AI 总结、云同步、Marketplace、Agent orchestration 或权限沙箱。低配额账号路由已加入本地构建，真实任务端到端接力仍未实测。
