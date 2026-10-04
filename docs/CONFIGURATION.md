# 配置参考

配置文件 `2ag.json` 与 `2ag.exe` 同目录，首次启动自动生成。

## 完整字段

```json
{
  "wallpaper_path": "",
  "blur": 20,
  "opacity": 0.55,
  "modal_opacity": 0.85,
  "language": "zh-CN",
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
| `language` | string | `"zh-CN"` | 界面语言 |

### 运行时

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `runtime_mode` | string | `"enhanced"` | 期望的运行形态：`official` / `enhanced`。**改这个字段不会自动重启宿主** —— 下一次启动时才生效 |

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

推荐通过 2Ag Manager 的界面修改（视觉工坊 / 重力加倍 / 环境诊断），改动会即时写回。

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
| `~/.2ag/active_account.txt` | 已确认生效账号的兼容记录；不保存选择意图 |

> selected account 是用户选择，credential owner 是系统凭据归属，active account 只代表已确认生效的宿主账号。系统凭据回读不能验证宿主内部身份；当前没有可靠身份来源时，面板显示“宿主身份未验证”，不会以请求邮箱或系统凭据冒充 active account。

添加账号窗口的登录网络模式只作用于这次官方宿主子进程：`AUTO` 沿用原环境；`DIRECT` 清除子进程代理变量并强制直连；`PROXY` 使用环境变量或 Windows 系统代理中解析到的 HTTP/HTTPS 代理。不修改系统设置，不持久化到配置。外部浏览器仍使用自己的网络设置。
