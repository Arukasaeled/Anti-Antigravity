# 配置参考

配置文件 `2ag.json` 与 `2ag.exe` 同目录，首次启动自动生成。

## 完整字段

```json
{
  "wallpaper_path": "",
  "blur": 20,
  "opacity": 0.55,
  "modal_opacity": 0.85,
  "language": "auto",
  "onboarding_version": 0,
  "runtime_mode": "enhanced",
  "network": {
    "enabled": true,
    "endpoint_overrides": [],
    "rule_targets": []
  },
  "privacy": {
    "blocked_hosts": ["google-analytics.com"]
  },
  "global_rules": "",
  "env_overrides": {},
  "plugins": []
}
```

## 字段说明

### 外观

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `wallpaper_path` | string | `""` | 自定义壁纸的绝对路径。出厂为空 = 跟随宿主原生背景。壁纸是用户自己的数据，2Ag 不预置任何图片 |
| `blur` | int | `20` | 背景模糊半径 |
| `opacity` | float | `0.55` | 遮罩暗度 |
| `modal_opacity` | float | `0.85` | 模态弹窗遮罩暗度 |

### 语言

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `language` | string | `"auto"` | 2Ag 界面语言：`auto` / `en-US` / `zh-CN`。Auto 使用 Windows 系统语言，`zh-*` 选中文，其它选 English；手动选择优先 |
| `onboarding_version` | int | `0` | 新安装尚未完成欢迎页。主动完成后保存为 `1`；旧配置缺少此字段时按已有用户处理，不反复显示欢迎页 |

Manager 通过独立 dictionary / `t()` 与显式 UI bindings 即时切换；G-Hub 使用同一配置解析后的语言。已有 `en-US` / `zh-CN` 保持不变。UI language 不改变 `gravity_boost.force_zh_cn`：后者是宿主汉化选项，仍独立控制。

ReAct 可跟随此设置，也可单独选择语言；独立语言与阶段折叠状态属于现有 workspace 的 `__react_presentation_v3` 偏好，不是第二套全局配置。原始工具内容不翻译。

欢迎页提供环境检查、语言与运行形态选择。选择模式只保存 configured mode；建立 Enhanced 副本不会启动宿主，只有明确点击启动才执行生命周期操作。从总览可重新打开欢迎页。

### 运行时

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `runtime_mode` | string | `"enhanced"` | 期望的运行形态：`official` / `enhanced`。**改这个字段不会自动启动、停止或重启宿主**；configured 与当前 effective 分别显示，明确的下一次生命周期操作才应用 |

### 网络

| 字段 | 类型 | 说明 |
|---|---|---|
| `network.enabled` | bool | 是否启用回环转发代理 |
| `network.endpoint_overrides` | array | 把明文 HTTP 请求改道到指定上游 |
| `network.rule_targets` | array | 前插规则的目标 JSON 字段 |
| `global_rules` | string | 与 `rule_targets` 配合的规则正文 |

### 隐私

| 字段 | 类型 | 说明 |
|---|---|---|
| `privacy.blocked_hosts` | array | 拦截的遥测域名（host/domain 级）。旧配置里的 `block_beacons` 已废弃，会被忽略 |

### 高级

| 字段 | 类型 | 说明 |
|---|---|---|
| `env_overrides` | object | 传给宿主进程的环境变量覆盖 |
| `plugins` | array | 侧车插件登记 |

## 修改方式

推荐通过 2Ag Manager 的界面修改。内存状态先更新，配置沿用约 300ms debounce 保存；`pending` 表示等待落盘，`saved` 才表示保存成功，`save_failed` 会提供日志与 UI 错误。状态可从 `/api/v1/persistence` 与 `persistence_changed` 事件读取。API 接受配置动作不等于磁盘已经保存。

直接编辑 `2ag.json` 也可行，但**需要重启 2Ag** 才会被读取。

## 位置

`2ag.json` 固定在 `2ag.exe` 同目录（便携语义）。数据目录则分两处：

| 路径 | 内容 |
|---|---|
| `<2ag.exe 目录>\2ag.json` | 配置 |
| `<2ag.exe 目录>\2ag.log` | 运行日志 |
| `<2ag.exe 目录>\app\` | 冻结宿主副本（增强形态，约 570 MB） |
| `~/.2ag/vault/` | DPAPI 加密的账号保险库 |
| `~/.2ag/broker-recovery.json` | 登录期间原凭据的 DPAPI 恢复记录；校验恢复成功后删除 |
| `~/.2ag/credential-operation.lock` | 跨进程账号操作锁；进程退出自动释放锁，文件可保留 |
| `~/.2ag/accounts.json` | 2Ag 自己的账号清单（只有邮箱/名称等元数据，**不含 token**） |
| `~/.2ag/profiles/<email>/` | 每账号一个宿主 profile 沙箱 |
| `~/.2ag/selected_account.txt` | 用户选择的账号，不代表宿主内部身份 |
| `~/.2ag/active_account.txt` | 确认账号的兼容记录；旧版内容读取只作选择提示，不恢复为身份真值 |
| `~/.2ag/workspace/extension-state.json` | 本地扩展与 ReAct 语言 / 折叠偏好 |

> selected account 是用户选择，credential owner 是系统凭据归属，active account 只代表已确认生效的宿主账号。系统凭据回读不能验证宿主内部身份；当前没有可靠身份来源时，面板显示“宿主身份未验证”，不会以请求邮箱或系统凭据冒充 active account。

添加账号窗口的登录网络模式只作用于这次官方宿主子进程：`AUTO` 沿用原环境；`DIRECT` 清除子进程代理变量并强制直连；`PROXY` 使用环境变量或 Windows 系统代理中解析到的 HTTP/HTTPS 代理。不修改系统设置，不持久化到配置。外部浏览器仍使用自己的网络设置。
