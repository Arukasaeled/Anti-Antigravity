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
    "blocked_hosts": ["google-analytics.com"],
    "block_beacons": true
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
| `privacy.blocked_hosts` | array | 拦截的遥测域名 |
| `privacy.block_beacons` | bool | 是否拦截信标请求 |

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
| `~/.2ag/profiles/<email>/` | 每账号一个宿主 profile 沙箱 |
| `~/.2ag/active_account.txt` | 上次让哪个账号上场的便签 |

> `active_account.txt` 只是 2Ag 自己的记录。**权威的登录身份是 Windows 凭据管理器里那条记录**，不是这张便签。面板在宿主存活时会以系统凭据为准。
