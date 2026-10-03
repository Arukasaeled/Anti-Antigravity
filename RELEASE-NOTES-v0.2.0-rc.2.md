# v0.2.0-rc.2 — Account Health / Live Trace 2.0

2Ag 的切号进度、回滚和原生核验在 Manager / G-Hub 共用同一后端状态。停止宿主前先保留恢复记录；新增可展开的 Account Health，区分实时登录与历史观察，展示最近验证和刷新时间，不展示凭据。

TRACE 增加当前动作、阶段、错误定位、时间信息、文件摘要／真实 diff hunks、关键词及 file:/kind: 搜索、最近会话、MD/JSON 导出和选中送 Capsule。最多保留 10,000 事件，时间线窗口化；关闭时取消原生流并清理缓存。只展示公开可观测数据，不读取隐藏 CoT。

实机冷启动使用已过期 access 后成功由原生服务刷新；地区受限目标失败后原账号经原生核验恢复。成功双账号矩阵仍受目标资格限制。5000 / 10000 压测是实际事件样本回放，不是原生实时长会话。

Windows x64 安装包：`dist/v020-live-trace-rc2/Anti-Antigravity-Setup-x64.exe`。安装前正常退出 Manager；同 AppId 支持升级并保留本机数据。安装包不附带 Google Antigravity 运行时，使用本机已有安装。

详细实机范围、性能数据、隐私边界与限制见 [验证记录](docs/RC2-REALITY-OBSERVABILITY.md)。插件 API 1.4 向后兼容，见 [插件说明](plugins/README.md)。
