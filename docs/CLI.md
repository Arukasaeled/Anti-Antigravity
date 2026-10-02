# 命令行参考

`2ag.exe` 不带参数启动的是 Manager。命令行子命令主要给脚本和排障用。

## 子命令

```
2ag manager                    打开 Manager 图形控制台（等同无参数启动）
2ag run [--debug]              只启动引擎 + 宿主，不开 Manager
2ag restore                    还原被改动过的宿主侧状态
2ag doctor                     打印环境诊断
```

### 视觉

```
2ag skin show                  查看当前视觉参数
2ag skin set-wallpaper <path>  设置壁纸
2ag skin set-blur <0..N>       设置模糊半径
2ag skin set-opacity <0..1>    设置不透明度
```

### 账号档案

```
2ag profile list               列出账号档案
2ag profile save               保存当前档案
2ag profile switch <name>      切换档案
```

## 常用组合

```powershell
# 只跑引擎（无界面），用来看日志
.\2ag.exe run --debug

# 环境出问题时的第一件事
.\2ag.exe doctor

# 宿主侧被改乱了，还原
.\2ag.exe restore
```

## 控制面 API

Manager 与引擎之间通过 `127.0.0.1:28470` 的回环 HTTP API 通信。这是个**本机回环接口**：

- 只监听 `127.0.0.1`，不对局域网开放；
- 不提供跨用户访问；
- 主要端点：`/api/v1/state`、`/api/v1/action`、`/api/v1/events`、`/api/v1/dashboard`、`/api/v1/host/*`、`/api/v1/accounts/*`、`/api/v1/sessions*`、`/api/v1/wallpaper*`、`/api/v1/compat`、`/api/v1/update-guardian`、`/api/v1/capability`。

这些端点供 Manager 自己使用，**不是为第三方集成设计的公开 API**，随时可能变动。
